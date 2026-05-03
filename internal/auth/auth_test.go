package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	// Mock tokeninfo endpoint to return 200 for valid tokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"scope":"openid"}`))
	}))
	defer srv.Close()
	origClient := HTTPClient
	HTTPClient = srv.Client()
	defer func() { HTTPClient = origClient }()

	// Patch the tokeninfo URL via a test server — we need to override
	// verifyToken's URL. Since we can't easily do that, use a transport
	// that redirects to the test server.
	HTTPClient = &http.Client{
		Transport: &tokenTestTransport{url: srv.URL},
	}

	ctx := context.Background()
	cfg := Config{Token: "test-token"}

	result := Check(ctx, cfg)
	if !result.Authenticated {
		t.Errorf("Authenticated = false, want true (static token should succeed with valid tokeninfo)")
	}
	if result.Method != "token" {
		t.Errorf("Method = %s, want 'token'", result.Method)
	}
}

func TestCheckWithInvalidStaticToken(t *testing.T) {
	// Mock tokeninfo endpoint to return 400 for invalid tokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()
	HTTPClient = &http.Client{
		Transport: &tokenTestTransport{url: srv.URL},
	}
	defer func() { HTTPClient = http.DefaultClient }()

	ctx := context.Background()
	cfg := Config{Token: "bad-token"}

	result := Check(ctx, cfg)
	if result.Authenticated {
		t.Errorf("Authenticated = true, want false for invalid static token")
	}
	if result.Error == "" {
		t.Errorf("Error should describe token verification failure")
	}
}

// tokenTestTransport redirects all requests to the test server URL.
type tokenTestTransport struct {
	url string
}

func (t *tokenTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq, _ := http.NewRequestWithContext(req.Context(), req.Method, t.url+req.URL.Path+"?"+req.URL.RawQuery, req.Body)
	return http.DefaultTransport.RoundTrip(newReq)
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
