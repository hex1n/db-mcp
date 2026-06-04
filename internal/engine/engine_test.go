package engine_test

import (
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
