package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/retry"
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
	HTTPClient   *http.Client
	OutputFields string // comma-separated fields to include in output
	MaxRetries   int    // 0 = no retry
	PageLimit    int    // max pages to fetch with --page-all (0 = unlimited)
	PageDelayMs  int    // milliseconds between page fetches
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

	// 3. Validate required params (path + query).
	if validErr := validateRequiredParams(cmd, globalFlags, queryParams); validErr != nil {
		dcxerrors.Emit(dcxerrors.MissingArgument, validErr.Error(), "")
		return nil
	}

	// 4. Handle pagination for list commands.
	if pageAll && cmd.Method.Action == "list" {
		return e.executePageAll(ctx, cmd, pathParams, queryParams, tok.AccessToken, format)
	}

	// 5. Build and execute request (with retry if configured).
	resp, err := retry.Do(e.HTTPClient, func() (*http.Request, error) {
		r, err := BuildRequest(cmd, pathParams, queryParams, tok.AccessToken, nil)
		if err != nil {
			return nil, err
		}
		return r.WithContext(ctx), nil
	}, e.MaxRetries)
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
		envelope := normalizeListResponse(raw, cmd.Service.Domain, cmd.Method.Resource)
		return output.RenderFiltered(format, envelope, e.OutputFields)
	}

	return output.RenderFiltered(format, raw, e.OutputFields)
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
	pagesFetched := 0

	for {
		params := make(map[string]string, len(queryParams))
		for k, v := range queryParams {
			params[k] = v
		}
		if pageToken != "" {
			params["pageToken"] = pageToken
		}

		paramsCopy := params // capture for closure
		resp, err := retry.Do(e.HTTPClient, func() (*http.Request, error) {
			r, err := BuildRequest(cmd, pathParams, paramsCopy, token, nil)
			if err != nil {
				return nil, err
			}
			return r.WithContext(ctx), nil
		}, e.MaxRetries)
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
		pagesFetched++

		// Check for next page.
		npt, hasMore := raw["nextPageToken"].(string)
		if !hasMore || npt == "" {
			break
		}

		// Check page limit (0 = unlimited).
		if e.PageLimit > 0 && pagesFetched >= e.PageLimit {
			// Stopped by limit — include next_page_token so agents can
			// continue with --page-token. The envelope only contains
			// accumulated items + source + token; per-page API metadata
			// (if any) is not preserved in the capped result.
			allItems = injectResourceIDsForDomain(allItems, cmd.Method.Resource, cmd.Service.Domain)
			envelope := ListEnvelope{
				Items:         allItems,
				Source:        sourceName(cmd.Service.Domain),
				NextPageToken: npt,
			}
			return output.RenderFiltered(format, envelope, e.OutputFields)
		}

		pageToken = npt

		// Delay between pages.
		if e.PageDelayMs > 0 {
			time.Sleep(time.Duration(e.PageDelayMs) * time.Millisecond)
		}
	}

	allItems = injectResourceIDsForDomain(allItems, cmd.Method.Resource, cmd.Service.Domain)
	envelope := ListEnvelope{
		Items:  allItems,
		Source: sourceName(cmd.Service.Domain),
	}
	return output.RenderFiltered(format, envelope, e.OutputFields)
}

