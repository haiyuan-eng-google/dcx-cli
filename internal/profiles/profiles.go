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

	// AlloyDB-specific.
	ClusterID string `yaml:"cluster_id,omitempty" json:"cluster_id,omitempty"`

	// Cloud SQL-specific.
	DBType string `yaml:"db_type,omitempty" json:"db_type,omitempty"` // "mysql" or "postgresql"

	// Looker-specific fields.
	LookerInstanceID  string   `yaml:"looker_instance_id,omitempty" json:"looker_instance_id,omitempty"`
	LookerInstanceURL string   `yaml:"looker_instance_url,omitempty" json:"looker_instance_url,omitempty"`
	LookerExplores    []string `yaml:"looker_explores,omitempty" json:"looker_explores,omitempty"`
	LookerClientID    string   `yaml:"looker_client_id,omitempty" json:"looker_client_id,omitempty"`
	LookerClientSecret string  `yaml:"looker_client_secret,omitempty" json:"looker_client_secret,omitempty"`

	// CA-specific fields.
	ContextSetID string `yaml:"context_set_id,omitempty" json:"context_set_id,omitempty"`
}

// IsQueryDataSource returns true if this profile type uses the QueryData API
// (Spanner, AlloyDB, CloudSQL) rather than the Chat/DataAgent API.
func (p *Profile) IsQueryDataSource() bool {
	switch p.SourceType {
	case Spanner, AlloyDB, CloudSQL:
		return true
	default:
		return false
	}
}

// LoadByName loads a profile by name from the profiles directory.
// Resolution order:
//  1. File basename match: name.yaml or name.yml in profiles dir
//  2. YAML name: field match: scan all profiles for matching name field
//  3. Direct file path: treat name as a file path
func LoadByName(name string) (*Profile, error) {
	dir := ProfilesDir()

	// 1. Try file basename: name.yaml, name.yml.
	for _, ext := range []string{".yaml", ".yml"} {
		path := filepath.Join(dir, name+ext)
		if _, err := os.Stat(path); err == nil {
			return LoadFile(path)
		}
	}

	// 2. Search by YAML name: field across all profile files.
	all, err := LoadAll()
	if err == nil {
		for _, p := range all {
			if p.Name == name {
				return &p, nil
			}
		}
	}

	// 3. Try loading the name as a direct file path.
	if _, err := os.Stat(name); err == nil {
		return LoadFile(name)
	}

	return nil, fmt.Errorf("profile not found: %s (searched %s and YAML name: fields)", name, dir)
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
		if p.LookerInstanceURL == "" {
			issues = append(issues, "looker_instance_url is required for looker")
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
