package mysqlengine

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/result"
)

func TestQuoteIdentifier(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "users", want: "`users`"},
		{name: "app.users", want: "`app`.`users`"},
		{name: " app.users ", want: "`app`.`users`"},
	}
	for _, tc := range cases {
		got, err := quoteIdentifier(tc.name)
		if err != nil {
			t.Fatalf("quoteIdentifier(%q) unexpected error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("quoteIdentifier(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestQuoteIdentifierRejectsExpressions(t *testing.T) {
	rejected := []string{
		"app.users where 1=1",
		"app.*",
		"`app`.`users`",
		"a.b.c",
		"",
		"app.",
		".users",
	}
	for _, value := range rejected {
		if _, err := quoteIdentifier(value); err == nil {
			t.Fatalf("quoteIdentifier(%q) should reject invalid identifier", value)
		}
	}
}

func TestMySQLLiveSmoke(t *testing.T) {
	runSQLLiveSmoke(t, "DB_MCP_TEST_MYSQL", "mysql")
}

func TestOceanBaseLiveSmoke(t *testing.T) {
	runSQLLiveSmoke(t, "DB_MCP_TEST_OCEANBASE", "oceanbase")
}

func runSQLLiveSmoke(t *testing.T, envPrefix, driver string) {
	t.Helper()
	ds, ok := sqlLiveDatasourceFromEnv(t, envPrefix, driver)
	if !ok {
		return
	}
	eng, err := newSQLEngine(ds, config.Config{MaxRows: 10, QueryTimeoutSeconds: 30})
	if err != nil {
		t.Fatalf("create sql engine: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	timeRes, err := waitForSQLCurrentTime(ctx, eng, 30*time.Second)
	if err != nil {
		t.Fatalf("current time: %v", err)
	}
	if !timeRes.Success || timeRes.Now == "" {
		t.Fatalf("unexpected current time result: %+v", timeRes)
	}

	queryRes, err := eng.Query(ctx, "select database() as db_name, now() as db_time", 1)
	if err != nil {
		t.Fatalf("query smoke: %v", err)
	}
	if !queryRes.Success || !containsString(queryRes.Columns, "db_name") || !containsString(queryRes.Columns, "db_time") {
		t.Fatalf("query smoke columns = %+v", queryRes)
	}

	assertInspectModeIsReadOnly(t, ds)
}

// assertInspectModeIsReadOnly verifies that inspect mode enforces a read-only
// session at the DB, not merely via the policy classifier: a write reaching the
// engine directly must still be rejected by the server.
func assertInspectModeIsReadOnly(t *testing.T, ds config.DatasourceConfig) {
	t.Helper()
	eng, err := newSQLEngine(ds, config.Config{MaxRows: 10, QueryTimeoutSeconds: 30, Mode: config.ModeInspect})
	if err != nil {
		t.Fatalf("create inspect-mode sql engine: %v", err)
	}
	defer eng.Close()

	const probe = "dbmcp_readonly_probe"
	if _, err := eng.Exec(context.Background(), "CREATE TABLE "+probe+" (id int)"); err == nil {
		// The guard failed: drop the table we should not have been able to create.
		if op, oerr := newSQLEngine(ds, config.Config{MaxRows: 10, QueryTimeoutSeconds: 30, Mode: config.ModeOperate}); oerr == nil {
			_, _ = op.Exec(context.Background(), "DROP TABLE IF EXISTS "+probe)
			_ = op.Close()
		}
		t.Fatalf("inspect mode must reject writes at the DB session, but CREATE TABLE succeeded")
	}
}

func waitForSQLCurrentTime(ctx context.Context, eng *sqlEngine, timeout time.Duration) (result.TimeResult, error) {
	deadline := time.Now().Add(timeout)
	var timeRes result.TimeResult
	var err error
	for {
		timeRes, err = eng.CurrentTime(ctx)
		if err == nil {
			return timeRes, nil
		}
		if time.Now().After(deadline) {
			return result.TimeResult{}, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func sqlLiveDatasourceFromEnv(t *testing.T, envPrefix, driver string) (config.DatasourceConfig, bool) {
	t.Helper()
	target := strings.TrimSpace(os.Getenv(envPrefix))
	if target == "" {
		t.Skipf("set %s=host:port/database and %s_USER to run the live %s smoke test", envPrefix, envPrefix, driver)
		return config.DatasourceConfig{}, false
	}

	hostPort, database, ok := strings.Cut(target, "/")
	if !ok || strings.TrimSpace(database) == "" {
		t.Fatalf("%s must be host:port/database, got %q", envPrefix, target)
	}
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("%s must include host:port before /database, got %q: %v", envPrefix, target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in %s: %v", envPrefix, err)
	}
	username := strings.TrimSpace(os.Getenv(envPrefix + "_USER"))
	if username == "" {
		t.Fatalf("%s_USER is required when %s is set", envPrefix, envPrefix)
	}

	return config.DatasourceConfig{
		Driver:   driver,
		Host:     host,
		Port:     port,
		Database: database,
		Username: username,
		Password: os.Getenv(envPrefix + "_PASSWORD"),
	}, true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
