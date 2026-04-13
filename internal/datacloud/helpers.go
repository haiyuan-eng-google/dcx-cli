// Package datacloud provides database helper commands that use the
// CA QueryData API to query INFORMATION_SCHEMA across Spanner, AlloyDB,
// and Cloud SQL.
package datacloud

import (
	"context"
	"fmt"

	"github.com/haiyuan-eng-google/dcx-cli/internal/ca"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
)

// SchemaDescribeResult is the output of schema describe.
type SchemaDescribeResult struct {
	Source     string      `json:"source"`
	Database   string      `json:"database"`
	Instance   string      `json:"instance"`
	Schema     interface{} `json:"schema"`
}

// DatabasesListResult is the output of databases list for AlloyDB/CloudSQL.
type DatabasesListResult struct {
	Items  interface{} `json:"items"`
	Source string      `json:"source"`
}

// SchemaDescribe queries INFORMATION_SCHEMA via the CA QueryData API to
// describe the schema of a database source.
func SchemaDescribe(ctx context.Context, client *ca.Client, token string, profile *profiles.Profile) (*SchemaDescribeResult, error) {
	question := schemaQueryForProfile(profile)

	raw, err := client.AskQueryDataRaw(ctx, token, profile, question)
	if err != nil {
		return nil, fmt.Errorf("schema describe: %w", err)
	}

	return &SchemaDescribeResult{
		Source:   sourceDisplayName(profile.SourceType),
		Database: profile.DatabaseID,
		Instance: profile.InstanceID,
		Schema:   raw,
	}, nil
}

// DatabasesList queries available databases on an AlloyDB or Cloud SQL
// instance via the CA QueryData API.
func DatabasesList(ctx context.Context, client *ca.Client, token string, profile *profiles.Profile) (*DatabasesListResult, error) {
	question := databasesListQuery(profile.SourceType, profile.DBType)

	raw, err := client.AskQueryDataRaw(ctx, token, profile, question)
	if err != nil {
		return nil, fmt.Errorf("databases list: %w", err)
	}

	return &DatabasesListResult{
		Items:  raw,
		Source: sourceDisplayName(profile.SourceType),
	}, nil
}

// schemaQuery returns the INFORMATION_SCHEMA query for the given profile.
func schemaQuery(st profiles.SourceType) string {
	switch st {
	case profiles.Spanner:
		return "SELECT TABLE_NAME, COLUMN_NAME, SPANNER_TYPE, IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS ORDER BY TABLE_NAME, ORDINAL_POSITION"
	case profiles.AlloyDB:
		return "SELECT table_name, column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' ORDER BY table_name, ordinal_position"
	default:
		return "SELECT table_name, column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema NOT IN ('information_schema', 'pg_catalog', 'mysql', 'sys', 'performance_schema') ORDER BY table_name, ordinal_position"
	}
}

// schemaQueryForProfile returns the INFORMATION_SCHEMA query respecting db_type.
func schemaQueryForProfile(p *profiles.Profile) string {
	if p.SourceType == profiles.CloudSQL && p.DBType == "mysql" {
		return "SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA NOT IN ('information_schema', 'mysql', 'sys', 'performance_schema') ORDER BY TABLE_NAME, ORDINAL_POSITION"
	}
	return schemaQuery(p.SourceType)
}

// databasesListQuery returns the query to list databases on the instance.
func databasesListQuery(st profiles.SourceType, dbType string) string {
	if st == profiles.CloudSQL && dbType == "mysql" {
		return "SHOW DATABASES"
	}
	switch st {
	case profiles.AlloyDB, profiles.CloudSQL:
		return "SELECT datname AS database_name FROM pg_database WHERE datistemplate = false ORDER BY datname"
	default:
		return "SHOW DATABASES"
	}
}

func sourceDisplayName(st profiles.SourceType) string {
	switch st {
	case profiles.Spanner:
		return "Spanner"
	case profiles.AlloyDB:
		return "AlloyDB"
	case profiles.CloudSQL:
		return "Cloud SQL"
	default:
		return string(st)
	}
}
