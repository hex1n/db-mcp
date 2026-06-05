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
	ModeInspect                = "inspect"
	ModeOperate                = "operate"
)

type Config struct {
	Default             string                      `toml:"default"`
	MaxRows             int                         `toml:"max_rows"`
	MaxValueBytes       int                         `toml:"max_value_bytes"`
	MaxResultBytes      int                         `toml:"max_result_bytes"`
	QueryTimeoutSeconds int                         `toml:"query_timeout_seconds"`
	Mode                string                      `toml:"mode"`
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
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, err
	}
	if meta.IsDefined("mode") && meta.IsDefined("read_only") {
		return cfg, errors.New("mode and read_only cannot both be configured")
	}
	cfg = ApplyDefaults(cfg)
	if err := ValidateMode(cfg.Mode); err != nil {
		return cfg, err
	}
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
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		if cfg.ReadOnly {
			cfg.Mode = ModeInspect
		} else {
			cfg.Mode = ModeOperate
		}
	}
	cfg.ReadOnly = cfg.Mode == ModeInspect
	return cfg
}

func ValidateMode(mode string) error {
	switch mode {
	case ModeInspect, ModeOperate:
		return nil
	default:
		return fmt.Errorf("unsupported mode %q (allowed: %s, %s)", mode, ModeInspect, ModeOperate)
	}
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
	resolver := datasourceResolver{
		cfg:        cfg,
		configPath: configPath,
		name:       name,
	}
	return resolver.resolve()
}

type datasourceResolver struct {
	cfg        Config
	configPath string
	name       string
	ds         DatasourceConfig
}

func (r *datasourceResolver) resolve() (DatasourceConfig, error) {
	if err := r.lookup(); err != nil {
		return DatasourceConfig{}, err
	}
	if err := r.enrichFromProperties(); err != nil {
		return DatasourceConfig{}, err
	}
	if err := r.applyDriverDefaults(); err != nil {
		return DatasourceConfig{}, err
	}
	if err := r.resolveSecrets(); err != nil {
		return DatasourceConfig{}, err
	}
	if err := r.applyConnectionDefaults(); err != nil {
		return DatasourceConfig{}, err
	}
	return r.ds, nil
}

func (r *datasourceResolver) lookup() error {
	ds, ok := r.cfg.Datasources[r.name]
	if !ok {
		return fmt.Errorf("datasource %q is not configured", r.name)
	}
	r.ds = ds
	return nil
}

func (r *datasourceResolver) enrichFromProperties() error {
	if r.ds.PropertiesFile == "" {
		return nil
	}
	if IsRedisDriver(r.ds) {
		return fmt.Errorf("datasource %q: properties_file is only supported for SQL drivers", r.name)
	}
	props, err := loadProperties(resolvePath(r.configPath, r.ds.PropertiesFile))
	if err != nil {
		return err
	}
	prefix := r.ds.PropertiesPrefix
	if prefix == "" {
		return fmt.Errorf("datasource %q properties_prefix is required when properties_file is set", r.name)
	}
	jdbcURL := props[prefix+".url"]
	host, port, database, err := parseJDBCMySQLURL(jdbcURL)
	if err != nil {
		return fmt.Errorf("datasource %q: %w", r.name, err)
	}
	if r.ds.Host == "" {
		r.ds.Host = host
	}
	if r.ds.Port == 0 {
		r.ds.Port = port
	}
	if r.ds.Database == "" {
		r.ds.Database = database
	}
	if r.ds.Username == "" {
		r.ds.Username = props[prefix+".username"]
	}
	if r.ds.Password == "" && r.ds.PasswordFrom == "" {
		r.ds.Password = props[prefix+".password"]
	}
	return nil
}

func (r *datasourceResolver) applyDriverDefaults() error {
	if r.ds.Driver == "" {
		r.ds.Driver = "mysql"
	}
	return nil
}

func (r *datasourceResolver) resolveSecrets() error {
	if r.ds.PasswordFrom == "" {
		return nil
	}
	password, err := resolveSecret(r.ds.PasswordFrom)
	if err != nil {
		return err
	}
	r.ds.Password = password
	return nil
}

func (r *datasourceResolver) applyConnectionDefaults() error {
	if IsRedisDriver(r.ds) {
		if r.ds.Port == 0 {
			r.ds.Port = 6379
		}
		if r.ds.Host == "" {
			return fmt.Errorf("datasource %q is missing host", r.name)
		}
		return nil
	}

	if r.ds.Port == 0 {
		r.ds.Port = 3306
	}
	if r.ds.Host == "" || r.ds.Database == "" || r.ds.Username == "" {
		return fmt.Errorf("datasource %q is missing host/database/username", r.name)
	}
	return nil
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
