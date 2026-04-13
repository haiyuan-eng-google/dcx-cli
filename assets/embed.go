// Package assets embeds Discovery Document JSON files into the binary.
package assets

import "embed"

//go:embed bigquery_v2_discovery.json
var BigQueryDiscovery []byte

//go:embed spanner_v1_discovery.json
var SpannerDiscovery []byte

//go:embed alloydb_v1_discovery.json
var AlloyDBDiscovery []byte

//go:embed sqladmin_v1_discovery.json
var CloudSQLDiscovery []byte

//go:embed looker_v1_discovery.json
var LookerDiscovery []byte

// FS provides filesystem access to all embedded assets.
//
//go:embed *.json
var FS embed.FS
