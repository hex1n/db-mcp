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
	if cfg.Mode != ModeOperate || cfg.ReadOnly {
		t.Fatalf("mode defaults = mode %q read_only %v, want operate/false", cfg.Mode, cfg.ReadOnly)
	}
}

func TestLoadModeInspectDerivesReadOnly(t *testing.T) {
	path := writeTempConfig(t, `
mode = "inspect"

[datasources.only]
driver = "redis"
host = "127.0.0.1"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Mode != ModeInspect || !cfg.ReadOnly {
		t.Fatalf("mode/read_only = %q/%v, want inspect/true", cfg.Mode, cfg.ReadOnly)
	}
}

func TestLoadLegacyReadOnlyDerivesInspectMode(t *testing.T) {
	path := writeTempConfig(t, `
read_only = true

[datasources.only]
driver = "redis"
host = "127.0.0.1"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Mode != ModeInspect || !cfg.ReadOnly {
		t.Fatalf("legacy read_only mode = %q/%v, want inspect/true", cfg.Mode, cfg.ReadOnly)
	}
}

func TestLoadRejectsModeAndReadOnlyTogether(t *testing.T) {
	path := writeTempConfig(t, `
mode = "inspect"
read_only = true

[datasources.only]
driver = "redis"
host = "127.0.0.1"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "mode and read_only cannot both be configured") {
		t.Fatalf("expected mode/read_only conflict, got: %v", err)
	}
}

func TestLoadRejectsInvalidMode(t *testing.T) {
	path := writeTempConfig(t, `
mode = "audit"

[datasources.only]
driver = "redis"
host = "127.0.0.1"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected invalid mode error, got: %v", err)
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

func TestResolveSQLFromProperties(t *testing.T) {
	dir := t.TempDir()
	propertiesPath := filepath.Join(dir, "db.properties")
	if err := os.WriteFile(propertiesPath, []byte(`
app.url=jdbc:mysql://db.example.internal:3307/appdb?useSSL=false
app.username=app_user
app.password=app_password
`), 0600); err != nil {
		t.Fatalf("write properties: %v", err)
	}
	cfg := Config{
		Default: "sql",
		Datasources: map[string]DatasourceConfig{
			"sql": {Driver: "mysql", PropertiesFile: "db.properties", PropertiesPrefix: "app"},
		},
	}

	ds, err := ResolveDatasource(cfg, filepath.Join(dir, ".db-mcp.toml"), "sql")
	if err != nil {
		t.Fatalf("resolve datasource: %v", err)
	}
	if ds.Host != "db.example.internal" || ds.Port != 3307 || ds.Database != "appdb" || ds.Username != "app_user" || ds.Password != "app_password" {
		t.Fatalf("resolved datasource = %+v", ds)
	}
}

func TestResolvePasswordFromEnvOverridesPropertiesPassword(t *testing.T) {
	t.Setenv("DB_MCP_TEST_PASSWORD", "from-env")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "db.properties"), []byte(`
app.url=jdbc:mysql://db.example.internal/appdb
app.username=app_user
app.password=from-properties
`), 0600); err != nil {
		t.Fatalf("write properties: %v", err)
	}
	cfg := Config{
		Default: "sql",
		Datasources: map[string]DatasourceConfig{
			"sql": {
				Driver:           "mysql",
				PropertiesFile:   "db.properties",
				PropertiesPrefix: "app",
				PasswordFrom:     "env:DB_MCP_TEST_PASSWORD",
			},
		},
	}

	ds, err := ResolveDatasource(cfg, filepath.Join(dir, ".db-mcp.toml"), "sql")
	if err != nil {
		t.Fatalf("resolve datasource: %v", err)
	}
	if ds.Password != "from-env" {
		t.Fatalf("password = %q, want env secret", ds.Password)
	}
}

func TestResolveSQLAppliesDriverAndPortDefaults(t *testing.T) {
	cfg := Config{
		Default: "sql",
		Datasources: map[string]DatasourceConfig{
			"sql": {Host: "127.0.0.1", Database: "app", Username: "root"},
		},
	}

	ds, err := ResolveDatasource(cfg, ".db-mcp.toml", "sql")
	if err != nil {
		t.Fatalf("resolve datasource: %v", err)
	}
	if ds.Driver != "mysql" || ds.Port != 3306 {
		t.Fatalf("driver/port = %q/%d, want mysql/3306", ds.Driver, ds.Port)
	}
}
