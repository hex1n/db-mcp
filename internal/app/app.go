package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/policy"
	"github.com/hex1n/db-mcp/internal/result"
)

type App struct {
	config     config.Config
	configPath string
	registry   *engine.Registry
	mu         sync.Mutex
	engines    map[string]engine.Engine
}

type ExecuteSQLInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; required when multiple datasources are configured"`
	SQL        string `json:"sql" jsonschema:"single SQL statement to execute"`
	MaxRows    int    `json:"max_rows,omitempty" jsonschema:"maximum rows returned for query statements"`
}

type TableInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
	Table      string `json:"table" jsonschema:"table name"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum sample rows"`
}

type DatasourceInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
}

type RedisKeyInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
	Key        string `json:"key" jsonschema:"redis key"`
}

type RedisScanInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
	Pattern    string `json:"pattern,omitempty" jsonschema:"MATCH pattern; defaults to *"`
	Count      int    `json:"count,omitempty" jsonschema:"maximum keys to return"`
}

type RedisCommandInput struct {
	Datasource string   `json:"datasource,omitempty" jsonschema:"datasource name; required when multiple datasources are configured"`
	Command    []string `json:"command" jsonschema:"redis command as argv, e.g. [\"GET\",\"foo\"]"`
}

func New(cfg config.Config, configPath string, registry *engine.Registry) *App {
	cfg = config.ApplyDefaults(cfg)
	return &App{
		config:     cfg,
		configPath: configPath,
		registry:   registry,
		engines:    make(map[string]engine.Engine),
	}
}

func (a *App) Config() config.Config {
	return a.config
}

func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, eng := range a.engines {
		_ = eng.Close()
	}
}

func (a *App) ConfiguredCategories() map[string]bool {
	cats := make(map[string]bool)
	for _, ds := range a.config.Datasources {
		if cat, ok := a.registry.Category(ds); ok {
			cats[cat] = true
		}
	}
	return cats
}

func (a *App) ListDatasources(ctx context.Context, in struct{}) (result.DatasourceInfo, error) {
	names := make([]string, 0, len(a.config.Datasources))
	for name := range a.config.Datasources {
		names = append(names, name)
	}
	sort.Strings(names)
	return result.DatasourceInfo{Default: a.config.Default, Datasources: names, ReadOnly: a.config.ReadOnly}, nil
}

func (a *App) CurrentDatasource(ctx context.Context, in DatasourceInput) (result.DatasourceInfo, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	ds, err := a.resolveDatasource(name)
	if err != nil {
		return result.DatasourceInfo{}, err
	}
	info := result.DatasourceInfo{
		Default:  a.config.Default,
		Name:     name,
		Driver:   ds.Driver,
		Host:     ds.Host,
		Port:     ds.Port,
		Database: ds.Database,
		Username: ds.Username,
		ReadOnly: a.config.ReadOnly,
	}
	if cat, ok := a.registry.Category(ds); ok && cat == engine.CategoryKV {
		info.RedisDB = ds.RedisDB
	}
	return info, nil
}

