package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func TestDeliverDrainsResponseAndReusesConnection(t *testing.T) {
	var newConnections atomic.Int64
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"delivered","email_id":"00000000-0000-0000-0000-000000000001"}`))
	}))
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	ts.Start()
	defer ts.Close()

	client := newAPIHTTPClient()
	defer client.CloseIdleConnections()
	srv := &server{apiURL: ts.URL, httpClient: client}
	payload := deliverPayload{Recipient: "test@example.com", Raw: "hello"}
	for i := 0; i < 25; i++ {
		if err := srv.deliver(context.Background(), payload); err != nil {
			t.Fatalf("deliver %d: %v", i, err)
		}
	}

	if got := newConnections.Load(); got != 1 {
		t.Fatalf("expected one reused HTTP connection, got %d", got)
	}
}

func TestValidateBatchResults(t *testing.T) {
	recipients := []string{"a@example.com", "b@example.com"}
	tests := []struct {
		name    string
		results []batchResult
		wantErr bool
	}{
		{
			name: "valid",
			results: []batchResult{
				{Recipient: "a@example.com", Status: "delivered"},
				{Recipient: "b@example.com", Status: "discarded"},
			},
		},
		{
			name:    "missing result",
			results: []batchResult{{Recipient: "a@example.com", Status: "delivered"}},
			wantErr: true,
		},
		{
			name: "wrong order",
			results: []batchResult{
				{Recipient: "b@example.com", Status: "delivered"},
				{Recipient: "a@example.com", Status: "delivered"},
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			results: []batchResult{
				{Recipient: "a@example.com", Status: "maybe"},
				{Recipient: "b@example.com", Status: "delivered"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBatchResults(recipients, tt.results)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBatchResults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseEmailDecodesRFC2047Base64AndQuotedPrintable(t *testing.T) {
	subjB64 := base64.StdEncoding.EncodeToString([]byte("登录验证码 842910"))
	htmlB64 := base64.StdEncoding.EncodeToString([]byte("<p>您的验证码是 <b>842910</b></p>"))
	raw := strings.Join([]string{
		"From: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("系统通知")) + "?= <noreply@example.com>",
		"Subject: =?UTF-8?B?" + subjB64 + "?=",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"bnd1\"",
		"",
		"--bnd1",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"=E6=82=A8=E7=9A=84=E9=AA=8C=E8=AF=81=E7=A0=81=E6=98=AF 842910",
		"--bnd1",
		"Content-Type: text/html; charset=utf-8",
		"Content-Transfer-Encoding: base64",
		"",
		htmlB64,
		"--bnd1",
		"Content-Type: text/plain; name=\"note.txt\"",
		"Content-Disposition: attachment; filename=\"note.txt\"",
		"",
		"attachment body should be ignored",
		"--bnd1--",
		"",
	}, "\r\n")

	sender, subject, textBody, htmlBody := parseEmail([]byte(raw), "fallback@example.com")
	if !strings.Contains(sender, "系统通知") {
		t.Fatalf("sender not decoded: %q", sender)
	}
	if subject != "登录验证码 842910" {
		t.Fatalf("subject=%q", subject)
	}
	if strings.TrimSpace(textBody) != "您的验证码是 842910" {
		t.Fatalf("textBody=%q", textBody)
	}
	if htmlBody != "<p>您的验证码是 <b>842910</b></p>" {
		t.Fatalf("htmlBody=%q", htmlBody)
	}
}

func TestParseEmailCapsDeepMultipartNesting(t *testing.T) {
	var b strings.Builder
	b.WriteString("From: a@example.com\r\nSubject: nested\r\nMIME-Version: 1.0\r\n")
	for i := 0; i < 40; i++ {
		if i > 0 {
			b.WriteString("--b" + string(rune('a'+(i-1)%26)) + "\r\n")
		}
		b.WriteString("Content-Type: multipart/mixed; boundary=\"b" + string(rune('a'+i%26)) + "\"\r\n\r\n")
	}
	b.WriteString("--bo\r\nContent-Type: text/plain\r\n\r\ntoo deep\r\n--bo--\r\n")

	_, subject, textBody, _ := parseEmail([]byte(b.String()), "")
	if subject != "nested" {
		t.Fatalf("subject=%q", subject)
	}
	if textBody != "" {
		t.Fatalf("expected deep nesting to stop before depth 40, got %q", textBody)
	}
}

func TestParseEmailTruncatesOverlongHeaders(t *testing.T) {
	longSender := strings.Repeat("a", 400) + "@example.com"
	longSubject := strings.Repeat("测", 1200)
	raw := "From: " + longSender + "\r\nSubject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(longSubject)) + "?=\r\n\r\nhi"
	sender, subject, _, _ := parseEmail([]byte(raw), "")
	if len([]rune(sender)) != maxSenderRunes {
		t.Fatalf("sender runes=%d want %d", len([]rune(sender)), maxSenderRunes)
	}
	if len([]rune(subject)) != maxSubjectRunes {
		t.Fatalf("subject runes=%d want %d", len([]rune(subject)), maxSubjectRunes)
	}
}

func TestParseEmailDecodesWindows1252(t *testing.T) {
	raw := "From: shop@example.com\r\nSubject: =?windows-1252?Q?Total_=8010_=93OK=94?=\r\nContent-Type: text/plain; charset=windows-1252\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nPrice: =805 =97 thank you"
	_, subject, textBody, _ := parseEmail([]byte(raw), "")
	if subject != "Total €10 “OK”" {
		t.Fatalf("subject=%q", subject)
	}
	if textBody != "Price: €5 — thank you" {
		t.Fatalf("textBody=%q", textBody)
	}
}

func encodeWithCharset(t *testing.T, enc *encoding.Encoder, s string) []byte {
	t.Helper()
	out, err := enc.Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encode %q: %v", s, err)
	}
	return out
}

func encodeQuotedPrintable(t *testing.T, data []byte) string {
	t.Helper()
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("quoted-printable write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("quoted-printable close: %v", err)
	}
	return buf.String()
}

func TestParseEmailDecodesSimplifiedChineseCharsets(t *testing.T) {
	tests := []struct {
		name           string
		headerCharset  string
		textCharset    string
		htmlCharset    string
		encoder        *encoding.Encoder
		fromName       string
		subject        string
		textBody       string
		htmlBody       string
	}{
		{
			name:          "GBK",
			headerCharset: "GBK",
			textCharset:   "gbk",
			htmlCharset:   "x-gbk",
			encoder:       simplifiedchinese.GBK.NewEncoder(),
			fromName:      "中文发件人",
			subject:       "登录验证码 123456",
			textBody:      "您好，您的登录验证码是 123456。",
			htmlBody:      "<p>您好，验证码：<b>123456</b></p>",
		},
		{
			name:          "GB18030",
			headerCharset: "GB18030",
			textCharset:   "gb18030",
			htmlCharset:   "gb-18030",
			encoder:       simplifiedchinese.GB18030.NewEncoder(),
			fromName:      "安全中心",
			subject:       "生僻字测试 𠮷 验证码 654321",
			textBody:      "内容含 GB18030 扩展字 𠮷，验证码 654321",
			htmlBody:      "<div>扩展字 <b>𠮷</b> 654321</div>",
		},
		{
			name:          "GB2312",
			headerCharset: "GB2312",
			textCharset:   "gb2312",
			htmlCharset:   "cp936",
			encoder:       simplifiedchinese.GBK.NewEncoder(),
			fromName:      "系统通知",
			subject:       "邮箱激活通知",
			textBody:      "请点击链接完成邮箱激活。",
			htmlBody:      "<p>请点击链接完成<strong>邮箱激活</strong>。</p>",
		},
		{
			name:          "HZ-GB-2312",
			headerCharset: "HZ-GB-2312",
			textCharset:   "hz-gb-2312",
			htmlCharset:   "hz-gb-2312",
			encoder:       simplifiedchinese.HZGB2312.NewEncoder(),
			fromName:      "邮件网关",
			subject:       "中文标题测试",
			textBody:      "正文测试内容 8888",
			htmlBody:      "<p>中文正文 8888</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fromEnc := encodeWithCharset(t, tt.encoder, tt.fromName)
			subjEnc := encodeWithCharset(t, tt.encoder, tt.subject)
			textEnc := encodeWithCharset(t, tt.encoder, tt.textBody)
			htmlEnc := encodeWithCharset(t, tt.encoder, tt.htmlBody)

			raw := strings.Join([]string{
				"From: =?" + tt.headerCharset + "?B?" + base64.StdEncoding.EncodeToString(fromEnc) + "?= <notice@example.com>",
				"Subject: =?" + tt.headerCharset + "?B?" + base64.StdEncoding.EncodeToString(subjEnc) + "?=",
				"MIME-Version: 1.0",
				`Content-Type: multipart/alternative; boundary="cn-bnd"`,
				"",
				"--cn-bnd",
				"Content-Type: text/plain; charset=" + tt.textCharset,
				"Content-Transfer-Encoding: quoted-printable",
				"",
				encodeQuotedPrintable(t, textEnc),
				"--cn-bnd",
				"Content-Type: text/html; charset=" + tt.htmlCharset,
				"Content-Transfer-Encoding: base64",
				"",
				base64.StdEncoding.EncodeToString(htmlEnc),
				"--cn-bnd--",
				"",
			}, "\r\n")

			sender, subject, textBody, htmlBody := parseEmail([]byte(raw), "fallback@example.com")
			if !strings.Contains(sender, tt.fromName) {
				t.Fatalf("sender=%q, want substring %q", sender, tt.fromName)
			}
			if subject != tt.subject {
				t.Fatalf("subject=%q, want %q", subject, tt.subject)
			}
			if strings.TrimSpace(textBody) != tt.textBody {
				t.Fatalf("textBody=%q, want %q", textBody, tt.textBody)
			}
			if htmlBody != tt.htmlBody {
				t.Fatalf("htmlBody=%q, want %q", htmlBody, tt.htmlBody)
			}
		})
	}
}

func TestParseEmailDecodesBig5(t *testing.T) {
	enc := traditionalchinese.Big5.NewEncoder()
	wantFrom := "系統通知"
	wantSubject := "登入驗證碼 952710"
	wantText := "您好，您的驗證碼是 952710。"
	wantHTML := "<p>繁體中文通知 <b>952710</b></p>"

	raw := strings.Join([]string{
		"From: =?Big5?B?" + base64.StdEncoding.EncodeToString(encodeWithCharset(t, enc, wantFrom)) + "?= <tw@example.com>",
		"Subject: =?big-5?B?" + base64.StdEncoding.EncodeToString(encodeWithCharset(t, enc, wantSubject)) + "?=",
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="big5-bnd"`,
		"",
		"--big5-bnd",
		"Content-Type: text/plain; charset=big5",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		encodeQuotedPrintable(t, encodeWithCharset(t, enc, wantText)),
		"--big5-bnd",
		"Content-Type: text/html; charset=cp950",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(encodeWithCharset(t, enc, wantHTML)),
		"--big5-bnd--",
		"",
	}, "\r\n")

	sender, subject, textBody, htmlBody := parseEmail([]byte(raw), "")
	if !strings.Contains(sender, wantFrom) {
		t.Fatalf("sender=%q, want substring %q", sender, wantFrom)
	}
	if subject != wantSubject {
		t.Fatalf("subject=%q, want %q", subject, wantSubject)
	}
	if strings.TrimSpace(textBody) != wantText {
		t.Fatalf("textBody=%q, want %q", textBody, wantText)
	}
	if htmlBody != wantHTML {
		t.Fatalf("htmlBody=%q, want %q", htmlBody, wantHTML)
	}
}

