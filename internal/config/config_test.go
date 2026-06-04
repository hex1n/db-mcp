package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".db-mcp.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadDefaultsSingleDatasource(t *testing.T) {
	path := writeTempConfig(t, `
[datasources.only]
driver = "redis"
host = "127.0.0.1"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Default != "only" {
		t.Fatalf("default datasource = %q, want only", cfg.Default)
	}
	if cfg.MaxRows != DefaultMaxRows || cfg.MaxValueBytes != DefaultMaxValueBytes || cfg.MaxResultBytes != DefaultMaxResultBytes {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadRequiresDefaultForMultipleDatasources(t *testing.T) {
	path := writeTempConfig(t, `
[datasources.a]
driver = "redis"
host = "127.0.0.1"

[datasources.b]
driver = "redis"
host = "127.0.0.1"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "default datasource is required") {
		t.Fatalf("expected multiple datasource default error, got: %v", err)
	}
}

func TestResolveRedisDefaults(t *testing.T) {
	cfg := Config{
		Default: "r",
		Datasources: map[string]DatasourceConfig{
			"r": {Driver: "redis", Host: "127.0.0.1"},
		},
	}
	ds, err := ResolveDatasource(cfg, ".db-mcp.toml", "r")
	if err != nil {
		t.Fatalf("redis datasource without database/username should resolve, got: %v", err)
	}
	if ds.Port != 6379 {
		t.Fatalf("redis default port = %d, want 6379", ds.Port)
	}
}

func TestResolveRedisRejectsProperties(t *testing.T) {
	cfg := Config{
		Default: "r",
		Datasources: map[string]DatasourceConfig{
			"r": {Driver: "redis", Host: "127.0.0.1", PropertiesFile: "x.properties", PropertiesPrefix: "db"},
		},
	}
	_, err := ResolveDatasource(cfg, ".db-mcp.toml", "r")
	if err == nil || !strings.Contains(err.Error(), "properties_file is only supported for SQL") {
		t.Fatalf("expected properties rejection for redis, got: %v", err)
	}
}
