package discovery

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// BuildRequest constructs an HTTP request for a Discovery API method.
func BuildRequest(cmd GeneratedCommand, pathParams map[string]string, queryParams map[string]string, token string) (*http.Request, error) {
	// Choose path template.
	pathTemplate := cmd.Method.Path
	if cmd.Service.UseFlatPath && cmd.Method.FlatPath != "" {
		pathTemplate = cmd.Method.FlatPath
	}

	// Substitute path parameters.
	resolvedPath, err := substitutePath(pathTemplate, pathParams)
	if err != nil {
		return nil, err
	}

	// Build full URL.
	fullURL := cmd.Service.BaseURL + resolvedPath

	// Add query parameters.
	if len(queryParams) > 0 {
		u, err := url.Parse(fullURL)
		if err != nil {
			return nil, fmt.Errorf("parsing URL: %w", err)
		}
		q := u.Query()
		for k, v := range queryParams {
			if v != "" {
				q.Set(k, v)
			}
		}
		u.RawQuery = q.Encode()
		fullURL = u.String()
	}

	req, err := http.NewRequest(cmd.Method.HTTPMethod, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	return req, nil
}

// substitutePath replaces path template variables with actual values.
// Handles both formats:
//   - {+paramName} (Discovery path format with + prefix)
//   - {paramName} or {resourceId} (flatPath format)
func substitutePath(template string, params map[string]string) (string, error) {
	result := template

	// Replace {+paramName} patterns first.
	for name, value := range params {
		placeholder := "{+" + name + "}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, value)
		}
	}

	// Replace {paramName} patterns (flatPath style).
	for name, value := range params {
		placeholder := "{" + name + "}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, value)
		}
	}

	// Check for any remaining unresolved placeholders.
	if idx := strings.Index(result, "{"); idx >= 0 {
		end := strings.Index(result[idx:], "}")
		if end > 0 {
			unresolved := result[idx : idx+end+1]
			return "", fmt.Errorf("unresolved path parameter: %s", unresolved)
		}
	}

	return result, nil
}

// ResolvePathParams builds the path parameter map from global flags and
// the service config's global param mappings.
func ResolvePathParams(cmd GeneratedCommand, globalFlags map[string]string) (map[string]string, error) {
	pathParams := make(map[string]string)

	// Handle "parent" param construction.
	if cmd.Service.ParentTemplate != "" {
		if _, hasParent := cmd.Method.Parameters["parent"]; hasParent {
			parent, err := resolveParentTemplate(cmd.Service.ParentTemplate, globalFlags)
			if err != nil {
				return nil, err
			}
			pathParams["parent"] = parent

			// For flatPath, we also need to expand the template segments.
			// e.g. flatPath "v1/projects/{projectsId}/instances" needs projectsId
			// which comes from the parent "projects/{project-id}"
			if cmd.Service.UseFlatPath {
				expandFlatPathParams(pathParams, cmd.Service.ParentTemplate, globalFlags)
			}
		}
	}

	// Map Discovery param names to global flag values.
	for discoveryParam, flagName := range cmd.Service.GlobalParamMappings {
		if val, ok := globalFlags[flagName]; ok && val != "" {
			pathParams[discoveryParam] = val

			// For flatPath, also set the "{resourcesId}" variant.
			// e.g. projectId -> projectsId (flatPath uses plural + "Id")
			flatKey := toFlatPathKey(discoveryParam)
			pathParams[flatKey] = val
		}
	}

	// Pass through command-specific path params (resource IDs like tableId,
	// databaseId, instance) that were provided as CLI flags and stored
	// in globalFlags by their Discovery param name.
	for paramName, param := range cmd.Method.Parameters {
		if param.Location != "path" {
			continue
		}
		// Skip already-handled params.
		if _, mapped := cmd.Service.GlobalParamMappings[paramName]; mapped {
			continue
		}
		if paramName == "parent" {
			continue
		}
		// Skip full-resource-path params in flatPath services — these are
		// resolved by flatPath segment expansion below.
		if cmd.Service.UseFlatPath && isFullResourcePathParam(param.Pattern) {
			continue
		}
		if val, ok := globalFlags[paramName]; ok && val != "" {
			pathParams[paramName] = val
			flatKey := toFlatPathKey(paramName)
			pathParams[flatKey] = val
		}
	}

	// For flatPath services, resolve ALL {xxxId} segments from the flatPath
	// using global flags. This handles intermediate parents (e.g., instancesId)
	// that aren't in the ParentTemplate.
	if cmd.Service.UseFlatPath && cmd.Method.FlatPath != "" {
		resolveFlatPathSegments(pathParams, cmd.Method.FlatPath, cmd.Service.ParentTemplate, globalFlags)
	}

	return pathParams, nil
}

// resolveParentTemplate substitutes global flag values into a parent template.
// e.g. "projects/{project-id}" with {"project-id": "my-project"} -> "projects/my-project"
func resolveParentTemplate(template string, flags map[string]string) (string, error) {
	result := template
	for name, value := range flags {
		placeholder := "{" + name + "}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	if strings.Contains(result, "{") {
		return "", fmt.Errorf("unresolved parent template: %s (available flags: %v)", result, flags)
	}
	return result, nil
}

// expandFlatPathParams extracts resource IDs from the parent template for flatPath.
// e.g. template "projects/{project-id}/locations/{location}" with flags
// produces: projectsId -> value, locationsId -> value
func expandFlatPathParams(params map[string]string, template string, flags map[string]string) {
	parts := strings.Split(template, "/")
	for i := 0; i+1 < len(parts); i += 2 {
		resource := parts[i]    // e.g. "projects"
		flagRef := parts[i+1]   // e.g. "{project-id}"
		flagName := strings.Trim(flagRef, "{}")
		if val, ok := flags[flagName]; ok && val != "" {
			flatKey := resource[:len(resource)] + "Id" // e.g. "projectsId"
			params[flatKey] = val
		}
	}
}

// resolveFlatPathSegments resolves ALL {xxxId} segments in a flatPath from
// global flags. Uses the ParentTemplate for known mappings, then tries the
// IDKey directly (CloudSQL style: {instance}), then derives flag names for
// unknown segments (Spanner style: {instancesId}).
func resolveFlatPathSegments(params map[string]string, flatPath, parentTemplate string, flags map[string]string) {
	segments := parseFlatPathSegments(flatPath)
	parentMap := buildParentFlagMap(parentTemplate)

	for _, seg := range segments {
		// Skip if already resolved.
		if _, ok := params[seg.IDKey]; ok {
			continue
		}

		// Try ParentTemplate mapping first.
		if mapped, ok := parentMap[seg.IDKey]; ok {
			if val, ok := flags[mapped]; ok && val != "" {
				params[seg.IDKey] = val
				continue
			}
		}

		// Try the IDKey directly as a flag name (CloudSQL style: {instance}).
		if val, ok := flags[seg.IDKey]; ok && val != "" {
			params[seg.IDKey] = val
			continue
		}

		// Derive flag name from resource name (Spanner style: {instancesId}).
		flagName := deriveFlagName(seg.Resource)
		if val, ok := flags[flagName]; ok && val != "" {
			params[seg.IDKey] = val
		}
	}
}

// toFlatPathKey converts a Discovery param name to its flatPath equivalent.
// e.g. "projectId" -> "projectsId", "datasetId" -> "datasetsId"
func toFlatPathKey(name string) string {
	// Common pattern: fooId -> foosId (pluralize the resource name)
	if strings.HasSuffix(name, "Id") {
		base := name[:len(name)-2]
		if !strings.HasSuffix(base, "s") {
			return base + "sId"
		}
	}
	return name
}
