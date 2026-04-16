// Package eval implements the deterministic CLI eval suite for dcx.
//
// These tests build the dcx binary and run it as a subprocess to validate
// command discovery, contract completeness, error handling, format support,
// and skill alignment — without requiring network access or live APIs.
//
// This suite is designed to be a CI gate: all categories must pass before
// the Go MVP can ship.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var dcxBinary string

func TestMain(m *testing.M) {
	// Build the dcx binary once for all eval tests.
	dir, err := os.MkdirTemp("", "dcx-eval-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	dcxBinary = filepath.Join(dir, "dcx")
	cmd := exec.Command("go", "build", "-o", dcxBinary, "./cmd/dcx")
	// Build from the repo root.
	cmd.Dir = filepath.Join("..", "..")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build dcx: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// cleanEnv returns a minimal environment with no Google credentials.
// Only PATH is inherited; all auth-related vars are excluded.
func cleanEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=/tmp/dcx-eval-nonexistent",
	}
}

func runDcx(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(dcxBinary, args...)
	cmd.Env = cleanEnv()
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// ============================================================
// Category 1: Command Discovery
// ============================================================

func TestCommandDiscovery(t *testing.T) {
	stdout, _, exitCode := runDcx(t, "meta", "commands", "--format", "json")
	if exitCode != 0 {
		t.Fatalf("meta commands exited %d", exitCode)
	}

	var commands []struct {
		Command string `json:"command"`
		Domain  string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(stdout), &commands); err != nil {
		t.Fatalf("parsing meta commands output: %v", err)
	}

	// All shipped commands must be present.
	requiredCommands := []string{
		// BigQuery (Discovery + static)
		"dcx datasets list", "dcx datasets get",
		"dcx datasets insert", "dcx datasets delete",
		"dcx tables list", "dcx tables get",
		"dcx tables insert", "dcx tables delete",
		"dcx jobs list", "dcx jobs get", "dcx jobs query",
		"dcx models list", "dcx models get",
		"dcx routines list", "dcx routines get",
		// Spanner
		"dcx spanner instances list", "dcx spanner instances get",
		"dcx spanner databases list", "dcx spanner databases get",
		"dcx spanner databases get-ddl", "dcx spanner schema describe",
		"dcx spanner databases create", "dcx spanner databases drop-database",
		"dcx spanner databases update-ddl",
		"dcx spanner backups list", "dcx spanner backups get",
		"dcx spanner backups create", "dcx spanner backups delete",
		"dcx spanner database-operations list",
		"dcx spanner instance-configs list", "dcx spanner instance-configs get",
		// AlloyDB
		"dcx alloydb clusters list", "dcx alloydb clusters get",
		"dcx alloydb instances list", "dcx alloydb instances get",
		"dcx alloydb databases list", "dcx alloydb schema describe",
		"dcx alloydb backups list", "dcx alloydb backups get",
		"dcx alloydb users list", "dcx alloydb users get",
		"dcx alloydb users create", "dcx alloydb users delete",
		"dcx alloydb operations list", "dcx alloydb operations get",
		// Cloud SQL
		"dcx cloudsql instances list", "dcx cloudsql instances get",
		"dcx cloudsql databases list", "dcx cloudsql databases get",
		"dcx cloudsql databases insert", "dcx cloudsql databases delete",
		"dcx cloudsql schema describe",
		"dcx cloudsql backup-runs list", "dcx cloudsql backup-runs get",
		"dcx cloudsql users list", "dcx cloudsql users get",
		"dcx cloudsql users insert", "dcx cloudsql users delete",
		"dcx cloudsql operations list", "dcx cloudsql operations get",
		"dcx cloudsql flags list", "dcx cloudsql tiers list",
		// Looker
		"dcx looker instances list", "dcx looker instances get",
		"dcx looker explores list", "dcx looker dashboards get",
		"dcx looker backups list", "dcx looker backups get",
		// CA
		"dcx ca ask",
		"dcx ca create-agent", "dcx ca list-agents", "dcx ca add-verified-query",
		// Auth
		"dcx auth check", "dcx auth status",
		// Profiles
		"dcx profiles list", "dcx profiles validate", "dcx profiles test",
		// Meta
		"dcx meta commands", "dcx meta describe", "dcx meta generate-skills",
		// MCP
		"dcx mcp serve",
		// Completion
		"dcx completion",
	}

	registered := make(map[string]bool)
	for _, c := range commands {
		registered[c.Command] = true
	}

	for _, want := range requiredCommands {
		if !registered[want] {
			t.Errorf("P0 command not registered: %s", want)
		}
	}

	if len(commands) < 82 {
		t.Errorf("expected at least 82 commands, got %d", len(commands))
	}
}

// ============================================================
// Category 2: Contract Completeness
// ============================================================

func TestContractCompleteness(t *testing.T) {
	// Get all commands.
	stdout, _, _ := runDcx(t, "meta", "commands", "--format", "json")
	var commands []struct {
		Command string `json:"command"`
	}
	json.Unmarshal([]byte(stdout), &commands)

	for _, cmd := range commands {
		// Skip mcp serve — it blocks on stdin.
		if cmd.Command == "dcx mcp serve" {
			continue
		}
		t.Run(cmd.Command, func(t *testing.T) {
			parts := strings.Fields(cmd.Command)
			args := append([]string{"meta", "describe"}, parts[1:]...)
			stdout, _, exitCode := runDcx(t, args...)
			if exitCode != 0 {
				t.Fatalf("meta describe %s exited %d", cmd.Command, exitCode)
			}

			var contract map[string]interface{}
			if err := json.Unmarshal([]byte(stdout), &contract); err != nil {
				t.Fatalf("meta describe %s: invalid JSON: %v", cmd.Command, err)
			}

			// Required contract keys.
			for _, key := range []string{"contract_version", "command", "domain", "flags", "exit_codes", "formats"} {
				if _, ok := contract[key]; !ok {
					t.Errorf("contract for %s missing key: %s", cmd.Command, key)
				}
			}

			// Formats must include all four.
			if formats, ok := contract["formats"].([]interface{}); ok {
				fmtSet := make(map[string]bool)
				for _, f := range formats {
					fmtSet[fmt.Sprintf("%v", f)] = true
				}
				for _, want := range []string{"json", "json-minified", "table", "text"} {
					if !fmtSet[want] {
						t.Errorf("contract for %s missing format: %s", cmd.Command, want)
					}
				}
			}
		})
	}
}

// ============================================================
// Category 3: Error Recovery
// ============================================================

func TestErrorRecovery(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string // substring expected in stderr JSON
	}{
		{
			name:    "unknown subcommand",
			args:    []string{"nonexistent"},
			wantErr: "unknown command",
		},
		{
			name:    "invalid format",
			args:    []string{"meta", "commands", "--format", "csv"},
			wantErr: "csv",
		},
		{
			name:    "meta describe unknown",
			args:    []string{"meta", "describe", "nonexistent"},
			wantErr: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, exitCode := runDcx(t, tt.args...)
			if exitCode == 0 {
				t.Error("expected non-zero exit code for invalid input")
			}

			// stderr should contain structured error or usage info.
			combined := stderr
			if !strings.Contains(strings.ToLower(combined), strings.ToLower(tt.wantErr)) {
				t.Errorf("stderr should contain %q; got: %s", tt.wantErr, combined)
			}
		})
	}
}

