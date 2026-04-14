package ca

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
)

const (
	// CA Chat API endpoint (BigQuery DataAgent).
	chatAPIURL = "https://datacatalog.googleapis.com/v1/projects/%s/locations/%s/dataAgents:chat"

	// CA QueryData API endpoint (Spanner, AlloyDB, Cloud SQL).
	queryDataAPIURL = "https://datacatalog.googleapis.com/v1/projects/%s/locations/%s:queryData"

	// Data Agents management API (v1alpha).
	// Docs: https://docs.cloud.google.com/gemini/data-agents/reference/rest/v1alpha/projects.locations.dataAgents
	dataAgentsBaseURL = "https://geminidataanalytics.googleapis.com/v1alpha/projects/%s/locations/%s/dataAgents"
	dataAgentURL      = "https://geminidataanalytics.googleapis.com/v1alpha/%s" // takes full resource name
)

// Client provides access to the Conversational Analytics APIs.
type Client struct {
	HTTPClient *http.Client
}

// NewClient creates a CA client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{HTTPClient: httpClient}
}

// Ask routes a question to the appropriate CA API based on profile source type.
func (c *Client) Ask(ctx context.Context, token string, profile *profiles.Profile, question, agent, tables string) (*AskResult, error) {
	if profile != nil && profile.IsQueryDataSource() {
		return c.askQueryData(ctx, token, profile, question)
	}
	return c.askChat(ctx, token, profile, question, agent, tables)
}

// askChat sends a question to the CA Chat API (BigQuery/Looker DataAgent).
func (c *Client) askChat(ctx context.Context, token string, profile *profiles.Profile, question, agent, tables string) (*AskResult, error) {
	projectID := ""
	location := "us" // default location for CA
	if profile != nil {
		projectID = profile.Project
		if profile.Location != "" {
			location = profile.Location
		}
	}
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required for ca ask")
	}

	url := fmt.Sprintf(chatAPIURL, projectID, location)

	reqBody := map[string]interface{}{
		"question": question,
	}
	if agent != "" {
		reqBody["agent"] = agent
	}
	if tables != "" {
		reqBody["tables"] = tables
	}
	// Looker profile: pass explores context.
	if profile != nil && profile.SourceType == profiles.Looker {
		if profile.LookerInstanceURL != "" {
			reqBody["looker_instance_url"] = profile.LookerInstanceURL
		}
		if len(profile.LookerExplores) > 0 {
			reqBody["looker_explores"] = profile.LookerExplores
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading chat response: %w", err)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("parsing chat response: %w", err)
	}

	source := "BigQuery"
	if profile != nil && profile.SourceType == profiles.Looker {
		source = "Looker"
	}

	return &AskResult{
		Question:    chatResp.Question,
		SQL:         chatResp.SQL,
		Results:     chatResp.Results,
		Explanation: chatResp.Explanation,
		Source:      source,
		Agent:       chatResp.Agent,
	}, nil
}

// askQueryData sends a question to the CA QueryData API (Spanner/AlloyDB/CloudSQL).
func (c *Client) askQueryData(ctx context.Context, token string, profile *profiles.Profile, question string) (*AskResult, error) {
	if profile.Project == "" {
		return nil, fmt.Errorf("project is required in profile")
	}

	location := profile.Location
	if location == "" {
		location = "us"
	}

	url := fmt.Sprintf(queryDataAPIURL, profile.Project, location)

	reqBody := QueryDataRequest{
		Question:   question,
		ProjectID:  profile.Project,
		SourceType: string(profile.SourceType),
		Location:   profile.Location,
		InstanceID: profile.InstanceID,
		DatabaseID: profile.DatabaseID,
		ClusterID:  profile.ClusterID,
		DBType:     profile.DBType,
	}
	if profile.ContextSetID != "" {
		reqBody.AgentContextReference = profile.ContextSetID
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling querydata request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating querydata request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querydata API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading querydata response: %w", err)
	}

	var qdResp QueryDataResponse
	if err := json.Unmarshal(respBody, &qdResp); err != nil {
		return nil, fmt.Errorf("parsing querydata response: %w", err)
	}

	return &AskResult{
		Question:    qdResp.Question,
		SQL:         qdResp.SQL,
		Results:     qdResp.Results,
		Explanation: qdResp.Explanation,
		Source:      sourceName(profile.SourceType),
	}, nil
}

// AskQueryDataRaw executes a raw SQL-like question via QueryData and returns
// the raw response. Used by database helpers (schema describe, databases list).
func (c *Client) AskQueryDataRaw(ctx context.Context, token string, profile *profiles.Profile, question string) (map[string]interface{}, error) {
	if profile.Project == "" {
		return nil, fmt.Errorf("project is required in profile")
	}

	location := profile.Location
	if location == "" {
		location = "us"
	}

	url := fmt.Sprintf(queryDataAPIURL, profile.Project, location)

	reqBody := QueryDataRequest{
		Question:   question,
		ProjectID:  profile.Project,
		SourceType: string(profile.SourceType),
		Location:   profile.Location,
		InstanceID: profile.InstanceID,
		DatabaseID: profile.DatabaseID,
		ClusterID:  profile.ClusterID,
		DBType:     profile.DBType,
	}
	if profile.ContextSetID != "" {
		reqBody.AgentContextReference = profile.ContextSetID
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling querydata request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating querydata request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querydata API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading querydata response: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("parsing querydata response: %w", err)
	}
	return raw, nil
}

