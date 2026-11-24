package main

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
