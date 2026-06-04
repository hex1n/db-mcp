package policy

import (
	"strings"
	"testing"

	"github.com/hex1n/db-mcp/internal/result"
)

func TestIsReadOnlyStatement(t *testing.T) {
	readOnly := []string{"SELECT 1", "  select * from t", "SHOW TABLES", "DESCRIBE t", "EXPLAIN SELECT 1"}
	for _, s := range readOnly {
		if !IsReadOnlyStatement(s) {
			t.Errorf("expected read-only: %q", s)
		}
	}
	writes := []string{
		"DELETE FROM t", "UPDATE t SET x=1", "INSERT INTO t VALUES (1)",
		"WITH c AS (SELECT 1) DELETE FROM t",
		"WITH c AS (SELECT 1) SELECT * FROM c",
		"SELECT * FROM t INTO OUTFILE '/tmp/x'",
		"select * from t into dumpfile '/tmp/x'",
		"",
	}
	for _, s := range writes {
		if IsReadOnlyStatement(s) {
			t.Errorf("expected NOT read-only: %q", s)
		}
	}
}

func TestEnsureRedisReadOnly(t *testing.T) {
	limits := result.Limits{MaxRows: 2, MaxValueBytes: 4, MaxResultBytes: 100}
	allowed := [][]string{{"PING"}, {"ping", "pong"}, {"TIME"}, {"TYPE", "k"}, {"TTL", "k"}, {"EXISTS", "a", "b"}, {"STRLEN", "k"}, {"GETRANGE", "k", "0", "3"}, {"HLEN", "h"}, {"LLEN", "l"}, {"ZCARD", "z"}}
	for _, argv := range allowed {
		if err := EnsureRedisReadOnly(argv, limits); err != nil {
			t.Errorf("%v should be allowed: %v", argv, err)
		}
	}
	rejected := [][]string{{"SET", "k", "v"}, {"DEL", "k"}, {"FLUSHALL"}, {"KEYS", "*"}, {"SCAN", "0"}, {"MGET", "a", "b"}, {"GET", "k"}, {"HGET", "h", "f"}, {"SMEMBERS", "s"}, {"HGETALL", "h"}, {"LRANGE", "l", "0", "10"}, {"ZRANGE", "z", "0", "10"}, {"HSCAN", "h", "0"}, {"DUMP", "k"}, {"EXISTS", "a", "b", "c"}, {"GETRANGE", "k", "0", "4"}, {"GETRANGE", "k", "0", "9223372036854775807"}, {"PING", "12345"}}
	for _, argv := range rejected {
		if err := EnsureRedisReadOnly(argv, limits); err == nil {
			t.Errorf("%v must be rejected in read-only mode", argv)
		}
	}
	if err := EnsureRedisReadOnly(nil, limits); err != nil {
		t.Fatalf("empty command should defer to the engine, got: %v", err)
	}
}

func TestRedisReadOnlyErrorMentionsMode(t *testing.T) {
	err := EnsureRedisReadOnly([]string{"GET", "k"}, result.Limits{MaxRows: 1, MaxValueBytes: 1, MaxResultBytes: 1})
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		t.Fatalf("expected read-only mode error, got: %v", err)
	}
}
