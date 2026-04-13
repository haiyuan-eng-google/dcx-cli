package contracts

import (
	"encoding/json"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	c := BuildContract("datasets list", "bigquery", "List datasets", nil, false, false)
	reg.Register(c)

	got, ok := reg.Get("dcx datasets list")
	if !ok {
		t.Fatal("expected to find registered contract")
	}
	if got.Command != "dcx datasets list" {
		t.Errorf("Command = %s, want 'dcx datasets list'", got.Command)
	}
	if got.Domain != "bigquery" {
		t.Errorf("Domain = %s, want 'bigquery'", got.Domain)
	}
}

func TestRegistryAll(t *testing.T) {
	reg := NewRegistry()
	reg.Register(BuildContract("tables list", "bigquery", "List tables", nil, false, false))
	reg.Register(BuildContract("datasets list", "bigquery", "List datasets", nil, false, false))

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d contracts, want 2", len(all))
	}
	// Should be sorted by command name.
	if all[0].Command != "dcx datasets list" {
		t.Errorf("First command = %s, want 'dcx datasets list'", all[0].Command)
	}
}

func TestRegistryListCommands(t *testing.T) {
	reg := NewRegistry()
	reg.Register(BuildContract("datasets list", "bigquery", "List datasets", nil, false, false))

	summaries := reg.ListCommands()
	if len(summaries) != 1 {
		t.Fatalf("ListCommands() returned %d, want 1", len(summaries))
	}
	if summaries[0].Command != "dcx datasets list" {
		t.Errorf("Command = %s, want 'dcx datasets list'", summaries[0].Command)
	}
}

func TestRegistryDescribe(t *testing.T) {
	reg := NewRegistry()
	reg.Register(BuildContract("datasets list", "bigquery", "List datasets", nil, false, false))

	// Full path.
	c, err := reg.Describe("dcx datasets list")
	if err != nil {
		t.Fatal(err)
	}
	if c.Command != "dcx datasets list" {
		t.Errorf("Command = %s, want 'dcx datasets list'", c.Command)
	}

	// Short path.
	c, err = reg.Describe("datasets list")
	if err != nil {
		t.Fatal(err)
	}
	if c.Command != "dcx datasets list" {
		t.Errorf("Command = %s, want 'dcx datasets list'", c.Command)
	}

	// Unknown.
	_, err = reg.Describe("nonexistent")
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestBuildContractIncludesGlobalFlags(t *testing.T) {
	extra := []FlagContract{
		{Name: "query", Type: "string", Description: "SQL query", Required: true},
	}
	c := BuildContract("jobs query", "bigquery", "Run a query", extra, false, true)

	if c.ContractVersion != "1" {
		t.Errorf("ContractVersion = %s, want '1'", c.ContractVersion)
	}
	if !c.SupportsDryRun {
		t.Error("SupportsDryRun should be true")
	}

	// Should include global flags + the extra flag.
	globalCount := len(GlobalFlags())
	if len(c.Flags) != globalCount+1 {
		t.Errorf("Flags count = %d, want %d", len(c.Flags), globalCount+1)
	}

	// Find the extra flag.
	found := false
	for _, f := range c.Flags {
		if f.Name == "query" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'query' flag in contract")
	}
}

func TestDefaultExitCodes(t *testing.T) {
	codes := DefaultExitCodes(false)
	if _, ok := codes["5"]; ok {
		t.Error("non-mutation should not have exit code 5")
	}

	codesMut := DefaultExitCodes(true)
	if _, ok := codesMut["5"]; !ok {
		t.Error("mutation should have exit code 5 (conflict)")
	}
}

func TestContractJSONRoundTrip(t *testing.T) {
	c := BuildContract("datasets list", "bigquery", "List datasets", nil, false, false)

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed CommandContract
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if parsed.Command != c.Command {
		t.Errorf("Round-trip Command = %s, want %s", parsed.Command, c.Command)
	}
	if parsed.ContractVersion != "1" {
		t.Errorf("ContractVersion = %s, want '1'", parsed.ContractVersion)
	}
}

func TestMetaContracts(t *testing.T) {
	mc := MetaCommandsContract()
	if mc.Domain != "meta" {
		t.Errorf("meta commands Domain = %s, want 'meta'", mc.Domain)
	}

	md := MetaDescribeContract()
	if md.Domain != "meta" {
		t.Errorf("meta describe Domain = %s, want 'meta'", md.Domain)
	}
}

func TestFindByPrefix(t *testing.T) {
	reg := NewRegistry()
	reg.Register(BuildContract("datasets list", "bigquery", "List datasets", nil, false, false))
	reg.Register(BuildContract("datasets get", "bigquery", "Get a dataset", nil, false, false))
	reg.Register(BuildContract("tables list", "bigquery", "List tables", nil, false, false))

	matches := reg.FindByPrefix("datasets")
	if len(matches) != 2 {
		t.Errorf("FindByPrefix('datasets') returned %d, want 2", len(matches))
	}
}