func TestDeliverAndDeliverBatchInternalAPIKeyHeader(t *testing.T) {
	type capturedReq struct {
		path      string
		hasHeader bool
		headerVal string
	}

	tests := []struct {
		name        string
		fieldKey    string
		envKey      string
		wantPresent bool
		wantVal     string
	}{
		{
			name:        "server field key set",
			fieldKey:    "field-secret-123",
			envKey:      "",
			wantPresent: true,
			wantVal:     "field-secret-123",
		},
		{
			name:        "env fallback key set",
			fieldKey:    "",
			envKey:      "env-secret-456",
			wantPresent: true,
			wantVal:     "env-secret-456",
		},
		{
			name:        "omitted when empty",
			fieldKey:    "",
			envKey:      "",
			wantPresent: false,
			wantVal:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("INTERNAL_API_KEY", tt.envKey)

			var mu sync.Mutex
			var captured []capturedReq

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				vals, present := r.Header["X-Internal-Key"]
				val := ""
				if len(vals) > 0 {
					val = vals[0]
				}
				mu.Lock()
				captured = append(captured, capturedReq{
					path:      r.URL.Path,
					hasHeader: present,
					headerVal: val,
				})
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/internal/deliver":
					_, _ = w.Write([]byte(`{"status":"delivered","email_id":"00000000-0000-0000-0000-000000000001"}`))
				case "/internal/deliver-batch":
					_, _ = w.Write([]byte(`{"results":[{"recipient":"a@example.com","status":"delivered"},{"recipient":"b@example.com","status":"delivered"}]}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()

			client := newAPIHTTPClient()
			defer client.CloseIdleConnections()
			srv := &server{
				apiURL:      ts.URL,
				internalKey: tt.fieldKey,
				httpClient:  client,
			}

			if err := srv.deliver(context.Background(), deliverPayload{
				Recipient: "a@example.com",
				Raw:       "hello",
			}); err != nil {
				t.Fatalf("deliver(): %v", err)
			}

			results, err := srv.deliverBatch(context.Background(), deliverBatchPayload{
				Recipients: []string{"a@example.com", "b@example.com"},
				Raw:        "hello",
			})
			if err != nil {
				t.Fatalf("deliverBatch(): %v", err)
			}
			if len(results) != 2 {
				t.Fatalf("deliverBatch() len=%d, want 2", len(results))
			}

			mu.Lock()
			defer mu.Unlock()
			if len(captured) != 2 {
				t.Fatalf("captured %d requests, want 2", len(captured))
			}
			for _, req := range captured {
				if req.hasHeader != tt.wantPresent {
					t.Fatalf("%s: X-Internal-Key present=%v, want %v", req.path, req.hasHeader, tt.wantPresent)
				}
				if req.headerVal != tt.wantVal {
					t.Fatalf("%s: X-Internal-Key=%q, want %q", req.path, req.headerVal, tt.wantVal)
				}
			}
		})
	}
}
