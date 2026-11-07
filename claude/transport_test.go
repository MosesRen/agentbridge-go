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

type fixtureTransport struct {
	mu     sync.Mutex
	closed bool
	input  chan json.RawMessage
}

func newFixtureTransport() *fixtureTransport {
	return &fixtureTransport{input: make(chan json.RawMessage, 32)}
}
func (t *fixtureTransport) Read(ctx context.Context) (json.RawMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case raw, ok := <-t.input:
		if !ok {
			return nil, io.EOF
		}
		return raw, nil
	}
}
func (t *fixtureTransport) Write(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return io.ErrClosedPipe
	}
	var frame struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string                     `json:"subtype"`
			Servers map[string]json.RawMessage `json:"servers"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return err
	}
	var reply any
	if frame.Type == "control_request" {
		reply = map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame.RequestID, "response": map[string]any{"commands": []any{}, "models": []any{}}}}

	} else if frame.Type == "user" {
		reply = map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "remote fixture"}
	}
	if reply != nil {
		data, _ := json.Marshal(reply)
		t.input <- data
	}
	return nil
}
func (t *fixtureTransport) EndInput(context.Context) error { return t.Close() }
func (t *fixtureTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.input)
	}
	return nil
}

func TestCustomTransportUsesSameProtocolWithoutCLI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := Run(ctx, "remote", Options{CLIPath: "/no/such/CLI", Transport: newFixtureTransport()})
	if err != nil || result.Result != "remote fixture" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCustomTransportLifetimeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, err := Dial(ctx, Options{Transport: newFixtureTransport()})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer client.Close()
	cancel()
	waitCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = client.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
