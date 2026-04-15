package discovery

// BigQueryConfig returns the ServiceConfig for BigQuery dynamic commands.
func BigQueryConfig() *ServiceConfig {
	return &ServiceConfig{
		Namespace:   "", // top-level (no namespace prefix)
		Domain:      "bigquery",
		BaseURL:     "https://bigquery.googleapis.com/bigquery/v2/",
		UseFlatPath: false, // BigQuery uses path with {+param} format
		AllowedMethods: []string{
			"datasets.list",
			"datasets.get",
			"tables.list",
			"tables.get",
			"jobs.list",
			"jobs.get",
			"models.list",
			"models.get",
			"routines.list",
			"routines.get",
		},
		GlobalParamMappings: map[string]string{
			"projectId": "project-id",
			"datasetId": "dataset-id",
			"location":  "location",
		},
	}
}

// SpannerConfig returns the ServiceConfig for Spanner Discovery commands.
func SpannerConfig() *ServiceConfig {
	return &ServiceConfig{
		Namespace:      "spanner",
		Domain:         "spanner",
		BaseURL:        "https://spanner.googleapis.com/",
		UseFlatPath:    true,
		ParentTemplate: "projects/{project-id}",
		AllowedMethods: []string{
			"instances.list",
			"instances.get",
			"databases.list",
			"databases.get",
			"databases.getDdl",
		},
		GlobalParamMappings: map[string]string{},
	}
}

// AlloyDBConfig returns the ServiceConfig for AlloyDB Discovery commands.
func AlloyDBConfig() *ServiceConfig {
	return &ServiceConfig{
		Namespace:      "alloydb",
		Domain:         "alloydb",
		BaseURL:        "https://alloydb.googleapis.com/",
		UseFlatPath:    true,
		ParentTemplate: "projects/{project-id}/locations/{location}",
		AllowedMethods: []string{
			"clusters.list",
			"clusters.get",
			"instances.list",
			"instances.get",
			"backups.list",
			"backups.get",
		},
		GlobalParamMappings: map[string]string{},
	}
}

// CloudSQLConfig returns the ServiceConfig for Cloud SQL Discovery commands.
func CloudSQLConfig() *ServiceConfig {
	return &ServiceConfig{
		Namespace:   "cloudsql",
		Domain:      "cloudsql",
		BaseURL:     "https://sqladmin.googleapis.com/",
		UseFlatPath: true,
		AllowedMethods: []string{
			"instances.list",
			"instances.get",
			"databases.list",
			"databases.get",
			"flags.list",
			"tiers.list",
		},
		GlobalParamMappings: map[string]string{
			"project": "project-id",
		},
	}
}

// LookerConfig returns the ServiceConfig for Looker Discovery commands.
func LookerConfig() *ServiceConfig {
	return &ServiceConfig{
		Namespace:      "looker",
		Domain:         "looker",
		BaseURL:        "https://looker.googleapis.com/",
		UseFlatPath:    true,
		ParentTemplate: "projects/{project-id}/locations/{location}",
		AllowedMethods: []string{
			"instances.list",
			"instances.get",
		},
		GlobalParamMappings: map[string]string{},
	}
}
