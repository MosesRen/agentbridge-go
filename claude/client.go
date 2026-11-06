package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// PermissionHandler decides requests that reach the host. CLI allow/deny rules
// may resolve a tool call before this callback runs. Handlers run concurrently,
// must respect ctx, and may call client control methods (but must not call Close).
type PermissionHandler func(ctx context.Context, request PermissionRequest) (PermissionDecision, error)

type controlReply struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response"`
	Error     string          `json:"error"`
}

type writeJob struct {
	data     []byte
	done     chan error
	ctx      context.Context
	endInput bool
}

// Client owns one CLI process and its native control protocol. Methods are safe
// for concurrent use; use one logical Receive consumer to preserve stream order.
// Send can queue turns; Receive exposes their messages in CLI order.
type Client struct {
	opts           Options
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	ctx            context.Context
	cancel         context.CancelCauseFunc
	done           chan struct{}
	exited         chan struct{}
	readerDone     chan struct{}
	writerDone     chan struct{}
	events         *messageQueue
	writes         chan writeJob
	seq            atomic.Uint64
	mu             sync.Mutex
	pending        map[string]chan controlReply
	callbacks      map[string]context.CancelFunc
	readErr        error
	waitErr        error
	err            error
	stderr         tailBuffer
	initialization json.RawMessage
	hooks          map[string]hookRegistration
	initRequest    map[string]any
	bridgeMu       sync.Mutex
	serverUpdateMu sync.Mutex
	serverConfigs  map[string]json.RawMessage
	bridges        map[string]*mcpBridge
	run            *runTracker
	inputClosed    atomic.Bool
	resumeCleanup  func()
}

// Dial starts Claude Code and completes the SDK initialize handshake. ctx owns
// the process lifetime. Initialization failures terminate and reap the child.
func Dial(ctx context.Context, options Options) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts, err := options.normalized()
	if err != nil {
		return nil, err
	}
	opts, cleanup, err := materializeStoreResume(ctx, opts)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	processCtx, cancel := context.WithCancelCause(ctx)
	c := &Client{opts: opts, ctx: processCtx, cancel: cancel,
		done: make(chan struct{}), exited: make(chan struct{}), readerDone: make(chan struct{}), writerDone: make(chan struct{}),
		events: newMessageQueue(opts.EventBuffer), writes: make(chan writeJob),
		pending: make(map[string]chan controlReply), callbacks: make(map[string]context.CancelFunc), bridges: make(map[string]*mcpBridge)}
	c.run = newRunTracker(c.opts)
	c.resumeCleanup = cleanup
	c.initRequest = c.initializeRequest()
	c.serverConfigs = initialMCPConfigs(opts)
	if opts.Transport != nil {
		c.stdout = &transportReader{ctx: c.ctx, transport: opts.Transport}
		go c.readLoop()
		go c.writeLoop()
		go c.superviseTransport()
	} else {
		c.cmd = exec.Command(opts.CLIPath, opts.args()...)
		c.cmd.WaitDelay = opts.CloseTimeout
		c.cmd.Dir, c.cmd.Env, c.cmd.Stderr = opts.Dir, opts.environment(), &c.stderr
		prepareProcess(c.cmd)
		c.stdin, err = c.cmd.StdinPipe()
		if err != nil {
			cancel(err)
			return nil, fmt.Errorf("claude: create stdin: %w", err)
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			_ = c.stdin.Close()
			cancel(err)
			return nil, fmt.Errorf("claude: create stdout: %w", err)
		}
		c.stdout, c.cmd.Stdout = reader, writer
		if err = c.cmd.Start(); err != nil {
			_ = c.stdin.Close()
			_ = reader.Close()
			_ = writer.Close()
			cancel(err)
			return nil, fmt.Errorf("claude: start executable: %w", err)
		}
		_ = writer.Close() // Only the child keeps the stdout write end.
		go c.readLoop()
		go c.writeLoop()
		go func() {
			err := c.cmd.Wait()
			c.mu.Lock()
			c.waitErr = err
			c.mu.Unlock()
			close(c.exited)
		}()
		go c.supervise()
	}
	initCtx, stop := context.WithTimeout(ctx, opts.StartupTimeout)
	defer stop()
	c.initialization, err = c.request(initCtx, c.initRequest, opts.StartupTimeout)
	if err == nil {
		err = c.checkInitialization(c.initialization)
	}
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("claude: initialize: %w", err)
	}
	success = true
	return c, nil
}

// Initialization returns the raw server capabilities received during Dial.
func (c *Client) Initialization() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.initialization)
}

func (c *Client) currentInitializeRequest() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	request := make(map[string]any, len(c.initRequest))
	for key, value := range c.initRequest {
		request[key] = value
	}
	return request
}