// CreateAgent creates a new BigQuery data agent via the Data Agents v1alpha API.
// The API returns a long-running Operation; we surface the operation name.
// Docs: https://docs.cloud.google.com/gemini/data-agents/reference/rest/v1alpha/projects.locations.dataAgents/create
func (c *Client) CreateAgent(ctx context.Context, token, projectID, location string, opts CreateAgentOpts) (*CreateAgentResult, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	if location == "" {
		location = "us"
	}

	// Build the DataAgent request body with the documented nested structure.
	tableRefs := make([]BigQueryTableReference, 0, len(opts.Tables)+len(opts.Views))
	for _, ref := range append(opts.Tables, opts.Views...) {
		tableRefs = append(tableRefs, parseBQTableRef(ref))
	}

	agentBody := DataAgent{
		DisplayName: opts.AgentID,
		DataAnalyticsAgent: &DataAnalyticsAgent{
			StagingContext: &AgentContext{
				SystemInstruction: opts.Instructions,
				DatasourceReferences: &DatasourceReferences{
					BQ: &BigQueryTableReferences{
						TableReferences: tableRefs,
					},
				},
				ExampleQueries: opts.ExampleQueries,
			},
		},
	}

	body, err := json.Marshal(agentBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling create-agent request: %w", err)
	}

	// dataAgentId is a query parameter, not in the body.
	apiURL := fmt.Sprintf(dataAgentsBaseURL, projectID, location)
	if opts.AgentID != "" {
		apiURL += "?dataAgentId=" + opts.AgentID
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create-agent API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	// Response is a long-running Operation.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading create-agent response: %w", err)
	}

	var operation map[string]interface{}
	if err := json.Unmarshal(respBody, &operation); err != nil {
		return nil, fmt.Errorf("parsing create-agent response: %w", err)
	}

	opName, _ := operation["name"].(string)

	return &CreateAgentResult{
		OperationName: opName,
		AgentID:       opts.AgentID,
		Status:        "operation_started",
	}, nil
}

// ListAgents lists all data agents in the given project, handling pagination.
// Docs: https://docs.cloud.google.com/gemini/data-agents/reference/rest/v1alpha/projects.locations.dataAgents/list
func (c *Client) ListAgents(ctx context.Context, token, projectID, location string) (*AgentsListResult, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	if location == "" {
		location = "us"
	}

	baseURL := fmt.Sprintf(dataAgentsBaseURL, projectID, location)
	var allAgents []AgentSummary
	pageToken := ""

	for {
		apiURL := baseURL
		if pageToken != "" {
			apiURL += "?pageToken=" + pageToken
		}

		httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Accept", "application/json")

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("list-agents API request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("list-agents: %s", extractErrorMessage(respBody, resp.StatusCode))
		}

		if err != nil {
			return nil, fmt.Errorf("reading list-agents response: %w", err)
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(respBody, &raw); err != nil {
			return nil, fmt.Errorf("parsing list-agents response: %w", err)
		}

		if items, ok := raw["dataAgents"].([]interface{}); ok {
			for _, item := range items {
				if m, ok := item.(map[string]interface{}); ok {
					allAgents = append(allAgents, parseAgentSummary(m))
				}
			}
		}

		// Check for next page.
		npt, _ := raw["nextPageToken"].(string)
		if npt == "" {
			break
		}
		pageToken = npt
	}

	if allAgents == nil {
		allAgents = []AgentSummary{}
	}

	return &AgentsListResult{
		Items:  allAgents,
		Source: "BigQuery",
	}, nil
}

func extractErrorMessage(body []byte, statusCode int) string {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		return apiErr.Error.Message
	}
	return fmt.Sprintf("API returned HTTP %d", statusCode)
}

