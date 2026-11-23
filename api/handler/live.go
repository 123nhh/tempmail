package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"tempmail/middleware"
	"tempmail/realtime"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// 续期的默认值：每次顺延一个邮箱有效期，单个邮箱最长存活 24 小时
const (
	defaultMaxLifetimeMinutes = 1440
	sseHeartbeat              = 15 * time.Second
)

type LiveHandler struct {
	store *store.Store
	hub   *realtime.Hub
}

func NewLiveHandler(s *store.Store, hub *realtime.Hub) *LiveHandler {
	return &LiveHandler{store: s, hub: hub}
}

// GET /api/mailboxes/:id/events - 以 SSE 推送该邮箱的新邮件
//
// 事件：
//
//	ready    连接建立，data 为邮箱信息
//	email    新邮件摘要，字段与收件列表中的单项一致
//	mailbox  邮箱到期时间变更（续期）
//	expired  邮箱已到期，服务端随后关闭连接
func (h *LiveHandler) Events(c *gin.Context) {
	account := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}
	mb, err := h.store.GetMailbox(c.Request.Context(), id, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	events, cancel := h.hub.Subscribe(id)
	defer cancel()

	w := c.Writer
	rc := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no") // 让 Nginx 不缓冲这个响应
	w.WriteHeader(http.StatusOK)

	send := func(event string, data any) bool {
		// 服务器全局 WriteTimeout 是 10 秒，长连接每次写之前单独放宽
		_ = rc.SetWriteDeadline(time.Now().Add(sseHeartbeat + 10*time.Second))
		c.SSEvent(event, data)
		return rc.Flush() == nil
	}

	// 建议断线后 3 秒重连
	_, _ = fmt.Fprint(w, "retry: 3000\n\n")
	if !send("ready", gin.H{"mailbox": mb}) {
		return
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	expiry := time.NewTimer(time.Until(mb.ExpiresAt))
	defer expiry.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			if ev.Type == "mailbox" {
				var payload struct {
					Mailbox struct {
						ExpiresAt time.Time `json:"expires_at"`
					} `json:"mailbox"`
				}
				if err := json.Unmarshal(ev.Data, &payload); err == nil && !payload.Mailbox.ExpiresAt.IsZero() {
					expiry.Reset(time.Until(payload.Mailbox.ExpiresAt))
				}
			}
			if !send(ev.Type, ev.Data) {
				return
			}
		case <-heartbeat.C:
			_ = rc.SetWriteDeadline(time.Now().Add(sseHeartbeat + 10*time.Second))
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case <-expiry.C:
			// 到期时间可能已被其他请求顺延，重新确认
			latest, err := h.store.GetMailbox(ctx, id, account.ID)
			if err != nil {
				send("expired", gin.H{"id": id})
				return
			}
			expiry.Reset(time.Until(latest.ExpiresAt))
		}
	}
}

// POST /api/mailboxes/:id/extend - 为未过期的邮箱续期
//
// 请求体可选：{"minutes": 60}。未传时按系统设置 mailbox_extend_minutes（默认同邮箱有效期）顺延，
// 总存活时间不超过 mailbox_max_lifetime_minutes（默认 1440，0 表示不限）。
// 响应格式与创建邮箱相同：{"mailbox": {...}}
func (h *LiveHandler) Extend(c *gin.Context) {
	account := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	var req struct {
		Minutes int `json:"minutes"`
	}
	_ = c.ShouldBindJSON(&req)

	ctx := c.Request.Context()
	step := h.intSetting(c, "mailbox_extend_minutes", 0)
	if step <= 0 {
		step = h.intSetting(c, "mailbox_ttl_minutes", 30)
	}
	if step <= 0 {
		step = 30
	}
	if req.Minutes < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "minutes must be positive"})
		return
	}
	// 调用方可以少续一些，但不能超过管理员设定的单次续期时长
	if req.Minutes > 0 && req.Minutes < step {
		step = req.Minutes
	}
	maxLife := h.intSetting(c, "mailbox_max_lifetime_minutes", defaultMaxLifetimeMinutes)

	mb, err := h.store.ExtendMailbox(ctx, id, account.ID, step, maxLife)
	switch {
	case errors.Is(err, store.ErrExtendLimitReached):
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("mailbox lifetime limit reached (%d minutes)", maxLife)})
		return
	case errors.Is(err, pgx.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.hub.Publish(ctx, mb.ID, "mailbox", gin.H{"mailbox": mb})
	c.JSON(http.StatusOK, gin.H{"mailbox": mb})
}

func (h *LiveHandler) intSetting(c *gin.Context, key string, fallback int) int {
	v, err := h.store.GetSetting(c.Request.Context(), key)
	if err != nil || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
