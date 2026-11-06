package claude

import (
	"context"
	"sync"
)

// An unbounded default queue keeps protocol routing independent from consumer
// speed. A caller can explicitly request a bounded, fail-on-overflow queue.
type messageQueue struct {
	mu     sync.Mutex
	items  []Message
	head   int
	limit  int
	closed bool
	notify chan struct{}
}

func newMessageQueue(limit int) *messageQueue {
	return &messageQueue{limit: limit, notify: make(chan struct{}, 1)}
}
func (q *messageQueue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
func (q *messageQueue) push(message Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.limit > 0 && len(q.items)-q.head >= q.limit {
		return ErrEventBufferFull
	}
	q.items = append(q.items, message)
	q.wake()
	return nil
}
func (q *messageQueue) close() { q.mu.Lock(); q.closed = true; q.mu.Unlock(); q.wake() }
func (q *messageQueue) pop(ctx context.Context, done <-chan struct{}, closedError func() error) (Message, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Message{}, err
		}
		q.mu.Lock()
		if q.head < len(q.items) {
			m := q.items[q.head]
			q.items[q.head] = Message{}
			q.head++
			if q.head == len(q.items) {
				q.items = nil
				q.head = 0
			}
			q.mu.Unlock()
			return m, nil
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			select {
			case <-ctx.Done():
				return Message{}, ctx.Err()
			case <-done:
				return Message{}, closedError()
			}
		}
		select {
		case <-ctx.Done():
			return Message{}, ctx.Err()
		case <-q.notify:
		case <-done:
		}
	}
}
