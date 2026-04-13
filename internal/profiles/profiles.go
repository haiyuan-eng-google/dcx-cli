// Package profiles manages dcx source profiles for CA and schema commands.
//
// Profiles are YAML files in ~/.config/dcx/profiles/ that configure
// connection details for Data Cloud sources.
package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SourceType identifies the Data Cloud source family.
type SourceType string

const (
	BigQuery    SourceType = "bigquery"
	Spanner     SourceType = "spanner"
	AlloyDB     SourceType = "alloy_db"
	CloudSQL    SourceType = "cloud_sql"
	Looker      SourceType = "looker"
	LookerStudio SourceType = "looker_studio"
)

// ValidSourceTypes lists all supported source types.
var ValidSourceTypes = []SourceType{BigQuery, Spanner, AlloyDB, CloudSQL, Looker, LookerStudio}

// Profile represents a dcx source profile.
type Profile struct {
	Name       string     `yaml:"name" json:"name"`
	SourceType SourceType `yaml:"source_type" json:"source_type"`
	Project    string     `yaml:"project" json:"project"`
	Location   string     `yaml:"location,omitempty" json:"location,omitempty"`
	InstanceID string     `yaml:"instance_id,omitempty" json:"instance_id,omitempty"`
	DatabaseID string     `yaml:"database_id,omitempty" json:"database_id,omitempty"`
	DatasetID  string     `yaml:"dataset_id,omitempty" json:"dataset_id,omitempty"`

	// Looker-specific fields.
	LookerInstanceID string `yaml:"looker_instance_id,omitempty" json:"looker_instance_id,omitempty"`
}

// ProfilesDir returns the default profiles directory path.
func ProfilesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "dcx", "profiles")
}

// LoadAll reads all YAML profile files from the profiles directory.
func LoadAll() ([]Profile, error) {
	dir := ProfilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}

	var profiles []Profile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		p, err := LoadFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading profile %s: %w", name, err)
		}
		profiles = append(profiles, *p)
	}

	return profiles, nil
}

// LoadFile reads a single profile from a YAML file.
func LoadFile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return &p, nil
}

// Validate checks a profile for completeness and correctness.
func (p *Profile) Validate() []string {
	var issues []string

	if p.Name == "" {
		issues = append(issues, "name is required")
	}
	if p.SourceType == "" {
		issues = append(issues, "source_type is required")
	} else if !isValidSourceType(p.SourceType) {
		issues = append(issues, fmt.Sprintf("invalid source_type %q; valid types: %s",
			p.SourceType, strings.Join(sourceTypeNames(), ", ")))
	}
	if p.Project == "" {
		issues = append(issues, "project is required")
	}

	// Source-specific validation.
	switch p.SourceType {
	case Spanner, AlloyDB, CloudSQL:
		if p.InstanceID == "" {
			issues = append(issues, "instance_id is required for "+string(p.SourceType))
		}
	case Looker:
		if p.LookerInstanceID == "" && p.InstanceID == "" {
			issues = append(issues, "looker_instance_id or instance_id is required for looker")
		}
	}

	return issues
}

// ValidationResult holds the result of profile validation.
type ValidationResult struct {
	Name       string   `json:"name"`
	SourceType string   `json:"source_type"`
	Valid      bool     `json:"valid"`
	Issues     []string `json:"issues,omitempty"`
}

func isValidSourceType(st SourceType) bool {
	for _, valid := range ValidSourceTypes {
		if st == valid {
			return true
		}
	}
	return false
}

func sourceTypeNames() []string {
	names := make([]string, len(ValidSourceTypes))
	for i, st := range ValidSourceTypes {
		names[i] = string(st)
	}
	return names
}