func (c *Client) supervise() {
	select {
	case <-c.ctx.Done():
		signalProcess(c.cmd, false)
		timer := time.NewTimer(c.opts.CloseTimeout)
		select {
		case <-c.exited:
		case <-timer.C:
			signalProcess(c.cmd, true)
			<-c.exited
		}
		timer.Stop()
		// A terminated parent can leave descendants holding inherited pipe ends.
		signalProcess(c.cmd, true)
		_ = c.stdout.Close()
	case <-c.exited:
		// Drain final records, but do not hang on a descendant retaining stdout.
		timer := time.NewTimer(c.opts.CloseTimeout)
		select {
		case <-c.readerDone:
		case <-c.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		signalProcess(c.cmd, true)
		_ = c.stdout.Close()
	}
	_ = c.stdin.Close()
	<-c.readerDone
	c.mu.Lock()
	switch {
	case c.readErr != nil:
		c.err = c.readErr
	case context.Cause(c.ctx) != nil:
		c.err = context.Cause(c.ctx)
	case c.waitErr != nil:
		code := -1
		var exit *exec.ExitError
		if errors.As(c.waitErr, &exit) {
			code = exit.ExitCode()
		}
		c.err = &ProcessError{ExitCode: code, Stderr: c.stderr.String(), Err: c.waitErr}
	default:
		c.err = io.EOF
	}
	c.mu.Unlock()
	c.cancel(ErrClosed)
	c.run.close()
	c.closeMCP()
	c.resumeCleanup()
	<-c.writerDone
	close(c.done)
}

func (c *Client) readLoop() {
	defer close(c.readerDone)
	defer c.events.close()
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, min(4096, c.opts.MaxMessageBytes)), c.opts.MaxMessageBytes)
	var readErr error
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			Type      string          `json:"type"`
			RequestID string          `json:"request_id"`
			Request   json.RawMessage `json:"request"`
			Response  controlReply    `json:"response"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.Type == "" {
			readErr = errors.New("claude: invalid stream-json envelope")
			break
		}
		switch envelope.Type {
		case "control_response":
			c.mu.Lock()
			ch := c.pending[envelope.Response.RequestID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- envelope.Response:
				default:
				}
			}
		case "control_request":
			c.dispatchPermission(envelope.RequestID, bytes.Clone(envelope.Request))
		case "control_cancel_request":
			c.mu.Lock()
			cancel := c.callbacks[envelope.RequestID]
			c.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		case "transcript_mirror":
			if err := c.mirror(line); err != nil {
				data, _ := json.Marshal(map[string]any{"type": "system", "subtype": "mirror_error", "error": "session store append failed"})
				if err := c.events.push(Message{Type: "system", Subtype: "mirror_error", Raw: data}); err != nil {
					readErr = err
				}
			}
		case "keep_alive", "keep-alive", "keep_alive_ping":
		default:
			var message Message
			if err := json.Unmarshal(line, &message); err != nil {
				readErr = fmt.Errorf("claude: decode message: %w", err)
				break
			}
			message.Raw = bytes.Clone(line)
			if !c.run.observe(message) {
				continue
			}
			if err := c.events.push(message); err != nil {
				readErr = err
			}
		}
		if readErr != nil {
			break
		}
	}
	if readErr == nil && c.ctx.Err() == nil {
		if err := scanner.Err(); err != nil {
			readErr = fmt.Errorf("claude: read stream: %w", err)
		}
	}
	if readErr != nil {
		c.mu.Lock()
		c.readErr = readErr
		c.mu.Unlock()
		c.cancel(readErr)
	}
}

func (c *Client) writeLoop() {
	defer close(c.writerDone)
	for {
		select {
		case <-c.ctx.Done():
			return
		case job := <-c.writes:
			err := job.ctx.Err()
			if err == nil {
				if c.opts.Transport != nil {
					if job.endInput {
						err = c.opts.Transport.EndInput(job.ctx)
						c.inputClosed.Store(true)
					} else {
						err = c.opts.Transport.Write(job.ctx, bytes.TrimSuffix(job.data, []byte{'\n'}))
					}
				} else if job.endInput {
					err = c.stdin.Close()
					c.inputClosed.Store(true)
				} else {
					_, err = c.stdin.Write(job.data)
				}
			}
			job.done <- err
			if err != nil && job.ctx.Err() == nil {
				c.cancel(fmt.Errorf("claude: write stream: %w", err))
				return
			}
		}
	}
}

func (c *Client) write(ctx context.Context, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.ctx.Err() != nil {
		return c.closedError()
	}
	if c.inputClosed.Load() {
		return ErrInputClosed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("claude: encode message: %w", err)
	}
	job := writeJob{data: append(data, '\n'), done: make(chan error, 1), ctx: ctx}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.closedError()
	case c.writes <- job:
	}
	select {
	case err := <-job.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.closedError()
	}
}

func (c *Client) closedError() error {
	select {
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	default:
		return context.Cause(c.ctx)
	}
}

// Send queues a plain text user message. Returning successfully only confirms
// the write; read through a result message to observe turn completion.
func (c *Client) Send(ctx context.Context, prompt string) error {
	content, _ := json.Marshal([]ContentBlock{{Type: "text", Text: prompt}})
	return c.SendMessage(ctx, UserMessage{Content: content})
}

func (c *Client) SendMessage(ctx context.Context, message UserMessage) error {
	for key := range message.Extra {
		switch key {
		case "type", "message", "session_id", "parent_tool_use_id", "uuid", "client_composed", "priority", "shouldQuery", "agent_id", "subagent_type", "task_description":
			return fmt.Errorf("claude: Extra cannot override %s", key)
		}
	}
	if message.Priority != "" && message.Priority != "now" && message.Priority != "next" && message.Priority != "later" {
		return fmt.Errorf("claude: invalid user message priority")
	}
	content := bytes.TrimSpace(message.Content)
	if !json.Valid(content) || len(content) == 0 || (content[0] != '"' && content[0] != '[') {
		return fmt.Errorf("claude: user content must be a JSON string or array")
	}
	if message.UUID == "" {
		var err error
		message.UUID, err = newUUID()
		if err != nil {
			return err
		}
	}
	tracked := message.ParentToolUseID == nil && message.AgentID == "" && (message.ShouldQuery == nil || *message.ShouldQuery)
	if tracked {
		c.run.sent(message.UUID)
	}
	wire := map[string]any{}
	for key, value := range message.Extra {
		wire[key] = value
	}
	wire["type"] = "user"
	wire["message"] = map[string]any{"role": "user", "content": content}
	wire["session_id"] = message.SessionID
	wire["parent_tool_use_id"] = message.ParentToolUseID
	wire["uuid"] = message.UUID
	if c.opts.VerbatimPrompts {
		wire["client_composed"] = true
	}
	if message.Priority != "" {
		wire["priority"] = message.Priority
	}
	if message.ShouldQuery != nil {
		wire["shouldQuery"] = *message.ShouldQuery
	}
	if message.AgentID != "" {
		wire["agent_id"] = message.AgentID
	}
	if message.SubagentType != "" {
		wire["subagent_type"] = message.SubagentType
	}
	if message.TaskDescription != "" {
		wire["task_description"] = message.TaskDescription
	}
	err := c.write(ctx, wire)
	if err != nil && tracked {
		c.run.unsent(message.UUID)
	}
	return err
}

// Receive returns the next native message. Canceling ctx does not interrupt a
// turn or terminate the process. On process exit it drains queued messages first.
func (c *Client) Receive(ctx context.Context) (Message, error) {
	return c.events.pop(ctx, c.done, c.closedError)
}

// Request sends a native SDK control request object, including its subtype, and
// correlates the response by ID. New CLI controls can be used without SDK changes.
// Canceling ctx stops waiting; it does not undo an already sent request.
func (c *Client) Request(ctx context.Context, request any) (json.RawMessage, error) {
	return c.request(ctx, request, c.opts.ControlTimeout)
}

func (c *Client) request(ctx context.Context, request any, timeout time.Duration) (json.RawMessage, error) {
	ctx, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	id := "go_" + strconv.FormatUint(c.seq.Add(1), 10)
	ch := make(chan controlReply, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	err := c.write(ctx, map[string]any{"type": "control_request", "request_id": id, "request": request})
	if err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		if reply.Subtype == "error" {
			return nil, &ControlError{RequestID: id, Message: reply.Error}
		}
		if reply.Subtype != "success" {
			return nil, fmt.Errorf("claude: invalid control response subtype %q", reply.Subtype)
		}
		return reply.Response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.closedError()
	}
}

// EndInput closes stdin after preceding writes; output continues until CLI exit.
func (c *Client) EndInput(ctx context.Context) error {
	if c.inputClosed.Load() {
		return nil
	}
	job := writeJob{ctx: ctx, done: make(chan error, 1), endInput: true}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.closedError()
	case c.writes <- job:
	}
	select {
	case err := <-job.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.closedError()
	}
}

func (c *Client) Interrupt(ctx context.Context) error {
	_, err := c.Request(ctx, map[string]any{"subtype": "interrupt"})
	return err
}

func (c *Client) SetModel(ctx context.Context, model string) error {
	_, err := c.Request(ctx, map[string]any{"subtype": "set_model", "model": model})
	return err
}

func (c *Client) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	if _, err := (Options{PermissionMode: mode}).normalized(); err != nil {
		return err
	}
	if mode == "" {
		return fmt.Errorf("claude: permission mode is required")
	}
	_, err := c.Request(ctx, map[string]any{"subtype": "set_permission_mode", "mode": mode})
	return err
}

// Close is idempotent, terminates the owned process, and waits for reaping.
// On Unix it signals the entire process group. On other systems only the direct
// process is terminated. Close does not wait for misbehaving permission callbacks.
func (c *Client) Close() error {
	c.cancel(ErrClosed)
	<-c.done
	return nil
}

// Wait waits for process cleanup. A normal process exit is nil; an explicit
// Close returns ErrClosed. Receive instead reports io.EOF for normal exit.
func (c *Client) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		err := c.closedError()
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

// Stderr returns the most recent 32 KiB of child stderr. It may contain sensitive
// information and is intentionally not included in formatted SDK errors.
func (c *Client) Stderr() string { return c.stderr.String() }

func (c *Client) dispatchPermission(id string, raw json.RawMessage) {
	duration := c.opts.CallbackTimeout
	var request struct {
		Subtype    string `json:"subtype"`
		CallbackID string `json:"callback_id"`
	}
	_ = json.Unmarshal(raw, &request)
	if request.Subtype == "hook_callback" {
		if hook, ok := c.hooks[request.CallbackID]; ok && hook.timeout > 0 && (duration == 0 || hook.timeout < duration) {
			duration = hook.timeout
		}
	}
	ctx, cancel := context.WithCancel(c.ctx)
	if duration > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(c.ctx, duration)
	}
	c.mu.Lock()
	if _, exists := c.callbacks[id]; exists {
		c.mu.Unlock()
		cancel()
		return
	}
	c.callbacks[id] = cancel
	c.mu.Unlock()
	go func() {
		defer cancel()
		defer func() { c.mu.Lock(); delete(c.callbacks, id); c.mu.Unlock() }()
		// Run user code separately so a context-ignoring callback cannot hold the
		// protocol open forever. Such callbacks remain the caller's responsibility.
		type outcome struct {
			decision any
			err      error
		}
		completed := make(chan outcome, 1)
		go func() { decision, err := c.serverControl(ctx, raw); completed <- outcome{decision, err} }()
		var result outcome
		select {
		case result = <-completed:
		case <-ctx.Done():
			// A canceled request has already been abandoned by the CLI.
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			result.err = ctx.Err()
		}
		// Completion and cancellation may become ready at the same time. Never
		// approve a request whose callback deadline or cancellation already fired.
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			result.err = err
		}
		response := map[string]any{"subtype": "success", "request_id": id, "response": result.decision}
		if result.err != nil {
			response = map[string]any{"subtype": "error", "request_id": id, "error": "host control handler failed"}
		}
		writeCtx, stop := context.WithTimeout(c.ctx, c.opts.ControlTimeout)
		defer stop()
		if err := c.write(writeCtx, map[string]any{"type": "control_response", "response": response}); err != nil && c.ctx.Err() == nil {
			c.cancel(err)
		}
	}()
}

func (c *Client) permission(ctx context.Context, raw json.RawMessage) (decision PermissionDecision, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("claude: permission handler panicked")
		}
	}()
	var request struct {
		Subtype string `json:"subtype"`
		PermissionRequest
	}
	if err = json.Unmarshal(raw, &request); err != nil {
		return decision, err
	}
	if request.Subtype != "can_use_tool" {
		return decision, fmt.Errorf("claude: unsupported server control %q", request.Subtype)
	}
	request.Raw = raw
	if c.opts.CanUseTool == nil {
		return PermissionDecision{Behavior: "deny", Message: "No host permission handler configured"}, nil
	}
	decision, err = c.opts.CanUseTool(ctx, request.PermissionRequest)
	if err != nil {
		return decision, err
	}
	if decision.Behavior != "allow" && decision.Behavior != "deny" {
		return decision, errors.New("claude: permission behavior must be allow or deny")
	}
	if decision.Behavior == "allow" && len(decision.UpdatedInput) == 0 {
		decision.UpdatedInput = request.Input
	}
	if decision.Behavior == "allow" && !json.Valid(decision.UpdatedInput) {
		return decision, errors.New("claude: invalid updated input")
	}
	return decision, nil
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 32 << 10
	n := len(p)
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		if len(b.data)+len(p) > limit {
			b.data = b.data[len(b.data)+len(p)-limit:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *tailBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }
