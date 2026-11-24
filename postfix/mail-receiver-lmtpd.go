// Package main: LMTP 守护进程版 mail-receiver。
//
// 替代原本每封邮件 fork 一次 Go 二进制的 pipe 模式：
//  1. Postfix 把虚拟域名邮件转发到本进程的 unix socket（LMTP）
//  2. 本进程长期常驻，单进程并发处理多条 LMTP 会话
//  3. 解析邮件（含 RFC 2047 邮件头、base64 / quoted-printable、常见字符集）后调 API /internal/deliver 入库
//  4. 入库失败时返回 LMTP 4xx，Postfix 把邮件保留在队列里自动重试，不会丢失
//
// 协议参考 RFC 2033 (LMTP)。Postfix 支持把这种 socket 当成 transport。
//
// 启动参数（环境变量）：
//
//	API_URL          API 地址，默认 http://api:8080
//	INTERNAL_API_KEY 可选内部接口鉴权密钥，非空时附带 X-Internal-Key 请求头
//	LMTPD_SOCKET     unix socket 路径，默认 /var/run/postfix/mail-receiver.sock
//	LMTPD_BIND       可选 TCP 监听 host:port，调试用
//	LMTPD_HOSTNAME   LMTP banner 中的主机名
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

const (
	maxMessageBytes       = 10 << 20
	maxDiscardBytes       = 20 << 20
	maxBatchResponseBytes = 4 << 20
	maxRecipientsPerTx    = 256
	maxMultipartDepth     = 16
	maxMultipartParts     = 128
	maxSenderRunes        = 320
	maxSubjectRunes       = 998
	apiTimeout            = 15 * time.Second
	shutdownDrainTimeout  = 10 * time.Second
)

var errMessageTooLarge = errors.New("message exceeds maximum size")

type deliverPayload struct {
	Recipient string `json:"recipient"`
	Sender    string `json:"sender"`
	Subject   string `json:"subject"`
	BodyText  string `json:"body_text"`
	BodyHTML  string `json:"body_html"`
	Raw       string `json:"raw"`
}

type deliverBatchPayload struct {
	Recipients []string `json:"recipients"`
	Sender     string   `json:"sender"`
	Subject    string   `json:"subject"`
	BodyText   string   `json:"body_text"`
	BodyHTML   string   `json:"body_html"`
	Raw        string   `json:"raw"`
}

