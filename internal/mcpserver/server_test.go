package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hex1n/db-mcp/internal/app"
	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/drivers"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/result"
)

func defaultRegistry() *engine.Registry {
	return drivers.DefaultRegistry()
}

func testApp() *app.App {
	return app.New(config.Config{
		Default:             "t",
		MaxRows:             500,
		QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"t": {Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"},
		},
	}, ".db-mcp.toml", defaultRegistry())
}

func connect(t *testing.T, application *app.App) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()

	server := New(application, "0.1.0")
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return cs, func() {
		_ = cs.Close()
		_ = ss.Close()
	}
}

func TestToolsRegistered(t *testing.T) {
	cs, done := connect(t, testApp())
	defer done()

	got := toolNames(t, cs)
	want := []string{
		"current_datasource",
		"describe_table",
		"execute_sql",
		"get_current_time",
		"list_datasources",
		"list_tables",
		"sample_table",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tool set mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestExecuteSQLRequiresSQL(t *testing.T) {
	cs, done := connect(t, testApp())
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "execute_sql",
		Arguments: map[string]any{"sql": "   "},
	})
	if err != nil {
		t.Fatalf("call tool returned transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error result, got success: %+v", res)
	}
	if text := contentText(res); !strings.Contains(text, "sql is required") {
		t.Fatalf("expected 'sql is required', got: %q", text)
	}
}

func TestHighRiskToolSchemasRequireDatasourceWhenMultipleConfigured(t *testing.T) {
	application := app.New(config.Config{
		Default: "t", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"t":     {Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"},
			"other": {Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"},
			"r":     {Driver: "redis", Host: "127.0.0.1"},
		},
	}, ".db-mcp.toml", defaultRegistry())
	cs, done := connect(t, application)
	defer done()

	byName := toolsByName(t, cs)
	for _, toolName := range []string{"execute_sql", "redis_command"} {
		required := requiredFields(t, byName[toolName])
		if !contains(required, "datasource") {
			t.Fatalf("%s schema required = %v, want datasource required", toolName, required)
		}
	}
}

func TestHighRiskToolSchemasAllowDefaultDatasourceWhenSingleConfigured(t *testing.T) {
	cs, done := connect(t, testApp())
	defer done()
	if required := requiredFields(t, toolsByName(t, cs)["execute_sql"]); contains(required, "datasource") {
		t.Fatalf("single SQL datasource execute_sql required = %v, datasource should be optional", required)
	}

	cs, done = connect(t, redisOnlyApp())
	defer done()
	if required := requiredFields(t, toolsByName(t, cs)["redis_command"]); contains(required, "datasource") {
		t.Fatalf("single Redis datasource redis_command required = %v, datasource should be optional", required)
	}
}

func requiredFields(t *testing.T, tool *mcp.Tool) []string {
	t.Helper()
	if tool == nil {
		t.Fatal("tool not registered")
	}
	switch schema := tool.InputSchema.(type) {
	case *jsonschema.Schema:
		return schema.Required
	default:
		var decoded struct {
			Required []string `json:"required"`
		}
		blob, err := json.Marshal(schema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		if err := json.Unmarshal(blob, &decoded); err != nil {
			t.Fatalf("unmarshal input schema: %v", err)
		}
		return decoded.Required
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func contentText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

type fakeKVEngine struct{}

func (fakeKVEngine) Kind() string { return "testkv" }
func (fakeKVEngine) CurrentTime(context.Context) (result.SQLResult, error) {
	return result.SQLResult{Success: true}, nil
}
func (fakeKVEngine) Close() error { return nil }

func testKVRegistry() *engine.Registry {
	registry := defaultRegistry()
	registry.Register("testkv", engine.Spec{
		Category: engine.CategoryKV,
		Factory: func(config.DatasourceConfig, config.Config) (engine.Engine, error) {
			return fakeKVEngine{}, nil
		},
	})
	return registry
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func TestConditionalToolRegistrationNoSQL(t *testing.T) {
	application := app.New(config.Config{
		Default: "k", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"k": {Driver: "testkv", Host: "x"},
		},
	}, ".db-mcp.toml", testKVRegistry())
	cs, done := connect(t, application)
	defer done()

	got := toolNames(t, cs)
	gotSet := make(map[string]bool, len(got))
	for _, n := range got {
		gotSet[n] = true
	}
	for _, meta := range []string{"list_datasources", "current_datasource", "get_current_time"} {
		if !gotSet[meta] {
			t.Fatalf("missing shared meta tool %q in %v", meta, got)
		}
	}
	for _, sqlTool := range []string{"list_tables", "describe_table", "sample_table", "execute_sql"} {
		if gotSet[sqlTool] {
			t.Fatalf("SQL tool %q must not be registered without a SQL datasource: %v", sqlTool, got)
		}
	}
}

func TestWrongKindSQLToolErrors(t *testing.T) {
	application := app.New(config.Config{
		Default: "t", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"t": {Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"},
			"k": {Driver: "testkv", Host: "x", Database: "d", Username: "u"},
		},
	}, ".db-mcp.toml", testKVRegistry())
	cs, done := connect(t, application)
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_tables",
		Arguments: map[string]any{"datasource": "k"},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result, got: %+v", res)
	}
	if text := contentText(res); !strings.Contains(text, "does not support SQL operations") {
		t.Fatalf("expected capability error, got: %q", text)
	}
}

func TestToolAnnotations(t *testing.T) {
	cs, done := connect(t, testApp())
	defer done()

	byName := toolsByName(t, cs)
	for _, name := range []string{
		"list_datasources", "current_datasource", "get_current_time",
		"list_tables", "describe_table", "sample_table",
	} {
		tool := byName[name]
		if tool == nil {
			t.Fatalf("tool %q not registered", name)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %q must set ReadOnlyHint=true, got %+v", name, tool.Annotations)
		}
		if tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			t.Fatalf("read-only tool %q must not be destructive", name)
		}
	}

	w := byName["execute_sql"]
	if w == nil || w.Annotations == nil {
		t.Fatalf("execute_sql missing annotations")
	}
	if w.Annotations.ReadOnlyHint {
		t.Fatalf("execute_sql must not be read-only")
	}
	if w.Annotations.DestructiveHint == nil || !*w.Annotations.DestructiveHint {
		t.Fatalf("execute_sql must set DestructiveHint=true, got %+v", w.Annotations)
	}
	if w.Title != "Execute SQL" {
		t.Fatalf("execute_sql title = %q, want %q", w.Title, "Execute SQL")
	}
}

func toolsByName(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

func readOnlySQLApp() *app.App {
	cfg := testApp().Config()
	cfg.ReadOnly = true
	return app.New(cfg, ".db-mcp.toml", defaultRegistry())
}

func TestReadOnlyBlocksSQLWrite(t *testing.T) {
	cs, done := connect(t, readOnlySQLApp())
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "execute_sql",
		Arguments: map[string]any{"sql": "DELETE FROM users"},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected read-only rejection, got success: %+v", res)
	}
	if text := contentText(res); !strings.Contains(text, "read-only mode") {
		t.Fatalf("expected read-only error, got: %q", text)
	}
}

func TestReadOnlyModeAnnotations(t *testing.T) {
	cs, done := connect(t, readOnlySQLApp())
	defer done()

	w := toolsByName(t, cs)["execute_sql"]
	if w == nil || w.Annotations == nil {
		t.Fatalf("execute_sql not registered with annotations")
	}
	if w.Annotations.ReadOnlyHint {
		t.Fatalf("read-only mode must not mark execute_sql ReadOnlyHint=true")
	}
	if w.Annotations.DestructiveHint == nil || !*w.Annotations.DestructiveHint {
		t.Fatalf("execute_sql must keep DestructiveHint=true in read-only mode, got %+v", w.Annotations)
	}
}

func TestReadOnlyRejectsBypasses(t *testing.T) {
	cs, done := connect(t, readOnlySQLApp())
	defer done()

	for _, sqlText := range []string{
		"WITH cte AS (SELECT 1) DELETE FROM users",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"SELECT * FROM users INTO OUTFILE '/tmp/x'",
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "execute_sql",
			Arguments: map[string]any{"sql": sqlText},
		})
		if err != nil {
			t.Fatalf("transport error for %q: %v", sqlText, err)
		}
		if !res.IsError {
			t.Fatalf("read-only mode must reject %q, got success", sqlText)
		}
		if text := contentText(res); !strings.Contains(text, "read-only mode") {
			t.Fatalf("expected read-only error for %q, got: %q", sqlText, text)
		}
	}
}

