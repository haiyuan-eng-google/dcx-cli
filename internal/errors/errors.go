// Package errors provides structured error handling for dcx.
//
// All errors are emitted as JSON envelopes on stderr with typed error codes
// and semantic exit codes. This ensures machine-safe error handling for
// agents, scripts, and CI pipelines.
package errors

import (
	"encoding/json"
	"fmt"
	"os"
)

// ErrorCode identifies the category of error.
type ErrorCode string

const (
	MissingArgument  ErrorCode = "MISSING_ARGUMENT"
	InvalidIdentifier ErrorCode = "INVALID_IDENTIFIER"
	InvalidConfig    ErrorCode = "INVALID_CONFIG"
	UnknownCommand   ErrorCode = "UNKNOWN_COMMAND"
	AuthError        ErrorCode = "AUTH_ERROR"
	APIError         ErrorCode = "API_ERROR"
	NotFound         ErrorCode = "NOT_FOUND"
	Conflict         ErrorCode = "CONFLICT"
	EvalFailed       ErrorCode = "EVAL_FAILED"
	InfraError       ErrorCode = "INFRA_ERROR"
	Internal         ErrorCode = "INTERNAL"
)

// ExitCode maps error categories to process exit codes.
// These must match the Rust implementation exactly.
const (
	ExitSuccess    = 0 // success
	ExitValidation = 1 // validation / eval failure
	ExitInfra      = 2 // infrastructure / API error (500, 502, 503, 504)
	ExitAuth       = 3 // auth error (401, 403)
	ExitNotFound   = 4 // not found (404)
	ExitConflict   = 5 // conflict (409)
)

// ExitCodeFor returns the semantic exit code for an error code.
func ExitCodeFor(code ErrorCode) int {
	switch code {
	case MissingArgument, InvalidIdentifier, InvalidConfig, UnknownCommand, EvalFailed:
		return ExitValidation
	case APIError, InfraError, Internal:
		return ExitInfra
	case AuthError:
		return ExitAuth
	case NotFound:
		return ExitNotFound
	case Conflict:
		return ExitConflict
	default:
		return ExitInfra
	}
}

// RetryableFor returns whether an error code's underlying condition is
// typically retryable.
func RetryableFor(code ErrorCode) bool {
	switch code {
	case APIError, InfraError:
		return true
	default:
		return false
	}
}

// ErrorDetail is the inner payload of an error envelope.
type ErrorDetail struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Hint      string    `json:"hint,omitempty"`
	ExitCode  int       `json:"exit_code"`
	Retryable bool      `json:"retryable"`
	Status    string    `json:"status"`
}

// ErrorEnvelope is the top-level JSON written to stderr.
type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

// Emit writes a structured error envelope to stderr and exits.
func Emit(code ErrorCode, message, hint string) {
	exitCode := ExitCodeFor(code)
	envelope := ErrorEnvelope{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			Hint:      hint,
			ExitCode:  exitCode,
			Retryable: RetryableFor(code),
			Status:    "error",
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		// Last-resort fallback if JSON marshal fails.
		fmt.Fprintf(os.Stderr, `{"error":{"code":"INTERNAL","message":"failed to marshal error","exit_code":2,"retryable":false,"status":"error"}}`)
		os.Exit(ExitInfra)
	}
	fmt.Fprintln(os.Stderr, string(data))
	os.Exit(exitCode)
}

// EmitWithExit writes a structured error envelope to stderr and exits
// with the given exit code, overriding the default for the error code.
func EmitWithExit(code ErrorCode, message, hint string, exitCode int) {
	envelope := ErrorEnvelope{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			Hint:      hint,
			ExitCode:  exitCode,
			Retryable: RetryableFor(code),
			Status:    "error",
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error":{"code":"INTERNAL","message":"failed to marshal error","exit_code":2,"retryable":false,"status":"error"}}`)
		os.Exit(ExitInfra)
	}
	fmt.Fprintln(os.Stderr, string(data))
	os.Exit(exitCode)
}

// New creates an ErrorEnvelope without emitting it. Useful for building
// errors that will be rendered by the caller (e.g., in tests or when
// the caller controls exit behavior).
func New(code ErrorCode, message, hint string) ErrorEnvelope {
	return ErrorEnvelope{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			Hint:      hint,
			ExitCode:  ExitCodeFor(code),
			Retryable: RetryableFor(code),
			Status:    "error",
		},
	}
}

// Write writes the envelope as JSON to stderr without exiting.
func (e ErrorEnvelope) Write() {
	data, err := json.Marshal(e)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error":{"code":"INTERNAL","message":"failed to marshal error","exit_code":2,"retryable":false,"status":"error"}}`)
		return
	}
	fmt.Fprintln(os.Stderr, string(data))
}

// ExitCodeFromHTTP maps an HTTP status code to a semantic exit code.
func ExitCodeFromHTTP(status int) int {
	switch {
	case status == 401 || status == 403:
		return ExitAuth
	case status == 404:
		return ExitNotFound
	case status == 409:
		return ExitConflict
	case status >= 500:
		return ExitInfra
	default:
		return ExitInfra
	}
}

// ErrorCodeFromHTTP maps an HTTP status code to an error code.
func ErrorCodeFromHTTP(status int) ErrorCode {
	switch {
	case status == 401 || status == 403:
		return AuthError
	case status == 404:
		return NotFound
	case status == 409:
		return Conflict
	case status >= 500:
		return InfraError
	default:
		return APIError
	}
}
