package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func testTextField(object map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(object[key], &value)
	return value
}

type alignmentTransport struct {
	mu     sync.Mutex
	input  chan json.RawMessage
	closed bool
	write  func(map[string]json.RawMessage)
	frames []map[string]json.RawMessage
}

func newAlignmentTransport() *alignmentTransport {
	return &alignmentTransport{input: make(chan json.RawMessage, 128)}
}
func (t *alignmentTransport) emit(value any) {
	raw, _ := json.Marshal(value)
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.input <- raw
	}
}
func (t *alignmentTransport) reply(frame map[string]json.RawMessage, response any, extras map[string]any) {
	var id string
	_ = json.Unmarshal(frame["request_id"], &id)
	r := map[string]any{"subtype": "success", "request_id": id, "response": response}
	for k, v := range extras {
		r[k] = v
	}
	t.emit(map[string]any{"type": "control_response", "response": r})
}
func (t *alignmentTransport) Read(ctx context.Context) (json.RawMessage, error) {
	select {
	case raw, ok := <-t.input:
		if !ok {
			return nil, io.EOF
		}
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (t *alignmentTransport) Write(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		return err
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return io.ErrClosedPipe
	}
	t.frames = append(t.frames, frame)
	t.mu.Unlock()
	if t.write != nil {
		t.write(frame)
	}
	return nil
}
func (t *alignmentTransport) EndInput(context.Context) error { return t.Close() }
func (t *alignmentTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.input)
	}
	return nil
}
func (t *alignmentTransport) snapshot() []map[string]json.RawMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]map[string]json.RawMessage{}, t.frames...)
}
func wireSubtype(frame map[string]json.RawMessage) string {
	var request map[string]json.RawMessage
	_ = json.Unmarshal(frame["request"], &request)
	return testTextField(request, "subtype")
}

func TestControlCancellationIsForwarded(t *testing.T) {
	transport := newAlignmentTransport()
	transport.write = func(f map[string]json.RawMessage) {
		if wireSubtype(f) == "initialize" {
			transport.reply(f, map[string]any{}, nil)
		}
	}
	client, err := Dial(context.Background(), Options{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = client.Request(ctx, map[string]any{"subtype": "waiting"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		for _, frame := range transport.snapshot() {
			if testTextField(frame, "type") == "control_cancel_request" {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatal("missing cancel frame")
		case <-time.After(time.Millisecond):
		}
	}
}
