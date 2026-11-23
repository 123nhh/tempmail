package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tempmail/model"

	"github.com/gin-gonic/gin"
)

type fakeStats struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeStats) GetStats(context.Context) (*model.Stats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &model.Stats{TotalEmails: int64(f.calls)}, nil
}

func init() { gin.SetMode(gin.TestMode) }

func serveStats(h *StatsHandler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/public/stats", nil)
	h.Get(c)
	return w
}

func TestStatsCachedWithinTTL(t *testing.T) {
	src := &fakeStats{}
	h := &StatsHandler{store: src}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); serveStats(h) }()
	}
	wg.Wait()
	if src.calls != 1 {
		t.Fatalf("expected 1 query for concurrent requests, got %d", src.calls)
	}

	h.cachedAt = time.Now().Add(-statsCacheTTL - time.Millisecond)
	w := serveStats(h)
	if src.calls != 2 || !strings.Contains(w.Body.String(), `"total_emails":2`) {
		t.Fatalf("expected refresh after TTL, calls=%d body=%s", src.calls, w.Body.String())
	}
}

func TestStatsErrorHidesDetails(t *testing.T) {
	h := &StatsHandler{store: &fakeStats{err: errors.New("pq: password authentication failed for user secret")}}
	w := serveStats(h)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "pq:") {
		t.Fatalf("internal error leaked: %s", w.Body.String())
	}
}
