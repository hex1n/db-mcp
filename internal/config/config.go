package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	DefaultMaxRows             = 500
	DefaultQueryTimeoutSeconds = 30
	DefaultMaxValueBytes       = 64 * 1024
	DefaultMaxResultBytes      = 1024 * 1024
)

type Config struct {
	Default             string                      `toml:"default"`
	MaxRows             int                         `toml:"max_rows"`
	MaxValueBytes       int                         `toml:"max_value_bytes"`
	MaxResultBytes      int                         `toml:"max_result_bytes"`
	QueryTimeoutSeconds int                         `toml:"query_timeout_seconds"`
	ReadOnly            bool                        `toml:"read_only"`
	Datasources         map[string]DatasourceConfig `toml:"datasources"`
}

type DatasourceConfig struct {
	Driver           string `toml:"driver"`
	Host             string `toml:"host"`
	Port             int    `toml:"port"`
	Database         string `toml:"database"`
	Username         string `toml:"username"`
	Password         string `toml:"password"`
	PasswordFrom     string `toml:"password_from"`
	PropertiesFile   string `toml:"properties_file"`
	PropertiesPrefix string `toml:"properties_prefix"`
	RedisDB          int    `toml:"redis_db"`
}

func DefaultPath() string {
	if value := strings.TrimSpace(os.Getenv("DB_MCP_CONFIG")); value != "" {
		return value
	}
	if projectDir := strings.TrimSpace(os.Getenv("CLAUDE_PROJECT_DIR")); projectDir != "" {
		return filepath.Join(projectDir, ".db-mcp.toml")
	}
	return ".db-mcp.toml"
}

func Load(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	cfg = ApplyDefaults(cfg)
	if len(cfg.Datasources) == 0 {
		return cfg, errors.New("no datasources configured")
	}
	if cfg.Default == "" {
		if len(cfg.Datasources) > 1 {
			return cfg, errors.New("default datasource is required when multiple datasources are configured")
		}
		for name := range cfg.Datasources {
			cfg.Default = name
		}
	}
	if _, ok := cfg.Datasources[cfg.Default]; !ok {
		return cfg, fmt.Errorf("default datasource %q is not configured", cfg.Default)
	}
	return cfg, nil
}

func ApplyDefaults(cfg Config) Config {
	if cfg.MaxRows <= 0 {
		cfg.MaxRows = DefaultMaxRows
	}
	if cfg.MaxValueBytes <= 0 {
		cfg.MaxValueBytes = DefaultMaxValueBytes
	}
	if cfg.MaxResultBytes <= 0 {
		cfg.MaxResultBytes = DefaultMaxResultBytes
	}
	if cfg.QueryTimeoutSeconds <= 0 {
		cfg.QueryTimeoutSeconds = DefaultQueryTimeoutSeconds
	}
	return cfg
}

func DriverName(ds DatasourceConfig) string {
	name := strings.ToLower(strings.TrimSpace(ds.Driver))
	if name == "" {
		name = "mysql"
	}
	return name
}

func IsRedisDriver(ds DatasourceConfig) bool {
	return DriverName(ds) == "redis"
}

func ResolveDatasource(cfg Config, configPath, name string) (DatasourceConfig, error) {
	ds, ok := cfg.Datasources[name]
	if !ok {
		return DatasourceConfig{}, fmt.Errorf("datasource %q is not configured", name)
	}

	if ds.PropertiesFile != "" {
		if IsRedisDriver(ds) {
			return DatasourceConfig{}, fmt.Errorf("datasource %q: properties_file is only supported for SQL drivers", name)
		}
		props, err := loadProperties(resolvePath(configPath, ds.PropertiesFile))
		if err != nil {
			return DatasourceConfig{}, err
		}
		prefix := ds.PropertiesPrefix
		if prefix == "" {
			return DatasourceConfig{}, fmt.Errorf("datasource %q properties_prefix is required when properties_file is set", name)
		}
		jdbcURL := props[prefix+".url"]
		host, port, database, err := parseJDBCMySQLURL(jdbcURL)
		if err != nil {
			return DatasourceConfig{}, fmt.Errorf("datasource %q: %w", name, err)
		}
		if ds.Host == "" {
			ds.Host = host
		}
		if ds.Port == 0 {
			ds.Port = port
		}
		if ds.Database == "" {
			ds.Database = database
		}
		if ds.Username == "" {
			ds.Username = props[prefix+".username"]
		}
		if ds.Password == "" && ds.PasswordFrom == "" {
			ds.Password = props[prefix+".password"]
		}
	}

	if ds.Driver == "" {
		ds.Driver = "mysql"
	}
	if ds.PasswordFrom != "" {
		password, err := resolveSecret(ds.PasswordFrom)
		if err != nil {
			return DatasourceConfig{}, err
		}
		ds.Password = password
	}

	if IsRedisDriver(ds) {
		if ds.Port == 0 {
			ds.Port = 6379
		}
		if ds.Host == "" {
			return DatasourceConfig{}, fmt.Errorf("datasource %q is missing host", name)
		}
		return ds, nil
	}

	if ds.Port == 0 {
		ds.Port = 3306
	}
	if ds.Host == "" || ds.Database == "" || ds.Username == "" {
		return DatasourceConfig{}, fmt.Errorf("datasource %q is missing host/database/username", name)
	}
	return ds, nil
}

func resolvePath(configPath, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	configDir := filepath.Dir(configPath)
	candidate := filepath.Clean(filepath.Join(configDir, path))
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	parentCandidate := filepath.Clean(filepath.Join(configDir, "..", path))
	if _, err := os.Stat(parentCandidate); err == nil {
		return parentCandidate
	}
	return candidate
}

func loadProperties(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	props := make(map[string]string)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		idx := strings.IndexAny(line, "=:")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		props[key] = value
	}
	return props, nil
}

func parseJDBCMySQLURL(jdbcURL string) (string, int, string, error) {
	if !strings.HasPrefix(jdbcURL, "jdbc:mysql://") {
		return "", 0, "", fmt.Errorf("unsupported jdbc url %q", jdbcURL)
	}
	raw := strings.TrimPrefix(jdbcURL, "jdbc:")
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", 0, "", err
	}
	port := 3306
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return "", 0, "", err
		}
		port = parsedPort
	}
	database := strings.TrimPrefix(parsed.Path, "/")
	if parsed.Hostname() == "" || database == "" {
		return "", 0, "", fmt.Errorf("jdbc url %q missing host or database", jdbcURL)
	}
	return parsed.Hostname(), port, database, nil
}

func resolveSecret(ref string) (string, error) {
	if strings.HasPrefix(ref, "env:") {
		name := strings.TrimPrefix(ref, "env:")
		value := os.Getenv(name)
		if value == "" {
			return "", fmt.Errorf("environment variable %s is empty", name)
		}
		return value, nil
	}
	return "", fmt.Errorf("unsupported password_from %q", ref)
}
