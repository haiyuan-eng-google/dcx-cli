package discovery

import (
	"os"
	"testing"
)

func TestParseFlatPathSegments(t *testing.T) {
	tests := []struct {
		name     string
		flatPath string
		want     []pathSegment
	}{
		{
			name:     "spanner instances (top-level)",
			flatPath: "v1/projects/{projectsId}/instances",
			want:     []pathSegment{{Resource: "projects", IDKey: "projectsId"}},
		},
		{
			name:     "spanner databases (nested under instances)",
			flatPath: "v1/projects/{projectsId}/instances/{instancesId}/databases",
			want: []pathSegment{
				{Resource: "projects", IDKey: "projectsId"},
				{Resource: "instances", IDKey: "instancesId"},
			},
		},
		{
			name:     "spanner databases get (nested with leaf ID)",
			flatPath: "v1/projects/{projectsId}/instances/{instancesId}/databases/{databasesId}",
			want: []pathSegment{
				{Resource: "projects", IDKey: "projectsId"},
				{Resource: "instances", IDKey: "instancesId"},
				{Resource: "databases", IDKey: "databasesId"},
			},
		},
		{
			name:     "alloydb instances (3-level nesting)",
			flatPath: "v1/projects/{projectsId}/locations/{locationsId}/clusters/{clustersId}/instances",
			want: []pathSegment{
				{Resource: "projects", IDKey: "projectsId"},
				{Resource: "locations", IDKey: "locationsId"},
				{Resource: "clusters", IDKey: "clustersId"},
			},
		},
		{
			name:     "cloudsql databases (simple param names)",
			flatPath: "v1/projects/{project}/instances/{instance}/databases",
			want: []pathSegment{
				{Resource: "projects", IDKey: "project"},
				{Resource: "instances", IDKey: "instance"},
			},
		},
		{
			name:     "bigquery datasets (no version prefix)",
			flatPath: "projects/{projectsId}/datasets",
			want:     []pathSegment{{Resource: "projects", IDKey: "projectsId"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFlatPathSegments(tt.flatPath)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d segments, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("segment[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDeriveFlagName(t *testing.T) {
	tests := []struct {
		resource string
		want     string
	}{
		{"instances", "instance-id"},
		{"clusters", "cluster-id"},
		{"databases", "database-id"},
		{"backups", "backup-id"},
		{"projects", "project-id"},
		{"locations", "location-id"},
	}
	for _, tt := range tests {
		got := deriveFlagName(tt.resource)
		if got != tt.want {
			t.Errorf("deriveFlagName(%q) = %q, want %q", tt.resource, got, tt.want)
		}
	}
}

func TestBuildParentFlagMap(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     map[string]string
	}{
		{
			name:     "spanner",
			template: "projects/{project-id}",
			want:     map[string]string{"projectsId": "project-id"},
		},
		{
			name:     "alloydb",
			template: "projects/{project-id}/locations/{location}",
			want: map[string]string{
				"projectsId":  "project-id",
				"locationsId": "location",
			},
		},
		{
			name:     "empty",
			template: "",
			want:     nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildParentFlagMap(tt.template)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d: %v", len(got), len(tt.want), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestIsFullResourcePathParam(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"^projects/[^/]+/instances/[^/]+$", true},
		{"^[^/]+$", false},
		{"-", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isFullResourcePathParam(tt.pattern)
		if got != tt.want {
			t.Errorf("isFullResourcePathParam(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}

// TestSpannerDatabasesListInfersInstanceIdFlag verifies that parsing Spanner's
// databases.list method adds an instance-id CLI flag from the flatPath.
func TestSpannerDatabasesListInfersInstanceIdFlag(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/spanner_v1_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := SpannerConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var dbListCmd *GeneratedCommand
	for i := range commands {
		if commands[i].CommandPath == "spanner databases list" {
			dbListCmd = &commands[i]
			break
		}
	}
	if dbListCmd == nil {
		t.Fatal("spanner databases list not found")
	}

	hasInstanceId := false
	for _, f := range dbListCmd.CommandFlags {
		if f.Name == "instance-id" {
			hasInstanceId = true
			if !f.Required {
				t.Error("instance-id should be required")
			}
			if f.Location != "path" {
				t.Errorf("instance-id location = %q, want path", f.Location)
			}
		}
	}
	if !hasInstanceId {
		names := flagNames(dbListCmd.CommandFlags)
		t.Errorf("spanner databases list must have instance-id flag; got: %v", names)
	}
}

// TestSpannerDatabasesGetInfersFlags verifies that databases.get gets both
// instance-id and database-id flags, and does NOT expose the full-path "name" param.
func TestSpannerDatabasesGetInfersFlags(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/spanner_v1_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := SpannerConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var dbGetCmd *GeneratedCommand
	for i := range commands {
		if commands[i].CommandPath == "spanner databases get" {
			dbGetCmd = &commands[i]
			break
		}
	}
	if dbGetCmd == nil {
		t.Fatal("spanner databases get not found")
	}

	flags := make(map[string]bool)
	for _, f := range dbGetCmd.CommandFlags {
		flags[f.Name] = true
	}

	if !flags["instance-id"] {
		t.Errorf("missing instance-id flag; got: %v", flagNames(dbGetCmd.CommandFlags))
	}
	if !flags["database-id"] {
		t.Errorf("missing database-id flag; got: %v", flagNames(dbGetCmd.CommandFlags))
	}
	if flags["name"] {
		t.Error("full-resource-path 'name' param should not be exposed as a CLI flag")
	}
}

// TestAlloyDBInstancesListInfersClusterIdFlag verifies that AlloyDB instances.list
// gets a cluster-id flag from the flatPath.
func TestAlloyDBInstancesListInfersClusterIdFlag(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/alloydb_v1_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := AlloyDBConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var instListCmd *GeneratedCommand
	for i := range commands {
		if commands[i].CommandPath == "alloydb instances list" {
			instListCmd = &commands[i]
			break
		}
	}
	if instListCmd == nil {
		t.Fatal("alloydb instances list not found")
	}

	hasClusterId := false
	for _, f := range instListCmd.CommandFlags {
		if f.Name == "cluster-id" {
			hasClusterId = true
			if !f.Required {
				t.Error("cluster-id should be required")
			}
		}
	}
	if !hasClusterId {
		t.Errorf("alloydb instances list must have cluster-id flag; got: %v",
			flagNames(instListCmd.CommandFlags))
	}
}

// TestCloudSQLDatabasesListKeepsInstanceFlag verifies that CloudSQL's existing
// individual "instance" param is NOT replaced by a derived "instance-id" flag.
func TestCloudSQLDatabasesListKeepsInstanceFlag(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/sqladmin_v1_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := CloudSQLConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var dbListCmd *GeneratedCommand
	for i := range commands {
		if commands[i].CommandPath == "cloudsql databases list" {
			dbListCmd = &commands[i]
			break
		}
	}
	if dbListCmd == nil {
		t.Fatal("cloudsql databases list not found")
	}

	flags := make(map[string]bool)
	for _, f := range dbListCmd.CommandFlags {
		flags[f.Name] = true
	}

	if !flags["instance"] {
		t.Errorf("CloudSQL must keep 'instance' flag; got: %v", flagNames(dbListCmd.CommandFlags))
	}
	if flags["instance-id"] {
		t.Error("CloudSQL should NOT have derived 'instance-id' flag (uses 'instance' directly)")
	}
}

// TestExtractItemsDynamic verifies extractItems handles non-standard keys.
func TestExtractItemsDynamic(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]interface{}
		want int
	}{
		{
			name: "known key: databases",
			raw:  map[string]interface{}{"databases": []interface{}{"a", "b"}, "nextPageToken": "x"},
			want: 2,
		},
		{
			name: "known key: operations",
			raw:  map[string]interface{}{"operations": []interface{}{"op1"}},
			want: 1,
		},
		{
			name: "known key: users",
			raw:  map[string]interface{}{"users": []interface{}{"u1", "u2", "u3"}},
			want: 3,
		},
		{
			name: "unknown key falls back to array scan",
			raw:  map[string]interface{}{"databaseRoles": []interface{}{"role1"}, "nextPageToken": "x"},
			want: 1,
		},
		{
			name: "no array returns raw as single item",
			raw:  map[string]interface{}{"name": "projects/x/databases/y"},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := extractItems(tt.raw)
			if len(items) != tt.want {
				t.Errorf("got %d items, want %d", len(items), tt.want)
			}
		})
	}
}

func flagNames(flags []ApiParam) []string {
	var names []string
	for _, f := range flags {
		names = append(names, f.Name)
	}
	return names
}
