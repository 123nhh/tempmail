package handler

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
)

// statsCacheTTL 统计结果缓存时间。/public/stats 无需认证，
// 缓存可避免每次请求都对大表执行多次 COUNT(*)。
const statsCacheTTL = 5 * time.Second

type statsSource interface {
	GetStats(ctx context.Context) (*model.Stats, error)
}

type StatsHandler struct {
	store statsSource

	mu       sync.Mutex
	cached   *model.Stats
	cachedAt time.Time
}

func NewStatsHandler(s *store.Store) *StatsHandler {
	return &StatsHandler{store: s}
}

// GET /public/stats  — 公开统计（无需认证）
// GET /api/stats     — 同上（认证后可调用）
func (h *StatsHandler) Get(c *gin.Context) {
	stats, err := h.get(c)
	if err != nil {
		log.Printf("[stats] query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计数据暂时不可用"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// get 返回缓存的统计结果；过期时由持锁的请求刷新，并发请求等待同一次查询，不会重复打库。
func (h *StatsHandler) get(c *gin.Context) (*model.Stats, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached != nil && time.Since(h.cachedAt) < statsCacheTTL {
		return h.cached, nil
	}
	stats, err := h.store.GetStats(c.Request.Context())
	if err != nil {
		return nil, err
	}
	h.cached, h.cachedAt = stats, time.Now()
	return stats, nil
}
