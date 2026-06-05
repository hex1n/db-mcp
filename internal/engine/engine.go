package engine

import (
	"context"
	"fmt"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/result"
)

type Engine interface {
	Kind() string
	CurrentTime(ctx context.Context) (result.TimeResult, error)
	Close() error
}

type SQLEngine interface {
	Engine
	Query(ctx context.Context, sqlText string, maxRows int) (result.SQLResult, error)
	Exec(ctx context.Context, sqlText string) (result.SQLResult, error)
	ListTables(ctx context.Context, maxRows int) (result.SQLResult, error)
	DescribeTable(ctx context.Context, table string, maxRows int) (result.SQLResult, error)
	SampleTable(ctx context.Context, table string, limit int) (result.SQLResult, error)
}

type KVEngine interface {
	Engine
	Scan(ctx context.Context, pattern string, count int) (result.RedisScanResult, error)
	Get(ctx context.Context, key string) (result.RedisGetResult, error)
	Type(ctx context.Context, key string) (result.RedisTypeResult, error)
	TTL(ctx context.Context, key string) (result.RedisTTLResult, error)
	Command(ctx context.Context, argv []string) (result.RedisCommandResult, error)
}

const (
	CategorySQL = "sql"
	CategoryKV  = "kv"
)

type Factory func(config.DatasourceConfig, config.Config) (Engine, error)

type Spec struct {
	Category string
	Factory  Factory
}

type Registration struct {
	Name string
	Spec Spec
}

type Registry struct {
	specs map[string]Spec
}

func NewRegistry(registrations ...Registration) *Registry {
	r := &Registry{specs: make(map[string]Spec, len(registrations))}
	for _, reg := range registrations {
		r.Register(reg.Name, reg.Spec)
	}
	return r
}

func (r *Registry) Register(name string, spec Spec) {
	r.specs[name] = spec
}

func (r *Registry) NewEngine(ds config.DatasourceConfig, cfg config.Config) (Engine, error) {
	spec, ok := r.specs[config.DriverName(ds)]
	if !ok {
		return nil, fmt.Errorf("unsupported driver %q", ds.Driver)
	}
	return spec.Factory(ds, cfg)
}

func (r *Registry) ValidateConfig(cfg config.Config) error {
	for name, ds := range cfg.Datasources {
		driver := config.DriverName(ds)
		if _, ok := r.specs[driver]; !ok {
			return fmt.Errorf("datasource %q: unsupported driver %q", name, driver)
		}
	}
	return nil
}

func (r *Registry) Category(ds config.DatasourceConfig) (string, bool) {
	spec, ok := r.specs[config.DriverName(ds)]
	if !ok {
		return "", false
	}
	return spec.Category, true
}
