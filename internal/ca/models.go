// Package ca provides the Conversational Analytics client for dcx.
//
// Two API paths:
//   - Chat API (BigQuery, Looker): uses DataAgent to answer questions
//   - QueryData API (Spanner, AlloyDB, Cloud SQL): executes NL-to-SQL
//     queries through the QueryData endpoint
package ca

// ExampleQuery is a verified question/SQL pair (called "exampleQueries"
// in the Data Agents API) that improves agent accuracy.
// YAML tag supports the user-facing "verified_queries" naming in YAML files.
type ExampleQuery struct {
	NaturalLanguageQuestion string `json:"naturalLanguageQuestion" yaml:"question"`
	SQLQuery                string `json:"sqlQuery" yaml:"query"`
}

// BigQueryTableReference identifies a BigQuery table for agent context.
type BigQueryTableReference struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
	TableID   string `json:"tableId"`
}

// BigQueryTableReferences wraps table references for the datasource field.
type BigQueryTableReferences struct {
	TableReferences []BigQueryTableReference `json:"tableReferences"`
}

// DatasourceReferences is a union type; BigQuery is the only variant
// supported for create-agent.
type DatasourceReferences struct {
	BQ *BigQueryTableReferences `json:"bq,omitempty"`
}

// AgentContext holds the data context for a DataAnalyticsAgent.
// This maps to stagingContext / publishedContext in the API.
type AgentContext struct {
	SystemInstruction    string                `json:"systemInstruction,omitempty"`
	DatasourceReferences *DatasourceReferences `json:"datasourceReferences,omitempty"`
	ExampleQueries       []ExampleQuery        `json:"exampleQueries,omitempty"`
}

// DataAnalyticsAgent is the typed agent payload nested inside DataAgent.
type DataAnalyticsAgent struct {
	StagingContext   *AgentContext `json:"stagingContext,omitempty"`
	PublishedContext *AgentContext `json:"publishedContext,omitempty"`
}

// DataAgent is the top-level resource for the Data Agents management API.
// Docs: https://docs.cloud.google.com/gemini/data-agents/reference/rest/v1alpha/projects.locations.dataAgents
type DataAgent struct {
	Name               string              `json:"name,omitempty"`
	DisplayName        string              `json:"displayName,omitempty"`
	Description        string              `json:"description,omitempty"`
	DataAnalyticsAgent *DataAnalyticsAgent  `json:"dataAnalyticsAgent,omitempty"`
	CreateTime         string              `json:"createTime,omitempty"`
	UpdateTime         string              `json:"updateTime,omitempty"`
}

// CreateAgentOpts holds the user-facing options for ca create-agent.
// These are mapped into the DataAgent API shape by the client.
type CreateAgentOpts struct {
	AgentID      string         // passed as ?dataAgentId= query param
	DisplayName  string
	Tables       []string       // "project.dataset.table" refs
	Views        []string       // additional view refs (also as table refs)
	ExampleQueries []ExampleQuery
	Instructions string
}

// AgentSummary is the dcx output representation for a single agent.
type AgentSummary struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name,omitempty"`
	ExampleQueries  int    `json:"example_queries_count,omitempty"`
	CreateTime      string `json:"create_time,omitempty"`
}

// AgentsListResult is the output of ca list-agents.
type AgentsListResult struct {
	Items  []AgentSummary `json:"items"`
	Source string         `json:"source"`
}

// CreateAgentResult is the output of ca create-agent.
// The API returns a long-running Operation; we surface the operation name
// and the agent name from the metadata.
type CreateAgentResult struct {
	OperationName string `json:"operation_name"`
	AgentID       string `json:"agent_id"`
	Status        string `json:"status"`
}

// PatchAgentOpts holds options for patching an agent (used by add-verified-query).
type PatchAgentOpts struct {
	AgentName      string         // full resource name
	ExampleQueries []ExampleQuery // queries to add
}

// AddVerifiedQueryResult is the output of ca add-verified-query.
type AddVerifiedQueryResult struct {
	Agent                string `json:"agent"`
	QueriesAdded         int    `json:"queries_added"`
	TotalVerifiedQueries int    `json:"total_verified_queries"`
	Status               string `json:"status"`
}

// AskResult is the unified output for ca ask across all source types.
type AskResult struct {
	Question    string      `json:"question"`
	SQL         string      `json:"sql,omitempty"`
	Results     interface{} `json:"results,omitempty"`
	Explanation string      `json:"explanation,omitempty"`
	Source      string      `json:"source,omitempty"`
	Agent       string      `json:"agent,omitempty"`
}

// StreamEventType identifies the kind of streaming event from the chat API.
type StreamEventType int

const (
	// EventThinking is an intermediate thinking/progress message.
	EventThinking StreamEventType = iota
	// EventAnswer is the final natural-language response.
	EventAnswer
	// EventSQL is the generated SQL query.
	EventSQL
	// EventResult is the query result data.
	EventResult
)

// StreamEvent represents a single incremental event from the chat API.
type StreamEvent struct {
	Type StreamEventType
	Text string // text content (thinking step, answer, or SQL)
}

// StreamCallback is called for each event during streaming chat responses.
type StreamCallback func(event StreamEvent)