// ============================================================
// Category 4: Exit Code Semantics
// ============================================================

func TestExitCodeSemantics(t *testing.T) {
	// Validation error → exit 1
	t.Run("validation_exit_1", func(t *testing.T) {
		_, stderr, exitCode := runDcx(t, "meta", "describe", "nonexistent", "command")
		if exitCode != 1 {
			t.Errorf("unknown command should exit 1 (validation), got %d; stderr: %s", exitCode, stderr)
		}

		// Verify error envelope on stderr.
		var envelope map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stderr)), &envelope); err == nil {
			if errObj, ok := envelope["error"].(map[string]interface{}); ok {
				if code, _ := errObj["exit_code"].(float64); code != 1 {
					t.Errorf("error envelope exit_code = %v, want 1", code)
				}
			}
		}
	})

	// Auth error → exit 3 (no credentials available).
	// cleanEnv() ensures no ambient ADC, so this must fail.
	t.Run("auth_exit_3", func(t *testing.T) {
		_, stderr, exitCode := runDcx(t, "auth", "check")
		if exitCode == 0 {
			t.Fatal("auth check with no credentials should fail (exit 3), but exited 0 — environment may be leaking credentials")
		}
		if exitCode != 3 {
			t.Errorf("auth failure should exit 3, got %d; stderr: %s", exitCode, stderr)
		}
	})
}

