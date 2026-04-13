package discovery

import (
	"encoding/json"
	"fmt"
	"strings"
)

// discoveryDoc represents the top-level structure of a Google Discovery Document.
type discoveryDoc struct {
	BaseURL     string                     `json:"baseUrl"`
	RootURL     string                     `json:"rootUrl"`
	ServicePath string                     `json:"servicePath"`
	Resources   map[string]discoveryResource `json:"resources"`
}

type discoveryResource struct {
	Methods   map[string]discoveryMethod   `json:"methods"`
	Resources map[string]discoveryResource `json:"resources"`
}

type discoveryMethod struct {
	ID             string                       `json:"id"`
	Description    string                       `json:"description"`
	HTTPMethod     string                       `json:"httpMethod"`
	Path           string                       `json:"path"`
	FlatPath       string                       `json:"flatPath"`
	Parameters     map[string]discoveryParam    `json:"parameters"`
	ParameterOrder []string                     `json:"parameterOrder"`
}

type discoveryParam struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Required    bool   `json:"required"`
	Pattern     string `json:"pattern"`
	Format      string `json:"format"`
}

// Parse extracts commands from a Discovery Document JSON according to
// the service config's allowlist.
func Parse(docJSON []byte, svc *ServiceConfig) ([]GeneratedCommand, error) {
	var doc discoveryDoc
	if err := json.Unmarshal(docJSON, &doc); err != nil {
		return nil, fmt.Errorf("parsing discovery document: %w", err)
	}

	if svc.BaseURL == "" {
		svc.BaseURL = doc.BaseURL
	}

	// Build allowlist set for fast lookup: "resource.method" -> true
	allowed := make(map[string]bool, len(svc.AllowedMethods))
	for _, m := range svc.AllowedMethods {
		allowed[m] = true
	}

	var commands []GeneratedCommand
	extractMethods(doc.Resources, nil, allowed, svc, &commands)
	return commands, nil
}

// extractMethods recursively walks the resource tree to find allowed methods.
func extractMethods(
	resources map[string]discoveryResource,
	parentPath []string,
	allowed map[string]bool,
	svc *ServiceConfig,
	out *[]GeneratedCommand,
) {
	for resName, res := range resources {
		currentPath := append(parentPath, resName)

		for methodName, method := range res.Methods {
			// Build the allowlist key: "resource.method" using the leaf resource name.
			allowKey := resName + "." + methodName
			if !allowed[allowKey] {
				continue
			}

			apiMethod := ApiMethod{
				ID:             method.ID,
				Resource:       resName,
				Action:         methodName,
				Description:    method.Description,
				HTTPMethod:     method.HTTPMethod,
				Path:           method.Path,
				FlatPath:       method.FlatPath,
				Parameters:     convertParams(method.Parameters),
				ParameterOrder: method.ParameterOrder,
			}

			// Build command path.
			var cmdPath string
			if svc.Namespace != "" {
				cmdPath = svc.Namespace + " " + resName + " " + methodName
			} else {
				cmdPath = resName + " " + methodName
			}

			// Convert method name from camelCase to kebab-case for CLI.
			cmdPath = camelToKebab(cmdPath)

			// Separate global-mapped params from command-specific flags.
			cmdFlags := extractCommandFlags(apiMethod.Parameters, svc.GlobalParamMappings)

			*out = append(*out, GeneratedCommand{
				CommandPath:  cmdPath,
				Method:       apiMethod,
				Service:      svc,
				CommandFlags: cmdFlags,
			})
		}

		// Recurse into sub-resources.
		if len(res.Resources) > 0 {
			extractMethods(res.Resources, currentPath, allowed, svc, out)
		}
	}
}

func convertParams(params map[string]discoveryParam) map[string]ApiParam {
	result := make(map[string]ApiParam, len(params))
	for name, p := range params {
		result[name] = ApiParam{
			Name:        name,
			Type:        p.Type,
			Description: p.Description,
			Location:    p.Location,
			Required:    p.Required,
			Pattern:     p.Pattern,
			Format:      p.Format,
		}
	}
	return result
}

// extractCommandFlags returns params that are NOT mapped to global flags
// and are query params (not path params handled by global mappings or parent template).
func extractCommandFlags(params map[string]ApiParam, globalMappings map[string]string) []ApiParam {
	var flags []ApiParam
	for name, param := range params {
		// Skip if this param is mapped to a global flag.
		if _, isGlobal := globalMappings[name]; isGlobal {
			continue
		}
		// Skip "parent" param — constructed from global flags via ParentTemplate.
		if name == "parent" {
			continue
		}
		// Skip pagination params — handled by dedicated --page-token/--page-all flags.
		if name == "pageToken" || name == "pageSize" {
			continue
		}
		// Only query params become flags; path params are handled via mappings.
		if param.Location == "query" {
			flags = append(flags, param)
		}
	}
	return flags
}

// camelToKebab converts camelCase segments in a space-separated path
// to kebab-case. e.g. "databases getDdl" -> "databases get-ddl"
func camelToKebab(s string) string {
	parts := strings.Split(s, " ")
	for i, part := range parts {
		var result []rune
		for j, r := range part {
			if j > 0 && r >= 'A' && r <= 'Z' {
				result = append(result, '-')
				result = append(result, r+32) // toLower
			} else {
				result = append(result, r)
			}
		}
		parts[i] = string(result)
	}
	return strings.Join(parts, " ")
}
