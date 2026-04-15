package discovery

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
)

func TestLoadBody_RawJSON(t *testing.T) {
	body, err := LoadBody(`{"datasetReference":{"datasetId":"test"}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body == nil {
		t.Fatal("body should not be nil")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
}

func TestLoadBody_FileRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	os.WriteFile(path, []byte(`{"name":"test"}`), 0644)

	body, err := LoadBody("@" + path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if parsed["name"] != "test" {
		t.Errorf("name = %v, want test", parsed["name"])
	}
}

func TestLoadBody_InvalidJSON(t *testing.T) {
	_, err := LoadBody(`{not json}`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %v, want 'not valid JSON'", err)
	}
}

func TestLoadBody_MissingFile(t *testing.T) {
	_, err := LoadBody("@/nonexistent/file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadBody_Empty(t *testing.T) {
	body, err := LoadBody("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != nil {
		t.Error("empty flag should return nil body")
	}
}

func TestBuildRequest_POSTWithBody(t *testing.T) {
	cmd := GeneratedCommand{
		Method: ApiMethod{
			HTTPMethod: "POST",
			Path:       "projects/{projectId}/datasets",
			RequestRef: "Dataset",
		},
		Service: &ServiceConfig{
			BaseURL: "https://bigquery.googleapis.com/bigquery/v2/",
		},
	}

	bodyJSON := `{"datasetReference":{"datasetId":"test"}}`
	req, err := BuildRequest(cmd, map[string]string{"projectId": "my-proj"}, nil, "tok", bytes.NewReader([]byte(bodyJSON)))
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", req.Header.Get("Content-Type"))
	}
	reqBody, _ := io.ReadAll(req.Body)
	if string(reqBody) != bodyJSON {
		t.Errorf("body = %s, want %s", reqBody, bodyJSON)
	}
}

func TestBuildRequest_DELETENoBody(t *testing.T) {
	cmd := GeneratedCommand{
		Method: ApiMethod{
			HTTPMethod: "DELETE",
			Path:       "projects/{projectId}/datasets/{datasetId}",
		},
		Service: &ServiceConfig{
			BaseURL: "https://bigquery.googleapis.com/bigquery/v2/",
		},
	}

	req, err := BuildRequest(cmd, map[string]string{"projectId": "p", "datasetId": "d"}, nil, "tok", nil)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if req.Method != "DELETE" {
		t.Errorf("method = %s, want DELETE", req.Method)
	}
	if req.Header.Get("Content-Type") != "" {
		t.Errorf("Content-Type should be empty for DELETE, got %s", req.Header.Get("Content-Type"))
	}
}

func TestConfirmDelete_ForceBypass(t *testing.T) {
	cmd := GeneratedCommand{
		Method: ApiMethod{HTTPMethod: "DELETE", Resource: "datasets"},
	}
	err := ConfirmDelete(cmd, true)
	if err != nil {
		t.Errorf("--force should bypass confirmation, got: %v", err)
	}
}

func TestConfirmDelete_NonTTYFailsClosed(t *testing.T) {
	// This test runs in CI where stdin is not a TTY.
	// It should fail closed without blocking.
	cmd := GeneratedCommand{
		Method:      ApiMethod{HTTPMethod: "DELETE", Resource: "datasets"},
		CommandPath: "datasets delete",
	}

	// Only test non-TTY behavior — skip if running interactively.
	if isStdinTTY() {
		t.Skip("stdin is a TTY, skipping non-TTY test")
	}

	err := ConfirmDelete(cmd, false)
	if err == nil {
		t.Fatal("expected error for non-TTY without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

func isStdinTTY() bool {
	fi, _ := os.Stdin.Stat()
	return fi.Mode()&os.ModeCharDevice != 0
}

func TestDryRunResult_Structure(t *testing.T) {
	cmd := GeneratedCommand{
		Method: ApiMethod{
			HTTPMethod: "POST",
			RequestRef: "Dataset",
		},
	}

	result := DryRunResult{
		Method:               cmd.Method.HTTPMethod,
		URL:                  "https://example.com/datasets",
		BodySchema:           cmd.Method.RequestRef,
		ConfirmationRequired: cmd.Method.RequiresConfirmation(),
	}

	data, _ := json.Marshal(result)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["method"] != "POST" {
		t.Errorf("method = %v", parsed["method"])
	}
	if parsed["body_schema"] != "Dataset" {
		t.Errorf("body_schema = %v", parsed["body_schema"])
	}
	if parsed["confirmation_required"] != false {
		t.Error("POST should not require confirmation")
	}
}

func TestDryRunResult_DELETERequiresConfirmation(t *testing.T) {
	cmd := GeneratedCommand{
		Method: ApiMethod{HTTPMethod: "DELETE"},
	}
	result := DryRunResult{
		Method:               "DELETE",
		URL:                  "https://example.com/datasets/test",
		ConfirmationRequired: cmd.Method.RequiresConfirmation(),
	}
	if !result.ConfirmationRequired {
		t.Error("DELETE should require confirmation")
	}
}

func TestApiMethod_IsMutation(t *testing.T) {
	tests := []struct {
		method string
		want   bool
	}{
		{"GET", false},
		{"POST", true},
		{"DELETE", true},
		{"PATCH", true},
		{"PUT", true},
	}
	for _, tt := range tests {
		m := ApiMethod{HTTPMethod: tt.method}
		if got := m.IsMutation(); got != tt.want {
			t.Errorf("ApiMethod{%s}.IsMutation() = %v, want %v", tt.method, got, tt.want)
		}
	}
}

func TestApiMethod_AcceptsBody(t *testing.T) {
	if (ApiMethod{RequestRef: "Dataset"}).AcceptsBody() != true {
		t.Error("method with RequestRef should accept body")
	}
	if (ApiMethod{}).AcceptsBody() != false {
		t.Error("method without RequestRef should not accept body")
	}
}

func TestMutationContractSetIsMutation(t *testing.T) {
	// Simulate what register.go does for a POST method.
	c := contracts.BuildContract("datasets insert", "bigquery", "Insert dataset", nil, true, true)
	if !c.IsMutation {
		t.Error("mutation contract should have IsMutation=true")
	}
	if !c.SupportsDryRun {
		t.Error("mutation contract should have SupportsDryRun=true")
	}

	// Read-only contract.
	c2 := contracts.BuildContract("datasets list", "bigquery", "List datasets", nil, false, false)
	if c2.IsMutation {
		t.Error("read contract should have IsMutation=false")
	}
}

func TestMCPExcludesMutations(t *testing.T) {
	// Verify the MCP server filters mutations by checking that
	// the contract registry's IsMutation flag is correctly set
	// for mutation vs read commands.
	reg := contracts.NewRegistry()
	reg.Register(contracts.BuildContract("datasets list", "bigquery", "List", nil, false, false))
	reg.Register(contracts.BuildContract("datasets insert", "bigquery", "Insert", nil, true, true))

	all := reg.All()
	var readCount, mutCount int
	for _, c := range all {
		if c.IsMutation {
			mutCount++
		} else {
			readCount++
		}
	}

	if readCount != 1 {
		t.Errorf("expected 1 read command, got %d", readCount)
	}
	if mutCount != 1 {
		t.Errorf("expected 1 mutation command, got %d", mutCount)
	}

	// Simulate MCP filtering.
	var mcpTools int
	for _, c := range all {
		if !c.IsMutation {
			mcpTools++
		}
	}
	if mcpTools != 1 {
		t.Errorf("MCP should expose 1 tool (read-only), got %d", mcpTools)
	}
}

func TestResolveURL(t *testing.T) {
	cmd := GeneratedCommand{
		Method: ApiMethod{
			HTTPMethod: "DELETE",
			Path:       "projects/{projectId}/datasets/{datasetId}",
		},
		Service: &ServiceConfig{
			BaseURL: "https://bigquery.googleapis.com/bigquery/v2/",
		},
	}

	url, err := ResolveURL(cmd, map[string]string{"projectId": "p", "datasetId": "d"}, map[string]string{"deleteContents": "true"})
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if !strings.Contains(url, "projects/p/datasets/d") {
		t.Errorf("URL = %s, want path with projects/p/datasets/d", url)
	}
	if !strings.Contains(url, "deleteContents=true") {
		t.Errorf("URL = %s, want query param deleteContents=true", url)
	}
}

// roundTripFunc implements http.RoundTripper for testing.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExecuteMutation_POST(t *testing.T) {
	// Mock HTTP client that captures the request.
	var capturedReq *http.Request
	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"name":"test-dataset"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}

	executor := NewExecutor(mockClient)
	cmd := GeneratedCommand{
		Method: ApiMethod{
			HTTPMethod: "POST",
			Path:       "projects/{projectId}/datasets",
			RequestRef: "Dataset",
		},
		Service: &ServiceConfig{
			BaseURL: "https://bigquery.googleapis.com/bigquery/v2/",
		},
	}

	bodyJSON := []byte(`{"datasetReference":{"datasetId":"test"}}`)
	err := executor.ExecuteMutation(cmd, map[string]string{"projectId": "p"}, nil, "tok", bodyJSON, "json")
	if err != nil {
		t.Fatalf("ExecuteMutation: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("request was not sent")
	}
	if capturedReq.Method != "POST" {
		t.Errorf("method = %s, want POST", capturedReq.Method)
	}
	if capturedReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %s", capturedReq.Header.Get("Content-Type"))
	}
	reqBody, _ := io.ReadAll(capturedReq.Body)
	if string(reqBody) != string(bodyJSON) {
		t.Errorf("body = %s", reqBody)
	}
}

func TestExecuteMutation_DELETEEmptyResponse(t *testing.T) {
	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 204,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     http.Header{},
			}, nil
		}),
	}

	executor := NewExecutor(mockClient)
	cmd := GeneratedCommand{
		Method: ApiMethod{
			HTTPMethod: "DELETE",
			Path:       "projects/{projectId}/datasets/{datasetId}",
		},
		Service: &ServiceConfig{
			BaseURL: "https://bigquery.googleapis.com/bigquery/v2/",
		},
	}

	err := executor.ExecuteMutation(cmd, map[string]string{"projectId": "p", "datasetId": "d"}, nil, "tok", nil, "json")
	if err != nil {
		t.Fatalf("ExecuteMutation DELETE: %v", err)
	}
}
