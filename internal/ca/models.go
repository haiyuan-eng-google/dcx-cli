// Package ca provides the Conversational Analytics client for dcx.
//
// Two API paths:
//   - Chat API (BigQuery, Looker): uses DataAgent to answer questions
//   - QueryData API (Spanner, AlloyDB, Cloud SQL): executes NL-to-SQL
//     queries through the QueryData endpoint
package ca

// ChatRequest is the request body for the CA Chat API (DataAgent).
type ChatRequest struct {
	Question string `json:"question"`
	Agent    string `json:"agent,omitempty"`
	Tables   string `json:"tables,omitempty"`
}

// ChatResponse is the response from the CA Chat API.
type ChatResponse struct {
	Question    string      `json:"question"`
	SQL         string      `json:"sql,omitempty"`
	Results     interface{} `json:"results,omitempty"`
	Explanation string      `json:"explanation,omitempty"`
	Agent       string      `json:"agent,omitempty"`
}

// QueryDataRequest is the request body for the CA QueryData API.
type QueryDataRequest struct {
	Question              string `json:"question"`
	ProjectID             string `json:"project_id"`
	SourceType            string `json:"source_type"`
	Location              string `json:"location,omitempty"`
	InstanceID            string `json:"instance_id,omitempty"`
	DatabaseID            string `json:"database_id,omitempty"`
	ClusterID             string `json:"cluster_id,omitempty"`
	DBType                string `json:"db_type,omitempty"`
	AgentContextReference string `json:"agent_context_reference,omitempty"`
}

// QueryDataResponse is the response from the CA QueryData API.
type QueryDataResponse struct {
	Question    string      `json:"question"`
	SQL         string      `json:"sql,omitempty"`
	Results     interface{} `json:"results,omitempty"`
	Explanation string      `json:"explanation,omitempty"`
	SourceType  string      `json:"source_type"`
}

// VerifiedQuery is a pre-authored question/SQL pair that improves agent accuracy.
type VerifiedQuery struct {
	Question string `json:"question" yaml:"question"`
	Query    string `json:"query" yaml:"query"`
}

// CreateAgentRequest is the request body for creating a data agent.
type CreateAgentRequest struct {
	Name             string          `json:"name"`
	Tables           []string        `json:"tables"`
	Views            []string        `json:"views,omitempty"`
	VerifiedQueries  []VerifiedQuery `json:"verified_queries,omitempty"`
	Instructions     string          `json:"instructions,omitempty"`
}

// AgentSummary is the compact representation for list-agents output.
type AgentSummary struct {
	Name            string   `json:"name"`
	Tables          []string `json:"tables,omitempty"`
	VerifiedQueries int      `json:"verified_queries_count,omitempty"`
	CreateTime      string   `json:"create_time,omitempty"`
}

// AgentsListResult is the output of ca list-agents.
type AgentsListResult struct {
	Items  []AgentSummary `json:"items"`
	Source string         `json:"source"`
}

// CreateAgentResult is the output of ca create-agent.
type CreateAgentResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// AddVerifiedQueryRequest is the request body for adding a verified query.
type AddVerifiedQueryRequest struct {
	Agent    string `json:"agent"`
	Question string `json:"question"`
	Query    string `json:"query"`
}

// AddVerifiedQueryResult is the output of ca add-verified-query.
type AddVerifiedQueryResult struct {
	Agent    string `json:"agent"`
	Question string `json:"question"`
	Status   string `json:"status"`
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
