package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/spf13/cobra"
)

const spannerDDLEndpoint = "https://spanner.googleapis.com/v1/projects/%s/instances/%s/databases/%s/ddl"

func (a *App) registerSpannerUpdateDdlCommand() {
	// Find or create the spanner -> databases group.
	var spannerCmd, dbsCmd *cobra.Command
	for _, child := range a.Root.Commands() {
		if child.Name() == "spanner" {
			spannerCmd = child
			break
		}
	}
	if spannerCmd == nil {
		return // Spanner not registered yet
	}
	for _, child := range spannerCmd.Commands() {
		if child.Name() == "databases" {
			dbsCmd = child
			break
		}
	}
	if dbsCmd == nil {
		return // databases group not registered yet
	}

	var ddlStatements []string
	var ddlFile string
	var instanceID, databaseID string
	var operationID, protoDescriptors string

	cmd := &cobra.Command{
		Use:   "update-ddl",
		Short: "Apply DDL statements to a Spanner database",
		Long: `Apply one or more DDL statements (CREATE TABLE, ALTER TABLE, etc.)
to a Cloud Spanner database. Returns a long-running operation.

Examples:
  # Single statement
  dcx spanner databases update-ddl --instance-id=myinst --database-id=mydb \
    --ddl "CREATE TABLE users (id INT64, name STRING(100)) PRIMARY KEY (id)"

  # Multiple statements
  dcx spanner databases update-ddl --instance-id=myinst --database-id=mydb \
    --ddl "CREATE TABLE t1 (id INT64) PRIMARY KEY (id)" \
    --ddl "CREATE INDEX idx ON t1 (id)"

  # From file (semicolon-delimited)
  dcx spanner databases update-ddl --instance-id=myinst --database-id=mydb \
    --ddl-file schema.sql`,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
				return nil
			}

			projectID := a.Opts.ProjectID
			if projectID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --project-id is missing", "")
				return nil
			}
			if instanceID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --instance-id is missing", "")
				return nil
			}
			if databaseID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --database-id is missing", "")
				return nil
			}

			// Collect DDL statements from flags and/or file.
			statements, err := collectDDLStatements(ddlStatements, ddlFile)
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
				return nil
			}
			if len(statements) == 0 {
				dcxerrors.Emit(dcxerrors.MissingArgument, "at least one DDL statement is required", "Use --ddl or --ddl-file")
				return nil
			}

			apiURL := fmt.Sprintf(spannerDDLEndpoint, projectID, instanceID, databaseID)
			body := map[string]interface{}{
				"statements": statements,
			}
			if operationID != "" {
				body["operationId"] = operationID
			}
			if protoDescriptors != "" {
				body["protoDescriptors"] = protoDescriptors
			}
			bodyJSON, _ := json.Marshal(body)

			// Dry-run: show what would be sent.
			if a.Opts.DryRun {
				return a.Render(format, map[string]interface{}{
					"method":                "PATCH",
					"url":                   apiURL,
					"body":                  body,
					"body_schema":           "UpdateDatabaseDdlRequest",
					"statement_count":       len(statements),
					"confirmation_required": false,
				})
			}

			// Resolve auth.
			ctx := context.Background()
			resolved, err := auth.Resolve(ctx, a.AuthConfig())
			if err != nil {
				dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "")
				return nil
			}
			tok, err := resolved.TokenSource.Token()
			if err != nil {
				dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
				return nil
			}

			// Execute request.
			req, err := http.NewRequestWithContext(ctx, "PATCH", apiURL, bytes.NewReader(bodyJSON))
			if err != nil {
				dcxerrors.Emit(dcxerrors.Internal, err.Error(), "")
				return nil
			}
			req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				dcxerrors.Emit(dcxerrors.InfraError, fmt.Sprintf("API request failed: %v", err), "")
				return nil
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)

			if resp.StatusCode >= 400 {
				var apiErr struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				message := fmt.Sprintf("API returned HTTP %d", resp.StatusCode)
				if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
					message = apiErr.Error.Message
				}
				code := dcxerrors.ErrorCodeFromHTTP(resp.StatusCode)
				dcxerrors.Emit(code, message, "")
				return nil
			}

			var raw map[string]interface{}
			if err := json.Unmarshal(respBody, &raw); err != nil {
				dcxerrors.Emit(dcxerrors.Internal, fmt.Sprintf("parsing response: %v", err), "")
				return nil
			}

			return a.Render(format, raw)
		},
	}

	cmd.Flags().StringArrayVar(&ddlStatements, "ddl", nil, "DDL statement (can be repeated)")
	cmd.Flags().StringVar(&ddlFile, "ddl-file", "", "Path to file with semicolon-delimited DDL statements")
	cmd.Flags().StringVar(&instanceID, "instance-id", "", "Spanner instance ID (required)")
	cmd.Flags().StringVar(&databaseID, "database-id", "", "Spanner database ID (required)")
	cmd.Flags().StringVar(&operationID, "operation-id", "", "Caller-supplied operation ID for idempotent retries")
	cmd.Flags().StringVar(&protoDescriptors, "proto-descriptors", "", "Base64-encoded proto descriptors for CREATE/ALTER PROTO BUNDLE")

	dbsCmd.AddCommand(cmd)

	a.Registry.Register(contracts.BuildContract(
		"spanner databases update-ddl", "spanner",
		"Apply DDL statements to a Spanner database",
		[]contracts.FlagContract{
			{Name: "ddl", Type: "string", Description: "DDL statement (can be repeated)"},
			{Name: "ddl-file", Type: "string", Description: "Path to file with semicolon-delimited DDL statements"},
			{Name: "instance-id", Type: "string", Description: "Spanner instance ID", Required: true},
			{Name: "database-id", Type: "string", Description: "Spanner database ID", Required: true},
			{Name: "operation-id", Type: "string", Description: "Caller-supplied operation ID for idempotent retries"},
			{Name: "proto-descriptors", Type: "string", Description: "Base64-encoded proto descriptors for CREATE/ALTER PROTO BUNDLE"},
		},
		true, true,
	))
}

// collectDDLStatements gathers DDL statements from --ddl flags and/or --ddl-file.
func collectDDLStatements(flags []string, filePath string) ([]string, error) {
	var statements []string

	// From --ddl flags.
	for _, s := range flags {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			statements = append(statements, trimmed)
		}
	}

	// From --ddl-file.
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading DDL file %s: %w", filePath, err)
		}
		for _, stmt := range strings.Split(string(data), ";") {
			trimmed := strings.TrimSpace(stmt)
			if trimmed != "" {
				statements = append(statements, trimmed)
			}
		}
	}

	return statements, nil
}
