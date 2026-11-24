// Package realtime 通过 Redis Pub/Sub 把新邮件事件推送给当前进程内的 SSE 订阅者。
// 投递接口通过有界异步队列 Publish，不阻塞投递 HTTP 响应；
// 每个 API 进程用一条 PSUBSCRIBE 连接接收全部邮箱事件，再按邮箱 ID 分发给本地订阅者，
// 同时限制单账户与全局 SSE 并发连接数，避免连接耗尽资源。
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

const (
	channelPrefix      = "tm:mailbox:"
	publishQueueSize   = 1024
	maxSubsPerAccount  = 20
	maxSubsGlobal      = 2000
	publishSendTimeout = 2 * time.Second
)

// Event 是推送给前端的一条消息，Data 为 JSON 对象。
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type publishItem struct {
	channel string
	payload []byte
}

type Hub struct {
	rdb   *redis.Client
	pubCh chan publishItem

	mu           sync.RWMutex
	subs         map[uuid.UUID]map[chan Event]struct{}
	accountCount map[uuid.UUID]int
	totalSubs    int
}

func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		rdb:          rdb,
		pubCh:        make(chan publishItem, publishQueueSize),
		subs:         make(map[uuid.UUID]map[chan Event]struct{}),
		accountCount: make(map[uuid.UUID]int),
	}
}

// Run 启动 Redis 订阅循环与异步发布协程，连接断开时自动重连，ctx 取消后退出。
func (h *Hub) Run(ctx context.Context) {
	if h == nil || h.rdb == nil {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.publishLoop(ctx)
	}()
	for ctx.Err() == nil {
		h.listen(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	wg.Wait()
}

func (h *Hub) publishLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-h.pubCh:
			pctx, cancel := context.WithTimeout(ctx, publishSendTimeout)
			err := h.rdb.Publish(pctx, item.channel, item.payload).Err()
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Printf("[realtime] publish error: %v", err)
			}
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

// Subscribe 为指定账户订阅某个邮箱的事件通道，超过单账户或全局上限时返回 ok=false。
func (h *Hub) Subscribe(accountID, mailboxID uuid.UUID) (<-chan Event, func(), bool) {
	c := make(chan Event, 16)
	h.mu.Lock()
	if h.totalSubs >= maxSubsGlobal || h.accountCount[accountID] >= maxSubsPerAccount {
		h.mu.Unlock()
		return nil, func() {}, false
	}
	if h.subs[mailboxID] == nil {
		h.subs[mailboxID] = make(map[chan Event]struct{})
	}
	h.subs[mailboxID][c] = struct{}{}
	h.accountCount[accountID]++
	h.totalSubs++
	h.mu.Unlock()

	var once sync.Once
	return c, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[mailboxID], c)
			if len(h.subs[mailboxID]) == 0 {
				delete(h.subs, mailboxID)
			}
			if h.accountCount[accountID] <= 1 {
				delete(h.accountCount, accountID)
			} else {
				h.accountCount[accountID]--
			}
			if h.totalSubs > 0 {
				h.totalSubs--
			}
			h.mu.Unlock()
		})
	}, true
}

// Publish 将事件非阻塞写入发布队列；队列满或 Redis 不可用时丢弃并记录日志，
// 绝不阻塞邮件投递事务后的 HTTP 响应（前端有兜底轮询）。
func (h *Hub) Publish(_ context.Context, mailboxID uuid.UUID, typ string, data any) {
	if h == nil || h.rdb == nil {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload, err := json.Marshal(Event{Type: typ, Data: raw})
	if err != nil {
		return
	}
	select {
	case h.pubCh <- publishItem{channel: channelPrefix + mailboxID.String(), payload: payload}:
	default:
		log.Printf("[realtime] publish queue full, dropping %s event for mailbox %s", typ, mailboxID)
	}
}
