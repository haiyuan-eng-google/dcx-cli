package discovery

import "strings"

// pathSegment represents a resource/{id} pair extracted from a flatPath.
type pathSegment struct {
	Resource string // e.g., "projects", "instances"
	IDKey    string // e.g., "projectsId", "instancesId"
}

// parseFlatPathSegments extracts resource/{id} pairs from a flatPath.
//
// Example: "v1/projects/{projectsId}/instances/{instancesId}/databases"
// returns: [{projects, projectsId}, {instances, instancesId}]
//
// The trailing resource (e.g., "databases") is excluded when it has no ID param.
// If the path ends with a resource/{id} pair, both are included.
func parseFlatPathSegments(flatPath string) []pathSegment {
	parts := strings.Split(flatPath, "/")
	var segments []pathSegment

	for i := 0; i < len(parts)-1; i++ {
		next := parts[i+1]
		if strings.HasPrefix(next, "{") && strings.HasSuffix(next, "}") {
			idKey := strings.Trim(next, "{}")
			segments = append(segments, pathSegment{Resource: parts[i], IDKey: idKey})
			i++ // skip the ID part
		}
	}

	return segments
}

// buildParentFlagMap builds a mapping from flatPath ID keys to CLI flag names
// using the service's ParentTemplate.
//
// Template "projects/{project-id}/locations/{location}" produces:
//
//	{"projectsId": "project-id", "locationsId": "location"}
func buildParentFlagMap(parentTemplate string) map[string]string {
	if parentTemplate == "" {
		return nil
	}
	m := make(map[string]string)
	parts := strings.Split(parentTemplate, "/")
	for i := 0; i+1 < len(parts); i += 2 {
		resource := parts[i]                     // e.g., "projects"
		flagName := strings.Trim(parts[i+1], "{}") // e.g., "project-id"
		flatKey := resource + "Id"               // e.g., "projectsId"
		m[flatKey] = flagName
	}
	return m
}

// deriveFlagName converts a plural resource name to a CLI flag name.
//
//	"instances" → "instance-id"
//	"clusters"  → "cluster-id"
//	"backups"   → "backup-id"
func deriveFlagName(resource string) string {
	singular := resource
	if strings.HasSuffix(singular, "ses") {
		// e.g., "databases" → "database" (not "databas")
		singular = singular[:len(singular)-1]
	} else if strings.HasSuffix(singular, "s") {
		singular = singular[:len(singular)-1]
	}
	return singular + "-id"
}

// isFullResourcePathParam returns true if a parameter's pattern indicates it
// expects a full resource path (e.g., "projects/x/instances/x/databases/x")
// rather than a simple ID value. These params are handled by flatPath segment
// resolution and should not be exposed as individual CLI flags.
//
// Detects literal path separators like "projects/" in the pattern, while
// ignoring "/" inside regex character classes like "[^/]".
func isFullResourcePathParam(pattern string) bool {
	if pattern == "" {
		return false
	}
	// Check for "word/" pattern which indicates a literal resource path segment,
	// e.g., "^projects/[^/]+/instances/[^/]+$" has "projects/" and "instances/".
	// Simple ID patterns like "^[^/]+$" only have "/" inside character classes.
	inCharClass := false
	for i, ch := range pattern {
		switch ch {
		case '[':
			inCharClass = true
		case ']':
			inCharClass = false
		case '/':
			if !inCharClass && i > 0 {
				return true
			}
		}
	}
	return false
}
