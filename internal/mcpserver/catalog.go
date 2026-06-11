package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
)

const (
	toolListDatasources   = "list_datasources"
	toolCurrentDatasource = "current_datasource"
	toolGetCurrentTime    = "get_current_time"
	toolListTables        = "list_tables"
	toolDescribeTable     = "describe_table"
	toolSampleTable       = "sample_table"
	toolExecuteSQL        = "execute_sql"
	toolRedisScan         = "redis_scan"
	toolRedisGet          = "redis_get"
	toolRedisType         = "redis_type"
	toolRedisTTL          = "redis_ttl"
	toolRedisCommand      = "redis_command"
)

type toolSpec struct {
	Name              string
	Title             string
	Description       string
	Annotations       *mcp.ToolAnnotations
	RequireDatasource bool
}

func buildToolCatalog(cfg config.Config, cats map[string]bool) []toolSpec {
	requireDatasource := len(cfg.Datasources) > 1
	specs := []toolSpec{
		{Name: toolListDatasources, Title: "List Datasources", Description: "List configured datasources without connecting to them.", Annotations: readOnlyAnnotations()},
		{Name: toolCurrentDatasource, Title: "Current Datasource", Description: "Show resolved connection information for one datasource without exposing its password.", Annotations: readOnlyAnnotations()},
		{Name: toolGetCurrentTime, Title: "Current Time", Description: "Get current server time for the selected datasource as an RFC3339 timestamp with timezone offset.", Annotations: readOnlyAnnotations()},
	}
	if cats[engine.CategorySQL] {
		specs = append(specs,
			toolSpec{Name: toolListTables, Title: "List Tables", Description: "List tables in the selected SQL datasource.", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolDescribeTable, Title: "Describe Table", Description: "Show full column metadata for a table in the selected SQL datasource.", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolSampleTable, Title: "Sample Table", Description: "Read a bounded sample from a table in the selected SQL datasource.", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolExecuteSQL, Title: "Execute SQL", Description: executeSQLDescription(cfg.Mode), Annotations: writeAnnotations(), RequireDatasource: requireDatasource},
		)
	}
	if cats[engine.CategoryKV] {
		specs = append(specs,
			toolSpec{Name: toolRedisScan, Title: "Redis Scan", Description: "Scan keys by MATCH pattern (bounded) in the selected Redis datasource. Prefer this over the blocking KEYS command.", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolRedisGet, Title: "Redis Get", Description: "Read a key's value in the selected Redis datasource; auto-detects type (string/list/set/hash/zset) with bounded output.", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolRedisType, Title: "Redis Type", Description: "Show the type of a key in the selected Redis datasource.", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolRedisTTL, Title: "Redis TTL", Description: "Show the TTL in seconds of a key in the selected Redis datasource (-1 no expiry, -2 missing).", Annotations: readOnlyAnnotations()},
			toolSpec{Name: toolRedisCommand, Title: "Redis Command", Description: redisCommandDescription(cfg.Mode), Annotations: writeAnnotations(), RequireDatasource: requireDatasource},
		)
	}
	return specs
}

func executeSQLDescription(mode string) string {
	desc := "Execute one SQL statement on the selected test SQL datasource."
	if mode == config.ModeInspect {
		return desc + " Inspect mode is ON: only reads are accepted (SELECT/SHOW/DESC/DESCRIBE/EXPLAIN and read-only WITH CTEs); writes and EXPLAIN ANALYZE are rejected and the DB session is read-only."
	}
	return desc + " Operate mode is ON: write statements are enabled and should only be used after explicit user confirmation."
}

func redisCommandDescription(mode string) string {
	desc := "Execute an arbitrary Redis command (argv form) on the selected test datasource."
	if mode == config.ModeInspect {
		return desc + " Inspect mode is ON: only allowlisted bounded read commands are accepted."
	}
	return desc + " Operate mode is ON: write/destructive commands (SET/DEL/FLUSHALL...) are enabled and should only be used after explicit user confirmation."
}

func toolByName(specs []toolSpec) map[string]toolSpec {
	byName := make(map[string]toolSpec, len(specs))
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	return byName
}

func newMCPTool(spec toolSpec) *mcp.Tool {
	return &mcp.Tool{
		Name:        spec.Name,
		Title:       spec.Title,
		Description: spec.Description,
		Annotations: spec.Annotations,
	}
}

func boolp(b bool) *bool { return &b }

func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolp(false)}
}

func writeAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: boolp(true), OpenWorldHint: boolp(false)}
}
