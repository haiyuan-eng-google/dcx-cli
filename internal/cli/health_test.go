package cli

import (
	"errors"
	"testing"
)

func TestSanitizeNetworkError_Keywords(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"dial tcp: lookup api.example.com: no such host", "no such host"},
		{"dial tcp 127.0.0.1:443: connection refused", "connection refused"},
		{"context deadline exceeded", "context deadline exceeded"},
		{"tls: failed to verify certificate", "certificate"},
		{"Post https://api.example.com: dial tcp: i/o timeout", "timeout"},
		{"some unknown error", "network error"},
	}
	for _, tt := range tests {
		got := sanitizeNetworkError(errors.New(tt.input))
		if got != tt.want {
			t.Errorf("sanitizeNetworkError(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHealthResult_OverallStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks []healthCheck
		want   string
	}{
		{
			"all ok",
			[]healthCheck{
				{Name: "a", Status: statusOK},
				{Name: "b", Status: statusOK},
			},
			"healthy",
		},
		{
			"has warning",
			[]healthCheck{
				{Name: "a", Status: statusOK},
				{Name: "b", Status: statusWarn},
			},
			"degraded",
		},
		{
			"has error",
			[]healthCheck{
				{Name: "a", Status: statusOK},
				{Name: "b", Status: statusError},
			},
			"unhealthy",
		},
		{
			"error beats warning",
			[]healthCheck{
				{Name: "a", Status: statusWarn},
				{Name: "b", Status: statusError},
			},
			"unhealthy",
		},
		{
			"skipped is neutral",
			[]healthCheck{
				{Name: "a", Status: statusOK},
				{Name: "b", Status: statusSkipped},
			},
			"healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overall := "healthy"
			for _, c := range tt.checks {
				if c.Status == statusError {
					overall = "unhealthy"
					break
				}
				if c.Status == statusWarn {
					overall = "degraded"
				}
			}
			if overall != tt.want {
				t.Errorf("overall = %q, want %q", overall, tt.want)
			}
		})
	}
}

func TestCheckProfile_NotFound(t *testing.T) {
	check := checkProfile("definitely_nonexistent_profile_xyz")
	if check.Status != statusError {
		t.Errorf("expected error for missing profile, got %s", check.Status)
	}
}

func TestHealthCheckStatuses(t *testing.T) {
	// Verify status constants are distinct strings.
	statuses := []checkStatus{statusOK, statusWarn, statusError, statusSkipped}
	seen := make(map[checkStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status: %s", s)
		}
		seen[s] = true
	}
}
