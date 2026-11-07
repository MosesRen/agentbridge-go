package claude

import (
	"context"
	"encoding/json"
	"io"
)

// Transport exchanges complete native SDK protocol objects. It enables custom
// sockets or remote bridges without changing the agent protocol. Implementations
// must unblock Read/Write on ctx cancellation or Close, and bound Close itself.
// The client owns the transport after Dial and closes it during cleanup.
type Transport interface {
	Read(context.Context) (json.RawMessage, error)
	Write(context.Context, json.RawMessage) error
	EndInput(context.Context) error
	Close() error
}

type transportReader struct {
	ctx       context.Context
	transport Transport
	buffer    []byte
}

func (r *transportReader) Read(p []byte) (int, error) {
	for len(r.buffer) == 0 {
		raw, err := r.transport.Read(r.ctx)
		if err != nil {
			return 0, err
		}
		r.buffer = append(append([]byte{}, raw...), '\n')
	}
	n := copy(p, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}
func (r *transportReader) Close() error { return nil }

func (c *Client) superviseTransport() {
	select {
	case <-c.ctx.Done():
	case <-c.readerDone:
	}
	c.mu.Lock()
	cause := c.readErr
	if cause == nil {
		cause = context.Cause(c.ctx)
	}
	if cause == nil {
		cause = io.EOF
	}
	c.err = cause
	c.mu.Unlock()
	c.cancel(ErrClosed)
	_ = c.opts.Transport.Close()
	<-c.readerDone
	<-c.writerDone
	c.run.close()
	c.closeMCP()
	close(c.exited)
	close(c.done)
}
