package claude

import (
	"context"
	"errors"
	"io"
)

// Stream owns a one-shot run, including background continuation turns.
// Next and Result must be called by one consumer.
// Close the stream even if iteration stops early. For background work or queued
// prompts, use Dial and decide when to close the persistent client yourself.
type Stream struct {
	client *Client
	ended  bool
	result *Result
	err    error
}

// Query starts a process and sends one prompt. It closes input once the run
// settles and yields output through process exit, including continuation turns.
// Close aborts any remaining work.
func Query(ctx context.Context, prompt string, options Options) (*Stream, error) {
	client, err := Dial(ctx, options)
	if err != nil {
		return nil, err
	}
	if err = client.Send(ctx, prompt); err != nil {
		_ = client.Close()
		return nil, err
	}
	go func() {
		if client.run.hasBidirectional() {
			if client.run.wait(client.ctx) != nil {
				return
			}
		}
		if err := client.EndInput(client.ctx); err != nil && client.ctx.Err() == nil {
			select {
			case <-client.exited:
			default:
				client.cancel(err)
			}
		}
	}()
	return &Stream{client: client}, nil
}

func (s *Stream) Next(ctx context.Context) (Message, error) {
	if s.ended {
		if s.err != nil {
			return Message{}, s.err
		}
		return Message{}, io.EOF
	}
	message, err := s.client.Receive(ctx)
	if err != nil {
		// A receive timeout does not invalidate the stream; the caller may retry.
		if ctx.Err() != nil {
			return Message{}, err
		}
		if errors.Is(err, io.EOF) {
			if s.result == nil {
				err = ErrMissingResult
			} else {
				err = s.err
			}
		} else if s.result != nil && s.err != nil {
			var process *ProcessError
			if errors.As(err, &process) {
				err = s.err
			}
		}
		s.ended, s.err = true, err
		if err == nil {
			return Message{}, io.EOF
		}
		return Message{}, err
	}
	if message.Type == "result" {
		s.result, s.err = message.Result()
	}
	return message, nil
}

// Result consumes remaining messages and returns the final turn's result. Agent
// failures return both a Result and a *ResultError, even if CLI exit status is 0.
func (s *Stream) Result(ctx context.Context) (*Result, error) {
	for !s.ended {
		if _, err := s.Next(ctx); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return s.result, err
		}
	}
	return s.result, s.err
}

func (s *Stream) Close() error { return s.client.Close() }

// Run executes a single foreground turn and closes its process before returning.
func Run(ctx context.Context, prompt string, options Options) (*Result, error) {
	stream, err := Query(ctx, prompt, options)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return stream.Result(ctx)
}