// normalizeListResponse wraps raw API responses in the dcx list envelope.
func normalizeListResponse(raw map[string]interface{}, domain, resource string) ListEnvelope {
	items := extractItems(raw)
	if resource != "" {
		items = injectResourceIDsForDomain(items, resource, domain)
	}
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
// Tries known keys first, then falls back to any top-level array-valued key
// (skipping metadata keys like nextPageToken). This handles new resource types
// without requiring code changes.
func extractItems(raw map[string]interface{}) []interface{} {
	// Known item keys used by Google APIs — checked first for determinism.
	knownKeys := []string{
		"datasets", "tables", "routines", "models", "jobs", // BigQuery
		"instances", "databases", "clusters", "backups", // Spanner/AlloyDB/CloudSQL/Looker
		"operations", "users", "backupRuns", "flags", // additional surfaces
		"items", // generic
	}

	for _, key := range knownKeys {
		if items, ok := raw[key]; ok {
			if arr, ok := items.([]interface{}); ok {
				return arr
			}
		}
	}

	// Fallback: find any top-level key whose value is a JSON array,
	// skipping pagination/metadata keys.
	skip := map[string]bool{"nextPageToken": true, "kind": true, "etag": true}
	for key, val := range raw {
		if skip[key] {
			continue
		}
		if arr, ok := val.([]interface{}); ok {
			return arr
		}
	}

	// Nothing found — return empty. This happens when the API returns
	// an empty list (e.g., {} with no items key for models/routines).
	return []interface{}{}
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

func readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return body, nil
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

	// 429: emit structured rate-limit error with Retry-After.
	if resp.StatusCode == 429 {
		dcxerrors.EmitRateLimited(message, resp.Header.Get("Retry-After"))
		return nil
	}

	code := dcxerrors.ErrorCodeFromHTTP(resp.StatusCode)
	dcxerrors.Emit(code, message, "")
	return nil
}

func validateRequiredParams(cmd GeneratedCommand, globalFlags map[string]string, queryParams map[string]string) error {
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

			// Check if param is "parent" and handled by template or flatPath.
			if paramName == "parent" {
				if err := validateParentFlags(cmd, globalFlags); err != nil {
					return err
				}
				continue
			}

			// Skip full-resource-path params in flatPath services — these are
			// validated via the individual flatPath segment flags.
			if cmd.Service.UseFlatPath && isFullResourcePathParam(param.Pattern) {
				continue
			}

			// Command-specific path param (resource ID).
			if val, ok := globalFlags[paramName]; !ok || val == "" {
				return fmt.Errorf("required flag --%s is missing", camelToKebab(paramName))
			}
		}

		if param.Location == "query" {
			if val, ok := queryParams[paramName]; !ok || val == "" {
				return fmt.Errorf("required flag --%s is missing", camelToKebab(paramName))
			}
		}
	}

	// For flatPath services, validate that all intermediate segment flags
	// are provided.
	if cmd.Service.UseFlatPath && cmd.Method.FlatPath != "" {
		if err := validateFlatPathFlags(cmd, globalFlags); err != nil {
			return err
		}
	}

	return nil
}

// validateParentFlags checks that all flags needed for the parent are provided.
// Uses the ParentTemplate for top-level parents and the flatPath for deeper ones.
func validateParentFlags(cmd GeneratedCommand, globalFlags map[string]string) error {
	if cmd.Service.ParentTemplate != "" {
		template := cmd.Service.ParentTemplate
		for name := range globalFlags {
			template = strings.ReplaceAll(template, "{"+name+"}", "OK")
		}
		if strings.Contains(template, "{") {
			return fmt.Errorf("parent template requires flags not provided: %s", cmd.Service.ParentTemplate)
		}
	}
	// Intermediate flags are validated by validateFlatPathFlags.
	return nil
}

// validateFlatPathFlags checks that all flatPath segment flags are provided.
func validateFlatPathFlags(cmd GeneratedCommand, globalFlags map[string]string) error {
	segments := parseFlatPathSegments(cmd.Method.FlatPath)
	parentMap := buildParentFlagMap(cmd.Service.ParentTemplate)

	for _, seg := range segments {
		// Try ParentTemplate mapping.
		if mapped, ok := parentMap[seg.IDKey]; ok {
			if val, ok := globalFlags[mapped]; ok && val != "" {
				continue
			}
			return fmt.Errorf("required flag --%s is missing", mapped)
		}

		// Try IDKey directly as a flag (CloudSQL style: {instance}).
		if val, ok := globalFlags[seg.IDKey]; ok && val != "" {
			continue
		}

		// Try derived flag name (Spanner style: {instancesId} → --instance-id).
		flagName := deriveFlagName(seg.Resource)
		if val, ok := globalFlags[flagName]; ok && val != "" {
			continue
		}

		return fmt.Errorf("required flag --%s is missing", flagName)
	}
	return nil
}
