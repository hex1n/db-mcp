package result

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Limits struct {
	MaxRows        int
	MaxValueBytes  int
	MaxResultBytes int
}

func NewLimits(maxRows, maxValueBytes, maxResultBytes int) Limits {
	return Limits{
		MaxRows:        maxRows,
		MaxValueBytes:  maxValueBytes,
		MaxResultBytes: maxResultBytes,
	}
}

type Budget struct {
	limits    Limits
	usedBytes int
	reasons   map[string]bool
}

func NewBudget(limits Limits) *Budget {
	return &Budget{limits: limits, reasons: make(map[string]bool)}
}

func (b *Budget) Truncated() bool {
	return len(b.reasons) > 0
}

func (b *Budget) Reason() string {
	if len(b.reasons) == 0 {
		return ""
	}
	reasons := make([]string, 0, len(b.reasons))
	for reason := range b.reasons {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return strings.Join(reasons, ",")
}

func (b *Budget) AddReason(reason string) {
	if b != nil && reason != "" {
		b.reasons[reason] = true
	}
}

func (b *Budget) Exhausted() bool {
	return b != nil && b.limits.MaxResultBytes > 0 && b.usedBytes >= b.limits.MaxResultBytes
}

func (b *Budget) NormalizeText(value string) string {
	if b == nil {
		return value
	}
	if b.limits.MaxValueBytes > 0 && len(value) > b.limits.MaxValueBytes {
		value = value[:b.limits.MaxValueBytes]
		b.AddReason("value_bytes")
	}
	if b.limits.MaxResultBytes <= 0 {
		return value
	}
	remaining := b.limits.MaxResultBytes - b.usedBytes
	if remaining <= 0 {
		b.AddReason("result_bytes")
		return ""
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.AddReason("result_bytes")
	}
	b.usedBytes += len(value)
	return value
}

func (b *Budget) AccountScalar(value any) any {
	if b == nil {
		return value
	}
	if b.limits.MaxResultBytes <= 0 {
		return value
	}
	text := fmt.Sprint(value)
	remaining := b.limits.MaxResultBytes - b.usedBytes
	if remaining <= 0 {
		b.AddReason("result_bytes")
		return nil
	}
	if len(text) > remaining {
		b.AddReason("result_bytes")
		return nil
	}
	b.usedBytes += len(text)
	return value
}

type SQLResult struct {
	Datasource       string   `json:"datasource"`
	SQL              string   `json:"sql,omitempty"`
	Success          bool     `json:"success"`
	Columns          []string `json:"columns,omitempty"`
	Data             [][]any  `json:"data,omitempty"`
	Rows             int      `json:"rows"`
	RowsAffected     int64    `json:"rows_affected,omitempty"`
	Truncated        bool     `json:"truncated,omitempty"`
	TruncationReason string   `json:"truncation_reason,omitempty"`
}

type DatasourceInfo struct {
	Default     string   `json:"default,omitempty"`
	Name        string   `json:"name,omitempty"`
	Driver      string   `json:"driver,omitempty"`
	Host        string   `json:"host,omitempty"`
	Port        int      `json:"port,omitempty"`
	Database    string   `json:"database,omitempty"`
	Username    string   `json:"username,omitempty"`
	RedisDB     int      `json:"redis_db,omitempty"`
	ReadOnly    bool     `json:"read_only,omitempty"`
	Datasources []string `json:"datasources,omitempty"`
}

type RedisScanResult struct {
	Datasource       string   `json:"datasource"`
	Success          bool     `json:"success"`
	Pattern          string   `json:"pattern,omitempty"`
	Keys             []string `json:"keys,omitempty"`
	Count            int      `json:"count"`
	Truncated        bool     `json:"truncated,omitempty"`
	TruncationReason string   `json:"truncation_reason,omitempty"`
}

type RedisGetResult struct {
	Datasource       string `json:"datasource"`
	Success          bool   `json:"success"`
	Key              string `json:"key"`
	Type             string `json:"type,omitempty"`
	Exists           bool   `json:"exists"`
	Value            any    `json:"value,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	TruncationReason string `json:"truncation_reason,omitempty"`
}

type RedisTypeResult struct {
	Datasource string `json:"datasource"`
	Success    bool   `json:"success"`
	Key        string `json:"key"`
	Type       string `json:"type"`
}

type RedisTTLResult struct {
	Datasource string `json:"datasource"`
	Success    bool   `json:"success"`
	Key        string `json:"key"`
	TTLSeconds int64  `json:"ttl_seconds"`
	Note       string `json:"note,omitempty"`
}

type RedisCommandResult struct {
	Datasource       string `json:"datasource"`
	Success          bool   `json:"success"`
	Command          string `json:"command,omitempty"`
	Result           any    `json:"result,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	TruncationReason string `json:"truncation_reason,omitempty"`
}

func NormalizeDBValue(value any, budget *Budget) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return budget.NormalizeText(string(v))
	case time.Time:
		return budget.NormalizeText(v.Format("2006-01-02 15:04:05"))
	case string:
		return budget.NormalizeText(v)
	default:
		return budget.AccountScalar(v)
	}
}

func NormalizeRedisValue(v any, budget *Budget) any {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return budget.NormalizeText(val)
	case []byte:
		return budget.NormalizeText(string(val))
	case []string:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if budget.Exhausted() {
				budget.AddReason("result_bytes")
				break
			}
			out = append(out, budget.NormalizeText(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for _, item := range val {
			if budget.Exhausted() {
				budget.AddReason("result_bytes")
				break
			}
			out = append(out, NormalizeRedisValue(item, budget))
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(val))
		for k, item := range val {
			if budget.Exhausted() {
				budget.AddReason("result_bytes")
				break
			}
			out[budget.NormalizeText(k)] = NormalizeRedisValue(item, budget)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			if budget.Exhausted() {
				budget.AddReason("result_bytes")
				break
			}
			out[budget.NormalizeText(k)] = NormalizeRedisValue(item, budget)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			if budget.Exhausted() {
				budget.AddReason("result_bytes")
				break
			}
			out[budget.NormalizeText(fmt.Sprintf("%v", k))] = NormalizeRedisValue(item, budget)
		}
		return out
	default:
		return budget.AccountScalar(val)
	}
}
