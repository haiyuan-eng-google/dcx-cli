package profiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	content := `name: test-spanner
source_type: spanner
project: my-project
location: us-central1
instance_id: my-instance
database_id: my-db
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if p.Name != "test-spanner" {
		t.Errorf("Name = %s, want 'test-spanner'", p.Name)
	}
	if p.SourceType != Spanner {
		t.Errorf("SourceType = %s, want 'spanner'", p.SourceType)
	}
	if p.Project != "my-project" {
		t.Errorf("Project = %s, want 'my-project'", p.Project)
	}
	if p.InstanceID != "my-instance" {
		t.Errorf("InstanceID = %s, want 'my-instance'", p.InstanceID)
	}
}

func TestValidateValid(t *testing.T) {
	p := Profile{
		Name:       "test",
		SourceType: BigQuery,
		Project:    "my-project",
	}
	issues := p.Validate()
	if len(issues) > 0 {
		t.Errorf("expected no issues, got: %v", issues)
	}
}

func TestValidateMissingFields(t *testing.T) {
	p := Profile{}
	issues := p.Validate()
	if len(issues) < 3 {
		t.Errorf("expected at least 3 issues (name, source_type, project), got %d: %v", len(issues), issues)
	}
}

func TestValidateSpannerRequiresInstanceID(t *testing.T) {
	p := Profile{
		Name:       "test",
		SourceType: Spanner,
		Project:    "my-project",
	}
	issues := p.Validate()
	found := false
	for _, issue := range issues {
		if issue == "instance_id is required for spanner" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected instance_id validation error for spanner, got: %v", issues)
	}
}

func TestValidateInvalidSourceType(t *testing.T) {
	p := Profile{
		Name:       "test",
		SourceType: "invalid",
		Project:    "my-project",
	}
	issues := p.Validate()
	found := false
	for _, issue := range issues {
		if len(issue) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for invalid source_type")
	}
}

func TestLoadAllFromEmptyDir(t *testing.T) {
	// Temporarily override profiles dir.
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")

	profiles, err := LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles from empty dir, got %d", len(profiles))
	}
}
