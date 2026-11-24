package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestHashKeyDeterministicAndFixedLength(t *testing.T) {
	h1 := hashKey("192.0.2.10")
	h2 := hashKey("192.0.2.10")
	h3 := hashKey("192.0.2.11")
	if h1 != h2 {
		t.Fatalf("hashKey not deterministic: %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("different inputs produced identical hash: %q", h1)
	}
	if len(h1) != 32 {
		t.Fatalf("expected 32 hex chars, got %d (%q)", len(h1), h1)
	}
}

func TestRateLimitFailOpenWithNilRedis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimitByIP(nil, "test", 1, 60))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("req %d status=%d", i, w.Code)
		}
	}
}

func TestFixedWindowRateLimitWithRedis(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR to run Redis rate-limit integration test")
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}

	gin.SetMode(gin.TestMode)
	scope := "test_" + time.Now().Format("150405.000000")
	r := gin.New()
	r.Use(RateLimitByIP(rdb, scope, 2, 1))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("req %d status=%d want 200", i, w.Code)
		}
	}

	// 3rd request must be rejected with 429 and Retry-After header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd req status=%d want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatalf("missing Retry-After header on 429 response")
	}

	// Repeated 429s must not extend the fixed window indefinitely.
	time.Sleep(1150 * time.Millisecond)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("post-window req status=%d want 200", w.Code)
	}
}
