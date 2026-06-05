package app

import (
	"context"
	"strings"
	"testing"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/result"
)

type fakeSQL struct {
	queryCalls int
	execCalls  int
}

func (f *fakeSQL) Kind() string { return "mysql" }
func (f *fakeSQL) CurrentTime(context.Context) (result.TimeResult, error) {
	return result.TimeResult{Success: true, Now: "2026-06-05 12:00:00"}, nil
}
func (f *fakeSQL) Close() error { return nil }
func (f *fakeSQL) Query(context.Context, string, int) (result.SQLResult, error) {
	f.queryCalls++
	return result.SQLResult{Success: true, Rows: 1}, nil
}
func (f *fakeSQL) Exec(context.Context, string) (result.SQLResult, error) {
	f.execCalls++
	return result.SQLResult{Success: true, RowsAffected: 1}, nil
}
func (f *fakeSQL) ListTables(context.Context, int) (result.SQLResult, error) {
	return result.SQLResult{Success: true, Rows: 1}, nil
}
func (f *fakeSQL) DescribeTable(context.Context, string, int) (result.SQLResult, error) {
	return result.SQLResult{Success: true, Rows: 1}, nil
}
func (f *fakeSQL) SampleTable(context.Context, string, int) (result.SQLResult, error) {
	return result.SQLResult{Success: true, Rows: 1}, nil
}

type fakeKV struct {
	commandCalls int
}

func (f *fakeKV) Kind() string { return "redis" }
func (f *fakeKV) CurrentTime(context.Context) (result.TimeResult, error) {
	return result.TimeResult{Success: true, Now: "2026-06-05 12:00:00"}, nil
}
func (f *fakeKV) Close() error { return nil }
func (f *fakeKV) Scan(context.Context, string, int) (result.RedisScanResult, error) {
	return result.RedisScanResult{Success: true}, nil
}
func (f *fakeKV) Get(context.Context, string) (result.RedisGetResult, error) {
	return result.RedisGetResult{Success: true, Exists: true}, nil
}
func (f *fakeKV) Type(context.Context, string) (result.RedisTypeResult, error) {
	return result.RedisTypeResult{Success: true, Type: "string"}, nil
}
func (f *fakeKV) TTL(context.Context, string) (result.RedisTTLResult, error) {
	return result.RedisTTLResult{Success: true}, nil
}
func (f *fakeKV) Command(context.Context, []string) (result.RedisCommandResult, error) {
	f.commandCalls++
	return result.RedisCommandResult{Success: true}, nil
}

func TestExecuteSQLUsesDefaultDatasourceForSingleDatasource(t *testing.T) {
	sql := &fakeSQL{}
	application := newTestAppWithSQL(sql, config.Config{
		Default: "sql",
		Datasources: map[string]config.DatasourceConfig{
			"sql": mysqlDatasource(),
		},
	})

	res, err := application.ExecuteSQL(context.Background(), ExecuteSQLInput{SQL: "SELECT 1"})
	if err != nil {
		t.Fatalf("execute sql: %v", err)
	}
	if res.Datasource != "sql" {
		t.Fatalf("datasource = %q, want sql", res.Datasource)
	}
	if sql.queryCalls != 1 || sql.execCalls != 0 {
		t.Fatalf("query/exec calls = %d/%d, want 1/0", sql.queryCalls, sql.execCalls)
	}
}

