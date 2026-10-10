package wip

import "strings"

// Error returns a diagnostic suitable for display. Kind remains the stable API.
func (e *Error) Error() string {
	parts := []string{"wip"}
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	message := e.Message
	if message == "" {
		message = string(e.Kind)
	}
	parts = append(parts, message)
	if e.Stderr != "" {
		parts = append(parts, strings.TrimSpace(e.Stderr))
	} else if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

// Unwrap retains process failures and context cancellation for errors.Is/As.
func (e *Error) Unwrap() error { return e.Err }
