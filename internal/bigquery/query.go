// Package bigquery provides the static BigQuery commands that cannot
// be generated from the Discovery Document.
package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
)

const bigqueryJobsURL = "https://bigquery.googleapis.com/bigquery/v2/projects/%s/queries"

// QueryRequest is the BigQuery jobs.query request body.
type QueryRequest struct {
	Query        string `json:"query"`
	UseLegacySQL bool   `json:"useLegacySql"`
	DryRun       bool   `json:"dryRun,omitempty"`
	MaxResults   int    `json:"maxResults,omitempty"`
	Location     string `json:"location,omitempty"`
}

// QueryResponse represents the BigQuery query response, used for
// rendering structured output.
type QueryResponse struct {
	Kind         string        `json:"kind,omitempty"`
	Schema       *TableSchema  `json:"schema,omitempty"`
	Rows         []TableRow    `json:"rows,omitempty"`
	TotalRows    string        `json:"totalRows,omitempty"`
	JobComplete  bool          `json:"jobComplete"`
	JobReference *JobReference `json:"jobReference,omitempty"`
	CacheHit     bool          `json:"cacheHit,omitempty"`
	// Dry-run specific fields.
	TotalBytesProcessed string           `json:"totalBytesProcessed,omitempty"`
	Statistics          *QueryStatistics `json:"statistics,omitempty"`
}

// TableSchema describes the schema of a BigQuery table.
type TableSchema struct {
	Fields []SchemaField `json:"fields"`
}

// SchemaField describes a single field in a BigQuery schema.
type SchemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode,omitempty"`
}

// TableRow is a single row in a BigQuery result set.
type TableRow struct {
	F []TableCell `json:"f"`
}

// TableCell is a single cell value.
type TableCell struct {
	V interface{} `json:"v"`
}

// JobReference identifies a BigQuery job.
type JobReference struct {
	ProjectID string `json:"projectId"`
	JobID     string `json:"jobId"`
	Location  string `json:"location"`
}

// QueryStatistics holds dry-run cost estimates.
type QueryStatistics struct {
	TotalBytesProcessed string `json:"totalBytesProcessed,omitempty"`
}

// ExecuteQuery runs a BigQuery query and renders the result.
func ExecuteQuery(
	ctx context.Context,
	authCfg auth.Config,
	projectID, query, location string,
	dryRun bool,
	maxResults int,
	format output.Format,
	outputFields string,
) error {
	if projectID == "" {
		dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --project-id is missing", "")
		return nil
	}
	if query == "" {
		dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --query is missing", "")
		return nil
	}

	// Resolve auth.
	resolved, err := auth.Resolve(ctx, authCfg)
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "Run 'dcx auth check' to verify credentials")
		return nil
	}

	tok, err := resolved.TokenSource.Token()
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
		return nil
	}

	// Build request.
	reqBody := QueryRequest{
		Query:        query,
		UseLegacySQL: false,
		DryRun:       dryRun,
		MaxResults:   maxResults,
		Location:     location,
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		dcxerrors.Emit(dcxerrors.Internal, fmt.Sprintf("marshaling request: %v", err), "")
		return nil
	}

	url := fmt.Sprintf(bigqueryJobsURL, projectID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		dcxerrors.Emit(dcxerrors.Internal, fmt.Sprintf("creating request: %v", err), "")
		return nil
	}

	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Execute request.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		dcxerrors.Emit(dcxerrors.InfraError, fmt.Sprintf("API request failed: %v", err), "Check network connectivity")
		return nil
	}
	defer resp.Body.Close()

	// Handle errors.
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		message := fmt.Sprintf("BigQuery API returned HTTP %d", resp.StatusCode)
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
			message = apiErr.Error.Message
		}
		code := dcxerrors.ErrorCodeFromHTTP(resp.StatusCode)
		dcxerrors.Emit(code, message, "")
		return nil
	}

	// Parse response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		dcxerrors.Emit(dcxerrors.InfraError, fmt.Sprintf("reading response: %v", err), "")
		return nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		dcxerrors.Emit(dcxerrors.Internal, fmt.Sprintf("parsing response: %v", err), "")
		return nil
	}

	return output.RenderFiltered(format, raw, outputFields)
}
