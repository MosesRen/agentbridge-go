//go:build integration

package claude

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Live tests are opt-in: they invoke the installed CLI and consume model usage.
func TestLiveCLI(t *testing.T) {
	if os.Getenv("AGENTBRIDGE_LIVE_TEST") != "1" {
		t.Skip("set AGENTBRIDGE_LIVE_TEST=1 to call the real Claude CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	options := Options{Dir: dir, Tools: []string{}, StrictMCPConfig: true,
		MCPConfig: json.RawMessage(`{"mcpServers":{}}`), NoSessionPersistence: true,
		SettingSources: []string{"user"}, MaxTurns: 1, MaxBudgetUSD: 0.20, IncludePartialMessages: true}
	t.Run("query", func(t *testing.T) {
		stream, err := Query(ctx, "Reply with exactly: BRIDGE_OK", options)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		var partials int
		for {
			message, err := stream.Next(ctx)
			if err != nil {
				t.Fatalf("read: %v; stderr: %s", err, stream.client.Stderr())
			}
			if message.Type == "stream_event" {
				partials++
			}
			if message.Type == "result" {
				break
			}
		}
		result, err := stream.Result(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(result.Result) != "BRIDGE_OK" {
			t.Fatalf("unexpected reply %q", result.Result)
		}
		if partials == 0 {
			t.Fatal("no partial streaming events")
		}
		t.Logf("session=%s, partial events=%d", result.SessionID, partials)
	})
	t.Run("persistent", func(t *testing.T) {
		client, err := Dial(ctx, options)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		var session string
		for i, prompt := range []string{"Remember the code word ORCHID. Reply only READY.", "What code word did I ask you to remember? Reply only that word."} {
			if err := client.Send(ctx, prompt); err != nil {
				t.Fatal(err)
			}
			for {
				message, err := client.Receive(ctx)
				if err != nil {
					t.Fatalf("read: %v; stderr: %s", err, client.Stderr())
				}
				if message.Type != "result" {
					continue
				}
				result, err := message.Result()
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					session = result.SessionID
				} else if result.SessionID != session {
					t.Fatal("session changed between turns")
				}
				want := "READY"
				if i == 1 {
					want = "ORCHID"
				}
				if strings.TrimSpace(result.Result) != want {
					t.Fatalf("reply=%q want=%q", result.Result, want)
				}
				break
			}
		}
		if err := client.SetPermissionMode(ctx, PermissionPlan); err != nil {
			t.Fatal(err)
		}
		if err := client.Interrupt(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
