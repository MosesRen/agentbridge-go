package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

func initialMCPConfigs(options Options) map[string]json.RawMessage {
	raw := options.mergedMCP
	if len(raw) == 0 {
		raw = options.MCPConfig
	}
	var config struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &config)
	if config.Servers == nil {
		config.Servers = map[string]json.RawMessage{}
	}
	return config.Servers
}

// setMCPServers follows the upstream replacement operation. Go instances are
// installed before the control request because the CLI can handshake with a new
// server over mcp_message while that request is still awaiting its response.
func (c *Client) setMCPServers(ctx context.Context, external map[string]json.RawMessage, sdkMaps ...map[string]*MCPServer) (json.RawMessage, error) {
	if len(sdkMaps) > 1 {
		return nil, fmt.Errorf("claude: supply at most one SDK server map")
	}
	next := map[string]*MCPServer{}
	if len(sdkMaps) == 1 {
		for name, server := range sdkMaps[0] {
			if name == "" {
				return nil, fmt.Errorf("claude: MCP name is required")
			}
			if err := server.validate(); err != nil {
				return nil, err
			}
			next[name] = server
		}
	}
	wire := map[string]json.RawMessage{}
	for name, raw := range external {
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil || object == nil {
			return nil, fmt.Errorf("claude: MCP config must be a JSON object")
		}
		if _, duplicate := next[name]; duplicate {
			return nil, fmt.Errorf("claude: duplicate external and SDK MCP server %q", name)
		}
		wire[name] = bytes.Clone(raw)
	}
	for name, server := range next {
		config := map[string]any{"type": "sdk", "name": name}
		if server.Timeout > 0 {
			config["timeout"] = server.Timeout.Milliseconds()
		}
		if server.ToolPolicies != nil {
			config["tools"] = server.ToolPolicies
		}
		wire[name], _ = json.Marshal(config)
	}
	c.serverUpdateMu.Lock()
	defer c.serverUpdateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.ctx.Err() != nil {
		return nil, c.closedError()
	}
	c.bridgeMu.Lock()
	previous := c.opts.SDKServers
	previousWire := c.serverConfigs
	oldBridges := c.bridges
	kept := map[string]*mcpBridge{}
	for name, bridge := range oldBridges {
		if server, ok := next[name]; ok && server == previous[name] {
			kept[name] = bridge
		}
	}
	c.opts.SDKServers = next
	c.bridges = kept
	c.bridgeMu.Unlock()
	if len(next) > 0 {
		c.run.requireBidirectional()
	}
	// The CLI cannot reconnect SDK servers via mcp_reconnect. Remove replaced
	// names first so print mode creates fresh SDK connections on the final set.
	withoutReplaced := map[string]json.RawMessage{}
	replacing := false
	for name, config := range wire {
		if next[name] != nil && previous[name] != nil && next[name] != previous[name] {
			replacing = true
			continue
		}
		withoutReplaced[name] = config
	}
	var response json.RawMessage
	var err error
	removedForReplacement := false
	if replacing {
		_, err = c.control(ctx, "mcp_set_servers", map[string]any{"servers": withoutReplaced})
		removedForReplacement = err == nil
	}
	if err == nil {
		response, err = c.control(ctx, "mcp_set_servers", map[string]any{"servers": wire})
	}
	var dispose []*mcpBridge
	c.bridgeMu.Lock()
	if err != nil {
		for name, bridge := range c.bridges {
			if oldBridges[name] != bridge {
				dispose = append(dispose, bridge)
			}
		}
		c.opts.SDKServers = previous
		c.bridges = oldBridges
	} else {
		for name, bridge := range oldBridges {
			if c.bridges[name] != bridge {
				dispose = append(dispose, bridge)
			}
		}
	}
	c.bridgeMu.Unlock()
	if err != nil && removedForReplacement && ctx.Err() == nil && c.ctx.Err() == nil {
		if _, rollbackErr := c.control(ctx, "mcp_set_servers", map[string]any{"servers": previousWire}); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("claude: restore MCP registry: %w", rollbackErr))
		}
	}
	closeMCPBridges(dispose, c.opts.CloseTimeout)
	if err != nil {
		return nil, err
	}
	c.serverConfigs = wire
	names := make([]string, 0, len(next))
	configs := map[string]any{}
	for name, server := range next {
		names = append(names, name)
		config := map[string]any{}
		if server.Timeout > 0 {
			config["timeout"] = server.Timeout.Milliseconds()
		}
		if server.ToolPolicies != nil {
			config["tools"] = server.ToolPolicies
		}
		if len(config) > 0 {
			configs[name] = config
		}
	}
	sort.Strings(names)
	c.mu.Lock()
	c.initRequest["sdkMcpServers"] = names
	c.initRequest["sdkMcpServerConfigs"] = configs
	c.mu.Unlock()
	return response, nil
}

func closeMCPBridges(bridges []*mcpBridge, timeout time.Duration) {
	if len(bridges) == 0 {
		return
	}
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
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}
