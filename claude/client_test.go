package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProtocolProcess is a fake CLI executed in a real child process. It never
// invokes Claude, uses credentials, accesses the network, or calls a model.
func TestProtocolProcess(t *testing.T) {
	if os.Getenv("AGENTBRIDGE_PROTOCOL_CHILD") != "1" {
		return
	}
	mode := os.Getenv("AGENTBRIDGE_PROTOCOL_MODE")
	write := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	result := func(turn int) {
		value := map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": "session-test", "result": fmt.Sprintf("answer %d", turn), "num_turns": turn,
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 20}, "total_cost_usd": 0.001}
		if mode == "error-result" {
			value["is_error"] = true
			value["errors"] = []string{"budget exhausted"}
		}
		write(value)
	}
	if mode == "exit" {
		fmt.Fprint(os.Stderr, "private diagnostic")
		os.Exit(7)
	}
	if mode == "malformed-init" {
		fmt.Println("not json")
		os.Exit(0)
	}
	turn := 0
	var delayed []map[string]any
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var frame struct {
			Type, RequestID string
			Request         json.RawMessage
			Response        json.RawMessage
			Message         struct{ Content json.RawMessage }
		}
		// request_id needs its native spelling; other fields match case-insensitively.
		var wire map[string]json.RawMessage
		_ = json.Unmarshal(scanner.Bytes(), &wire)
		_ = json.Unmarshal(scanner.Bytes(), &frame)
		_ = json.Unmarshal(wire["request_id"], &frame.RequestID)
		switch frame.Type {
		case "control_request":
			var request struct{ Subtype, Model string }
			_ = json.Unmarshal(frame.Request, &request)
			if request.Subtype == "initialize" && mode == "hang-init" {
				continue
			}
			if request.Subtype == "never" {
				continue
			}
			reply := map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame.RequestID, "response": map[string]any{"model": request.Model, "commands": []string{"interrupt"}}}}
			if request.Subtype == "fail" {
				reply["response"] = map[string]any{"subtype": "error", "request_id": frame.RequestID, "error": "unsupported"}
			}
			if request.Subtype == "set_model" && mode == "reorder" {
				delayed = append(delayed, reply)
				if len(delayed) == 2 {
					write(delayed[1])
					write(delayed[0])
					delayed = nil
				}
				continue
			}
			write(reply)
		case "user":
			turn++
			if mode == "hang-turn" {
				continue
			}
			if mode == "missing" {
				os.Exit(0)
			}
			if mode == "malformed" {
				fmt.Println("{broken")
				continue
			}
			if mode == "overflow" {
				for i := 0; i < 500; i++ {
					write(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta"}})
				}
				continue
			}
			if mode == "permission" || mode == "cancel-permission" {
				write(map[string]any{"type": "control_request", "request_id": "permission-1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "tool_use_id": "tool-1", "input": map[string]any{"command": "pwd"}}})
				continue
			}
			text := "hello"
			if mode == "large" {
				text = strings.Repeat("x", 128<<10)
			}
			write(map[string]any{"type": "system", "subtype": "init", "session_id": "session-test"})
			write(map[string]any{"type": "assistant", "future_field": "preserved", "message": map[string]any{"role": "assistant", "content": []map[string]any{{"type": "text", "text": text}, {"type": "thinking", "thinking": "not exposed by Text"}}}})
			write(map[string]any{"type": "future_event", "payload": true})
			result(turn)
		case "control_response":
			var response struct {
				Subtype  string
				Response PermissionDecision
			}
			_ = json.Unmarshal(frame.Response, &response)
			if response.Subtype == "error" {
				write(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": true, "errors": []string{"permission failed"}})
				continue
			}
			data, _ := json.Marshal(response.Response)
			write(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": string(data)}})
			result(turn)
		}
	}
	os.Exit(0)
}

func fakeOptions(t *testing.T, mode string) Options {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{CLIPath: path, commandPrefix: []string{"-test.run=^TestProtocolProcess$", "--"},
		Env:            map[string]string{"AGENTBRIDGE_PROTOCOL_CHILD": "1", "AGENTBRIDGE_PROTOCOL_MODE": mode, "GORACE": "atexit_sleep_ms=0"},
		StartupTimeout: 3 * time.Second, ControlTimeout: 3 * time.Second, CloseTimeout: 100 * time.Millisecond}
}

func dialFake(t *testing.T, options Options) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, err := Dial(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func turnMessages(t *testing.T, client *Client) []Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var messages []Message
	for {
		message, err := client.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
		if message.Type == "result" {
			return messages
		}
	}
}

func TestPersistentTurns(t *testing.T) {
	client := dialFake(t, fakeOptions(t, "normal"))
	if !strings.Contains(string(client.Initialization()), "commands") {
		t.Fatal("missing initialization")
	}
	for turn := 1; turn <= 2; turn++ {
		if err := client.Send(context.Background(), "prompt with `literal` $() and\nnewline"); err != nil {
			t.Fatal(err)
		}
		messages := turnMessages(t, client)
		if len(messages) != 4 || messages[1].Text() != "hello" || messages[2].Type != "future_event" {
			t.Fatalf("unexpected messages: %+v", messages)
		}
		if !strings.Contains(string(messages[1].Raw), "future_field") {
			t.Fatal("lost raw fields")
		}
		result, err := messages[3].Result()
		if err != nil || result.NumTurns != turn || result.Usage.OutputTokens != 20 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	if err := client.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.SetPermissionMode(context.Background(), PermissionPlan); err != nil {
		t.Fatal(err)
	}
}

func TestRunAndAgentError(t *testing.T) {
	for _, mode := range []string{"normal", "error-result"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := Run(ctx, "hello", fakeOptions(t, mode))
			if result == nil {
				t.Fatalf("missing result: %v", err)
			}
			if mode == "normal" && err != nil {
				t.Fatal(err)
			}
			if mode == "error-result" {
				var failure *ResultError
				if !errors.As(err, &failure) || !strings.Contains(err.Error(), "budget exhausted") {
					t.Fatalf("expected agent error, got %v", err)
				}
			}
		})
	}
}

func TestStreamAndMissingResult(t *testing.T) {
	for _, mode := range []string{"normal", "missing"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stream, err := Query(ctx, "hello", fakeOptions(t, mode))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			result, err := stream.Result(ctx)
			if mode == "missing" {
				if !errors.Is(err, ErrMissingResult) {
					t.Fatalf("got %v", err)
				}
				return
			}
			if err != nil || result.Result != "answer 1" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if _, err = stream.Next(ctx); !errors.Is(err, io.EOF) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestPermissionCallbacks(t *testing.T) {
	for _, behavior := range []string{"default-deny", "allow", "deny", "invalid", "panic", "timeout"} {
		t.Run(behavior, func(t *testing.T) {
			options := fakeOptions(t, "permission")
			options.CallbackTimeout = 40 * time.Millisecond
			if behavior != "default-deny" {
				options.CanUseTool = func(ctx context.Context, request PermissionRequest) (PermissionDecision, error) {
					if request.ToolName != "Bash" || request.ToolUseID != "tool-1" || !json.Valid(request.Raw) {
						return PermissionDecision{}, errors.New("bad request")
					}
					if behavior == "panic" {
						panic("private panic")
					}
					if behavior == "timeout" {
						<-ctx.Done()
						return PermissionDecision{}, ctx.Err()
					}
					return PermissionDecision{Behavior: behavior, Message: "host decision"}, nil
				}
			}
			client := dialFake(t, options)
			if err := client.Send(context.Background(), "permission"); err != nil {
				t.Fatal(err)
			}
			messages := turnMessages(t, client)
			result, err := messages[len(messages)-1].Result()
			if behavior == "invalid" || behavior == "panic" || behavior == "timeout" {
				if err == nil || !result.IsError {
					t.Fatalf("expected failure: %+v, %v", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var decision PermissionDecision
			if err := json.Unmarshal([]byte(messages[0].Text()), &decision); err != nil {
				t.Fatal(err)
			}
			want := behavior
			if want == "default-deny" {
				want = "deny"
			}
			if decision.Behavior != want {
				t.Fatalf("decision=%+v", decision)
			}
			if behavior == "allow" && string(decision.UpdatedInput) != `{"command":"pwd"}` {
				t.Fatalf("original input not preserved: %s", decision.UpdatedInput)
			}
		})
	}
}

func TestControlCorrelationAndErrors(t *testing.T) {
	client := dialFake(t, fakeOptions(t, "reorder"))
	var wg sync.WaitGroup
	for _, model := range []string{"one", "two"} {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			response, err := client.Request(context.Background(), map[string]any{"subtype": "set_model", "model": model})
			if err != nil {
				t.Error(err)
				return
			}
			var decoded struct{ Model string }
			_ = json.Unmarshal(response, &decoded)
			if decoded.Model != model {
				t.Errorf("got %q, want %q", decoded.Model, model)
			}
		}(model)
	}
	wg.Wait()
	_, err := client.Request(context.Background(), map[string]any{"subtype": "fail"})
	var control *ControlError
	if !errors.As(err, &control) {
		t.Fatalf("got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = client.Request(ctx, map[string]any{"subtype": "never"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if err = client.Interrupt(context.Background()); err != nil {
		t.Fatal("request timeout killed the client:", err)
	}
}

func TestReceiveTimeoutDoesNotKillProcess(t *testing.T) {
	client := dialFake(t, fakeOptions(t, "normal"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Receive(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if err := client.Send(context.Background(), "still alive"); err != nil {
		t.Fatal(err)
	}
	turnMessages(t, client)
}

func TestInitializationFailureCleanup(t *testing.T) {
	for _, mode := range []string{"hang-init", "malformed-init", "exit"} {
		t.Run(mode, func(t *testing.T) {
			options := fakeOptions(t, mode)
			if mode == "hang-init" {
				options.StartupTimeout = 200 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, err := Dial(ctx, options)
			if client != nil || err == nil {
				t.Fatalf("client=%v err=%v", client, err)
			}
			if mode == "hang-init" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("got %v", err)
			}
			if mode == "exit" {
				var failure *ProcessError
				if !errors.As(err, &failure) || failure.ExitCode != 7 || failure.Stderr != "private diagnostic" {
					t.Fatalf("unexpected error: %v", err)
				}
				if strings.Contains(err.Error(), "private diagnostic") {
					t.Fatal("stderr leaked into Error()")
				}
			}
		})
	}
}

func TestBufferAndMalformedStream(t *testing.T) {
	for _, mode := range []string{"large", "overflow", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			options := fakeOptions(t, mode)
			if mode == "overflow" {
				options.EventBuffer = 2
			}
			client := dialFake(t, options)
			if err := client.Send(context.Background(), "go"); err != nil {
				t.Fatal(err)
			}
			if mode == "large" {
				messages := turnMessages(t, client)
				if len(messages[1].Text()) != 128<<10 {
					t.Fatal("truncated large frame")
				}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := client.Wait(ctx)
			if mode == "overflow" && !errors.Is(err, ErrEventBufferFull) {
				t.Fatalf("got %v", err)
			}
			if mode == "malformed" && (err == nil || !strings.Contains(err.Error(), "invalid stream-json")) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestLifetimeCancellationAndClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, err := Dial(ctx, fakeOptions(t, "hang-turn"))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err = client.Send(ctx, "wait"); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	waitCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err = client.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if !client.cmd.ProcessState.Exited() && client.cmd.ProcessState.ExitCode() != -1 {
		t.Fatal("child not reaped")
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMessageSizeLimit(t *testing.T) {
	options := fakeOptions(t, "large")
	options.MaxMessageBytes = 32 << 10
	client := dialFake(t, options)
	if err := client.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Wait(ctx); err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("expected record size limit error, got %v", err)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	t.Setenv("CLAUDECODE", "parent")
	t.Setenv("AGENTBRIDGE_ENV_TEST", "inherited")
	options := Options{Env: map[string]string{"AGENTBRIDGE_ENV_TEST": "override"}}
	for _, entry := range options.environment() {
		if strings.HasPrefix(entry, "CLAUDECODE=") {
			t.Fatal("nested marker inherited")
		}
		if strings.HasPrefix(entry, "AGENTBRIDGE_ENV_TEST=") && entry != "AGENTBRIDGE_ENV_TEST=override" {
			t.Fatal("environment override lost")
		}
	}
}

func TestOptionsAndArgBoundaries(t *testing.T) {
	prompt := "--flag-like prompt"
	options, err := (Options{SystemPrompt: &prompt, Resume: "--flag-like-id", Tools: []string{}, AllowedTools: []string{"Bash(git status)"}}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(options.args(), "\n")
	for _, want := range []string{"--resume=--flag-like-id", "--tools=\n", "--allowedTools=Bash(git status)"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
	client := &Client{opts: options}
	request := client.initializeRequest()
	data, _ := json.Marshal(request["systemPrompt"])
	if string(data) != `["--flag-like prompt"]` {
		t.Fatalf("prompt was not bound to initialization: %s", data)
	}
	for _, options := range []Options{{MaxTurns: -1}, {EventBuffer: -1}, {Resume: "a", Continue: true}, {ForkSession: true}, {MCPConfig: json.RawMessage(`[]`)}, {PermissionMode: "invalid"}} {
		if _, err := options.normalized(); err == nil {
			t.Errorf("accepted invalid options: %+v", options)
		}
	}
}
