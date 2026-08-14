package docker

import "fmt"

// Error wraps a Docker Engine API failure.
type Error struct {
	Op          string
	StatusCode  int
	Unavailable bool
	Err         error
}

func (e *Error) Error() string {
	if e.Unavailable {
		return fmt.Sprintf("docker: runtime unavailable during %s: %v", e.Op, e.Err)
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("docker: %s failed (status %d): %v", e.Op, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("docker: %s failed: %v", e.Op, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }
