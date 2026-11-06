//go:build unix

package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestUnixProtocolProcess(t *testing.T) {
	mode := os.Getenv("AGENTBRIDGE_UNIX_CHILD")
	if mode == "" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	if mode == "descendant" {
		// Preserve an inherited stdout descriptor and ignore graceful termination.
		for {
			time.Sleep(time.Hour)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &frame)
		if frame.Type == "control_request" {
			data, _ := json.Marshal(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame.RequestID, "response": map[string]any{}}})
			fmt.Println(string(data))
		}
		if frame.Type == "user" {
			child := exec.Command(os.Args[0], "-test.run=^TestUnixProtocolProcess$")
			child.Env = append(os.Environ(), "AGENTBRIDGE_UNIX_CHILD=descendant")
			child.Stdout = os.Stdout
			if err := child.Start(); err != nil {
				os.Exit(3)
			}
			data, _ := json.Marshal(map[string]any{"type": "descendant", "pid": child.Process.Pid})
			fmt.Println(string(data))
		}
	}
	os.Exit(0)
}

func TestCloseKillsProcessGroup(t *testing.T) {
	options := fakeOptions(t, "unused")
	options.commandPrefix = []string{"-test.run=^TestUnixProtocolProcess$", "--"}
	options.Env["AGENTBRIDGE_UNIX_CHILD"] = "parent"
	client := dialFake(t, options)
	if err := client.Send(context.Background(), "spawn"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	message, err := client.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		PID int `json:"pid"`
	}
	if err = json.Unmarshal(message.Raw, &info); err != nil || info.PID <= 0 {
		t.Fatalf("invalid child info: %s", message.Raw)
	}
	defer syscall.Kill(info.PID, syscall.SIGKILL)
	start := time.Now()
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Close did not enforce its termination deadline")
	}
	if client.cmd.ProcessState == nil {
		t.Fatal("parent was not reaped")
	}
	// A killed descendant may remain a zombie until reaped by init. Verify that
	// it no longer owns its inherited descriptor by the closed stream/reader.
	select {
	case <-client.readerDone:
	default:
		t.Fatal("stdout reader leaked")
	}
	for attempt := 0; attempt < 50; attempt++ {
		err = syscall.Kill(info.PID, 0)
		if err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// On Linux containers PID 1 may not reap orphans. A zombie is dead and has no
	// resources other than its process-table entry; inspect its state there.
	data, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", info.PID))
	if readErr == nil {
		var pid int
		var name, state string
		_, _ = fmt.Sscanf(string(data), "%d %s %s", &pid, &name, &state)
		if state == "Z" {
			return
		}
	}
	t.Fatalf("descendant %d still exists after process group cleanup", info.PID)
}
