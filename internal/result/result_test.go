package result

import (
	"strings"
	"testing"
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