func (a *App) GetCurrentTime(ctx context.Context, in DatasourceInput) (result.SQLResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.engineFor(name)
	if err != nil {
		return result.SQLResult{}, err
	}
	res, err := eng.CurrentTime(ctx)
	if err != nil {
		return result.SQLResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) ListTables(ctx context.Context, in DatasourceInput) (result.SQLResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.sqlEngineFor(name)
	if err != nil {
		return result.SQLResult{}, err
	}
	res, err := eng.ListTables(ctx, a.config.MaxRows)
	if err != nil {
		return result.SQLResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) DescribeTable(ctx context.Context, in TableInput) (result.SQLResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.sqlEngineFor(name)
	if err != nil {
		return result.SQLResult{}, err
	}
	res, err := eng.DescribeTable(ctx, in.Table, a.config.MaxRows)
	if err != nil {
		return result.SQLResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) SampleTable(ctx context.Context, in TableInput) (result.SQLResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.sqlEngineFor(name)
	if err != nil {
		return result.SQLResult{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = policy.CapRows(limit, a.config.MaxRows)
	res, err := eng.SampleTable(ctx, in.Table, limit)
	if err != nil {
		return result.SQLResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) ExecuteSQL(ctx context.Context, in ExecuteSQLInput) (result.SQLResult, error) {
	sqlText := strings.TrimSpace(in.SQL)
	if sqlText == "" {
		return result.SQLResult{}, errors.New("sql is required")
	}
	name, err := policy.DatasourceNameForTool("execute_sql", in.Datasource, len(a.config.Datasources), a.config.Default)
	if err != nil {
		return result.SQLResult{}, err
	}
	eng, err := a.sqlEngineFor(name)
	if err != nil {
		return result.SQLResult{}, err
	}
	query := policy.IsQueryStatement(sqlText)
	if a.config.ReadOnly && !policy.IsReadOnlyStatement(sqlText) {
		return result.SQLResult{}, errors.New("read-only mode: refusing statement that is not a guaranteed read (allowed: SELECT, SHOW, DESC, DESCRIBE, EXPLAIN)")
	}
	var res result.SQLResult
	if query {
		res, err = eng.Query(ctx, sqlText, policy.CapRows(in.MaxRows, a.config.MaxRows))
	} else {
		res, err = eng.Exec(ctx, sqlText)
	}
	if err != nil {
		return result.SQLResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) RedisScan(ctx context.Context, in RedisScanInput) (result.RedisScanResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.kvEngineFor(name)
	if err != nil {
		return result.RedisScanResult{}, err
	}
	res, err := eng.Scan(ctx, in.Pattern, in.Count)
	if err != nil {
		return result.RedisScanResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) RedisGet(ctx context.Context, in RedisKeyInput) (result.RedisGetResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.kvEngineFor(name)
	if err != nil {
		return result.RedisGetResult{}, err
	}
	res, err := eng.Get(ctx, in.Key)
	if err != nil {
		return result.RedisGetResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) RedisType(ctx context.Context, in RedisKeyInput) (result.RedisTypeResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.kvEngineFor(name)
	if err != nil {
		return result.RedisTypeResult{}, err
	}
	res, err := eng.Type(ctx, in.Key)
	if err != nil {
		return result.RedisTypeResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) RedisTTL(ctx context.Context, in RedisKeyInput) (result.RedisTTLResult, error) {
	name := policy.DatasourceName(in.Datasource, a.config.Default)
	eng, err := a.kvEngineFor(name)
	if err != nil {
		return result.RedisTTLResult{}, err
	}
	res, err := eng.TTL(ctx, in.Key)
	if err != nil {
		return result.RedisTTLResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) RedisCommand(ctx context.Context, in RedisCommandInput) (result.RedisCommandResult, error) {
	name, err := policy.DatasourceNameForTool("redis_command", in.Datasource, len(a.config.Datasources), a.config.Default)
	if err != nil {
		return result.RedisCommandResult{}, err
	}
	eng, err := a.kvEngineFor(name)
	if err != nil {
		return result.RedisCommandResult{}, err
	}
	if a.config.ReadOnly {
		limits := result.NewLimits(a.config.MaxRows, a.config.MaxValueBytes, a.config.MaxResultBytes)
		if err := policy.EnsureRedisReadOnly(in.Command, limits); err != nil {
			return result.RedisCommandResult{}, err
		}
	}
	res, err := eng.Command(ctx, in.Command)
	if err != nil {
		return result.RedisCommandResult{}, err
	}
	res.Datasource = name
	return res, nil
}

func (a *App) engineFor(name string) (engine.Engine, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if eng := a.engines[name]; eng != nil {
		return eng, nil
	}

	ds, err := a.resolveDatasource(name)
	if err != nil {
		return nil, err
	}
	eng, err := a.registry.NewEngine(ds, a.config)
	if err != nil {
		return nil, err
	}
	a.engines[name] = eng
	return eng, nil
}

func (a *App) sqlEngineFor(name string) (engine.SQLEngine, error) {
	eng, err := a.engineFor(name)
	if err != nil {
		return nil, err
	}
	sqlEng, ok := eng.(engine.SQLEngine)
	if !ok {
		return nil, fmt.Errorf("datasource %q (kind %s) does not support SQL operations", name, eng.Kind())
	}
	return sqlEng, nil
}

func (a *App) kvEngineFor(name string) (engine.KVEngine, error) {
	eng, err := a.engineFor(name)
	if err != nil {
		return nil, err
	}
	kvEng, ok := eng.(engine.KVEngine)
	if !ok {
		return nil, fmt.Errorf("datasource %q (kind %s) does not support key-value operations", name, eng.Kind())
	}
	return kvEng, nil
}

func (a *App) resolveDatasource(name string) (config.DatasourceConfig, error) {
	return config.ResolveDatasource(a.config, a.configPath, name)
}
