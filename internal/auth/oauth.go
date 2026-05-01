package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Default OAuth2 client ID for dcx (desktop app flow).
// This is a public client ID — the secret is not sensitive for desktop apps.
const (
	defaultClientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	defaultClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"
)

// OAuthScopes are the scopes requested during dcx auth login.
var OAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/bigquery",
}

// storedCredentials is the JSON shape saved to ~/.config/dcx/credentials.json.
type storedCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

// credentialsPath returns the path to ~/.config/dcx/credentials.json.
func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "dcx", "credentials.json")
}

// Login performs an OAuth2 authorization code flow with a local redirect.
// Opens the browser for user consent, captures the auth code via a local
// HTTP server, exchanges it for a refresh token, and saves it to disk.
func Login(ctx context.Context) (*oauth2.Token, error) {
	config := &oauth2.Config{
		ClientID:     defaultClientID,
		ClientSecret: defaultClientSecret,
		Scopes:       OAuthScopes,
		Endpoint:     google.Endpoint,
		RedirectURL:  "http://localhost:0", // will be set after listener binds
	}

	// Start a local HTTP server to capture the redirect.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://localhost:%d", port)

	// Generate the auth URL.
	state := fmt.Sprintf("dcx-%d", time.Now().UnixNano())
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Fprintf(os.Stderr, "\nOpen this URL in your browser to log in:\n\n  %s\n\nWaiting for authorization...\n", authURL)

	// Wait for the redirect with the auth code.
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authorization denied: %s", errMsg)
			fmt.Fprintf(w, "<html><body><h2>Authorization denied</h2><p>%s</p><p>You can close this tab.</p></body></html>", errMsg)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code received")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}
		codeCh <- code
		fmt.Fprint(w, "<html><body><h2>dcx authorized</h2><p>You can close this tab and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(120 * time.Second):
		return nil, fmt.Errorf("authorization timed out after 120 seconds")
	}

	// Exchange the code for a token.
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	// Save the refresh token.
	if err := saveCredentials(config, token); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save credentials: %v\n", err)
	}

	return token, nil
}

// saveCredentials writes the refresh token to ~/.config/dcx/credentials.json.
func saveCredentials(config *oauth2.Config, token *oauth2.Token) error {
	creds := storedCredentials{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RefreshToken: token.RefreshToken,
		TokenType:    "authorized_user",
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadStoredCredentials loads saved OAuth credentials from disk and returns
// a TokenSource. Returns nil if no stored credentials exist.
func LoadStoredCredentials(ctx context.Context) (*ResolvedAuth, error) {
	path := credentialsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // no stored credentials — not an error
	}

	var creds storedCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parsing stored credentials: %w", err)
	}

	if creds.RefreshToken == "" {
		return nil, nil
	}

	config := &oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Scopes:       OAuthScopes,
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{RefreshToken: creds.RefreshToken}
	ts := config.TokenSource(ctx, token)

	return &ResolvedAuth{
		Method:      "stored_oauth",
		Source:      path,
		TokenSource: ts,
	}, nil
}

// Logout removes stored credentials.
func Logout() error {
	path := credentialsPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