func redisOnlyApp() *app.App {
	return app.New(config.Config{
		Default: "r", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"r": {Driver: "redis", Host: "127.0.0.1"},
		},
	}, ".db-mcp.toml", defaultRegistry())
}

func TestRedisToolsRegisteredKVOnly(t *testing.T) {
	cs, done := connect(t, redisOnlyApp())
	defer done()

	got := toolNames(t, cs)
	want := []string{
		"current_datasource",
		"get_current_time",
		"list_datasources",
		"redis_command",
		"redis_get",
		"redis_scan",
		"redis_ttl",
		"redis_type",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("redis-only tool set mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestRedisLiveSmoke(t *testing.T) {
	addr := os.Getenv("DB_MCP_TEST_REDIS")
	if addr == "" {
		t.Skip("set DB_MCP_TEST_REDIS=host:port to run the live redis smoke test")
	}
	host, portStr, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("DB_MCP_TEST_REDIS must be host:port, got %q", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in DB_MCP_TEST_REDIS: %v", err)
	}

	application := app.New(config.Config{
		Default: "r", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"r": {Driver: "redis", Host: host, Port: port},
		},
	}, ".db-mcp.toml", defaultRegistry())
	cs, done := connect(t, application)
	defer done()
	ctx := context.Background()

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s transport error: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s failed: %s", name, contentText(res))
		}
		return res
	}

	const key = "obmcp:smoke"
	call("redis_command", map[string]any{"command": []any{"SET", key, "hello"}})
	defer call("redis_command", map[string]any{"command": []any{"DEL", key}})

	getRes := call("redis_get", map[string]any{"key": key})
	if blob, _ := json.Marshal(getRes); !strings.Contains(string(blob), "hello") {
		t.Fatalf("redis_get did not return the stored value: %s", blob)
	}

	typeRes := call("redis_type", map[string]any{"key": key})
	if blob, _ := json.Marshal(typeRes); !strings.Contains(string(blob), "string") {
		t.Fatalf("redis_type expected 'string': %s", blob)
	}

	call("redis_ttl", map[string]any{"key": key})
	call("redis_scan", map[string]any{"pattern": "obmcp:*"})
}