type batchResult struct {
	Recipient string `json:"recipient"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
}

type batchResponse struct {
	Results []batchResult `json:"results"`
}

type server struct {
	hostname    string
	apiURL      string
	internalKey string
	httpClient  *http.Client
	conns       atomic.Int64
	sessionsWG  sync.WaitGroup
}

func (s *server) apiInternalKey() string {
	if s.internalKey != "" {
		return s.internalKey
	}
	return os.Getenv("INTERNAL_API_KEY")
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	apiURL := strings.TrimRight(getenv("API_URL", "http://api:8080"), "/")
	hostname := getenv("LMTPD_HOSTNAME", "mail-receiver.local")

	srv := &server{
		hostname:    hostname,
		apiURL:      apiURL,
		internalKey: os.Getenv("INTERNAL_API_KEY"),
		httpClient:  newAPIHTTPClient(),
	}

	listeners := make([]net.Listener, 0, 2)

	if sockPath := getenv("LMTPD_SOCKET", "/var/run/postfix/mail-receiver.sock"); sockPath != "" {
		_ = os.Remove(sockPath)
		l, err := net.Listen("unix", sockPath)
		if err != nil {
			log.Fatalf("listen unix %s: %v", sockPath, err)
		}
		// Postfix 默认以 postfix 用户连接（entrypoint 会 chown postfix:postfix），限制为 0660
		if err := os.Chmod(sockPath, 0o660); err != nil {
			log.Printf("chmod socket: %v", err)
		}
		log.Printf("LMTP listening on unix:%s", sockPath)
		listeners = append(listeners, l)
	}

	if bind := os.Getenv("LMTPD_BIND"); bind != "" {
		l, err := net.Listen("tcp", bind)
		if err != nil {
			log.Fatalf("listen tcp %s: %v", bind, err)
		}
		log.Printf("LMTP listening on tcp:%s", bind)
		listeners = append(listeners, l)
	}

	if len(listeners) == 0 {
		log.Fatal("no listener configured (LMTPD_SOCKET / LMTPD_BIND)")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var acceptWG sync.WaitGroup
	for _, l := range listeners {
		acceptWG.Add(1)
		go func(l net.Listener) {
			defer acceptWG.Done()
			srv.acceptLoop(ctx, l)
		}(l)
	}

	<-ctx.Done()
	log.Printf("shutting down, draining %d active conns", srv.conns.Load())
	for _, l := range listeners {
		_ = l.Close()
	}
	acceptWG.Wait()

	// Wait for in-flight LMTP sessions to finish up to shutdownDrainTimeout.
	drained := make(chan struct{})
	go func() {
		srv.sessionsWG.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		log.Printf("all LMTP sessions drained cleanly")
	case <-time.After(shutdownDrainTimeout):
		log.Printf("drain timeout reached with %d active conns remaining", srv.conns.Load())
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		log.Printf("invalid %s=%q, using %d", key, raw, fallback)
		return fallback
	}
	return v
}

func newAPIHTTPClient() *http.Client {
	maxIdle := getenvInt("LMTP_HTTP_MAX_IDLE_CONNS", 512)
	maxIdlePerHost := getenvInt("LMTP_HTTP_MAX_IDLE_CONNS_PER_HOST", 256)
	maxConnsPerHost := getenvInt("LMTP_HTTP_MAX_CONNS_PER_HOST", 512)

	return &http.Client{
		Timeout: apiTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          maxIdle,
			MaxIdleConnsPerHost:   maxIdlePerHost,
			MaxConnsPerHost:       maxConnsPerHost,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: apiTimeout,
			ForceAttemptHTTP2:     false,
		},
	}
}

func (s *server) acceptLoop(ctx context.Context, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			log.Printf("accept: %v", err)
			return
		}
		s.conns.Add(1)
		s.sessionsWG.Add(1)
		go func() {
			defer s.sessionsWG.Done()
			defer s.conns.Add(-1)
			s.handle(ctx, conn)
		}()
	}
}

type session struct {
	srv        *server
	conn       net.Conn
	r          *bufio.Reader
	w          *bufio.Writer
	greeted    bool
	inMail     bool
	from       string
	recipients []string
}

func (s *session) resetTx() {
	s.inMail = false
	s.from = ""
	s.recipients = s.recipients[:0]
}

func (s *server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))

	sess := &session{
		srv:  s,
		conn: conn,
		r:    bufio.NewReaderSize(conn, 64*1024),
		w:    bufio.NewWriterSize(conn, 64*1024),
	}
	sess.writeLine("220 %s LMTP ready", s.hostname)

	for {
		if ctx.Err() != nil && !sess.inMail {
			return
		}
		line, err := sess.readLine()
		if err != nil {
			return
		}
		cmd, args := parseCommand(line)
		switch cmd {
		case "LHLO":
			sess.greeted = true
			sess.resetTx()
			sess.writeLine("250-%s", s.hostname)
			sess.writeLine("250-PIPELINING")
			sess.writeLine("250-8BITMIME")
			sess.writeLine("250-SIZE %d", maxMessageBytes)
			sess.writeLine("250 ENHANCEDSTATUSCODES")
		case "MAIL":
			if !sess.greeted {
				sess.writeLine("503 5.5.1 send LHLO first")
				continue
			}
			sess.resetTx()
			sess.from = extractAddr(args)
			sess.inMail = true
			sess.writeLine("250 2.1.0 OK")
		case "RCPT":
			if !sess.inMail {
				sess.writeLine("503 5.5.1 send MAIL first")
				continue
			}
			addr := extractAddr(args)
			if addr == "" {
				sess.writeLine("501 5.1.3 bad recipient")
				continue
			}
			if len(sess.recipients) >= maxRecipientsPerTx {
				sess.writeLine("452 4.5.3 too many recipients")
				continue
			}
			sess.recipients = append(sess.recipients, addr)
			sess.writeLine("250 2.1.5 OK")
		case "DATA":
			if !sess.inMail || len(sess.recipients) == 0 {
				sess.writeLine("503 5.5.1 no recipients")
				continue
			}
			sess.writeLine("354 end with <CR><LF>.<CR><LF>")
			data, err := sess.readData()
			if errors.Is(err, errMessageTooLarge) {
				// RFC 2033: reply once per accepted recipient after DATA terminator.
				for range sess.recipients {
					sess.writeLine("552 5.3.4 message exceeds %d bytes", maxMessageBytes)
				}
				sess.resetTx()
				_ = sess.conn.SetDeadline(time.Now().Add(2 * time.Minute))
				continue
			}
			if err != nil {
				return
			}
			// Give in-flight delivery its own bounded timeout so graceful shutdown can finish it.
			deliverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiTimeout)
			sess.deliverAll(deliverCtx, data)
			cancel()
			sess.resetTx()
			_ = sess.conn.SetDeadline(time.Now().Add(2 * time.Minute))
		case "RSET":
			sess.resetTx()
			sess.writeLine("250 2.1.5 OK")
		case "NOOP":
			sess.writeLine("250 2.0.0 OK")
		case "QUIT":
			sess.writeLine("221 2.0.0 bye")
			return
		case "":
			// 忽略空行
		default:
			sess.writeLine("502 5.5.2 unrecognized %q", cmd)
		}
	}
}

func (s *session) readLine() (string, error) {
	line, err := s.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (s *session) writeLine(format string, args ...interface{}) {
	fmt.Fprintf(s.w, format, args...)
	s.w.WriteString("\r\n")
	_ = s.w.Flush()
}

func (s *session) readData() ([]byte, error) {
	var buf bytes.Buffer
	oversized := false
	totalRead := 0
	for {
		line, err := s.r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if bytes.Equal(line, []byte(".\r\n")) || bytes.Equal(line, []byte(".\n")) {
			break
		}
		// dot-stuffing (RFC 5321 Section 4.5.2)
		if len(line) > 0 && line[0] == '.' {
			line = line[1:]
		}
		totalRead += len(line)
		if totalRead > maxDiscardBytes {
			return nil, fmt.Errorf("message exceeds hard disconnect limit %d bytes", maxDiscardBytes)
		}
		if oversized {
			continue
		}
		if buf.Len()+len(line) > maxMessageBytes {
			oversized = true
			buf.Reset()
			continue
		}
		buf.Write(line)
	}
	if oversized {
		return nil, errMessageTooLarge
	}
	return buf.Bytes(), nil
}

func (s *session) deliverAll(ctx context.Context, raw []byte) {
	sender, subject, bodyText, bodyHTML := parseEmail(raw, s.from)
	rawUTF8 := sanitizeUTF8(string(raw))

	rcpts := make([]string, 0, len(s.recipients))
	for _, r := range s.recipients {
		rcpts = append(rcpts, strings.ToLower(strings.TrimSpace(r)))
	}

	if len(rcpts) > 1 {
		results, err := s.srv.deliverBatch(ctx, deliverBatchPayload{
			Recipients: rcpts,
			Sender:     sender,
			Subject:    subject,
			BodyText:   bodyText,
			BodyHTML:   bodyHTML,
			Raw:        rawUTF8,
		})
		if err != nil {
			log.Printf("deliver-batch: %v", err)
			s.writeTemporaryFailures(len(rcpts))
			return
		}
		if err := validateBatchResults(rcpts, results); err != nil {
			log.Printf("deliver-batch invalid response: %v", err)
			s.writeTemporaryFailures(len(rcpts))
			return
		}
		for _, r := range results {
			s.writeLine("250 2.0.0 <%s> %s", r.Recipient, r.Status)
		}
		return
	}

	for _, rcpt := range rcpts {
		payload := deliverPayload{
			Recipient: rcpt,
			Sender:    sender,
			Subject:   subject,
			BodyText:  bodyText,
			BodyHTML:  bodyHTML,
			Raw:       rawUTF8,
		}
		if err := s.srv.deliver(ctx, payload); err != nil {
			log.Printf("deliver %s: %v", rcpt, err)
			s.writeLine("451 4.5.0 temporary delivery failure")
			continue
		}
		s.writeLine("250 2.0.0 <%s> accepted", rcpt)
	}
}

func (s *session) writeTemporaryFailures(count int) {
	for i := 0; i < count; i++ {
		s.writeLine("451 4.5.0 temporary delivery failure")
	}
}

func validateBatchResults(recipients []string, results []batchResult) error {
	if len(results) != len(recipients) {
		return fmt.Errorf("got %d results for %d recipients", len(results), len(recipients))
	}
	for i := range recipients {
		if !strings.EqualFold(strings.TrimSpace(results[i].Recipient), recipients[i]) {
			return fmt.Errorf("result %d recipient mismatch", i)
		}
		if results[i].Status != "delivered" && results[i].Status != "discarded" {
			return fmt.Errorf("result %d has invalid status %q", i, results[i].Status)
		}
	}
	return nil
}

func parseCommand(line string) (cmd, args string) {
	idx := strings.IndexAny(line, " \t")
	if idx == -1 {
		return strings.ToUpper(line), ""
	}
	return strings.ToUpper(line[:idx]), strings.TrimSpace(line[idx+1:])
}

func extractAddr(args string) string {
	idx := strings.IndexByte(args, ':')
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(args[idx+1:])
	rest = strings.TrimSpace(strings.SplitN(rest, " ", 2)[0])
	rest = strings.TrimPrefix(rest, "<")
	rest = strings.TrimSuffix(rest, ">")
	return rest
}

func (s *server) deliver(ctx context.Context, payload deliverPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL+"/internal/deliver", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := s.apiInternalKey(); key != "" {
		req.Header.Set("X-Internal-Key", key)
	}

	res, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := readAndDrain(res.Body, 1024)
		return fmt.Errorf("api HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(respBody)))
	}
	_, err = readAndDrain(res.Body, 0)
	return err
}

func (s *server) deliverBatch(ctx context.Context, payload deliverBatchPayload) ([]batchResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL+"/internal/deliver-batch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := s.apiInternalKey(); key != "" {
		req.Header.Set("X-Internal-Key", key)
	}

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	respBody, readErr := readAndDrain(res.Body, maxBatchResponseBytes)
	if readErr != nil {
		return nil, fmt.Errorf("read batch response: %w", readErr)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("api HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed batchResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode batch response: %w", err)
	}
	return parsed.Results, nil
}

// readAndDrain keeps HTTP/1.1 connections reusable. When limit is positive,
// only that many response bytes are retained while the remainder is discarded.
func readAndDrain(r io.Reader, limit int64) ([]byte, error) {
	var kept bytes.Buffer
	if limit > 0 {
		if _, err := io.Copy(&kept, io.LimitReader(r, limit)); err != nil {
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		return nil, err
	}
	return kept.Bytes(), nil
}

// parseEmail extracts headers and decoded text/HTML bodies with strict depth,
// part count, and byte budgets so malformed or deeply nested MIME cannot exhaust CPU/memory.
func parseEmail(raw []byte, fallbackFrom string) (sender, subject, bodyText, bodyHTML string) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err == nil {
		sender = decodeHeader(msg.Header.Get("From"))
		subject = decodeHeader(msg.Header.Get("Subject"))
		budget := &parseBudget{remainingBytes: maxMessageBytes, maxParts: maxMultipartParts}
		bodyText, bodyHTML = extractBodies(msg, budget)
	}
	if sender == "" {
		sender = fallbackFrom
	}
	return truncateRunes(sanitizeUTF8(sender), maxSenderRunes),
		truncateRunes(sanitizeUTF8(subject), maxSubjectRunes),
		sanitizeUTF8(bodyText),
		sanitizeUTF8(bodyHTML)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}

type parseBudget struct {
	remainingBytes int64
	parts          int
	maxParts       int
}

func (b *parseBudget) readLimited(r io.Reader) []byte {
	if b.remainingBytes <= 0 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(r, b.remainingBytes))
	b.remainingBytes -= int64(len(data))
	return data
}

func decodeHeader(raw string) string {
	if raw == "" {
		return ""
	}
	dec := &mime.WordDecoder{CharsetReader: charsetReader}
	decoded, err := dec.DecodeHeader(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func chineseDecoder(charset string) *encoding.Decoder {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "gb18030", "gb-18030", "gbk", "gb2312", "gb_2312", "gb_2312-80", "csgb2312", "cp936", "x-gbk":
		return simplifiedchinese.GB18030.NewDecoder()
	case "hz-gb-2312":
		return simplifiedchinese.HZGB2312.NewDecoder()
	case "big5", "big-5", "csbig5", "cp950":
		return traditionalchinese.Big5.NewDecoder()
	default:
		return nil
	}
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	if dec := chineseDecoder(charset); dec != nil {
		return io.LimitReader(transform.NewReader(io.LimitReader(input, maxMessageBytes), dec), maxMessageBytes), nil
	}
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "iso-8859-1", "latin1", "latin-1", "iso_8859-1":
		data, err := io.ReadAll(io.LimitReader(input, maxMessageBytes))
		if err != nil {
			return nil, err
		}
		return strings.NewReader(decodeLatin1(data)), nil
	case "windows-1252", "cp1252":
		data, err := io.ReadAll(io.LimitReader(input, maxMessageBytes))
		if err != nil {
			return nil, err
		}
		return strings.NewReader(decodeWindows1252(data)), nil
	default:
		// Return original stream for unsupported charsets rather than failing header decoding.
		return input, nil
	}
}

var cp1252Extra = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

func decodeLatin1(data []byte) string {
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, c := range data {
		b.WriteRune(rune(c))
	}
	return b.String()
}

func decodeWindows1252(data []byte) string {
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, c := range data {
		if c >= 0x80 && c <= 0x9F {
			b.WriteRune(cp1252Extra[c-0x80])
		} else {
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

func decodeTransferAndCharset(r io.Reader, transferEncoding, charset string, budget *parseBudget) string {
	var decodedReader io.Reader = r
	switch strings.ToLower(strings.TrimSpace(transferEncoding)) {
	case "base64":
		raw := budget.readLimited(r)
		cleaned := stripASCIIWhitespace(raw)
		out, err := base64.StdEncoding.DecodeString(string(cleaned))
		if err != nil {
			out, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(cleaned), "="))
			if err != nil {
				out = raw
			}
		}
		return convertCharset(out, charset)
	case "quoted-printable":
		decodedReader = quotedprintable.NewReader(r)
	}
	data := budget.readLimited(decodedReader)
	return convertCharset(data, charset)
}

func stripASCIIWhitespace(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for _, c := range in {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			out = append(out, c)
		}
	}
	return out
}

func convertCharset(data []byte, charset string) string {
	if dec := chineseDecoder(charset); dec != nil {
		if out, err := dec.Bytes(data); err == nil {
			return string(out)
		}
		return string(data)
	}
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "latin-1", "iso_8859-1":
		return decodeLatin1(data)
	case "windows-1252", "cp1252":
		return decodeWindows1252(data)
	default:
		return string(data)
	}
}

func sanitizeUTF8(s string) string {
	if s == "" {
		return ""
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return s
}

func isAttachment(disposition string) bool {
	if disposition == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(disposition)
	if err != nil {
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(disposition)), "attachment")
	}
	return strings.EqualFold(mediaType, "attachment")
}

func extractBodies(msg *mail.Message, budget *parseBudget) (string, string) {
	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = "text/plain"
		params = nil
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "multipart/") {
		return extractMultipart(msg.Body, params["boundary"], 0, budget)
	}
	if isAttachment(msg.Header.Get("Content-Disposition")) {
		return "", ""
	}
	body := decodeTransferAndCharset(msg.Body, msg.Header.Get("Content-Transfer-Encoding"), params["charset"], budget)
	if mediaType == "text/html" {
		return "", body
	}
	return body, ""
}

func extractMultipart(r io.Reader, boundary string, depth int, budget *parseBudget) (string, string) {
	if depth >= maxMultipartDepth || budget.remainingBytes <= 0 || budget.parts >= budget.maxParts {
		return "", ""
	}
	if boundary == "" {
		return string(budget.readLimited(r)), ""
	}
	var textBody, htmlBody string
	mr := multipart.NewReader(r, boundary)
	for {
		if budget.parts >= budget.maxParts || budget.remainingBytes <= 0 {
			break
		}
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		budget.parts++
		ct := part.Header.Get("Content-Type")
		mediaType, params, parseErr := mime.ParseMediaType(ct)
		if parseErr != nil {
			mediaType = "text/plain"
			params = nil
		}
		mediaType = strings.ToLower(mediaType)
		if strings.HasPrefix(mediaType, "multipart/") {
			nt, nh := extractMultipart(part, params["boundary"], depth+1, budget)
			if textBody == "" {
				textBody = nt
			}
			if htmlBody == "" {
				htmlBody = nh
			}
			_ = part.Close()
			continue
		}
		if isAttachment(part.Header.Get("Content-Disposition")) {
			_ = part.Close()
			continue
		}
		switch mediaType {
		case "text/plain":
			if textBody == "" {
				textBody = decodeTransferAndCharset(part, part.Header.Get("Content-Transfer-Encoding"), params["charset"], budget)
			}
		case "text/html":
			if htmlBody == "" {
				htmlBody = decodeTransferAndCharset(part, part.Header.Get("Content-Transfer-Encoding"), params["charset"], budget)
			}
		}
		_ = part.Close()
	}
	return textBody, htmlBody
}
