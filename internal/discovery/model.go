// Package discovery implements dynamic command generation from Google Cloud
// Discovery Documents. Commands are generated declaratively from bundled
// JSON, not hand-coded per resource.
package discovery

// ServiceConfig defines how a Discovery Document maps to dcx commands.
type ServiceConfig struct {
	// Namespace is the CLI namespace (e.g. "spanner", "alloydb").
	// Empty string means top-level (BigQuery).
	Namespace string

	// Domain is the contract domain (e.g. "bigquery", "spanner").
	Domain string

	// BaseURL is the API base URL from the Discovery Document.
	BaseURL string

	// AllowedMethods lists the method IDs to generate commands for.
	// Format: "resource.method" (e.g. "datasets.list", "instances.get").
	AllowedMethods []string

	// GlobalParamMappings maps Discovery param names to dcx global flag names.
	// e.g. {"projectId": "project-id", "datasetId": "dataset-id"}
	GlobalParamMappings map[string]string

	// ParentTemplate is used for services that use a "parent" path param.
	// e.g. "projects/{project-id}" for Spanner/AlloyDB/Cloud SQL.
	// If set, the "parent" path param is constructed from global flags.
	ParentTemplate string

	// UseFlatPath controls whether to use flatPath (true) or path (false)
	// for URL construction. Spanner/AlloyDB/Cloud SQL use flatPath.
	UseFlatPath bool
}

// ApiParam describes a parameter from the Discovery Document.
type ApiParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Location    string `json:"location"` // "path" or "query"
	Required    bool   `json:"required"`
	Pattern     string `json:"pattern,omitempty"`
	Format      string `json:"format,omitempty"`
	Repeated    bool   `json:"repeated,omitempty"`
}

// ApiMethod describes an extracted method from the Discovery Document.
type ApiMethod struct {
	ID             string              `json:"id"`       // e.g. "bigquery.datasets.list"
	Resource       string              `json:"resource"` // e.g. "datasets"
	Action         string              `json:"action"`   // e.g. "list"
	Description    string              `json:"description"`
	HTTPMethod     string              `json:"httpMethod"` // "GET", "POST", etc.
	Path           string              `json:"path"`       // URL path template
	FlatPath       string              `json:"flatPath"`   // Simplified path template
	Parameters     map[string]ApiParam `json:"parameters"`
	ParameterOrder []string            `json:"parameterOrder"`
	RequestRef     string              `json:"requestRef,omitempty"` // Schema $ref for request body (e.g. "Dataset")
}

// IsMutation returns true if the method modifies state (non-GET).
func (m ApiMethod) IsMutation() bool {
	return m.HTTPMethod != "GET"
}

// AcceptsBody returns true if the method expects a JSON request body.
func (m ApiMethod) AcceptsBody() bool {
	return m.RequestRef != ""
}

// RequiresConfirmation returns true if the method is destructive (DELETE).
func (m ApiMethod) RequiresConfirmation() bool {
	return m.HTTPMethod == "DELETE"
}

// GeneratedCommand is a fully resolved command ready for CLI registration.
type GeneratedCommand struct {
	// CLI command path: e.g. "datasets list" or "spanner instances list"
	CommandPath string

	// The API method this command executes.
	Method ApiMethod

	// Service config that generated this command.
	Service *ServiceConfig

	// CommandFlags are the non-global flags specific to this command.
	// Global flags (project-id, etc.) are excluded.
	CommandFlags []ApiParam
}
