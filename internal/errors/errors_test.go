package errors

import (
	"encoding/json"
	"testing"
)

func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want int
	}{
		{MissingArgument, ExitValidation},
		{InvalidIdentifier, ExitValidation},
		{InvalidConfig, ExitValidation},
		{UnknownCommand, ExitValidation},
		{EvalFailed, ExitValidation},
		{APIError, ExitInfra},
		{InfraError, ExitInfra},
		{Internal, ExitInfra},
		{AuthError, ExitAuth},
		{NotFound, ExitNotFound},
		{Conflict, ExitConflict},
	}
	for _, tt := range tests {
		if got := ExitCodeFor(tt.code); got != tt.want {
			t.Errorf("ExitCodeFor(%s) = %d, want %d", tt.code, got, tt.want)
		}
	}
}

func TestRetryableFor(t *testing.T) {
	retryable := []ErrorCode{APIError, InfraError}
	notRetryable := []ErrorCode{MissingArgument, AuthError, NotFound, Conflict, Internal}

	for _, code := range retryable {
		if !RetryableFor(code) {
			t.Errorf("RetryableFor(%s) = false, want true", code)
		}
	}
	for _, code := range notRetryable {
		if RetryableFor(code) {
			t.Errorf("RetryableFor(%s) = true, want false", code)
		}
	}
}

func TestNew(t *testing.T) {
	env := New(NotFound, "Dataset not found: x", "Check dataset ID")

	if env.Error.Code != NotFound {
		t.Errorf("Code = %s, want %s", env.Error.Code, NotFound)
	}
	if env.Error.Message != "Dataset not found: x" {
		t.Errorf("Message = %s, want 'Dataset not found: x'", env.Error.Message)
	}
	if env.Error.Hint != "Check dataset ID" {
		t.Errorf("Hint = %s, want 'Check dataset ID'", env.Error.Hint)
	}
	if env.Error.ExitCode != ExitNotFound {
		t.Errorf("ExitCode = %d, want %d", env.Error.ExitCode, ExitNotFound)
	}
	if env.Error.Retryable {
		t.Error("Retryable = true, want false for NOT_FOUND")
	}
	if env.Error.Status != "error" {
		t.Errorf("Status = %s, want 'error'", env.Error.Status)
	}
}

func TestEnvelopeJSONShape(t *testing.T) {
	env := New(APIError, "Bad gateway", "Retry in 30s")
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Unmarshal into a generic map to verify JSON keys.
	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	errObj, ok := raw["error"]
	if !ok {
		t.Fatal("missing top-level 'error' key")
	}

	requiredKeys := []string{"code", "message", "hint", "exit_code", "retryable", "status"}
	for _, key := range requiredKeys {
		if _, ok := errObj[key]; !ok {
			t.Errorf("missing key 'error.%s'", key)
		}
	}
}

func TestEnvelopeJSONOmitsEmptyHint(t *testing.T) {
	env := New(AuthError, "Unauthorized", "")
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if _, ok := raw["error"]["hint"]; ok {
		t.Error("expected 'hint' to be omitted when empty")
	}
}

func TestExitCodeFromHTTP(t *testing.T) {
	tests := []struct {
		status int
		want   int
	}{
		{401, ExitAuth},
		{403, ExitAuth},
		{404, ExitNotFound},
		{409, ExitConflict},
		{500, ExitInfra},
		{502, ExitInfra},
		{503, ExitInfra},
		{504, ExitInfra},
		{400, ExitInfra}, // default for unmapped client errors
	}
	for _, tt := range tests {
		if got := ExitCodeFromHTTP(tt.status); got != tt.want {
			t.Errorf("ExitCodeFromHTTP(%d) = %d, want %d", tt.status, got, tt.want)
		}
	}
}

func TestErrorCodeFromHTTP(t *testing.T) {
	tests := []struct {
		status int
		want   ErrorCode
	}{
		{401, AuthError},
		{403, AuthError},
		{404, NotFound},
		{409, Conflict},
		{500, InfraError},
		{502, InfraError},
		{400, APIError},
	}
	for _, tt := range tests {
		if got := ErrorCodeFromHTTP(tt.status); got != tt.want {
			t.Errorf("ErrorCodeFromHTTP(%d) = %s, want %s", tt.status, got, tt.want)
		}
	}
}
