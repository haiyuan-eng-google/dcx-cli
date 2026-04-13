package auth

import (
	"context"
	"testing"
)

func TestResolveStaticToken(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Token: "test-token-123"}

	resolved, err := Resolve(ctx, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.Method != "token" {
		t.Errorf("Method = %s, want 'token'", resolved.Method)
	}
	if resolved.Source != "--token" {
		t.Errorf("Source = %s, want '--token'", resolved.Source)
	}

	tok, err := resolved.TokenSource.Token()
	if err != nil {
		t.Fatalf("TokenSource.Token: %v", err)
	}
	if tok.AccessToken != "test-token-123" {
		t.Errorf("AccessToken = %s, want 'test-token-123'", tok.AccessToken)
	}
}

func TestResolveTokenEnvVar(t *testing.T) {
	t.Setenv("DCX_TOKEN", "env-token-456")
	ctx := context.Background()
	cfg := Config{} // no flag

	resolved, err := Resolve(ctx, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.Method != "token" {
		t.Errorf("Method = %s, want 'token'", resolved.Method)
	}
	if resolved.Source != "DCX_TOKEN" {
		t.Errorf("Source = %s, want 'DCX_TOKEN'", resolved.Source)
	}
}

func TestResolveFlagOverridesEnv(t *testing.T) {
	t.Setenv("DCX_TOKEN", "env-token")
	ctx := context.Background()
	cfg := Config{Token: "flag-token"}

	resolved, err := Resolve(ctx, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.Source != "--token" {
		t.Errorf("Source = %s, want '--token' (flag should override env)", resolved.Source)
	}

	tok, _ := resolved.TokenSource.Token()
	if tok.AccessToken != "flag-token" {
		t.Errorf("AccessToken = %s, want 'flag-token'", tok.AccessToken)
	}
}

func TestResolveCredentialsFileNotFound(t *testing.T) {
	ctx := context.Background()
	cfg := Config{CredentialsFile: "/nonexistent/creds.json"}

	_, err := Resolve(ctx, cfg)
	if err == nil {
		t.Error("expected error for nonexistent credentials file")
	}
}

func TestCheckWithStaticToken(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Token: "test-token"}

	result := Check(ctx, cfg)
	if !result.Authenticated {
		t.Errorf("Authenticated = false, want true (static token should succeed)")
	}
	if result.Method != "token" {
		t.Errorf("Method = %s, want 'token'", result.Method)
	}
}

func TestCheckWithNoCredentials(t *testing.T) {
	// Clear all auth env vars so no credentials are found.
	t.Setenv("DCX_TOKEN", "")
	t.Setenv("DCX_CREDENTIALS_FILE", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	// Note: this test may still succeed via default ADC (gcloud) on dev machines.
	// That's OK — we're testing that Check returns a valid result.
	ctx := context.Background()
	cfg := Config{}

	result := Check(ctx, cfg)
	// Result will be either authenticated (via gcloud) or not.
	// We just verify the shape is valid.
	if result.Authenticated {
		if result.Method == "" {
			t.Error("Authenticated=true but Method is empty")
		}
	} else {
		if result.Error == "" {
			t.Error("Authenticated=false but Error is empty")
		}
	}
}
