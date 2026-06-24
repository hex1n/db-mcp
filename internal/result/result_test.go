package result

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeDBValueBudget(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 4, MaxResultBytes: 100})
	got := NormalizeDBValue([]byte("abcdef"), budget)
	if got != "abcd" {
		t.Fatalf("normalized value = %#v, want abcd", got)
	}
	if !budget.Truncated() || budget.Reason() != "value_bytes" {
		t.Fatalf("expected value byte truncation, got truncated=%v reason=%q", budget.Truncated(), budget.Reason())
	}
}

func TestNormalizeDBValueStringifiesLargeIntegers(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"bigint max", int64(9223372036854775807), "9223372036854775807"},
		{"bigint min", int64(-9223372036854775808), "-9223372036854775808"},
		{"bigint unsigned max", uint64(18446744073709551615), "18446744073709551615"},
		{"snowflake id", int64(1234567890123456789), "1234567890123456789"},
		{"small int still string", int64(3), "3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 64, MaxResultBytes: 1024})
			got := NormalizeDBValue(tc.value, budget)
			if got != tc.want {
				t.Fatalf("NormalizeDBValue(%v) = %#v, want string %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestNormalizeDBValuePreservesIntegerPrecisionEndToEnd exercises the exact path
// that used to corrupt values: marshal a row, then re-parse it the way a float64
// consumer (e.g. JavaScript JSON.parse) does. As strings the digits survive; as
// JSON numbers an int64 beyond 2^53 would not.
func TestNormalizeDBValuePreservesIntegerPrecisionEndToEnd(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 64, MaxResultBytes: 1024})
	const want = "1234567890123456789"
	cell := NormalizeDBValue(int64(1234567890123456789), budget)

	wire, err := json.Marshal([]any{cell})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(wire), `"`+want+`"`) {
		t.Fatalf("wire form should quote the integer, got %s", wire)
	}

	var reparsed []any // numbers would decode to float64 here, like JS JSON.parse
	if err := json.Unmarshal(wire, &reparsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := reparsed[0]; got != want {
		t.Fatalf("after float64 round-trip got %#v, want %q", got, want)
	}
}

func TestNormalizeRedisValueStringifiesLargeIntegers(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 64, MaxResultBytes: 1024})
	got := NormalizeRedisValue(int64(9223372036854775807), budget)
	if got != "9223372036854775807" {
		t.Fatalf("NormalizeRedisValue(int64 max) = %#v, want string", got)
	}

	nested := NormalizeRedisValue([]any{int64(1234567890123456789), "abc"}, budget).([]any)
	if nested[0] != "1234567890123456789" || nested[1] != "abc" {
		t.Fatalf("unexpected nested redis value: %#v", nested)
	}
}

func TestNormalizeRedisValueBudget(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 4, MaxResultBytes: 8})
	got := NormalizeRedisValue([]any{"abcdef", "ghij", "kl"}, budget).([]any)
	if len(got) != 2 || got[0] != "abcd" || got[1] != "ghij" {
		t.Fatalf("unexpected normalized redis value: %#v", got)
	}
	if !budget.Truncated() || !strings.Contains(budget.Reason(), "value_bytes") || !strings.Contains(budget.Reason(), "result_bytes") {
		t.Fatalf("expected value and result truncation, got truncated=%v reason=%q", budget.Truncated(), budget.Reason())
	}
}

func TestNormalizeTextTruncatesUTF8Safely(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 5, MaxResultBytes: 100})
	got := budget.NormalizeText("你好abc")
	if got != "你" {
		t.Fatalf("normalized value = %q, want first complete rune", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("normalized value is not valid UTF-8: %q", got)
	}
	if !strings.Contains(budget.Reason(), "value_bytes") {
		t.Fatalf("expected value byte truncation, got %q", budget.Reason())
	}
}

func TestNormalizeTextResultBudgetTruncatesUTF8Safely(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 100, MaxResultBytes: 5})
	got := budget.NormalizeText("你好abc")
	if got != "你" {
		t.Fatalf("normalized value = %q, want first complete rune", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("normalized value is not valid UTF-8: %q", got)
	}
	if !strings.Contains(budget.Reason(), "result_bytes") {
		t.Fatalf("expected result byte truncation, got %q", budget.Reason())
	}
}

func TestNormalizeRedisValueLargeArrayTruncatesResult(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 10, MaxValueBytes: 100, MaxResultBytes: 6})
	got := NormalizeRedisValue([]any{"abc", "def", "ghi"}, budget).([]any)
	if len(got) != 2 {
		t.Fatalf("normalized array length = %d, want 2: %#v", len(got), got)
	}
	if !budget.Truncated() || !strings.Contains(budget.Reason(), "result_bytes") {
		t.Fatalf("expected result byte truncation, got truncated=%v reason=%q", budget.Truncated(), budget.Reason())
	}
}

func TestBudgetMarkElementCount(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 3, MaxValueBytes: 64, MaxResultBytes: 1024})
	budget.MarkElementCount(4, 3)
	if !budget.Truncated() || budget.Reason() != "element_count" {
		t.Fatalf("expected element_count truncation, got truncated=%v reason=%q", budget.Truncated(), budget.Reason())
	}

	complete := NewBudget(Limits{MaxRows: 3, MaxValueBytes: 64, MaxResultBytes: 1024})
	complete.MarkElementCount(3, 3)
	if complete.Truncated() || complete.Reason() != "" {
		t.Fatalf("complete element count should not truncate, got truncated=%v reason=%q", complete.Truncated(), complete.Reason())
	}
}

func TestBudgetApplyToResultTypes(t *testing.T) {
	budget := NewBudget(Limits{MaxRows: 3, MaxValueBytes: 64, MaxResultBytes: 1024})
	budget.MarkElementCount(4, 3)
	budget.MarkRowCount()

	sql := SQLResult{}
	budget.ApplyToSQLResult(&sql)
	if !sql.Truncated || sql.TruncationReason != "element_count,row_count" {
		t.Fatalf("sql truncation = %v/%q", sql.Truncated, sql.TruncationReason)
	}

	redisGet := RedisGetResult{}
	budget.ApplyToRedisGetResult(&redisGet)
	if !redisGet.Truncated || redisGet.TruncationReason != "element_count,row_count" {
		t.Fatalf("redis get truncation = %v/%q", redisGet.Truncated, redisGet.TruncationReason)
	}

	redisScan := RedisScanResult{}
	budget.ApplyToRedisScanResult(&redisScan)
	if !redisScan.Truncated || redisScan.TruncationReason != "element_count,row_count" {
		t.Fatalf("redis scan truncation = %v/%q", redisScan.Truncated, redisScan.TruncationReason)
	}

	redisCommand := RedisCommandResult{}
	budget.ApplyToRedisCommandResult(&redisCommand)
	if !redisCommand.Truncated || redisCommand.TruncationReason != "element_count,row_count" {
		t.Fatalf("redis command truncation = %v/%q", redisCommand.Truncated, redisCommand.TruncationReason)
	}
}
