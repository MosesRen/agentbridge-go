package claude

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrClosed          = errors.New("claude: client closed")
	ErrInputClosed     = errors.New("claude: input stream closed")
	ErrEventBufferFull = errors.New("claude: event buffer full; consume messages promptly or increase EventBuffer")
	ErrMissingResult   = errors.New("claude: stream ended without a result")
)

type ResultError struct{ Result *Result }

func (e *ResultError) Error() string {
	detail := strings.Join(e.Result.Errors, "; ")
	if detail == "" {
		detail = e.Result.Result
	}
	if detail == "" {
		detail = e.Result.Subtype
	}
	return "claude: agent failed: " + detail
}

// ProcessError retains the exit status and a bounded stderr tail. Stderr is
// available explicitly and omitted from Error(), since it may contain secrets.
type ProcessError struct {
	ExitCode int
	Stderr   string
	Err      error
}

func (e *ProcessError) Error() string {
	return fmt.Sprintf("claude: process exited with code %d", e.ExitCode)
}
func (e *ProcessError) Unwrap() error { return e.Err }

type ControlError struct{ RequestID, Message string }

func (e *ControlError) Error() string { return "claude: control request failed: " + e.Message }
