package ca

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
)

const (
	// CA base URL — v1beta matches the working Rust implementation.
	caBaseURL = "https://geminidataanalytics.googleapis.com/v1beta"

	// :chat endpoint for BigQuery/Looker/Studio (streaming messages).
	chatURLFmt = caBaseURL + "/projects/%s/locations/%s:chat"

	// :queryData endpoint for database sources (Spanner, AlloyDB, Cloud SQL).
	queryDataURLFmt = caBaseURL + "/projects/%s/locations/%s:queryData"

	// Agent management endpoints (sync variants return result directly).
	dataAgentsURLFmt    = caBaseURL + "/projects/%s/locations/%s/dataAgents"
	dataAgentURLFmt     = caBaseURL + "/%s" // takes full resource name
	createAgentSyncFmt  = caBaseURL + "/projects/%s/locations/%s/dataAgents:createSync?dataAgentId=%s"
	updateAgentSyncFmt  = caBaseURL + "/%s:updateSync?updateMask=%s"
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

// Ask routes a question to the appropriate CA endpoint based on source type.
// BigQuery/Looker/Studio use :chat (streaming messages).
// Spanner/AlloyDB/CloudSQL use :queryData (NL-to-SQL).
// This matches the working Rust implementation.
func (c *Client) Ask(ctx context.Context, token string, profile *profiles.Profile, question, agent, tables string) (*AskResult, error) {
	return c.AskStream(ctx, token, profile, question, agent, tables, nil)
}

// AskStream is like Ask but accepts an optional StreamCallback that is called
// for each message as it arrives from the chat API. This enables real-time
// display of thinking steps and the final answer. If cb is nil, behaves
// identically to Ask. QueryData sources ignore the callback.
func (c *Client) AskStream(ctx context.Context, token string, profile *profiles.Profile, question, agent, tables string, cb StreamCallback) (*AskResult, error) {
	if profile != nil && profile.IsQueryDataSource() {
		return c.askQueryData(ctx, token, profile, question)
	}
	return c.askChat(ctx, token, profile, question, agent, tables, cb)
}

// askChat sends a question to the :chat endpoint (BigQuery/Looker/Studio).
// Uses messages + userMessage + inlineContext structure.
// If cb is non-nil, each message is streamed to the callback as it arrives.
func (c *Client) askChat(ctx context.Context, token string, profile *profiles.Profile, question, agent, tables string, cb StreamCallback) (*AskResult, error) {
	projectID := ""
	location := "us"
	if profile != nil {
		projectID = profile.Project
		if profile.Location != "" {
			location = profile.Location
		}
	}
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required for ca ask")
	}

	userMessage := map[string]interface{}{
		"userMessage": map[string]interface{}{
			"text": question,
		},
	}

	body := map[string]interface{}{
		"messages": []interface{}{userMessage},
	}

	// Agent context (BigQuery with named agent).
	// Agent resources live in locations/global regardless of chat endpoint location.
	if agent != "" {
		agentResource := fmt.Sprintf("projects/%s/locations/global/dataAgents/%s", projectID, agent)
		body["data_agent_context"] = map[string]interface{}{
			"data_agent": agentResource,
		}
	}

	// Inline tables (BigQuery without agent).
	if agent == "" && tables != "" {
		tableRefs := buildBQTableRefs(tables)
		body["inlineContext"] = map[string]interface{}{
			"datasource_references": map[string]interface{}{
				"bq": map[string]interface{}{
					"tableReferences": tableRefs,
				},
			},
		}
	}

	// Looker profile: pass explore references.
	if profile != nil && profile.SourceType == profiles.Looker {
		exploreRefs := buildLookerExploreRefs(profile)
		body["inlineContext"] = map[string]interface{}{
			"datasourceReferences": map[string]interface{}{
				"looker": map[string]interface{}{
					"exploreReferences": exploreRefs,
				},
			},
		}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling chat request: %w", err)
	}

	apiURL := fmt.Sprintf(chatURLFmt, projectID, location)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-server-timeout", "300")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	// Parse the streaming response, invoking cb for each message if provided.
	result, err := parseStreamingChat(resp.Body, question, agent, cb)
	if err != nil {
		return nil, err
	}

	if profile != nil {
		result.Source = sourceName(profile.SourceType)
	} else {
		result.Source = "BigQuery"
	}

	return result, nil
}

