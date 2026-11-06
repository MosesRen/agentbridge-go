//go:build integration && !windows

package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type protocolCapture struct {
	Args       []string                   `json:"args"`
	Initialize map[string]json.RawMessage `json:"initialize"`
	User       json.RawMessage            `json:"user"`
}

func normalizeArgs(args []string) map[string]string {
	result := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		key, value, equals := strings.Cut(arg, "=")
		if !equals && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++
			value = args[i]
		}
		result[key] = value
	}
	return result
}

func TestUpstreamOptionsParity(t *testing.T) {
	sdk := os.Getenv("AGENTBRIDGE_UPSTREAM_SDK")
	if sdk == "" {
		t.Skip("set AGENTBRIDGE_UPSTREAM_SDK to the pinned official sdk.mjs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture, err := filepath.Abs("testdata/capture-cli.mjs")
	if err != nil {
		t.Fatal(err)
	}
	// Use a private wrapper so the repository fixture needs no executable bit.
	wrapper := filepath.Join(t.TempDir(), "fake-claude")
	if err = os.WriteFile(wrapper, []byte("#!/bin/sh\nexec node '"+strings.ReplaceAll(fixture, "'", "'\"'\"'")+"' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"defaults", "configured"} {
		t.Run(scenario, func(t *testing.T) {
			goFile, tsFile := filepath.Join(t.TempDir(), "go.json"), filepath.Join(t.TempDir(), "ts.json")
			goOptions := Options{CLIPath: wrapper, Env: map[string]string{"AGENTBRIDGE_CAPTURE_FILE": goFile}, CanUseTool: func(context.Context, PermissionRequest) (PermissionDecision, error) {
				return PermissionDecision{Behavior: "deny"}, nil
			}}
			tsOptions := map[string]any{}
			if scenario == "configured" {
				prompt := "custom fixture prompt"
				goOptions.SystemPrompt = &prompt
				goOptions.Model = "fixture-model"
				goOptions.MaxTurns = 3
				goOptions.MaxBudgetUSD = .2
				goOptions.Tools = []string{"Read"}
				goOptions.DisallowedTools = []string{"Bash"}
				goOptions.PermissionMode = PermissionPlan
				goOptions.SettingSources = []string{"user"}
				goOptions.Skills = []string{"review"}
				goOptions.Effort = "high"
				goOptions.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 2048}
				goOptions.Agents = map[string]AgentDefinition{"reviewer": {Description: "Review", Prompt: "Review code"}}
				goOptions.JSONSchema = json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`)
				tsOptions = map[string]any{"systemPrompt": prompt, "model": "fixture-model", "maxTurns": 3, "maxBudgetUsd": .2, "tools": []string{"Read"}, "disallowedTools": []string{"Bash"}, "permissionMode": "plan", "settingSources": []string{"user"}, "skills": []string{"review"}, "effort": "high", "thinking": map[string]any{"type": "enabled", "budgetTokens": 2048}, "agents": map[string]any{"reviewer": map[string]any{"description": "Review", "prompt": "Review code"}}, "outputFormat": map[string]any{"type": "json_schema", "schema": json.RawMessage(goOptions.JSONSchema)}}
			}
			if _, err := Run(ctx, "fixture prompt", goOptions); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(tsOptions)
			cmd := exec.CommandContext(ctx, "node", "testdata/capture-upstream.mjs")
			cmd.Env = append(os.Environ(), "AGENTBRIDGE_UPSTREAM_SDK="+sdk, "AGENTBRIDGE_CAPTURE_FILE="+tsFile, "AGENTBRIDGE_UPSTREAM_OPTIONS="+string(data))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("official SDK fixture failed: %v %s", err, output)
			}
			load := func(path string) protocolCapture {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var capture protocolCapture
				if err = json.Unmarshal(data, &capture); err != nil {
					t.Fatal(err)
				}
				return capture
			}
			goCapture, tsCapture := load(goFile), load(tsFile)
			goArgs, tsArgs := normalizeArgs(goCapture.Args), normalizeArgs(tsCapture.Args)
			for _, flag := range []string{"--system-prompt", "--model", "--max-turns", "--max-budget-usd", "--tools", "--disallowedTools", "--permission-mode", "--setting-sources", "--allowedTools", "--effort", "--max-thinking-tokens", "--json-schema"} {
				goValue, goOK := goArgs[flag]
				tsValue, tsOK := tsArgs[flag]
				if flag == "--json-schema" && goOK && tsOK {
					var a, b any
					_ = json.Unmarshal([]byte(goValue), &a)
					_ = json.Unmarshal([]byte(tsValue), &b)
					if !reflect.DeepEqual(a, b) {
						t.Errorf("schema differs: %s vs %s", goValue, tsValue)
					}
					continue
				}
				if goOK != tsOK || goValue != tsValue {
					t.Errorf("%s: Go (%t,%q), upstream (%t,%q)", flag, goOK, goValue, tsOK, tsValue)
				}
			}
			for _, field := range []string{"agents", "skills", "systemPrompt", "appendSystemPrompt"} {
				var a, b any
				_ = json.Unmarshal(goCapture.Initialize[field], &a)
				_ = json.Unmarshal(tsCapture.Initialize[field], &b)
				if !reflect.DeepEqual(a, b) {
					t.Errorf("initialize.%s differs: Go=%v upstream=%v", field, a, b)
				}
			}
		})
	}
}
