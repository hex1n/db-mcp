package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hex1n/db-mcp/internal/app"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/result"
)

type executeSQLRequiredDatasourceInput struct {
	Datasource string `json:"datasource" jsonschema:"datasource name"`
	SQL        string `json:"sql" jsonschema:"single SQL statement to execute"`
	MaxRows    int    `json:"max_rows,omitempty" jsonschema:"maximum rows returned for query statements"`
}

type redisCommandRequiredDatasourceInput struct {
	Datasource string   `json:"datasource" jsonschema:"datasource name"`
	Command    []string `json:"command" jsonschema:"redis command as argv, e.g. [\"GET\",\"foo\"]"`
}

func New(application *app.App, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "db-mcp", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "list_datasources", Title: "List Datasources", Description: "List configured datasources without connecting to them.", Annotations: readOnlyAnnotations()}, adapt(application.ListDatasources))
	mcp.AddTool(server, &mcp.Tool{Name: "current_datasource", Title: "Current Datasource", Description: "Show resolved connection information for one datasource without exposing its password.", Annotations: readOnlyAnnotations()}, adapt(application.CurrentDatasource))
	mcp.AddTool(server, &mcp.Tool{Name: "get_current_time", Title: "Current Time", Description: "Get current server time for the selected datasource.", Annotations: readOnlyAnnotations()}, adapt(application.GetCurrentTime))

	cats := application.ConfiguredCategories()
	cfg := application.Config()

	sqlWriteDesc := "Execute one SQL statement on the selected test SQL datasource. Write statements should only be used after explicit user confirmation."
	redisWriteDesc := "Execute an arbitrary Redis command (argv form) on the selected test datasource. Write/destructive commands (SET/DEL/FLUSHALL...) should only be used after explicit user confirmation."
	if cfg.ReadOnly {
		sqlWriteDesc += " Read-only mode is ON: only SELECT/SHOW/DESC/DESCRIBE/EXPLAIN are accepted; CTE/WITH and writes are rejected."
		redisWriteDesc += " Read-only mode is ON: only allowlisted bounded read commands are accepted."
	}

	if cats[engine.CategorySQL] {
		mcp.AddTool(server, &mcp.Tool{Name: "list_tables", Title: "List Tables", Description: "List tables in the selected SQL datasource.", Annotations: readOnlyAnnotations()}, adapt(application.ListTables))
		mcp.AddTool(server, &mcp.Tool{Name: "describe_table", Title: "Describe Table", Description: "Show full column metadata for a table in the selected SQL datasource.", Annotations: readOnlyAnnotations()}, adapt(application.DescribeTable))
		mcp.AddTool(server, &mcp.Tool{Name: "sample_table", Title: "Sample Table", Description: "Read a bounded sample from a table in the selected SQL datasource.", Annotations: readOnlyAnnotations()}, adapt(application.SampleTable))
		addExecuteSQLTool(server, application, sqlWriteDesc, len(cfg.Datasources) > 1)
	}

	if cats[engine.CategoryKV] {
		mcp.AddTool(server, &mcp.Tool{Name: "redis_scan", Title: "Redis Scan", Description: "Scan keys by MATCH pattern (bounded) in the selected Redis datasource. Prefer this over the blocking KEYS command.", Annotations: readOnlyAnnotations()}, adapt(application.RedisScan))
		mcp.AddTool(server, &mcp.Tool{Name: "redis_get", Title: "Redis Get", Description: "Read a key's value in the selected Redis datasource; auto-detects type (string/list/set/hash/zset) with bounded output.", Annotations: readOnlyAnnotations()}, adapt(application.RedisGet))
		mcp.AddTool(server, &mcp.Tool{Name: "redis_type", Title: "Redis Type", Description: "Show the type of a key in the selected Redis datasource.", Annotations: readOnlyAnnotations()}, adapt(application.RedisType))
		mcp.AddTool(server, &mcp.Tool{Name: "redis_ttl", Title: "Redis TTL", Description: "Show the TTL in seconds of a key in the selected Redis datasource (-1 no expiry, -2 missing).", Annotations: readOnlyAnnotations()}, adapt(application.RedisTTL))
		addRedisCommandTool(server, application, redisWriteDesc, len(cfg.Datasources) > 1)
	}

	return server
}

func addExecuteSQLTool(server *mcp.Server, application *app.App, description string, requireDatasource bool) {
	tool := &mcp.Tool{Name: "execute_sql", Title: "Execute SQL", Description: description, Annotations: writeAnnotations()}
	if requireDatasource {
		mcp.AddTool(server, tool, adapt(func(ctx context.Context, in executeSQLRequiredDatasourceInput) (result.SQLResult, error) {
			return application.ExecuteSQL(ctx, app.ExecuteSQLInput{
				Datasource: in.Datasource,
				SQL:        in.SQL,
				MaxRows:    in.MaxRows,
			})
		}))
		return
	}
	mcp.AddTool(server, tool, adapt(application.ExecuteSQL))
}

func addRedisCommandTool(server *mcp.Server, application *app.App, description string, requireDatasource bool) {
	tool := &mcp.Tool{Name: "redis_command", Title: "Redis Command", Description: description, Annotations: writeAnnotations()}
	if requireDatasource {
		mcp.AddTool(server, tool, adapt(func(ctx context.Context, in redisCommandRequiredDatasourceInput) (result.RedisCommandResult, error) {
			return application.RedisCommand(ctx, app.RedisCommandInput{
				Datasource: in.Datasource,
				Command:    in.Command,
			})
		}))
		return
	}
	mcp.AddTool(server, tool, adapt(application.RedisCommand))
}

func adapt[In, Out any](fn func(context.Context, In) (Out, error)) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := fn(ctx, in)
		var zero Out
		if err != nil {
			return nil, zero, err
		}
		return nil, out, nil
	}
}

func boolp(b bool) *bool { return &b }

func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolp(false)}
}

func writeAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: boolp(true), OpenWorldHint: boolp(false)}
}