// askQueryData sends a question to the :queryData endpoint (Spanner/AlloyDB/CloudSQL).
// Uses prompt + context.datasourceReferences structure.
func (c *Client) askQueryData(ctx context.Context, token string, profile *profiles.Profile, question string) (*AskResult, error) {
	if profile.Project == "" {
		return nil, fmt.Errorf("project is required in profile")
	}

	location := profile.Location
	if location == "" {
		location = "us"
	}

	datasourceRef := buildQueryDataDatasourceRef(profile)

	body := map[string]interface{}{
		"prompt": question,
		"context": map[string]interface{}{
			"datasourceReferences": datasourceRef,
		},
		"generationOptions": map[string]interface{}{
			"generateQueryResult":           true,
			"generateNaturalLanguageAnswer": true,
			"generateExplanation":           true,
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling queryData request: %w", err)
	}

	apiURL := fmt.Sprintf(queryDataURLFmt, profile.Project, location)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating queryData request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-server-timeout", "300")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("queryData API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading queryData response: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("parsing queryData response: %w", err)
	}

	sql, _ := raw["generatedQuery"].(string)
	explanation, _ := raw["intentExplanation"].(string)
	nlAnswer, _ := raw["naturalLanguageAnswer"].(string)

	result := &AskResult{
		Question:    question,
		SQL:         sql,
		Explanation: explanation,
		Source:      sourceName(profile.SourceType),
	}
	if nlAnswer != "" {
		result.Explanation = nlAnswer
	}
	if qr, ok := raw["queryResult"]; ok {
		result.Results = qr
	}

	return result, nil
}

// AskQueryDataRaw executes a question via queryData and returns the raw
// response. Used by database helpers (schema describe, databases list).
func (c *Client) AskQueryDataRaw(ctx context.Context, token string, profile *profiles.Profile, question string) (map[string]interface{}, error) {
	if profile.Project == "" {
		return nil, fmt.Errorf("project is required in profile")
	}

	location := profile.Location
	if location == "" {
		location = "us"
	}

	reqBody := map[string]interface{}{
		"prompt": question,
		"context": map[string]interface{}{
			"datasourceReferences": buildQueryDataDatasourceRef(profile),
		},
		"generationOptions": map[string]interface{}{
			"generateQueryResult": true,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling queryData request: %w", err)
	}

	apiURL := fmt.Sprintf(queryDataURLFmt, profile.Project, location)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating queryData request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("queryData API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading queryData response: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("parsing queryData response: %w", err)
	}
	return raw, nil
}

// CreateAgent creates a data agent synchronously via :createSync.
// Uses publishedContext (not stagingContext) matching the Rust implementation.
// Agent management always uses locations/global; regional locations are rejected by the API.
func (c *Client) CreateAgent(ctx context.Context, token, projectID, _ string, opts CreateAgentOpts) (*CreateAgentResult, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	location := "global"

	tableRefs := buildBQTableRefs(strings.Join(opts.Tables, ","))
	for _, v := range opts.Views {
		tableRefs = append(tableRefs, parseBQTableRefMap(v))
	}

	publishedContext := map[string]interface{}{
		"datasourceReferences": map[string]interface{}{
			"bq": map[string]interface{}{
				"tableReferences": tableRefs,
			},
		},
		"exampleQueries": opts.ExampleQueries,
	}
	if opts.Instructions != "" {
		publishedContext["systemInstruction"] = opts.Instructions
	}

	agentBody := map[string]interface{}{
		"displayName": opts.DisplayName,
		"dataAnalyticsAgent": map[string]interface{}{
			"publishedContext": publishedContext,
		},
	}

	body, err := json.Marshal(agentBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling create-agent request: %w", err)
	}

	apiURL := fmt.Sprintf(createAgentSyncFmt, projectID, location, opts.AgentID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-server-timeout", "300")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create-agent API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readAPIError(resp)
	}

	// :createSync returns the DataAgent directly (not an Operation).
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading create-agent response: %w", err)
	}

	var agent map[string]interface{}
	if err := json.Unmarshal(respBody, &agent); err != nil {
		return nil, fmt.Errorf("parsing create-agent response: %w", err)
	}

	name, _ := agent["name"].(string)

	return &CreateAgentResult{
		OperationName: name,
		AgentID:       opts.AgentID,
		Status:        "created",
	}, nil
}

