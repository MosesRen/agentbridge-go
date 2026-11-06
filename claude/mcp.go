package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServer wraps the official Go MCP server. Register tools with mcp.AddTool,
// or resources/prompts on Server, before sharing it with a Client. Each Client
// opens its own MCP session, so a definition can be reused across clients.
type MCPServer struct {
	Server       *mcp.Server
	Timeout      time.Duration
	ToolPolicies []MCPToolPolicy
}
type MCPToolPolicy struct {
	Name             string `json:"name"`
	PermissionPolicy string `json:"permission_policy,omitempty"`
	OrgMaxPermission string `json:"org_max_permission,omitempty"`
}

func NewMCPServer(name, version string) *MCPServer {
	return &MCPServer{Server: mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil)}
}

func (s *MCPServer) validate() error {
	if s == nil || s.Server == nil {
		return fmt.Errorf("claude: nil MCP server")
	}
	return nil
}

type mcpBridge struct {
	connection mcp.Connection
	session    *mcp.ServerSession
	mu         sync.Mutex
	pending    map[jsonrpc.ID]chan json.RawMessage
	failure    error
	done       chan struct{}
}

func newMCPBridge(ctx context.Context, server *MCPServer) (*mcpBridge, error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, err
	}
	connection, err := clientTransport.Connect(ctx)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	b := &mcpBridge{connection: connection, session: session, pending: map[jsonrpc.ID]chan json.RawMessage{}, done: make(chan struct{})}
	go b.read(ctx)
	return b, nil
}

func (b *mcpBridge) read(ctx context.Context) {
	defer close(b.done)
	for {
		message, err := b.connection.Read(ctx)
		if err != nil {
			b.mu.Lock()
			b.failure = err
			for id, ch := range b.pending {
				close(ch)
				delete(b.pending, id)
			}
			b.mu.Unlock()
			return
		}
		switch message := message.(type) {
		case *jsonrpc.Response:
			data, err := jsonrpc.EncodeMessage(message)
			if err != nil {
				continue
			}
			b.mu.Lock()
			ch := b.pending[message.ID]
			delete(b.pending, message.ID)
			if ch != nil {
				ch <- data
				close(ch)
			}
			b.mu.Unlock()
		case *jsonrpc.Request:
			// As upstream does, discard notifications and reject unsupported
			// server-initiated sampling/roots requests instead of leaving them hung.
			if message.IsCall() {
				reply := &jsonrpc.Response{ID: message.ID, Error: &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "host-to-CLI MCP requests are not supported"}}
				_ = b.connection.Write(ctx, reply)
			}
		}
	}
}

func (b *mcpBridge) handle(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	message, err := jsonrpc.DecodeMessage(raw)
	if err != nil {
		return nil, err
	}
	request, isRequest := message.(*jsonrpc.Request)
	if !isRequest || !request.IsCall() {
		if err = b.connection.Write(ctx, message); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"jsonrpc":"2.0","result":{}}`), nil
	}
	ch := make(chan json.RawMessage, 1)
	b.mu.Lock()
	if b.failure != nil {
		err = b.failure
		b.mu.Unlock()
		return nil, err
	}
	if _, exists := b.pending[request.ID]; exists {
		b.mu.Unlock()
		return nil, fmt.Errorf("claude: duplicate in-flight MCP request ID")
	}
	b.pending[request.ID] = ch
	b.mu.Unlock()
	if err = b.connection.Write(ctx, message); err != nil {
		b.mu.Lock()
		delete(b.pending, request.ID)
		b.mu.Unlock()
		return nil, err
	}
	select {
	case response, ok := <-ch:
		if !ok {
			return nil, errors.New("claude: MCP session closed")
		}
		return response, nil
	case <-ctx.Done():
		params, _ := json.Marshal(map[string]any{"requestId": request.ID.Raw()})
		go func() {
			notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			_ = b.connection.Write(notifyCtx, &jsonrpc.Request{Method: "notifications/cancelled", Params: params})
		}()
		// Keep the ID reserved until its response arrives, so a late response
		// cannot be misdelivered to a newly reused ID.
		return nil, ctx.Err()
	}
}

func (b *mcpBridge) close() { _ = b.connection.Close(); _ = b.session.Close() }

func (c *Client) mcpMessage(ctx context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
	c.bridgeMu.Lock()
	bridge := c.bridges[name]
	if bridge == nil {
		server := c.opts.SDKServers[name]
		if server == nil {
			c.bridgeMu.Unlock()
			return nil, fmt.Errorf("claude: unknown SDK MCP server %q", name)
		}
		var err error
		bridge, err = newMCPBridge(c.ctx, server)
		if err != nil {
			c.bridgeMu.Unlock()
			return nil, err
		}
		c.bridges[name] = bridge
	}
	c.bridgeMu.Unlock()
	return bridge.handle(ctx, raw)
}

func (c *Client) closeMCP() {
	c.bridgeMu.Lock()
	bridges := make([]*mcpBridge, 0, len(c.bridges))
	for _, b := range c.bridges {
		bridges = append(bridges, b)
	}
	c.bridgeMu.Unlock()
	for _, bridge := range bridges {
		_ = bridge.connection.Close()
	}
	done := make(chan struct{})
	go func() {
		for _, bridge := range bridges {
			_ = bridge.session.Close()
		}
		close(done)
	}()
	timer := time.NewTimer(c.opts.CloseTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}
