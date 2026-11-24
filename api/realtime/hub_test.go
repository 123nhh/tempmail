package realtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestHubSubscribeCapsAndCleanup(t *testing.T) {
	h := NewHub(nil)
	acctA := uuid.New()
	acctB := uuid.New()
	mb := uuid.New()

	unsubs := make([]func(), 0, maxSubsPerAccount)
	for i := 0; i < maxSubsPerAccount; i++ {
		ch, unsub, ok := h.Subscribe(acctA, mb)
		if !ok || ch == nil {
			t.Fatalf("sub %d failed unexpectedly", i)
		}
		unsubs = append(unsubs, unsub)
	}

	// Next subscription for acctA should be rejected.
	if _, _, ok := h.Subscribe(acctA, mb); ok {
		t.Fatalf("expected subscription beyond per-account cap (%d) to fail", maxSubsPerAccount)
	}

	// Another account can still subscribe.
	chB, unsubB, ok := h.Subscribe(acctB, mb)
	if !ok || chB == nil {
		t.Fatalf("expected second account to subscribe")
	}

	// Dispatch reaches active subscribers.
	ev := Event{Type: "email", Data: json.RawMessage(`{"id":"1"}`)}
	h.dispatch(mb, ev)
	select {
	case got := <-chB:
		if got.Type != "email" {
			t.Fatalf("unexpected event type: %q", got.Type)
		}
	default:
		t.Fatalf("expected event on subscriber channel")
	}

	// Releasing one slot allows acctA to subscribe again; calling unsub twice is idempotent.
	unsubs[0]()
	unsubs[0]()
	_, unsubAgain, ok := h.Subscribe(acctA, mb)
	if !ok {
		t.Fatalf("expected subscribe after cleanup to succeed")
	}
	unsubAgain()
	for _, fn := range unsubs[1:] {
		fn()
	}
	unsubB()

	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.totalSubs != 0 || len(h.subs) != 0 || len(h.accountCount) != 0 {
		t.Fatalf("expected empty hub after unsubscribing all, got total=%d subs=%d accts=%d",
			h.totalSubs, len(h.subs), len(h.accountCount))
	}
}

func TestHubPublishIsNonBlockingWhenQueueFull(t *testing.T) {
	// Dummy client (never dialed because Run is not started).
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer rdb.Close()

	h := NewHub(rdb)
	mb := uuid.New()
	for i := 0; i < publishQueueSize+50; i++ {
		h.Publish(context.Background(), mb, "email", map[string]int{"i": i})
	}
	if len(h.pubCh) != publishQueueSize {
		t.Fatalf("expected bounded queue size %d, got %d", publishQueueSize, len(h.pubCh))
	}
}