// ============================================================
// Category 5: Help Completeness
// ============================================================

func TestHelpCompleteness(t *testing.T) {
	stdout, _, _ := runDcx(t, "meta", "commands", "--format", "json")
	var commands []struct {
		Command string `json:"command"`
	}
	json.Unmarshal([]byte(stdout), &commands)

	for _, cmd := range commands {
		if cmd.Command == "dcx mcp serve" {
			continue
		}
		t.Run(cmd.Command, func(t *testing.T) {
			parts := strings.Fields(cmd.Command)
			args := append(parts[1:], "--help") // skip "dcx" prefix
			stdout, _, exitCode := runDcx(t, args...)
			if exitCode != 0 {
				t.Errorf("--help for %s exited %d", cmd.Command, exitCode)
			}
			if len(stdout) == 0 {
				t.Errorf("--help for %s produced no output", cmd.Command)
			}
			// Help should contain "Usage:" section.
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("--help for %s missing 'Usage:' section", cmd.Command)
			}
		})
	}
}

// ============================================================
// Category 6: Format Support
// ============================================================

func TestFormatSupport(t *testing.T) {
	formats := []string{"json", "json-minified", "table", "text"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			stdout, _, exitCode := runDcx(t, "meta", "commands", "--format", format)
			if exitCode != 0 {
				t.Fatalf("meta commands --format %s exited %d", format, exitCode)
			}
			if len(strings.TrimSpace(stdout)) == 0 {
				t.Errorf("meta commands --format %s produced empty output", format)
			}
		})
	}

	// json-minified should be single line.
	t.Run("json-minified_is_single_line", func(t *testing.T) {
		stdout, _, _ := runDcx(t, "meta", "commands", "--format", "json-minified")
		trimmed := strings.TrimSpace(stdout)
		if strings.Count(trimmed, "\n") > 0 {
			t.Error("json-minified should be a single line")
		}
		// Should be valid JSON.
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			t.Errorf("json-minified output is not valid JSON: %v", err)
		}
	})
}

// ============================================================
// Category 7: Preflight Validation (auth check)
// ============================================================

func TestPreflightValidation(t *testing.T) {
	// auth check with a static token should return structured JSON.
	stdout, _, exitCode := runDcx(t, "auth", "check", "--token", "test-eval-token")
	if exitCode != 0 {
		t.Fatalf("auth check --token exited %d", exitCode)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("auth check output is not valid JSON: %v", err)
	}

	if auth, ok := result["authenticated"].(bool); !ok || !auth {
		t.Error("auth check with --token should report authenticated=true")
	}
	if method, ok := result["method"].(string); !ok || method != "token" {
		t.Errorf("auth check method = %v, want 'token'", result["method"])
	}
}

// ============================================================
// Category 8: Auth Preflight (missing credentials)
// ============================================================

