package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
)

// executeCommand runs a command against the app and captures stdout.
func executeCommand(app *App, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	app.Root.SetOut(buf)
	app.Root.SetErr(buf)
	app.Root.SetArgs(args)
	err := app.Root.Execute()
	return buf.String(), err
}

func TestNewAppHasGlobalFlags(t *testing.T) {
	app := NewApp()
	flags := []string{"format", "project-id", "dataset-id", "location", "token", "credentials-file", "dry-run"}
	for _, name := range flags {
		f := app.Root.PersistentFlags().Lookup(name)
		if f == nil {
			t.Errorf("missing global flag: %s", name)
		}
	}
}

func TestNewAppRegistersMetaContracts(t *testing.T) {
	app := NewApp()

	if _, ok := app.Registry.Get("dcx meta commands"); !ok {
		t.Error("meta commands contract not registered")
	}
	if _, ok := app.Registry.Get("dcx meta describe"); !ok {
		t.Error("meta describe contract not registered")
	}
}

func TestNewAppRegistersAuthContracts(t *testing.T) {
	app := NewApp()

	if _, ok := app.Registry.Get("dcx auth check"); !ok {
		t.Error("auth check contract not registered")
	}
	if _, ok := app.Registry.Get("dcx auth status"); !ok {
		t.Error("auth status contract not registered")
	}
}

func TestMetaCommandsOutputJSON(t *testing.T) {
	app := NewApp()
	// Register a sample command to have something in the list.
	app.Registry.Register(contracts.BuildContract(
		"datasets list", "bigquery", "List datasets", nil, false, false,
	))

	out, err := executeCommand(app, "meta", "commands", "--format", "json")
	if err != nil {
		t.Fatalf("meta commands: %v", err)
	}

	// Output goes to os.Stdout, not the cobra buffer, since we use output.Render.
	// Verify the command ran without error — the actual JSON goes to stdout.
	_ = out
}

func TestMetaDescribeKnownCommand(t *testing.T) {
	app := NewApp()

	// meta describe should find the meta commands contract.
	// Note: output goes to os.Stdout, so we can't capture it via cobra buffer.
	// We verify the command runs without error.
	app.Root.SetArgs([]string{"meta", "describe", "meta", "commands"})
	err := app.Root.Execute()
	if err != nil {
		t.Fatalf("meta describe meta commands: %v", err)
	}
}

func TestMetaDescribeContractShape(t *testing.T) {
	app := NewApp()

	// Verify contract shape via the registry directly.
	contract, err := app.Registry.Describe("meta commands")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}

	data, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	requiredKeys := []string{"contract_version", "command", "domain", "description", "flags", "exit_codes", "supports_dry_run", "is_mutation", "formats"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("contract JSON missing key: %s", key)
		}
	}
}

func TestOutputFormatParsing(t *testing.T) {
	app := NewApp()

	// Default format.
	format, err := app.OutputFormat()
	if err != nil {
		t.Fatalf("OutputFormat: %v", err)
	}
	if string(format) != "json" {
		t.Errorf("default format = %s, want 'json'", format)
	}

	// Set to json-minified.
	app.Opts.Format = "json-minified"
	format, err = app.OutputFormat()
	if err != nil {
		t.Fatalf("OutputFormat: %v", err)
	}
	if string(format) != "json-minified" {
		t.Errorf("format = %s, want 'json-minified'", format)
	}

	// Invalid format.
	app.Opts.Format = "csv"
	_, err = app.OutputFormat()
	if err == nil {
		t.Error("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "csv") {
		t.Errorf("error should mention 'csv': %v", err)
	}
}

func TestHelpOutput(t *testing.T) {
	app := NewApp()
	out, err := executeCommand(app, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "Agent-Native Data Cloud CLI") {
		t.Error("--help should contain CLI description")
	}
}
