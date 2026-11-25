package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newInternalTestRouter(key string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/internal")
	g.Use(internalAuthMiddleware(key))
	g.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	g.POST("/deliver", maxBodySizeMiddleware(16), func(c *gin.Context) {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.String(http.StatusOK, "ok")
	})
	return r
}

func TestInternalAuthMiddlewareWithKey(t *testing.T) {
	r := newInternalTestRouter("s3cret")
	cases := []struct {
		name   string
		header string
		remote string
		want   int
	}{
		{"missing key", "", "127.0.0.1:1234", http.StatusUnauthorized},
		{"wrong key", "nope", "127.0.0.1:1234", http.StatusUnauthorized},
		{"prefix key", "s3cre", "127.0.0.1:1234", http.StatusUnauthorized},
		{"correct key from public ip", "s3cret", "203.0.113.9:1234", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/ping", nil)
			req.RemoteAddr = tc.remote
			if tc.header != "" {
				req.Header.Set("X-Internal-Key", tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d (body=%s)", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusUnauthorized && !strings.Contains(w.Body.String(), `"error":"unauthorized"`) {
				t.Fatalf("unexpected body %s", w.Body.String())
			}
		})
	}
}

func TestInternalAuthMiddlewareWithoutKey(t *testing.T) {
	r := newInternalTestRouter("")
	cases := []struct {
		name   string
		remote string
		xff    string
		want   int
	}{
		{"ipv4 loopback", "127.0.0.1:5555", "", http.StatusOK},
		{"ipv6 loopback", "[::1]:5555", "", http.StatusOK},
		{"docker private", "172.18.0.5:5555", "", http.StatusOK},
		{"rfc1918 10/8", "10.1.2.3:5555", "", http.StatusOK},
		{"public ip", "203.0.113.9:5555", "", http.StatusForbidden},
		{"public ip spoofing xff", "203.0.113.9:5555", "127.0.0.1", http.StatusForbidden},
		{"garbage remote addr", "not-an-ip", "", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/ping", nil)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d (body=%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestMaxBodySizeMiddleware(t *testing.T) {
	r := newInternalTestRouter("")

	small := httptest.NewRequest(http.MethodPost, "/internal/deliver", strings.NewReader(`{"a":1}`))
	small.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, small)
	if w.Code != http.StatusOK {
		t.Fatalf("small body: got %d, want 200", w.Code)
	}

	big := httptest.NewRequest(http.MethodPost, "/internal/deliver", strings.NewReader(`{"a":"`+strings.Repeat("x", 64)+`"}`))
	big.RemoteAddr = "127.0.0.1:1"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversized body: got %d, want 413", w.Code)
	}

	chunked := httptest.NewRequest(http.MethodPost, "/internal/deliver", strings.NewReader(`{"a":"`+strings.Repeat("x", 64)+`"}`))
	chunked.RemoteAddr = "127.0.0.1:1"
	chunked.ContentLength = -1
	w = httptest.NewRecorder()
	r.ServeHTTP(w, chunked)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("undeclared oversized body: got %d, want 400/413", w.Code)
	}
}

// LMTP 侧单封邮件最大 10 MiB，投递时 raw + 正文一起发送并经 JSON 转义，
// 上限必须覆盖这种最坏情况，否则合法邮件会被 413 拒收。
func TestInternalDeliverLimitCoversMaxMessage(t *testing.T) {
	const maxMessageBytes = 10 << 20
	if worst := int64(4*maxMessageBytes + 1<<20); internalDeliverMaxBodyBytes < worst {
		t.Fatalf("internalDeliverMaxBodyBytes = %d, want >= %d", internalDeliverMaxBodyBytes, worst)
	}
}
