package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

// checkStatus is the status of a single health check.
type checkStatus string

const (
	statusOK      checkStatus = "ok"
	statusWarn    checkStatus = "warn"
	statusError   checkStatus = "error"
	statusSkipped checkStatus = "skipped"
)

// healthCheck is a single check result.
type healthCheck struct {
	Name    string      `json:"name"`
	Status  checkStatus `json:"status"`
	Detail  string      `json:"detail,omitempty"`
	Latency string      `json:"latency,omitempty"`
}

// healthResult is the full health report.
type healthResult struct {
	Overall string        `json:"overall"` // "healthy", "degraded", "unhealthy"
	Checks  []healthCheck `json:"checks"`
}

func (a *App) addHealthCommand() {
	cmd := &cobra.Command{
		Use:     "health",
		Aliases: []string{"doctor"},
		Short:   "Run diagnostic checks on auth, project, and API access",
		Long: `Run a bounded set of diagnostic checks:
  - Auth source and credential resolution
  - Token acquisition
  - Project ID presence
  - Project access (BigQuery API call)
  - CA API reachability (if --location set)
  - Spanner API reachability (if --profile points to Spanner)
  - Profile validation (if --profile set)

Exit code 0 for healthy/degraded (warnings). Nonzero for auth/project/API failures.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			profileName, _ := cmd.Flags().GetString("profile")
			result := runHealthChecks(a, profileName)

			if err := a.Render(format, result); err != nil {
				return err
			}

			// Exit nonzero for unhealthy without emitting a second envelope.
			if result.Overall == "unhealthy" {
				os.Exit(2)
			}
			return nil
		},
	}

	cmd.Flags().String("profile", "", "Source profile to check connectivity for")

	a.Root.AddCommand(cmd)

	a.Registry.Register(contracts.BuildContract(
		"health", "diagnostics",
		"Run diagnostic checks on auth, project, and API access (alias: dcx doctor)",
		[]contracts.FlagContract{
			{Name: "profile", Type: "string", Description: "Source profile to check connectivity for"},
		}, false, false,
	))
}

// healthTimeout is the maximum time for the entire health check run.
const healthTimeout = 15 * time.Second

func runHealthChecks(a *App, profileName string) healthResult {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	var checks []healthCheck
	hasError := false

	// 1. Auth source resolution.
	authCfg := a.AuthConfig()
	resolved, resolveErr := auth.Resolve(ctx, authCfg)
	if resolveErr != nil {
		checks = append(checks, healthCheck{
			Name:   "auth_source",
			Status: statusError,
			Detail: resolveErr.Error(),
		})
		hasError = true
	} else {
		checks = append(checks, healthCheck{
			Name:   "auth_source",
			Status: statusOK,
			Detail: fmt.Sprintf("method=%s, source=%s", resolved.Method, resolved.Source),
		})
	}

	// 2. Token acquisition.
	if resolved != nil {
		start := time.Now()
		_, tokenErr := resolved.TokenSource.Token()
		latency := time.Since(start).Round(time.Millisecond).String()
		if tokenErr != nil {
			checks = append(checks, healthCheck{
				Name:    "token_acquisition",
				Status:  statusError,
				Detail:  tokenErr.Error(),
				Latency: latency,
			})
			hasError = true
		} else {
			checks = append(checks, healthCheck{
				Name:    "token_acquisition",
				Status:  statusOK,
				Detail:  "token obtained",
				Latency: latency,
			})
		}
	} else {
		checks = append(checks, healthCheck{
			Name:   "token_acquisition",
			Status: statusSkipped,
			Detail: "no auth source",
		})
	}

	// 3. Project ID presence.
	projectID := a.Opts.ProjectID
	if projectID == "" {
		checks = append(checks, healthCheck{
			Name:   "project_id",
			Status: statusWarn,
			Detail: "not set (use --project-id or gcloud config)",
		})
	} else {
		checks = append(checks, healthCheck{
			Name:   "project_id",
			Status: statusOK,
			Detail: projectID,
		})
	}

	// 4. Project access (lightweight BigQuery datasets.list).
	if projectID != "" && resolved != nil && !hasError {
		check := checkBigQueryAccess(ctx, resolved.TokenSource, projectID)
		checks = append(checks, check)
		if check.Status == statusError {
			hasError = true
		}
	} else if projectID == "" {
		checks = append(checks, healthCheck{
			Name:   "bigquery_access",
			Status: statusSkipped,
			Detail: "no project ID",
		})
	} else {
		checks = append(checks, healthCheck{
			Name:   "bigquery_access",
			Status: statusSkipped,
			Detail: "auth failed",
		})
	}

	// 5. CA API reachability (if location set).
	location := a.Opts.Location
	if location != "" && projectID != "" && resolved != nil && !hasError {
		check := checkCAAccess(ctx, resolved.TokenSource, projectID, location)
		checks = append(checks, check)
	} else {
		reason := "no --location"
		if projectID == "" {
			reason = "no project ID"
		} else if hasError {
			reason = "auth failed"
		}
		checks = append(checks, healthCheck{
			Name:   "ca_access",
			Status: statusSkipped,
			Detail: reason,
		})
	}

	// 6. Profile validation (if --profile set).
	if profileName != "" {
		check := checkProfile(profileName)
		checks = append(checks, check)

		// 7. Spanner access if profile is Spanner.
		if check.Status == statusOK && resolved != nil && !hasError {
			p, _ := profiles.LoadByName(profileName)
			if p != nil && p.SourceType == profiles.Spanner {
				spannerCheck := checkSpannerAccess(ctx, resolved.TokenSource, p)
				checks = append(checks, spannerCheck)
			}
		}
	}

	// Determine overall status.
	overall := "healthy"
	for _, c := range checks {
		if c.Status == statusError {
			overall = "unhealthy"
			break
		}
		if c.Status == statusWarn {
			overall = "degraded"
		}
	}

	return healthResult{
		Overall: overall,
		Checks:  checks,
	}
}

func checkBigQueryAccess(ctx context.Context, ts oauth2.TokenSource, projectID string) healthCheck {
	tok, err := ts.Token()
	if err != nil {
		return healthCheck{Name: "bigquery_access", Status: statusError, Detail: err.Error()}
	}

	url := fmt.Sprintf("https://bigquery.googleapis.com/bigquery/v2/projects/%s/datasets?maxResults=1", projectID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Round(time.Millisecond).String()

	if err != nil {
		return healthCheck{Name: "bigquery_access", Status: statusError, Detail: sanitizeNetworkError(err), Latency: latency}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return healthCheck{Name: "bigquery_access", Status: statusOK, Detail: "API reachable", Latency: latency}
	}
	if resp.StatusCode == 403 {
		return healthCheck{Name: "bigquery_access", Status: statusError, Detail: "permission denied", Latency: latency}
	}
	return healthCheck{Name: "bigquery_access", Status: statusError, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode), Latency: latency}
}

func checkCAAccess(ctx context.Context, ts oauth2.TokenSource, projectID, location string) healthCheck {
	tok, err := ts.Token()
	if err != nil {
		return healthCheck{Name: "ca_access", Status: statusError, Detail: err.Error()}
	}

	url := fmt.Sprintf("https://geminidataanalytics.googleapis.com/v1beta/projects/%s/locations/%s/dataAgents", projectID, location)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Round(time.Millisecond).String()

	if err != nil {
		return healthCheck{Name: "ca_access", Status: statusError, Detail: sanitizeNetworkError(err), Latency: latency}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return healthCheck{Name: "ca_access", Status: statusOK, Detail: "API reachable", Latency: latency}
	}
	if resp.StatusCode == 403 {
		return healthCheck{Name: "ca_access", Status: statusWarn, Detail: "permission denied (API may not be enabled)", Latency: latency}
	}
	if resp.StatusCode == 404 {
		return healthCheck{Name: "ca_access", Status: statusWarn, Detail: "API not found (may not be enabled for this project)", Latency: latency}
	}
	return healthCheck{Name: "ca_access", Status: statusWarn, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode), Latency: latency}
}

func checkProfile(name string) healthCheck {
	p, err := profiles.LoadByName(name)
	if err != nil {
		return healthCheck{Name: "profile", Status: statusError, Detail: err.Error()}
	}
	issues := p.Validate()
	if len(issues) > 0 {
		return healthCheck{Name: "profile", Status: statusError, Detail: strings.Join(issues, "; ")}
	}
	return healthCheck{Name: "profile", Status: statusOK, Detail: fmt.Sprintf("name=%s, type=%s", p.Name, p.SourceType)}
}

func checkSpannerAccess(ctx context.Context, ts oauth2.TokenSource, p *profiles.Profile) healthCheck {
	tok, err := ts.Token()
	if err != nil {
		return healthCheck{Name: "spanner_access", Status: statusError, Detail: err.Error()}
	}

	url := fmt.Sprintf("https://spanner.googleapis.com/v1/projects/%s/instances?pageSize=1", p.Project)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Round(time.Millisecond).String()

	if err != nil {
		return healthCheck{Name: "spanner_access", Status: statusError, Detail: sanitizeNetworkError(err), Latency: latency}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return healthCheck{Name: "spanner_access", Status: statusOK, Detail: "API reachable", Latency: latency}
	}
	return healthCheck{Name: "spanner_access", Status: statusWarn, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode), Latency: latency}
}

// sanitizeNetworkError extracts a diagnostic message from a network error
// without leaking full URLs or tokens.
func sanitizeNetworkError(err error) string {
	msg := err.Error()
	// Extract the useful part: DNS, TLS, timeout, connection refused.
	for _, keyword := range []string{"no such host", "connection refused", "timeout", "TLS", "certificate", "dial tcp", "context deadline exceeded"} {
		if strings.Contains(msg, keyword) {
			return keyword
		}
	}
	return "network error"
}
