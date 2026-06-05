package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hex1n/db-mcp/internal/app"
	"github.com/hex1n/db-mcp/internal/result"
)

func New(application *app.App, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "db-mcp", Version: version}, nil)
	specs := toolByName(buildToolCatalog(application.Config(), application.ConfiguredCategories()))

	addSharedTools(server, application, specs)
	addSQLTools(server, application, specs)
	addRedisTools(server, application, specs)

	return server
}

func RunStdio(ctx context.Context, application *app.App, version string) error {
	return New(application, version).Run(ctx, &mcp.StdioTransport{})
}

func addSharedTools(server *mcp.Server, application *app.App, specs map[string]toolSpec) {
	mcp.AddTool(server, newMCPTool(specs[toolListDatasources]), adapt(application.ListDatasources))
	mcp.AddTool(server, newMCPTool(specs[toolCurrentDatasource]), adapt(func(ctx context.Context, in datasourceToolInput) (result.DatasourceInfo, error) {
		return application.CurrentDatasource(ctx, app.DatasourceInput{Datasource: in.Datasource})
	}))
	mcp.AddTool(server, newMCPTool(specs[toolGetCurrentTime]), adapt(func(ctx context.Context, in datasourceToolInput) (result.TimeResult, error) {
		return application.GetCurrentTime(ctx, app.DatasourceInput{Datasource: in.Datasource})
	}))
}

func addSQLTools(server *mcp.Server, application *app.App, specs map[string]toolSpec) {
	if spec, ok := specs[toolListTables]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in datasourceToolInput) (result.SQLResult, error) {
			return application.ListTables(ctx, app.DatasourceInput{Datasource: in.Datasource})
		}))
	}
	if spec, ok := specs[toolDescribeTable]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in tableToolInput) (result.SQLResult, error) {
			return application.DescribeTable(ctx, app.TableInput{Datasource: in.Datasource, Table: in.Table, Limit: in.Limit})
		}))
	}
	if spec, ok := specs[toolSampleTable]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in tableToolInput) (result.SQLResult, error) {
			return application.SampleTable(ctx, app.TableInput{Datasource: in.Datasource, Table: in.Table, Limit: in.Limit})
		}))
	}
	if spec, ok := specs[toolExecuteSQL]; ok {
		addExecuteSQLTool(server, application, spec)
	}
}

func addRedisTools(server *mcp.Server, application *app.App, specs map[string]toolSpec) {
	if spec, ok := specs[toolRedisScan]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in redisScanInput) (result.RedisScanResult, error) {
			return application.RedisScan(ctx, app.RedisScanInput{Datasource: in.Datasource, Pattern: in.Pattern, Count: in.Count})
		}))
	}
	if spec, ok := specs[toolRedisGet]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in redisKeyInput) (result.RedisGetResult, error) {
			return application.RedisGet(ctx, app.RedisKeyInput{Datasource: in.Datasource, Key: in.Key})
		}))
	}
	if spec, ok := specs[toolRedisType]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in redisKeyInput) (result.RedisTypeResult, error) {
			return application.RedisType(ctx, app.RedisKeyInput{Datasource: in.Datasource, Key: in.Key})
		}))
	}
	if spec, ok := specs[toolRedisTTL]; ok {
		mcp.AddTool(server, newMCPTool(spec), adapt(func(ctx context.Context, in redisKeyInput) (result.RedisTTLResult, error) {
			return application.RedisTTL(ctx, app.RedisKeyInput{Datasource: in.Datasource, Key: in.Key})
		}))
	}
	if spec, ok := specs[toolRedisCommand]; ok {
		addRedisCommandTool(server, application, spec)
	}
}

func addExecuteSQLTool(server *mcp.Server, application *app.App, spec toolSpec) {
	tool := newMCPTool(spec)
	if spec.RequireDatasource {
		mcp.AddTool(server, tool, adapt(func(ctx context.Context, in executeSQLRequiredDatasourceInput) (result.SQLResult, error) {
			return application.ExecuteSQL(ctx, app.ExecuteSQLInput{
				Datasource: in.Datasource,
				SQL:        in.SQL,
				MaxRows:    in.MaxRows,
			})
		}))
		return
	}
	mcp.AddTool(server, tool, adapt(func(ctx context.Context, in executeSQLInput) (result.SQLResult, error) {
		return application.ExecuteSQL(ctx, app.ExecuteSQLInput{
			Datasource: in.Datasource,
			SQL:        in.SQL,
			MaxRows:    in.MaxRows,
		})
	}))
}

func addRedisCommandTool(server *mcp.Server, application *app.App, spec toolSpec) {
	tool := newMCPTool(spec)
	if spec.RequireDatasource {
		mcp.AddTool(server, tool, adapt(func(ctx context.Context, in redisCommandRequiredDatasourceInput) (result.RedisCommandResult, error) {
			return application.RedisCommand(ctx, app.RedisCommandInput{
				Datasource: in.Datasource,
				Command:    in.Command,
			})
		}))
		return
	}
	mcp.AddTool(server, tool, adapt(func(ctx context.Context, in redisCommandInput) (result.RedisCommandResult, error) {
		return application.RedisCommand(ctx, app.RedisCommandInput{
			Datasource: in.Datasource,
			Command:    in.Command,
		})
	}))
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