// ListAgents lists all data agents in the given project, handling pagination.
// Agent management always uses locations/global; regional locations are rejected by the API.
func (c *Client) ListAgents(ctx context.Context, token, projectID, _ string) (*AgentsListResult, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	location := "global"

	baseURL := fmt.Sprintf(dataAgentsURLFmt, projectID, location)
	var allAgents []AgentSummary
	pageToken := ""

	for {
		apiURL := baseURL
		if pageToken != "" {
			apiURL += "?pageToken=" + url.QueryEscape(pageToken)
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

// AddVerifiedQuery appends example queries to an existing agent via GET + :updateSync.
// Uses publishedContext (matching Rust implementation).
// Agent management always uses locations/global; regional locations are rejected by the API.
func (c *Client) AddVerifiedQuery(ctx context.Context, token, projectID, _ string, opts PatchAgentOpts) (*AddVerifiedQueryResult, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	location := "global"

	// 1. GET the existing agent to read current exampleQueries.
	agentResourceName := fmt.Sprintf("projects/%s/locations/%s/dataAgents/%s", projectID, location, opts.AgentName)
	getURL := fmt.Sprintf(dataAgentURLFmt, agentResourceName)

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

	// 2. Parse existing agent and append new exampleQueries to publishedContext.
	var agent map[string]interface{}
	if err := json.Unmarshal(getBody, &agent); err != nil {
		return nil, fmt.Errorf("parsing existing agent: %w", err)
	}

	daa, _ := agent["dataAnalyticsAgent"].(map[string]interface{})
	if daa == nil {
		daa = map[string]interface{}{}
	}
	published, _ := daa["publishedContext"].(map[string]interface{})
	if published == nil {
		published = map[string]interface{}{}
	}

	existingQueries, _ := published["exampleQueries"].([]interface{})
	for _, eq := range opts.ExampleQueries {
		existingQueries = append(existingQueries, map[string]interface{}{
			"naturalLanguageQuestion": eq.NaturalLanguageQuestion,
			"sqlQuery":               eq.SQLQuery,
		})
	}
	published["exampleQueries"] = existingQueries

	// 3. :updateSync with the updated publishedContext.
	updateBody := map[string]interface{}{
		"name": agentResourceName,
		"dataAnalyticsAgent": map[string]interface{}{
			"publishedContext": published,
		},
	}

	patchBody, err := json.Marshal(updateBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling update request: %w", err)
	}

	updateURL := fmt.Sprintf(updateAgentSyncFmt, agentResourceName,
		"dataAnalyticsAgent.publishedContext.exampleQueries")

	patchReq, err := http.NewRequestWithContext(ctx, "PATCH", updateURL, bytes.NewReader(patchBody))
	if err != nil {
		return nil, fmt.Errorf("creating update request: %w", err)
	}
	patchReq.Header.Set("Authorization", "Bearer "+token)
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("x-server-timeout", "300")

	patchResp, err := c.HTTPClient.Do(patchReq)
	if err != nil {
		return nil, fmt.Errorf("updateSync agent failed: %w", err)
	}
	defer patchResp.Body.Close()

	if patchResp.StatusCode >= 400 {
		return nil, readAPIError(patchResp)
	}

	total := len(existingQueries)

	return &AddVerifiedQueryResult{
		Agent:              opts.AgentName,
		QueriesAdded:       len(opts.ExampleQueries),
		TotalVerifiedQueries: total,
		Status:             "added",
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

// buildDatasourceReferences constructs the correct nested datasource
// reference for the queryData API based on the profile's source type.
// buildBQTableRefs parses a comma-separated table list into reference maps.
func buildBQTableRefs(tables string) []map[string]string {
	var refs []map[string]string
	for _, ref := range strings.Split(tables, ",") {
		trimmed := strings.TrimSpace(ref)
		if trimmed != "" {
			refs = append(refs, parseBQTableRefMap(trimmed))
		}
	}
	return refs
}

// buildLookerExploreRefs builds explore reference objects from a profile.
func buildLookerExploreRefs(profile *profiles.Profile) []map[string]interface{} {
	instanceURL := profile.LookerInstanceURL
	explores := profile.LookerExplores
	var refs []map[string]interface{}
	for _, exp := range explores {
		parts := strings.SplitN(exp, "/", 2)
		if len(parts) == 2 {
			ref := map[string]interface{}{
				"lookerInstanceUri": instanceURL,
				"lookmlModel":      parts[0],
				"explore":          parts[1],
			}
			refs = append(refs, ref)
		}
	}
	return refs
}

// buildQueryDataDatasourceRef builds the datasourceReferences for the
// queryData endpoint, matching the Rust implementation's per-source structures.
func buildQueryDataDatasourceRef(profile *profiles.Profile) map[string]interface{} {
	location := profile.Location
	if location == "" {
		location = "us"
	}

	switch profile.SourceType {
	case profiles.AlloyDB:
		inner := map[string]interface{}{
			"databaseReference": map[string]interface{}{
				"projectId":  profile.Project,
				"region":     location,
				"clusterId":  profile.ClusterID,
				"instanceId": profile.InstanceID,
				"databaseId": profile.DatabaseID,
			},
		}
		if profile.ContextSetID != "" {
			inner["agentContextReference"] = map[string]interface{}{
				"contextSetId": fmt.Sprintf("projects/%s/locations/%s/contextSets/%s",
					profile.Project, location, profile.ContextSetID),
			}
		}
		return map[string]interface{}{"alloydb": inner}

	case profiles.Spanner:
		inner := map[string]interface{}{
			"databaseReference": map[string]interface{}{
				"engine":     "GOOGLE_SQL",
				"projectId":  profile.Project,
				"instanceId": profile.InstanceID,
				"databaseId": profile.DatabaseID,
			},
		}
		if profile.ContextSetID != "" {
			inner["agentContextReference"] = map[string]interface{}{
				"contextSetId": fmt.Sprintf("projects/%s/locations/%s/contextSets/%s",
					profile.Project, location, profile.ContextSetID),
			}
		}
		return map[string]interface{}{"spannerReference": inner}

	case profiles.CloudSQL:
		engine := "POSTGRESQL"
		if profile.DBType == "mysql" {
			engine = "MYSQL"
		}
		inner := map[string]interface{}{
			"databaseReference": map[string]interface{}{
				"engine":     engine,
				"projectId":  profile.Project,
				"region":     location,
				"instanceId": profile.InstanceID,
				"databaseId": profile.DatabaseID,
			},
		}
		if profile.ContextSetID != "" {
			inner["agentContextReference"] = map[string]interface{}{
				"contextSetId": fmt.Sprintf("projects/%s/locations/%s/contextSets/%s",
					profile.Project, location, profile.ContextSetID),
			}
		}
		return map[string]interface{}{"cloudSqlReference": inner}

	default:
		return nil
	}
}

// parseStreamingChat reads the chat response body and dispatches each message
// to the callback as it arrives from the network. Handles both wire formats:
//   - JSON array: streamed incrementally via json.NewDecoder (common case)
//   - Newline-delimited: read line by line from a bufio.Scanner (fallback)
//
// The complete AskResult is returned at the end regardless of format.
func parseStreamingChat(body io.Reader, question, agent string, cb StreamCallback) (*AskResult, error) {
	// Peek at the first non-whitespace byte to detect the wire format
	// without consuming the stream.
	br := bufio.NewReader(body)
	firstByte, err := peekNonSpace(br)
	if err != nil {
		return nil, fmt.Errorf("reading chat response: %w", err)
	}

	var messages []map[string]interface{}

	if firstByte == '[' {
		// JSON array — stream directly from the response body.
		decoder := json.NewDecoder(br)

		// Consume opening '['.
		if _, err := decoder.Token(); err != nil {
			return nil, fmt.Errorf("parsing chat response array: %w", err)
		}
		for decoder.More() {
			var msg map[string]interface{}
			if err := decoder.Decode(&msg); err != nil {
				return nil, fmt.Errorf("parsing chat message: %w", err)
			}
			messages = append(messages, msg)
			dispatchStreamEvent(msg, cb)
		}
	} else {
		// Newline-delimited — read line by line from the response body.
		scanner := bufio.NewScanner(br)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || line == "," {
				continue
			}
			var msg map[string]interface{}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue // skip unparseable lines
			}
			messages = append(messages, msg)
			dispatchStreamEvent(msg, cb)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("reading chat response lines: %w", err)
		}
	}

	return buildAskResult(messages, question, agent), nil
}

// peekNonSpace reads ahead in a bufio.Reader until a non-whitespace byte
// is found, returning that byte without consuming it.
func peekNonSpace(br *bufio.Reader) (byte, error) {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			if err := br.UnreadByte(); err != nil {
				return 0, err
			}
			return b, nil
		}
	}
}

// dispatchStreamEvent sends a single message to the callback if non-nil.
func dispatchStreamEvent(msg map[string]interface{}, cb StreamCallback) {
	if cb == nil {
		return
	}
	sm, ok := msg["systemMessage"].(map[string]interface{})
	if !ok {
		return
	}

	if text, ok := sm["text"].(map[string]interface{}); ok {
		textType, _ := text["textType"].(string)
		parts := extractTextParts(text)
		if len(parts) == 0 {
			return
		}
		switch textType {
		case "FINAL_RESPONSE":
			cb(StreamEvent{Type: EventAnswer, Text: strings.Join(parts, "\n")})
		default:
			cb(StreamEvent{Type: EventThinking, Text: parts[0]})
		}
	}

	if data, ok := sm["data"].(map[string]interface{}); ok {
		if sql, ok := data["generatedSql"].(string); ok {
			cb(StreamEvent{Type: EventSQL, Text: sql})
		}
		if _, ok := data["result"]; ok {
			cb(StreamEvent{Type: EventResult})
		}
	}
}

// buildAskResult extracts the final answer from a list of parsed messages.
func buildAskResult(messages []map[string]interface{}, question, agent string) *AskResult {
	result := &AskResult{
		Question: question,
		Agent:    agent,
	}
	for _, msg := range messages {
		sm, ok := msg["systemMessage"].(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := sm["text"].(map[string]interface{}); ok {
			textType, _ := text["textType"].(string)
			if textType == "FINAL_RESPONSE" {
				for _, s := range extractTextParts(text) {
					if result.Explanation == "" {
						result.Explanation = s
					} else {
						result.Explanation += "\n" + s
					}
				}
			}
		}
		if data, ok := sm["data"].(map[string]interface{}); ok {
			if sql, ok := data["generatedSql"].(string); ok {
				result.SQL = sql
			}
			if qr, ok := data["result"]; ok {
				result.Results = qr
			}
		}
	}
	return result
}

// extractTextParts extracts string parts from a text message.
func extractTextParts(text map[string]interface{}) []string {
	parts, ok := text["parts"].([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, p := range parts {
		if s, ok := p.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// parseChatResponse parses the streaming chat response (JSON array of messages)
// and extracts the CA answer. The response is an array of objects like:
//
//	{"systemMessage": {"text": {"parts": [...], "textType": "FINAL_RESPONSE"}}}
//	{"systemMessage": {"data": {"generatedSql": "SELECT ..."}}}
//	{"systemMessage": {"data": {"result": {"data": [...], "schema": {...}}}}}
func parseChatResponse(body []byte, question, agent string) (*AskResult, error) {
	var messages []map[string]interface{}

	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &messages); err != nil {
			return nil, fmt.Errorf("parsing chat response: %w", err)
		}
	} else {
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "," {
				continue
			}
			var msg map[string]interface{}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}
			messages = append(messages, msg)
		}
	}

	result := &AskResult{
		Question: question,
		Agent:    agent,
	}

	for _, msg := range messages {
		sm, ok := msg["systemMessage"].(map[string]interface{})
		if !ok {
			continue
		}

		// Extract text messages (FINAL_RESPONSE).
		if text, ok := sm["text"].(map[string]interface{}); ok {
			textType, _ := text["textType"].(string)
			if textType == "FINAL_RESPONSE" {
				if parts, ok := text["parts"].([]interface{}); ok {
					for _, p := range parts {
						if s, ok := p.(string); ok {
							if result.Explanation == "" {
								result.Explanation = s
							} else {
								result.Explanation += "\n" + s
							}
						}
					}
				}
			}
		}

		// Extract data messages (generatedSql, result).
		if data, ok := sm["data"].(map[string]interface{}); ok {
			if sql, ok := data["generatedSql"].(string); ok {
				result.SQL = sql
			}
			if qr, ok := data["result"]; ok {
				result.Results = qr
			}
		}
	}

	return result, nil
}

func parseBQTableRefMap(ref string) map[string]string {
	parts := strings.SplitN(ref, ".", 3)
	switch len(parts) {
	case 3:
		return map[string]string{"projectId": parts[0], "datasetId": parts[1], "tableId": parts[2]}
	case 2:
		return map[string]string{"datasetId": parts[0], "tableId": parts[1]}
	default:
		return map[string]string{"tableId": ref}
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
