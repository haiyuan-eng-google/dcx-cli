package discovery

import (
	"testing"
)

func TestSubstitutePath(t *testing.T) {
	tests := []struct {
		name     string
		template string
		params   map[string]string
		want     string
		wantErr  bool
	}{
		{
			name:     "BigQuery path with + prefix",
			template: "projects/{+projectId}/datasets",
			params:   map[string]string{"projectId": "my-project"},
			want:     "projects/my-project/datasets",
		},
		{
			name:     "BigQuery flatPath",
			template: "projects/{projectsId}/datasets",
			params:   map[string]string{"projectsId": "my-project"},
			want:     "projects/my-project/datasets",
		},
		{
			name:     "multiple params",
			template: "projects/{+projectId}/datasets/{+datasetId}",
			params:   map[string]string{"projectId": "proj", "datasetId": "ds"},
			want:     "projects/proj/datasets/ds",
		},
		{
			name:     "unresolved param",
			template: "projects/{+projectId}/datasets/{+datasetId}",
			params:   map[string]string{"projectId": "proj"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePath(tt.template, tt.params)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("substitutePath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildRequestBigQuery(t *testing.T) {
	cmd := GeneratedCommand{
		CommandPath: "datasets list",
		Method: ApiMethod{
			HTTPMethod: "GET",
			Path:       "projects/{+projectId}/datasets",
			FlatPath:   "projects/{projectsId}/datasets",
		},
		Service: &ServiceConfig{
			BaseURL:     "https://bigquery.googleapis.com/bigquery/v2/",
			UseFlatPath: false,
		},
	}

	pathParams := map[string]string{"projectId": "my-project"}
	queryParams := map[string]string{"maxResults": "10"}

	req, err := BuildRequest(cmd, pathParams, queryParams, "test-token", nil)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	expected := "https://bigquery.googleapis.com/bigquery/v2/projects/my-project/datasets?maxResults=10"
	if req.URL.String() != expected {
		t.Errorf("URL = %s, want %s", req.URL.String(), expected)
	}

	if req.Header.Get("Authorization") != "Bearer test-token" {
		t.Error("missing or wrong Authorization header")
	}
}

func TestResolvePathParamsBigQuery(t *testing.T) {
	cmd := GeneratedCommand{
		CommandPath: "datasets list",
		Method: ApiMethod{
			Parameters: map[string]ApiParam{
				"projectId": {Name: "projectId", Location: "path", Required: true},
			},
		},
		Service: BigQueryConfig(),
	}

	flags := map[string]string{
		"project-id": "my-project",
		"dataset-id": "",
		"location":   "",
	}

	params, err := ResolvePathParams(cmd, flags)
	if err != nil {
		t.Fatalf("ResolvePathParams: %v", err)
	}

	if params["projectId"] != "my-project" {
		t.Errorf("projectId = %s, want 'my-project'", params["projectId"])
	}
}

func TestToFlatPathKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"projectId", "projectsId"},
		{"datasetId", "datasetsId"},
		{"instancesId", "instancesId"}, // already plural
	}
	for _, tt := range tests {
		got := toFlatPathKey(tt.input)
		if got != tt.want {
			t.Errorf("toFlatPathKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
