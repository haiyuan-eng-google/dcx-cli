package cli

import (
	"github.com/haiyuan-eng-google/dcx-cli/assets"
	"github.com/haiyuan-eng-google/dcx-cli/internal/discovery"
)

// registerDataCloudDiscoveryCommands registers Discovery-driven commands
// for Spanner, AlloyDB, Cloud SQL, and Looker.
func (a *App) registerDataCloudDiscoveryCommands() {
	opts := a.discoveryOpts()

	// Spanner: instances list, databases list/get/get-ddl
	discovery.RegisterCommands(a.Root, a.Registry, assets.SpannerDiscovery, discovery.SpannerConfig(), opts)

	// AlloyDB: clusters list, instances list
	discovery.RegisterCommands(a.Root, a.Registry, assets.AlloyDBDiscovery, discovery.AlloyDBConfig(), opts)

	// Cloud SQL: instances list, databases list
	discovery.RegisterCommands(a.Root, a.Registry, assets.CloudSQLDiscovery, discovery.CloudSQLConfig(), opts)

	// Looker: instances list
	discovery.RegisterCommands(a.Root, a.Registry, assets.LookerDiscovery, discovery.LookerConfig(), opts)
}