func TestAuthPreflight(t *testing.T) {
	// runDcx uses cleanEnv() — no ambient credentials.
	_, stderr, exitCode := runDcx(t, "auth", "check", "--format", "json")

	if exitCode == 0 {
		t.Fatal("auth check with no credentials should fail (exit 3), but exited 0 — environment may be leaking credentials")
	}

	// Should exit 3 (auth error).
	if exitCode != 3 {
		t.Errorf("exit code = %d, want 3; stderr: %s", exitCode, stderr)
	}

	// Stderr should have error envelope with AUTH_ERROR.
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		t.Fatal("expected error envelope on stderr, got empty")
	}

	var envelope map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		t.Fatalf("stderr is not valid error envelope JSON: %v\nraw: %s", err, trimmed)
	}
	errObj := envelope["error"]
	if code, ok := errObj["code"].(string); !ok || code != "AUTH_ERROR" {
		t.Errorf("error code = %v, want AUTH_ERROR", errObj["code"])
	}
}

// ============================================================
// Category 9: Skill Alignment
// ============================================================

func TestSkillAlignment(t *testing.T) {
	// Get all registered commands.
	stdout, _, _ := runDcx(t, "meta", "commands", "--format", "json")
	var commands []struct {
		Command string `json:"command"`
	}
	json.Unmarshal([]byte(stdout), &commands)

	registered := make(map[string]bool)
	for _, c := range commands {
		registered[c.Command] = true
	}

	// Read skill files and extract command references.
	skillDir := filepath.Join("..", "..", "skills")
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		t.Fatalf("reading skills dir: %v", err)
	}

	// Known P1 (deferred) commands that skills may reference.
	// These are logged but not failures.
	p1Commands := map[string]bool{
		"dcx auth login":             true,
		"dcx auth logout":            true,
		"dcx looker explores get":    true,
		"dcx looker dashboards list": true,
		"dcx generate-skills":        true,
	}

	// Regex to find dcx command references in skill docs.
	cmdPattern := regexp.MustCompile(`dcx\s+(?:[\w-]+\s+)*[\w-]+`)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(skillDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			matches := cmdPattern.FindAllString(string(data), -1)
			for _, match := range matches {
				match = strings.TrimRight(match, ".,;:!?)")
				if match == "dcx" {
					continue
				}

				if !registered[match] {
					// Try prefix match (skills may reference command groups
					// or commands with trailing args).
					parts := strings.Fields(match)
					found := false
					for i := len(parts); i >= 2; i-- {
						candidate := strings.Join(parts[:i], " ")
						if registered[candidate] {
							found = true
							break
						}
					}
					// Also check if this is a valid command group prefix
					// (e.g. "dcx looker explores" where "dcx looker explores list" exists).
					if !found {
						prefix := match + " "
						for cmd := range registered {
							if strings.HasPrefix(cmd, prefix) {
								found = true
								break
							}
						}
					}
					if found {
						continue
					}

					// Check if this is a known P1 command.
					isP1 := false
					for p1Cmd := range p1Commands {
						if strings.HasPrefix(match, p1Cmd) {
							isP1 = true
							break
						}
					}

					// Also skip prose fragments that look like commands
					// but contain non-command words.
					looksLikeProse := false
					for _, word := range []string{"usage", "authentication", "understand", "resolves", "credentials"} {
						if strings.Contains(strings.ToLower(match), word) {
							looksLikeProse = true
							break
						}
					}

					if isP1 {
						t.Logf("P1 (deferred): skill %s references %s", entry.Name(), match)
					} else if looksLikeProse {
						// Skip prose that matched the regex.
					} else {
						t.Errorf("skill %s references unregistered P0 command: %s", entry.Name(), match)
					}
				}
			}
		})
	}
}

// ============================================================
// Category 10: Dry-Run Success
// ============================================================

