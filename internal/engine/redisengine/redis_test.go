package redisengine

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hex1n/db-mcp/internal/config"
)

func TestRedisLiveSmoke(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("DB_MCP_TEST_REDIS"))
	if target == "" {
		t.Skip("set DB_MCP_TEST_REDIS=host:port to run the live redis smoke test")
	}
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatalf("DB_MCP_TEST_REDIS must be host:port, got %q: %v", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in DB_MCP_TEST_REDIS: %v", err)
	}

	eng, err := newRedisEngine(config.DatasourceConfig{Driver: "redis", Host: host, Port: port}, config.Config{MaxRows: 500, QueryTimeoutSeconds: 30})
	if err != nil {
		t.Fatalf("create redis engine: %v", err)
	}
	defer eng.Close()
	ctx := context.Background()

	const key = "dbmcp:smoke"
	if _, err := eng.Command(ctx, []string{"SET", key, "hello"}); err != nil {
		t.Fatalf("SET smoke key: %v", err)
	}
	defer func() {
		if _, err := eng.Command(context.Background(), []string{"DEL", key}); err != nil {
			t.Fatalf("DEL smoke key: %v", err)
		}
	}()

	getRes, err := eng.Get(ctx, key)
	if err != nil {
		t.Fatalf("redis get: %v", err)
	}
	if !getRes.Exists || getRes.Value != "hello" {
		t.Fatalf("redis_get result = %+v, want value hello", getRes)
	}
	typeRes, err := eng.Type(ctx, key)
	if err != nil {
		t.Fatalf("redis type: %v", err)
	}
	if typeRes.Type != "string" {
		t.Fatalf("redis type = %q, want string", typeRes.Type)
	}
	if _, err := eng.TTL(ctx, key); err != nil {
		t.Fatalf("redis ttl: %v", err)
	}
	scanRes, err := eng.Scan(ctx, "dbmcp:*", 500)
	if err != nil {
		t.Fatalf("redis scan: %v", err)
	}
	if scanRes.Count == 0 {
		t.Fatalf("redis scan did not find smoke key: %+v", scanRes)
	}
}
