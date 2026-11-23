// Package realtime 通过 Redis Pub/Sub 把新邮件事件推送给当前进程内的 SSE 订阅者。
// 投递接口只负责 Publish；每个 API 进程用一条 PSUBSCRIBE 连接接收全部邮箱事件，
// 再按邮箱 ID 分发给本地订阅者，避免每个浏览器连接各占一条 Redis 连接。
package realtime

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const channelPrefix = "tm:mailbox:"

// Event 是推送给前端的一条消息，Data 为 JSON 对象。
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Hub struct {
	rdb *redis.Client

	mu   sync.RWMutex
	subs map[uuid.UUID]map[chan Event]struct{}
}

func NewHub(rdb *redis.Client) *Hub {
	return &Hub{rdb: rdb, subs: make(map[uuid.UUID]map[chan Event]struct{})}
}

// Run 持续订阅 Redis，连接断开时自动重连，ctx 取消后退出。
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		h.listen(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (h *Hub) listen(ctx context.Context) {
	ps := h.rdb.PSubscribe(ctx, channelPrefix+"*")
	defer ps.Close()
	if _, err := ps.Receive(ctx); err != nil {
		if ctx.Err() == nil {
			log.Printf("[realtime] subscribe error: %v", err)
		}
		return
	}
	ch := ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			id, err := uuid.Parse(strings.TrimPrefix(msg.Channel, channelPrefix))
			if err != nil {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				continue
			}
			h.dispatch(id, ev)
		}
	}
}

func (h *Hub) dispatch(mailboxID uuid.UUID, ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.subs[mailboxID] {
		// 慢消费者直接丢弃事件，前端会用兜底轮询补齐
		select {
		case c <- ev:
		default:
		}
	}
}

// Subscribe 返回事件通道和取消函数；调用方必须在连接结束时调用取消函数。
func (h *Hub) Subscribe(mailboxID uuid.UUID) (<-chan Event, func()) {
	c := make(chan Event, 16)
	h.mu.Lock()
	if h.subs[mailboxID] == nil {
		h.subs[mailboxID] = make(map[chan Event]struct{})
	}
	h.subs[mailboxID][c] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return c, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[mailboxID], c)
			if len(h.subs[mailboxID]) == 0 {
				delete(h.subs, mailboxID)
			}
			h.mu.Unlock()
		})
	}
}

// Publish 把事件发到 Redis；失败只记日志，不影响邮件投递结果。
func (h *Hub) Publish(ctx context.Context, mailboxID uuid.UUID, typ string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload, err := json.Marshal(Event{Type: typ, Data: raw})
	if err != nil {
		return
	}
	// 用独立的短超时，避免调用方请求取消导致事件丢失
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := h.rdb.Publish(pctx, channelPrefix+mailboxID.String(), payload).Err(); err != nil {
		log.Printf("[realtime] publish error: %v", err)
	}
}
