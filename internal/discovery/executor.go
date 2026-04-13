package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
)

// ListEnvelope is the normalized output for list commands.
// All Discovery list commands produce this shape.
type ListEnvelope struct {
	Items         interface{} `json:"items"`
	Source        string      `json:"source"`
	NextPageToken string      `json:"next_page_token,omitempty"`
}

// Executor runs Discovery-generated commands.
type Executor struct {
	HTTPClient *http.Client
}

// NewExecutor creates an Executor with the given HTTP client.
func NewExecutor(client *http.Client) *Executor {
	if client == nil {
		client = http.DefaultClient
	}
	return &Executor{HTTPClient: client}
}

// Execute runs a Discovery command: validate → auth → request → render.
func (e *Executor) Execute(
	ctx context.Context,
	cmd GeneratedCommand,
	authCfg auth.Config,
	globalFlags map[string]string,
	queryParams map[string]string,
	format output.Format,
	pageAll bool,
) error {
	// 1. Resolve auth.
	resolved, err := auth.Resolve(ctx, authCfg)
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "Run 'dcx auth check' to verify credentials")
		return nil
	}

	tok, err := resolved.TokenSource.Token()
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "Check credentials")
		return nil
	}

	// 2. Resolve path params from global flags.
	pathParams, err := ResolvePathParams(cmd, globalFlags)
	if err != nil {
		dcxerrors.Emit(dcxerrors.MissingArgument, err.Error(), "")
		return nil
	}

	// 3. Validate required path params.
	if validErr := validateRequiredParams(cmd, globalFlags); validErr != nil {
		dcxerrors.Emit(dcxerrors.MissingArgument, validErr.Error(), "")
		return nil
	}

	// 4. Handle pagination for list commands.
	if pageAll && cmd.Method.Action == "list" {
		return e.executePageAll(ctx, cmd, pathParams, queryParams, tok.AccessToken, format)
	}

	// 5. Build and execute request.
	req, err := BuildRequest(cmd, pathParams, queryParams, tok.AccessToken)
	if err != nil {
		dcxerrors.Emit(dcxerrors.Internal, fmt.Sprintf("building request: %v", err), "")
		return nil
	}
	req = req.WithContext(ctx)

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		dcxerrors.Emit(dcxerrors.InfraError, fmt.Sprintf("API request failed: %v", err), "Check network connectivity")
		return nil
	}
	defer resp.Body.Close()

	// 6. Handle error responses.
	if resp.StatusCode >= 400 {
		return handleErrorResponse(resp)
	}

	// 7. Parse and render response.
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

	// Wrap in normalized envelope for list commands.
	if cmd.Method.Action == "list" {
		envelope := normalizeListResponse(raw, cmd.Service.Domain)
		return output.Render(format, envelope)
	}

	return output.Render(format, raw)
}

// executePageAll fetches all pages and combines results.
func (e *Executor) executePageAll(
	ctx context.Context,
	cmd GeneratedCommand,
	pathParams, queryParams map[string]string,
	token string,
	format output.Format,
) error {
	var allItems []interface{}
	pageToken := ""

	for {
		params := make(map[string]string, len(queryParams))
		for k, v := range queryParams {
			params[k] = v
		}
		if pageToken != "" {
			params["pageToken"] = pageToken
		}

		req, err := BuildRequest(cmd, pathParams, params, token)
		if err != nil {
			dcxerrors.Emit(dcxerrors.Internal, fmt.Sprintf("building request: %v", err), "")
			return nil
		}
		req = req.WithContext(ctx)

		resp, err := e.HTTPClient.Do(req)
		if err != nil {
			dcxerrors.Emit(dcxerrors.InfraError, fmt.Sprintf("API request failed: %v", err), "")
			return nil
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return handleErrorResponse(resp)
		}

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

		items := extractItems(raw)
		allItems = append(allItems, items...)

		// Check for next page.
		if npt, ok := raw["nextPageToken"].(string); ok && npt != "" {
			pageToken = npt
		} else {
			break
		}
	}

	envelope := ListEnvelope{
		Items:  allItems,
		Source: sourceName(cmd.Service.Domain),
	}
	return output.Render(format, envelope)
}

// normalizeListResponse wraps raw API responses in the dcx list envelope.
func normalizeListResponse(raw map[string]interface{}, domain string) ListEnvelope {
	items := extractItems(raw)
	var npt string
	if token, ok := raw["nextPageToken"].(string); ok {
		npt = token
	}

	return ListEnvelope{
		Items:         items,
		Source:        sourceName(domain),
		NextPageToken: npt,
	}
}

// extractItems finds the items array in a raw API response.
// Different APIs use different keys for the items array.
func extractItems(raw map[string]interface{}) []interface{} {
	// Common item keys used by Google APIs.
	itemKeys := []string{
		"datasets", "tables", "routines", "models",       // BigQuery
		"instances", "databases", "clusters", "backups",    // Spanner/AlloyDB/CloudSQL/Looker
		"items",                                            // generic fallback
	}

	for _, key := range itemKeys {
		if items, ok := raw[key]; ok {
			if arr, ok := items.([]interface{}); ok {
				return arr
			}
		}
	}

	// If no known items key, return the raw response as a single item.
	return []interface{}{raw}
}

func sourceName(domain string) string {
	switch domain {
	case "bigquery":
		return "BigQuery"
	case "spanner":
		return "Spanner"
	case "alloydb":
		return "AlloyDB"
	case "cloudsql":
		return "Cloud SQL"
	case "looker":
		return "Looker"
	default:
		return strings.Title(domain)
	}
}

func handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	// Try to extract error message from Google API error format.
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}

	message := fmt.Sprintf("API returned HTTP %d", resp.StatusCode)
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		message = apiErr.Error.Message
	}

	code := dcxerrors.ErrorCodeFromHTTP(resp.StatusCode)
	dcxerrors.Emit(code, message, "")
	return nil
}

func validateRequiredParams(cmd GeneratedCommand, globalFlags map[string]string) error {
	for paramName, param := range cmd.Method.Parameters {
		if !param.Required {
			continue
		}

		if param.Location == "path" {
			// Check if param is mapped to a global flag.
			if flagName, ok := cmd.Service.GlobalParamMappings[paramName]; ok {
				if val, ok := globalFlags[flagName]; !ok || val == "" {
					return fmt.Errorf("required flag --%s is missing", flagName)
				}
				continue
			}

			// Check if param is "parent" and handled by template.
			if paramName == "parent" && cmd.Service.ParentTemplate != "" {
				template := cmd.Service.ParentTemplate
				for name := range globalFlags {
					template = strings.ReplaceAll(template, "{"+name+"}", "OK")
				}
				if strings.Contains(template, "{") {
					return fmt.Errorf("parent template requires flags not provided: %s", cmd.Service.ParentTemplate)
				}
				continue
			}

			// Command-specific path param (resource ID).
			if val, ok := globalFlags[paramName]; !ok || val == "" {
				return fmt.Errorf("required flag --%s is missing", camelToKebab(paramName))
			}
		}
	}
	return nil
}