func TestRedisToolAnnotations(t *testing.T) {
	cs, done := connect(t, redisOnlyApp())
	defer done()

	byName := toolsByName(t, cs)
	for _, name := range []string{"redis_scan", "redis_get", "redis_type", "redis_ttl"} {
		tool := byName[name]
		if tool == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("redis tool %q must set ReadOnlyHint=true, got %+v", name, tool)
		}
	}

	cmd := byName["redis_command"]
	if cmd == nil || cmd.Annotations == nil {
		t.Fatalf("redis_command missing annotations")
	}
	if cmd.Annotations.ReadOnlyHint {
		t.Fatalf("redis_command must not be read-only")
	}
	if cmd.Annotations.DestructiveHint == nil || !*cmd.Annotations.DestructiveHint {
		t.Fatalf("redis_command must set DestructiveHint=true, got %+v", cmd.Annotations)
	}
}

func TestReadOnlyBlocksRedisWrite(t *testing.T) {
	application := redisOnlyApp()
	cfg := application.Config()
	cfg.ReadOnly = true
	application = app.New(cfg, ".db-mcp.toml", defaultRegistry())
	cs, done := connect(t, application)
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "redis_command",
		Arguments: map[string]any{"command": []any{"SET", "k", "v"}},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected read-only rejection, got success: %+v", res)
	}
	if text := contentText(res); !strings.Contains(text, "read-only mode") {
		t.Fatalf("expected read-only error, got: %q", text)
	}
}

func TestRedisCommandRequiresExplicitDatasourceWhenMultipleConfigured(t *testing.T) {
	application := app.New(config.Config{
		Default: "r", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"r":     {Driver: "redis", Host: "127.0.0.1"},
			"other": {Driver: "redis", Host: "127.0.0.1"},
		},
	}, ".db-mcp.toml", defaultRegistry())
	cs, done := connect(t, application)
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "redis_command",
		Arguments: map[string]any{"command": []any{"PING"}},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected explicit datasource error, got success: %+v", res)
	}
	if text := contentText(res); !strings.Contains(text, "datasource") || !strings.Contains(text, "required") {
		t.Fatalf("expected datasource schema validation error, got: %q", text)
	}
}

func TestRedisCommandSingleDatasourceMayUseDefault(t *testing.T) {
	cs, done := connect(t, redisOnlyApp())
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "redis_command",
		Arguments: map[string]any{"command": []any{}},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected command validation error, got success: %+v", res)
	}
	text := contentText(res)
	if strings.Contains(text, "requires explicit datasource") || !strings.Contains(text, "command is required") {
		t.Fatalf("expected command validation after default datasource resolution, got: %q", text)
	}
}

func TestReadOnlyRejectsUnboundedRedisRawReads(t *testing.T) {
	application := redisOnlyApp()
	cfg := application.Config()
	cfg.ReadOnly = true
	application = app.New(cfg, ".db-mcp.toml", defaultRegistry())
	cs, done := connect(t, application)
	defer done()

	for _, command := range [][]any{{"SCAN", "0", "COUNT", "1000000"}, {"MGET", "a", "b"}, {"GET", "k"}} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "redis_command",
			Arguments: map[string]any{"command": command},
		})
		if err != nil {
			t.Fatalf("transport error for %v: %v", command, err)
		}
		if !res.IsError {
			t.Fatalf("expected read-only rejection for %v", command)
		}
		if text := contentText(res); !strings.Contains(text, "read-only mode") {
			t.Fatalf("expected read-only error for %v, got: %q", command, text)
		}
	}
}

func TestExecuteSQLRequiresExplicitDatasourceWhenMultipleConfigured(t *testing.T) {
	application := app.New(config.Config{
		Default: "t", MaxRows: 500, QueryTimeoutSeconds: 30,
		Datasources: map[string]config.DatasourceConfig{
			"t":     {Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"},
			"other": {Driver: "mysql", Host: "127.0.0.1", Port: 3306, Database: "d", Username: "u", Password: "p"},
		},
	}, ".db-mcp.toml", defaultRegistry())
	cs, done := connect(t, application)
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "execute_sql",
		Arguments: map[string]any{"sql": "SELECT 1"},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected explicit datasource error, got success: %+v", res)
	}
	if text := contentText(res); !strings.Contains(text, "datasource") || !strings.Contains(text, "required") {
		t.Fatalf("expected datasource schema validation error, got: %q", text)
	}
}
