package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hex1n/db-mcp/internal/result"
)

func DatasourceName(name, defaultName string) string {
	if strings.TrimSpace(name) == "" {
		return defaultName
	}
	return strings.TrimSpace(name)
}

// DatasourceNameForTool resolves the datasource for a tool call. When the name
// is empty it falls back to defaultName, unless requireExplicit is set (the
// caller's rule for tools that must not guess, e.g. writes with multiple
// datasources configured). The tool argument is only an error label, so policy
// stays decoupled from the concrete tool names.
func DatasourceNameForTool(tool, name string, requireExplicit bool, defaultName string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed != "" {
		return trimmed, nil
	}
	if requireExplicit {
		return "", fmt.Errorf("%s requires explicit datasource when multiple datasources are configured", tool)
	}
	return defaultName, nil
}

func CapRows(rows, maxRows int) int {
	if rows <= 0 {
		return maxRows
	}
	if rows > maxRows {
		return maxRows
	}
	return rows
}

func IsQueryStatement(sqlText string) bool {
	trimmed := strings.TrimSpace(strings.TrimLeft(sqlText, "(\ufeff"))
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "select", "show", "desc", "describe", "explain", "with":
		return true
	default:
		return false
	}
}

var selectIntoFilePattern = regexp.MustCompile(`(?i)\binto\s+(outfile|dumpfile)\b`)

// writeStatementPattern matches DML verbs that can be the main statement of a
// `WITH ... <write>` CTE (MySQL allows WITH to precede SELECT/UPDATE/DELETE).
var writeStatementPattern = regexp.MustCompile(`(?i)\b(insert|update|delete|replace)\b`)

// IsReadOnlyStatement reports whether a statement is a guaranteed read. It is a
// fast, engine-agnostic first line of defense; in inspect mode the SQL engine
// additionally enforces a read-only DB session so a statement that slips past
// this classifier (e.g. a side-effecting function) is still rejected.
func IsReadOnlyStatement(sqlText string) bool {
	trimmed := strings.TrimSpace(strings.TrimLeft(sqlText, "(\ufeff"))
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "select", "show", "desc", "describe":
		return !selectIntoFilePattern.MatchString(trimmed)
	case "explain":
		// Plain EXPLAIN/DESCRIBE only builds a plan and never executes the
		// inner statement, so `EXPLAIN UPDATE ...` is safe. EXPLAIN ANALYZE
		// (MySQL 8.0.18+) actually runs the statement, so it is rejected.
		if len(fields) >= 2 && strings.EqualFold(fields[1], "analyze") {
			return false
		}
		return !selectIntoFilePattern.MatchString(trimmed)
	case "with":
		// Read-only CTE. Allowed only when it carries no DML verb so a
		// `WITH ... DELETE` cannot slip through the read-only gate.
		return !writeStatementPattern.MatchString(trimmed) && !selectIntoFilePattern.MatchString(trimmed)
	default:
		return false
	}
}

type redisReadOnlyCommandRule struct {
	minArgs int
	maxArgs int
}

const redisMaxArgsFromRows = -1

var redisReadOnlyCommandRules = map[string]redisReadOnlyCommandRule{
	"PING":        {minArgs: 0, maxArgs: 1},
	"TIME":        {minArgs: 0, maxArgs: 0},
	"DBSIZE":      {minArgs: 0, maxArgs: 0},
	"TYPE":        {minArgs: 1, maxArgs: 1},
	"TTL":         {minArgs: 1, maxArgs: 1},
	"PTTL":        {minArgs: 1, maxArgs: 1},
	"EXPIRETIME":  {minArgs: 1, maxArgs: 1},
	"PEXPIRETIME": {minArgs: 1, maxArgs: 1},
	"EXISTS":      {minArgs: 1, maxArgs: redisMaxArgsFromRows},
	"STRLEN":      {minArgs: 1, maxArgs: 1},
	"GETRANGE":    {minArgs: 3, maxArgs: 3},
	"SUBSTR":      {minArgs: 3, maxArgs: 3},
	"HLEN":        {minArgs: 1, maxArgs: 1},
	"HEXISTS":     {minArgs: 2, maxArgs: 2},
	"HSTRLEN":     {minArgs: 2, maxArgs: 2},
	"LLEN":        {minArgs: 1, maxArgs: 1},
	"SCARD":       {minArgs: 1, maxArgs: 1},
	"SISMEMBER":   {minArgs: 2, maxArgs: 2},
	"ZCARD":       {minArgs: 1, maxArgs: 1},
	"ZSCORE":      {minArgs: 2, maxArgs: 2},
	"ZRANK":       {minArgs: 2, maxArgs: 2},
	"ZREVRANK":    {minArgs: 2, maxArgs: 2},
	"ZCOUNT":      {minArgs: 3, maxArgs: 3},
	"ZLEXCOUNT":   {minArgs: 3, maxArgs: 3},
	"XLEN":        {minArgs: 1, maxArgs: 1},
	"GETBIT":      {minArgs: 2, maxArgs: 2},
	"GEODIST":     {minArgs: 3, maxArgs: 4},
}

func EnsureRedisReadOnly(argv []string, limits result.Limits) error {
	if len(argv) == 0 {
		return nil
	}
	cmd := strings.ToUpper(strings.TrimSpace(argv[0]))
	rule, ok := redisReadOnlyCommandRules[cmd]
	if !ok {
		return fmt.Errorf("read-only mode: refusing Redis command %q (use redis_get/redis_scan for bounded data reads)", cmd)
	}
	argc := len(argv) - 1
	maxArgs := rule.maxArgs
	if maxArgs == redisMaxArgsFromRows {
		maxArgs = limits.MaxRows
	}
	if argc < rule.minArgs || argc > maxArgs {
		return fmt.Errorf("read-only mode: Redis command %q expects %d-%d args, got %d", cmd, rule.minArgs, maxArgs, argc)
	}
	if cmd == "PING" && argc == 1 && len(argv[1]) > limits.MaxValueBytes {
		return fmt.Errorf("read-only mode: Redis command %q argument exceeds max_value_bytes", cmd)
	}
	if cmd == "GETRANGE" || cmd == "SUBSTR" {
		return validateRedisRangeRead(argv, limits)
	}
	return nil
}

func validateRedisRangeRead(argv []string, limits result.Limits) error {
	start, err := strconv.ParseInt(argv[2], 10, 64)
	if err != nil {
		return fmt.Errorf("read-only mode: Redis command %q has invalid start offset: %w", strings.ToUpper(argv[0]), err)
	}
	end, err := strconv.ParseInt(argv[3], 10, 64)
	if err != nil {
		return fmt.Errorf("read-only mode: Redis command %q has invalid end offset: %w", strings.ToUpper(argv[0]), err)
	}
	if start < 0 || end < start {
		return fmt.Errorf("read-only mode: Redis command %q requires a non-negative bounded range", strings.ToUpper(argv[0]))
	}
	if end-start >= int64(limits.MaxValueBytes) {
		return fmt.Errorf("read-only mode: Redis command %q range exceeds max_value_bytes", strings.ToUpper(argv[0]))
	}
	return nil
}
