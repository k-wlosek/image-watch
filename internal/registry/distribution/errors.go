package distribution

import "fmt"

// ErrorClass distinguishes categories of registry failure.
type ErrorClass string

const (
	ErrClassNetwork              ErrorClass = "network_failure"
	ErrClassAuthentication       ErrorClass = "authentication_failure"
	ErrClassAuthorization        ErrorClass = "authorization_failure"
	ErrClassRateLimit            ErrorClass = "rate_limit"
	ErrClassRepoNotFound         ErrorClass = "repository_not_found"
	ErrClassManifestNotFound     ErrorClass = "manifest_not_found"
	ErrClassUnsupportedMediaType ErrorClass = "unsupported_media_type"
	ErrClassInvalidReference     ErrorClass = "invalid_image_reference"
	ErrClassRegistry             ErrorClass = "registry_error" // catch-all for unexpected non-2xx
)

// Error is a classified registry-layer error.
type Error struct {
	Class      ErrorClass
	Repository string
	Message    string
	StatusCode int
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("registry: %s: %s (repository=%s, status=%d)", e.Class, e.Message, e.Repository, e.StatusCode)
	}
	return fmt.Sprintf("registry: %s: %s (repository=%s)", e.Class, e.Message, e.Repository)
}

func (e *Error) Unwrap() error { return e.Err }

// IsTransient reports whether the error class is worth retrying.
func (e *Error) IsTransient() bool {
	switch e.Class {
	case ErrClassNetwork, ErrClassAuthentication, ErrClassAuthorization, ErrClassRateLimit, ErrClassRegistry:
		return true
	default:
		return false
	}
}

func newError(class ErrorClass, repository, message string, statusCode int, err error) *Error {
	return &Error{Class: class, Repository: repository, Message: message, StatusCode: statusCode, Err: err}
}

// classifyStatus maps an HTTP status code to an ErrorClass.
func classifyStatus(statusCode int) ErrorClass {
	switch statusCode {
	case 401:
		return ErrClassAuthentication
	case 403:
		return ErrClassAuthorization
	case 404:
		return ErrClassRepoNotFound
	case 429:
		return ErrClassRateLimit
	case 406, 415:
		return ErrClassUnsupportedMediaType
	default:
		return ErrClassRegistry
	}
}