func TestExecuteSQLRequiresDatasourceWhenMultipleConfigured(t *testing.T) {
	sql := &fakeSQL{}
	application := newTestAppWithSQL(sql, config.Config{
		Default: "sql",
		Datasources: map[string]config.DatasourceConfig{
			"sql":   mysqlDatasource(),
			"other": mysqlDatasource(),
		},
	})

	_, err := application.ExecuteSQL(context.Background(), ExecuteSQLInput{SQL: "SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "requires explicit datasource") {
		t.Fatalf("expected explicit datasource error, got %v", err)
	}
	if sql.queryCalls != 0 || sql.execCalls != 0 {
		t.Fatalf("engine should not be called, got query/exec calls %d/%d", sql.queryCalls, sql.execCalls)
	}
}

func TestInspectModeRejectsSQLWriteBeforeExec(t *testing.T) {
	sql := &fakeSQL{}
	application := newTestAppWithSQL(sql, config.Config{
		Mode:    config.ModeInspect,
		Default: "sql",
		Datasources: map[string]config.DatasourceConfig{
			"sql": mysqlDatasource(),
		},
	})

	_, err := application.ExecuteSQL(context.Background(), ExecuteSQLInput{SQL: "DELETE FROM users"})
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		t.Fatalf("expected read-only error, got %v", err)
	}
	if sql.execCalls != 0 {
		t.Fatalf("exec should not be called in inspect mode, got %d calls", sql.execCalls)
	}
}

func TestInspectModeRejectsRedisWriteBeforeCommand(t *testing.T) {
	kv := &fakeKV{}
	application := newTestAppWithKV(kv, config.Config{
		Mode:    config.ModeInspect,
		Default: "redis",
		Datasources: map[string]config.DatasourceConfig{
			"redis": redisDatasource(),
		},
	})

	_, err := application.RedisCommand(context.Background(), RedisCommandInput{Command: []string{"SET", "k", "v"}})
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		t.Fatalf("expected read-only error, got %v", err)
	}
	if kv.commandCalls != 0 {
		t.Fatalf("redis command should not be called in inspect mode, got %d calls", kv.commandCalls)
	}
}

func TestWrongKindOperationErrorsAtAppSeam(t *testing.T) {
	application := newTestAppWithKV(&fakeKV{}, config.Config{
		Default: "redis",
		Datasources: map[string]config.DatasourceConfig{
			"redis": redisDatasource(),
		},
	})

	_, err := application.ListTables(context.Background(), DatasourceInput{})
	if err == nil || !strings.Contains(err.Error(), "does not support SQL operations") {
		t.Fatalf("expected wrong-kind SQL error, got %v", err)
	}
}

func TestEngineForCachesEngines(t *testing.T) {
	var factoryCalls int
	registry := engine.NewRegistry(engine.Registration{
		Name: "redis",
		Spec: engine.Spec{
			Category: engine.CategoryKV,
			Factory: func(config.DatasourceConfig, config.Config) (engine.Engine, error) {
				factoryCalls++
				return &fakeKV{}, nil
			},
		},
	})
	application := New(config.Config{
		Default: "redis",
		Datasources: map[string]config.DatasourceConfig{
			"redis": redisDatasource(),
		},
	}, ".db-mcp.toml", registry)

	for i := 0; i < 2; i++ {
		if _, err := application.GetCurrentTime(context.Background(), DatasourceInput{}); err != nil {
			t.Fatalf("get current time %d: %v", i, err)
		}
	}
	if factoryCalls != 1 {
		t.Fatalf("factory calls = %d, want 1", factoryCalls)
	}
}

func newTestAppWithSQL(sql *fakeSQL, cfg config.Config) *App {
	registry := engine.NewRegistry(engine.Registration{
		Name: "mysql",
		Spec: engine.Spec{
			Category: engine.CategorySQL,
			Factory: func(config.DatasourceConfig, config.Config) (engine.Engine, error) {
				return sql, nil
			},
		},
	})
	return New(cfg, ".db-mcp.toml", registry)
}

func newTestAppWithKV(kv *fakeKV, cfg config.Config) *App {
	registry := engine.NewRegistry(engine.Registration{
		Name: "redis",
		Spec: engine.Spec{
			Category: engine.CategoryKV,
			Factory: func(config.DatasourceConfig, config.Config) (engine.Engine, error) {
				return kv, nil
			},
		},
	})
	return New(cfg, ".db-mcp.toml", registry)
}

func mysqlDatasource() config.DatasourceConfig {
	return config.DatasourceConfig{Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "app", Username: "root"}
}

func redisDatasource() config.DatasourceConfig {
	return config.DatasourceConfig{Driver: "redis", Host: "127.0.0.1", Port: 6379}
}
