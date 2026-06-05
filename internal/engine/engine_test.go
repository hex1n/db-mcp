package engine_test

import (
	"strings"
	"testing"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/engine/mysqlengine"
)

func TestRegistryDriverRouting(t *testing.T) {
	registry := engine.NewRegistry(mysqlengine.Registrations()...)
	cfg := config.Config{QueryTimeoutSeconds: 30}
	base := config.DatasourceConfig{Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"}

	cases := []struct {
		driver   string
		wantKind string
	}{
		{"", "mysql"},
		{"mysql", "mysql"},
		{"MySQL", "mysql"},
		{"oceanbase", "oceanbase"},
	}
	for _, c := range cases {
		ds := base
		ds.Driver = c.driver
		eng, err := registry.NewEngine(ds, cfg)
		if err != nil {
			t.Fatalf("driver %q: unexpected error: %v", c.driver, err)
		}
		if eng.Kind() != c.wantKind {
			t.Fatalf("driver %q: kind = %q, want %q", c.driver, eng.Kind(), c.wantKind)
		}
		if _, ok := eng.(engine.SQLEngine); !ok {
			t.Fatalf("driver %q: engine does not implement SQLEngine", c.driver)
		}
		_ = eng.Close()
	}

	ds := base
	ds.Driver = "mongodb"
	if _, err := registry.NewEngine(ds, cfg); err == nil {
		t.Fatal("expected error for unsupported driver 'mongodb', got nil")
	}
}

func TestRegistryValidateConfigRejectsUnsupportedDriver(t *testing.T) {
	registry := engine.NewRegistry(mysqlengine.Registrations()...)
	cfg := config.Config{
		Default: "bad",
		Datasources: map[string]config.DatasourceConfig{
			"ok":  {Driver: "mysql"},
			"bad": {Driver: "mongodb"},
		},
	}

	err := registry.ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected unsupported driver validation error, got nil")
	}
	if !strings.Contains(err.Error(), `datasource "bad"`) || !strings.Contains(err.Error(), `unsupported driver "mongodb"`) {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestRegistryValidateConfigAcceptsDefaultMySQLDriver(t *testing.T) {
	registry := engine.NewRegistry(mysqlengine.Registrations()...)
	cfg := config.Config{
		Default: "implicit",
		Datasources: map[string]config.DatasourceConfig{
			"implicit": {},
		},
	}

	if err := registry.ValidateConfig(cfg); err != nil {
		t.Fatalf("implicit mysql driver should validate: %v", err)
	}
}
