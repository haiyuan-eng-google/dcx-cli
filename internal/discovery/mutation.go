package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
)

// LoadBody reads and validates a JSON request body from a --body flag value.
// Accepts either a raw JSON string or @filepath to read from a file.
// Returns the parsed JSON as bytes, or an error if invalid.
func LoadBody(bodyFlag string) ([]byte, error) {
	if bodyFlag == "" {
		return nil, nil
	}

	var raw []byte
	var err error

	if strings.HasPrefix(bodyFlag, "@") {
		path := bodyFlag[1:]
		raw, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading body file %s: %w", path, err)
		}
	} else {
		raw = []byte(bodyFlag)
	}

	// Validate JSON before sending.
	if !json.Valid(raw) {
		return nil, fmt.Errorf("--body is not valid JSON")
	}

	return raw, nil
}

// ConfirmDelete checks whether a DELETE operation should proceed.
// Returns nil if confirmed, error if denied or non-interactive.
func ConfirmDelete(cmd GeneratedCommand, force bool) error {
	if force {
		return nil
	}

	// Non-TTY stdin: fail closed with structured error.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("DELETE requires --force when stdin is not a terminal")
	}

	// Interactive confirmation.
	fmt.Fprintf(os.Stderr, "This will delete %s %s. Continue? [y/N] ", cmd.Method.Resource, cmd.CommandPath)
	var response string
	fmt.Scanln(&response)
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		return fmt.Errorf("aborted by user")
	}

	return nil
}

// DryRunResult is the structured output of a --dry-run mutation.
type DryRunResult struct {
	Method               string      `json:"method"`
	URL                  string      `json:"url"`
	Body                 interface{} `json:"body,omitempty"`
	BodySchema           string      `json:"body_schema,omitempty"`
	ConfirmationRequired bool        `json:"confirmation_required"`
}

// RenderDryRun outputs the dry-run result showing what would be sent.
func RenderDryRun(format output.Format, cmd GeneratedCommand, resolvedURL string, bodyBytes []byte) error {
	result := DryRunResult{
		Method:               cmd.Method.HTTPMethod,
		URL:                  resolvedURL,
		ConfirmationRequired: cmd.Method.RequiresConfirmation(),
	}

	if cmd.Method.AcceptsBody() {
		result.BodySchema = cmd.Method.RequestRef
	}

	if len(bodyBytes) > 0 {
		var parsed interface{}
		json.Unmarshal(bodyBytes, &parsed)
		result.Body = parsed
	}

	return output.Render(format, result)
}

// ExecuteMutation runs a mutation command with body, confirmation, and dry-run support.
func (e *Executor) ExecuteMutation(
	cmd GeneratedCommand,
	pathParams map[string]string,
	queryParams map[string]string,
	token string,
	bodyBytes []byte,
	format output.Format,
) error {
	var body io.Reader
	if len(bodyBytes) > 0 {
		body = bytes.NewReader(bodyBytes)
	}

	req, err := BuildRequest(cmd, pathParams, queryParams, token, body)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return handleErrorResponse(resp)
	}

	// Parse and render response.
	respBody, err := readResponseBody(resp)
	if err != nil {
		return err
	}

	if len(respBody) == 0 || string(respBody) == "" {
		// Some DELETE methods return empty bodies.
		return output.Render(format, map[string]interface{}{
			"status": "success",
			"method": cmd.Method.HTTPMethod,
		})
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		// Response might not be JSON (some DELETEs return 204 with no body).
		return output.Render(format, map[string]interface{}{
			"status": "success",
			"method": cmd.Method.HTTPMethod,
		})
	}

	return output.Render(format, raw)
}
