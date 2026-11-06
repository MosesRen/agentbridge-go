package claude

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// runTracker distinguishes a completed turn from a completed one-shot run.
// It follows the upstream task ledger plus session-state protocol; perpetual
// shells/monitors are deliberately excluded from the agent task ledger.
type runTracker struct {
	mu            sync.Mutex
	done          chan struct{}
	finished      bool
	result        bool
	turn          bool
	state         string
	tasks         map[string]bool
	timer         *time.Timer
	ceiling       time.Duration
	bidirectional bool
	pending       []string
	generation    uint64
}

func newRunTracker(o Options) *runTracker {
	return &runTracker{done: make(chan struct{}), tasks: map[string]bool{}, ceiling: o.RunEndTimeout,
		bidirectional: o.CanUseTool != nil || len(o.Hooks) > 0 || len(o.SDKServers) > 0 || o.OnElicitation != nil || o.OnUserDialog != nil || o.OnControlRequest != nil}
}

func (r *runTracker) hasBidirectional() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bidirectional
}
func (r *runTracker) requireBidirectional() { r.mu.Lock(); r.bidirectional = true; r.mu.Unlock() }

func (r *runTracker) finishLocked() {
	if !r.finished {
		r.finished = true
		if r.timer != nil {
			r.timer.Stop()
		}
		close(r.done)
	}
}
func (r *runTracker) close() { r.mu.Lock(); defer r.mu.Unlock(); r.finishLocked() }
func (r *runTracker) stopTimer() {
	r.generation++
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
}
func (r *runTracker) armTimer() {
	r.stopTimer()
	if r.ceiling <= 0 || r.finished || r.turn || len(r.tasks) > 0 {
		return
	}
	generation := r.generation
	r.timer = time.AfterFunc(r.ceiling, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if generation == r.generation && !r.turn && len(r.tasks) == 0 && r.state != "requires_action" && len(r.pending) == 0 {
			r.finishLocked()
		}
	})
}

func (r *runTracker) sent(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		r.finished = false
		r.done = make(chan struct{})
	}
	r.result = false
	r.pending = append(r.pending, id)
	r.stopTimer()
}
func (r *runTracker) unsent(id string) { r.mu.Lock(); defer r.mu.Unlock(); r.removePending(id) }
func (r *runTracker) removePending(id string) {
	for i, old := range r.pending {
		if old == id {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			return
		}
	}
}
func (r *runTracker) wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		done := r.done
		finished := r.finished
		r.mu.Unlock()
		if finished {
			return nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *runTracker) observe(message Message) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	var frame struct {
		State            string   `json:"state"`
		SDKHostOnly      bool     `json:"sdk_host_only"`
		TaskID           string   `json:"task_id"`
		TaskType         string   `json:"task_type"`
		UserMessageUUID  string   `json:"user_message_uuid"`
		UserMessageUUIDs []string `json:"user_message_uuids"`
		Patch            struct {
			Status string `json:"status"`
		} `json:"patch"`
	}
	_ = json.Unmarshal(message.Raw, &frame)
	if message.Type == "system" {
		switch message.Subtype {
		case "task_started":
			if frame.TaskType == "local_agent" || frame.TaskType == "local_workflow" {
				r.tasks[frame.TaskID] = true
				r.stopTimer()
			}
		case "task_notification":
			delete(r.tasks, frame.TaskID)
		case "task_updated":
			switch frame.Patch.Status {
			case "completed", "failed", "stopped", "killed":
				delete(r.tasks, frame.TaskID)
			}
		case "session_state_changed":
			r.state = frame.State
			if frame.State == "requires_action" {
				r.stopTimer()
			} else if frame.State == "idle" && r.result && len(r.tasks) == 0 && len(r.pending) == 0 {
				r.finishLocked()
			} else if r.result {
				r.armTimer()
			}
			return !frame.SDKHostOnly
		}
	}
	if message.Type == "result" {
		if len(frame.UserMessageUUIDs) > 0 {
			for _, id := range frame.UserMessageUUIDs {
				r.removePending(id)
			}
		} else if frame.UserMessageUUID != "" {
			r.removePending(frame.UserMessageUUID)
		} else if len(r.pending) > 0 {
			r.pending = r.pending[1:]
		}
		r.result = true
		r.turn = false
		if len(r.pending) == 0 && len(r.tasks) == 0 && (r.state == "" || r.state == "idle" || !r.bidirectional) {
			r.finishLocked()
		} else if r.state != "requires_action" {
			r.armTimer()
		}
	} else if (message.Type == "assistant" || message.Type == "stream_event") && message.ParentToolUseID == nil {
		r.turn = true
		r.stopTimer()
	}
	return true
}
