// Package auth implements the 5-tier credential resolution chain for dcx.
//
// Resolution priority (must match Rust implementation):
//  1. DCX_TOKEN env var / --token flag (static bearer token)
//  2. DCX_CREDENTIALS_FILE env var / --credentials-file (service account JSON)
//  3. Stored dcx auth login credentials (OAuth refresh_token — future)
//  4. GOOGLE_APPLICATION_CREDENTIALS (standard ADC file)
//  5. Default ADC (gcloud / metadata server)
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scopes used for Google Cloud API access.
var DefaultScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/bigquery",
}

// Config holds auth-related flag and env values.
type Config struct {
	Token           string // --token flag or DCX_TOKEN env
	CredentialsFile string // --credentials-file flag or DCX_CREDENTIALS_FILE env
}

// ResolvedAuth describes which auth method was used.
type ResolvedAuth struct {
	Method      string `json:"method"`       // "token", "credentials_file", "adc_file", "default_adc"
	Source      string `json:"source"`       // e.g. "DCX_TOKEN", "--token", file path
	ProjectID   string `json:"project_id,omitempty"`
	TokenSource oauth2.TokenSource
}

// StatusResult is the output of `auth status` / `auth check`.
type StatusResult struct {
	Authenticated bool   `json:"authenticated"`
	Method        string `json:"method"`
	Source        string `json:"source"`
	ProjectID     string `json:"project_id,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Resolve returns a TokenSource using the 5-tier priority chain.
func Resolve(ctx context.Context, cfg Config) (*ResolvedAuth, error) {
	// Tier 1: Static bearer token (flag takes precedence over env).
	token := cfg.Token
	if token == "" {
		token = os.Getenv("DCX_TOKEN")
	}
	if token != "" {
		source := "--token"
		if cfg.Token == "" {
			source = "DCX_TOKEN"
		}
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		return &ResolvedAuth{Method: "token", Source: source, TokenSource: ts}, nil
	}

	// Tier 2: Service account credentials file (flag > env).
	credsFile := cfg.CredentialsFile
	if credsFile == "" {
		credsFile = os.Getenv("DCX_CREDENTIALS_FILE")
	}
	if credsFile != "" {
		source := "--credentials-file"
		if cfg.CredentialsFile == "" {
			source = "DCX_CREDENTIALS_FILE"
		}
		return resolveCredentialsFile(ctx, credsFile, source)
	}

	// Tier 3: Stored OAuth credentials (dcx auth login).
	// Not yet implemented — will use keyring in a future phase.

	// Tier 4: GOOGLE_APPLICATION_CREDENTIALS (standard ADC file).
	if adcFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); adcFile != "" {
		return resolveCredentialsFile(ctx, adcFile, "GOOGLE_APPLICATION_CREDENTIALS")
	}

	// Tier 5: Default ADC (gcloud / metadata server).
	creds, err := google.FindDefaultCredentials(ctx, DefaultScopes...)
	if err != nil {
		return nil, fmt.Errorf("no credentials found: %w", err)
	}
	projectID := creds.ProjectID
	return &ResolvedAuth{
		Method:      "default_adc",
		Source:      "gcloud/metadata",
		ProjectID:   projectID,
		TokenSource: creds.TokenSource,
	}, nil
}

func resolveCredentialsFile(ctx context.Context, path, source string) (*ResolvedAuth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading credentials file %s: %w", path, err)
	}

	creds, err := google.CredentialsFromJSON(ctx, data, DefaultScopes...)
	if err != nil {
		return nil, fmt.Errorf("parsing credentials file %s: %w", path, err)
	}

	// Extract project_id if present in the JSON.
	var parsed struct {
		ProjectID string `json:"project_id"`
	}
	json.Unmarshal(data, &parsed)

	return &ResolvedAuth{
		Method:      "credentials_file",
		Source:      source,
		ProjectID:   parsed.ProjectID,
		TokenSource: creds.TokenSource,
	}, nil
}

// Check performs an auth preflight check and returns a StatusResult.
func Check(ctx context.Context, cfg Config) StatusResult {
	resolved, err := Resolve(ctx, cfg)
	if err != nil {
		return StatusResult{
			Authenticated: false,
			Error:         err.Error(),
		}
	}

	// Try to obtain a token to verify credentials work.
	_, tokenErr := resolved.TokenSource.Token()
	if tokenErr != nil {
		return StatusResult{
			Authenticated: false,
			Method:        resolved.Method,
			Source:         resolved.Source,
			Error:         tokenErr.Error(),
		}
	}

	return StatusResult{
		Authenticated: true,
		Method:        resolved.Method,
		Source:         resolved.Source,
		ProjectID:     resolved.ProjectID,
	}
}