func TestDryRunSuccess(t *testing.T) {
	// jobs query --dry-run should not error even without auth (it needs project-id though).
	// With a token but invalid project, dry-run still sends the request — this test
	// verifies the flag is accepted.
	stdout, _, exitCode := runDcx(t, "jobs", "query", "--help")
	if exitCode != 0 {
		t.Fatal("jobs query --help failed")
	}
	if !strings.Contains(stdout, "--dry-run") {
		t.Error("jobs query should accept --dry-run flag")
	}

	// Verify --dry-run is in the contract.
	stdout, _, exitCode = runDcx(t, "meta", "describe", "jobs", "query", "--format", "json")
	if exitCode != 0 {
		t.Fatal("meta describe jobs query failed")
	}
	var contract map[string]interface{}
	json.Unmarshal([]byte(stdout), &contract)
	if dryRun, ok := contract["supports_dry_run"].(bool); !ok || !dryRun {
		t.Error("jobs query contract should have supports_dry_run=true")
	}
}

// ============================================================
// Category 11: JSON Contract Stability
// ============================================================

func TestJSONContractStability(t *testing.T) {
	// Verify error envelope has correct top-level keys.
	t.Run("error_envelope_keys", func(t *testing.T) {
		_, stderr, _ := runDcx(t, "meta", "describe", "nonexistent")
		trimmed := strings.TrimSpace(stderr)
		if trimmed == "" {
			t.Skip("no stderr output")
		}

		var envelope map[string]map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
			t.Fatalf("error envelope is not valid JSON: %v\nraw: %s", err, trimmed)
		}

		errObj, ok := envelope["error"]
		if !ok {
			t.Fatal("missing top-level 'error' key")
		}

		requiredKeys := []string{"code", "message", "exit_code", "retryable", "status"}
		for _, key := range requiredKeys {
			if _, ok := errObj[key]; !ok {
				t.Errorf("error envelope missing key: %s", key)
			}
		}

		// Status must be "error".
		if status, _ := errObj["status"].(string); status != "error" {
			t.Errorf("error.status = %q, want \"error\"", status)
		}
	})

	// Verify meta commands output shape.
	t.Run("meta_commands_shape", func(t *testing.T) {
		stdout, _, exitCode := runDcx(t, "meta", "commands", "--format", "json")
		if exitCode != 0 {
			t.Fatal("meta commands failed")
		}

		var commands []map[string]interface{}
		if err := json.Unmarshal([]byte(stdout), &commands); err != nil {
			t.Fatalf("meta commands output is not a JSON array: %v", err)
		}

		if len(commands) == 0 {
			t.Fatal("meta commands returned empty array")
		}

		// Each command entry must have command, domain, description.
		for _, cmd := range commands {
			for _, key := range []string{"command", "domain", "description"} {
				if _, ok := cmd[key]; !ok {
					t.Errorf("command entry missing key %q: %v", key, cmd)
					break
				}
			}
		}
	})

	// Verify auth check output shape.
	t.Run("auth_check_shape", func(t *testing.T) {
		stdout, _, exitCode := runDcx(t, "auth", "check", "--token", "eval-token", "--format", "json")
		if exitCode != 0 {
			t.Fatal("auth check failed")
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("auth check is not valid JSON: %v", err)
		}

		for _, key := range []string{"authenticated", "method", "source"} {
			if _, ok := result[key]; !ok {
				t.Errorf("auth check output missing key: %s", key)
			}
		}
	})

	// Verify contract shape for a sample command.
	t.Run("contract_shape", func(t *testing.T) {
		stdout, _, exitCode := runDcx(t, "meta", "describe", "datasets", "list", "--format", "json")
		if exitCode != 0 {
			t.Fatal("meta describe datasets list failed")
		}

		var contract map[string]interface{}
		if err := json.Unmarshal([]byte(stdout), &contract); err != nil {
			t.Fatalf("contract is not valid JSON: %v", err)
		}

		requiredKeys := []string{
			"contract_version", "command", "domain", "description",
			"flags", "exit_codes", "supports_dry_run", "is_mutation", "formats",
		}
		for _, key := range requiredKeys {
			if _, ok := contract[key]; !ok {
				t.Errorf("contract missing key: %s", key)
			}
		}

		if contract["contract_version"] != "1" {
			t.Errorf("contract_version = %v, want \"1\"", contract["contract_version"])
		}
	})
}