// AddVerifiedQuery adds example queries to an existing data agent via PATCH.
// There is no standalone addVerifiedQuery method in the API; verified queries
// (exampleQueries) are fields on the DataAgent resource updated via patch.
// Docs: https://docs.cloud.google.com/gemini/data-agents/reference/rest/v1alpha/projects.locations.dataAgents
func (c *Client) AddVerifiedQuery(ctx context.Context, token, projectID, location string, opts PatchAgentOpts) (*AddVerifiedQueryResult, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	if location == "" {
		location = "us"
	}

	// First GET the existing agent to read current exampleQueries.
	agentResourceName := fmt.Sprintf("projects/%s/locations/%s/dataAgents/%s", projectID, location, opts.AgentName)
	getURL := fmt.Sprintf(dataAgentURL, agentResourceName)

	getReq, err := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating get request: %w", err)
	}
	getReq.Header.Set("Authorization", "Bearer "+token)
	getReq.Header.Set("Accept", "application/json")

	getResp, err := c.HTTPClient.Do(getReq)
	if err != nil {
		return nil, fmt.Errorf("get agent failed: %w", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode >= 400 {
		return nil, readAPIError(getResp)
	}

	getBody, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading get response: %w", err)
	}

	var existing DataAgent
	if err := json.Unmarshal(getBody, &existing); err != nil {
		return nil, fmt.Errorf("parsing existing agent: %w", err)
	}

	// Append new example queries to staging context.
	if existing.DataAnalyticsAgent == nil {
		existing.DataAnalyticsAgent = &DataAnalyticsAgent{}
	}
	if existing.DataAnalyticsAgent.StagingContext == nil {
		existing.DataAnalyticsAgent.StagingContext = &AgentContext{}
	}
	existing.DataAnalyticsAgent.StagingContext.ExampleQueries = append(
		existing.DataAnalyticsAgent.StagingContext.ExampleQueries,
		opts.ExampleQueries...,
	)

	// PATCH the agent with updated exampleQueries.
	patchBody, err := json.Marshal(existing)
	if err != nil {
		return nil, fmt.Errorf("marshaling patch request: %w", err)
	}

	patchURL := fmt.Sprintf(dataAgentURL, agentResourceName) +
		"?updateMask=dataAnalyticsAgent.stagingContext.exampleQueries"

	patchReq, err := http.NewRequestWithContext(ctx, "PATCH", patchURL, bytes.NewReader(patchBody))
	if err != nil {
		return nil, fmt.Errorf("creating patch request: %w", err)
	}
	patchReq.Header.Set("Authorization", "Bearer "+token)
	patchReq.Header.Set("Content-Type", "application/json")

	patchResp, err := c.HTTPClient.Do(patchReq)
	if err != nil {
		return nil, fmt.Errorf("patch agent failed: %w", err)
	}
	defer patchResp.Body.Close()

	if patchResp.StatusCode >= 400 {
		return nil, readAPIError(patchResp)
	}

	// PATCH returns a long-running Operation, not the updated agent.
	patchRespBody, err := io.ReadAll(patchResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading patch response: %w", err)
	}

	var patchOp map[string]interface{}
	if err := json.Unmarshal(patchRespBody, &patchOp); err != nil {
		return nil, fmt.Errorf("parsing patch response: %w", err)
	}

	opName, _ := patchOp["name"].(string)
	done, _ := patchOp["done"].(bool)

	status := "operation_started"
	if done {
		status = "completed"
	}

	return &AddVerifiedQueryResult{
		Agent:         opts.AgentName,
		QueriesAdded:  len(opts.ExampleQueries),
		OperationName: opName,
		Status:        status,
	}, nil
}

// parseAgentSummary extracts a summary from a raw DataAgent JSON map.
func parseAgentSummary(m map[string]interface{}) AgentSummary {
	agent := AgentSummary{}
	if n, ok := m["name"].(string); ok {
		agent.Name = n
	}
	if d, ok := m["displayName"].(string); ok {
		agent.DisplayName = d
	}
	if t, ok := m["createTime"].(string); ok {
		agent.CreateTime = t
	}
	// Count exampleQueries from stagingContext.
	if daa, ok := m["dataAnalyticsAgent"].(map[string]interface{}); ok {
		for _, ctxKey := range []string{"stagingContext", "publishedContext"} {
			if ctx, ok := daa[ctxKey].(map[string]interface{}); ok {
				if eqs, ok := ctx["exampleQueries"].([]interface{}); ok {
					agent.ExampleQueries = len(eqs)
				}
			}
		}
	}
	return agent
}

// parseBQTableRef parses "project.dataset.table" into a BigQueryTableReference.
func parseBQTableRef(ref string) BigQueryTableReference {
	parts := strings.SplitN(ref, ".", 3)
	switch len(parts) {
	case 3:
		return BigQueryTableReference{ProjectID: parts[0], DatasetID: parts[1], TableID: parts[2]}
	case 2:
		return BigQueryTableReference{DatasetID: parts[0], TableID: parts[1]}
	default:
		return BigQueryTableReference{TableID: ref}
	}
}

func readAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	message := fmt.Sprintf("API returned HTTP %d", resp.StatusCode)
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		message = apiErr.Error.Message
	}
	return fmt.Errorf("%s", message)
}

func sourceName(st profiles.SourceType) string {
	switch st {
	case profiles.BigQuery:
		return "BigQuery"
	case profiles.Spanner:
		return "Spanner"
	case profiles.AlloyDB:
		return "AlloyDB"
	case profiles.CloudSQL:
		return "Cloud SQL"
	case profiles.Looker:
		return "Looker"
	default:
		return string(st)
	}
}
