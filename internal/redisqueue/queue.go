package redisqueue

import (
	"sync"
	"sync/atomic"
	"time"
)

const retentionWindow = time.Minute

type queueItem struct {
	enqueuedAt time.Time
	payload    []byte
}

type queue struct {
	mu    sync.Mutex
	items []queueItem
	head  int
}

var (
	enabled atomic.Bool
	global  queue
)

func SetEnabled(value bool) {
	enabled.Store(value)
	if !value {
		global.clear()
	}
}

func Enabled() bool {
	return enabled.Load()
}

func Enqueue(payload []byte) {
	if !Enabled() {
		return
	}
	if len(payload) == 0 {
		return
	}
	global.enqueue(payload)
}

func PopOldest(count int) [][]byte {
	if !Enabled() {
		return nil
	}
	if count <= 0 {
		return nil
	}
	return global.popOldest(count)
}

func SnapshotPayloads() [][]byte {
	return global.snapshotPayloads()
}

func LoadPayloads(payloads [][]byte) int {
	if len(payloads) == 0 {
		return 0
	}
	return global.loadPayloads(payloads)
}

func (q *queue) clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
	q.head = 0
}

func (q *queue) enqueue(payload []byte) {
	now := time.Now()

	q.mu.Lock()
	defer q.mu.Unlock()

	q.pruneLocked(now)
	q.items = append(q.items, queueItem{
		enqueuedAt: now,
		payload:    append([]byte(nil), payload...),
	})
	q.maybeCompactLocked()
}

func (q *queue) popOldest(count int) [][]byte {
	now := time.Now()

	q.mu.Lock()
	defer q.mu.Unlock()

	q.pruneLocked(now)
	available := len(q.items) - q.head
	if available <= 0 {
		q.items = nil
		q.head = 0
		return nil
	}
	if count > available {
		count = available
	}

	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		item := q.items[q.head+i]
		out = append(out, item.payload)
	}
	q.head += count
	q.maybeCompactLocked()
	return out
}

func (q *queue) snapshotPayloads() [][]byte {
	q.mu.Lock()
	defer q.mu.Unlock()

	available := len(q.items) - q.head
	if available <= 0 {
		return nil
	}

	out := make([][]byte, 0, available)
	for i := q.head; i < len(q.items); i++ {
		out = append(out, append([]byte(nil), q.items[i].payload...))
	}
	return out
}

func (q *queue) loadPayloads(payloads [][]byte) int {
	now := time.Now()

	q.mu.Lock()
	defer q.mu.Unlock()

	loaded := 0
	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		q.items = append(q.items, queueItem{
			enqueuedAt: now,
			payload:    append([]byte(nil), payload...),
		})
		loaded++
	}
	q.maybeCompactLocked()
	return loaded
}

func (q *queue) pruneLocked(now time.Time) {
	if q.head >= len(q.items) {
		q.items = nil
		q.head = 0
		return
	}

	cutoff := now.Add(-retentionWindow)
	for q.head < len(q.items) && q.items[q.head].enqueuedAt.Before(cutoff) {
		q.head++
	}
}

func (q *queue) maybeCompactLocked() {
	if q.head == 0 {
		return
	}
	if q.head >= len(q.items) {
		q.items = nil
		q.head = 0
		return
	}
	if q.head < 1024 && q.head*2 < len(q.items) {
		return
	}
	q.items = append([]queueItem(nil), q.items[q.head:]...)
	q.head = 0
}
