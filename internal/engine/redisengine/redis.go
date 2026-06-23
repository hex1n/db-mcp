package redisengine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/result"
)

func Registrations() []engine.Registration {
	return []engine.Registration{{
		Name: "redis",
		Spec: engine.Spec{
			Category: engine.CategoryKV,
			Factory: func(ds config.DatasourceConfig, cfg config.Config) (engine.Engine, error) {
				return newRedisEngine(ds, cfg)
			},
		},
	}}
}

type redisEngine struct {
	kind         string
	client       *redis.Client
	maxRows      int
	queryTimeout time.Duration
	limits       result.Limits
}

func newRedisEngine(ds config.DatasourceConfig, cfg config.Config) (*redisEngine, error) {
	cfg = config.ApplyDefaults(cfg)
	timeout := time.Duration(cfg.QueryTimeoutSeconds) * time.Second
	limits := result.NewLimits(cfg.MaxRows, cfg.MaxValueBytes, cfg.MaxResultBytes)
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", ds.Host, ds.Port),
		Username:     ds.Username,
		Password:     ds.Password,
		DB:           ds.RedisDB,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		PoolSize:     4,
	})
	return &redisEngine{
		kind:         config.DriverName(ds),
		client:       client,
		maxRows:      limits.MaxRows,
		queryTimeout: timeout,
		limits:       limits,
	}, nil
}

func (e *redisEngine) Kind() string { return e.kind }

func (e *redisEngine) Close() error { return e.client.Close() }

func (e *redisEngine) withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, e.queryTimeout)
}

func (e *redisEngine) capCount(n int) int {
	if n <= 0 || n > e.maxRows {
		return e.maxRows
	}
	return n
}

func (e *redisEngine) CurrentTime(parent context.Context) (result.TimeResult, error) {
	ctx, cancel := e.withTimeout(parent)
	defer cancel()
	tm, err := e.client.Time(ctx).Result()
	if err != nil {
		return result.TimeResult{}, err
	}
	// Redis TIME is an absolute Unix epoch, so any zone renders the same
	// instant; emit RFC3339 with the offset to keep it unambiguous.
	return result.TimeResult{Success: true, Now: tm.Format(time.RFC3339), Timezone: tm.Format("-07:00")}, nil
}

func (e *redisEngine) Scan(parent context.Context, pattern string, count int) (result.RedisScanResult, error) {
	if strings.TrimSpace(pattern) == "" {
		pattern = "*"
	}
	limit := e.capCount(count)
	ctx, cancel := e.withTimeout(parent)
	defer cancel()

	keys := make([]string, 0, limit)
	budget := result.NewBudget(e.limits)
	iter := e.client.Scan(ctx, 0, pattern, int64(limit)).Iterator()
	for iter.Next(ctx) {
		if len(keys) >= limit {
			budget.MarkElementCount(int64(limit+1), len(keys))
			break
		}
		keys = append(keys, budget.NormalizeText(iter.Val()))
		if budget.Exhausted() {
			break
		}
	}
	if err := iter.Err(); err != nil {
		return result.RedisScanResult{}, err
	}
	res := result.RedisScanResult{Success: true, Pattern: pattern, Keys: keys, Count: len(keys)}
	budget.ApplyToRedisScanResult(&res)
	return res, nil
}

func (e *redisEngine) Type(parent context.Context, key string) (result.RedisTypeResult, error) {
	ctx, cancel := e.withTimeout(parent)
	defer cancel()
	t, err := e.client.Type(ctx, key).Result()
	if err != nil {
		return result.RedisTypeResult{}, err
	}
	return result.RedisTypeResult{Success: true, Key: key, Type: t}, nil
}

func (e *redisEngine) TTL(parent context.Context, key string) (result.RedisTTLResult, error) {
	ctx, cancel := e.withTimeout(parent)
	defer cancel()
	secs, err := e.client.Do(ctx, "TTL", key).Int64()
	if err != nil {
		return result.RedisTTLResult{}, err
	}
	res := result.RedisTTLResult{Success: true, Key: key, TTLSeconds: secs}
	switch secs {
	case -1:
		res.Note = "key exists but has no expiration"
	case -2:
		res.Note = "key does not exist"
	}
	return res, nil
}

func (e *redisEngine) Get(parent context.Context, key string) (result.RedisGetResult, error) {
	ctx, cancel := e.withTimeout(parent)
	defer cancel()

	t, err := e.client.Type(ctx, key).Result()
	if err != nil {
		return result.RedisGetResult{}, err
	}
	res := result.RedisGetResult{Success: true, Key: key, Type: t}
	limit := e.maxRows
	budget := result.NewBudget(e.limits)

	switch t {
	case "none":
		res.Exists = false
		return res, nil
	case "string":
		n, err := e.client.StrLen(ctx, key).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		maxValueBytes := int64(e.limits.MaxValueBytes)
		if n > maxValueBytes {
			v, err := e.client.GetRange(ctx, key, 0, maxValueBytes-1).Result()
			if err != nil {
				return result.RedisGetResult{}, err
			}
			res.Value = budget.NormalizeText(v)
			budget.AddReason("value_bytes")
		} else {
			v, err := e.client.Get(ctx, key).Result()
			if err != nil {
				return result.RedisGetResult{}, err
			}
			res.Value = result.NormalizeRedisValue(v, budget)
		}
	case "list":
		total, err := e.client.LLen(ctx, key).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		v, err := e.client.LRange(ctx, key, 0, int64(limit-1)).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		normalized := result.NormalizeRedisValue(v, budget)
		res.Value = normalized
		returned := len(v)
		if values, ok := normalized.([]string); ok {
			returned = len(values)
		}
		budget.MarkElementCount(total, returned)
	case "set":
		total, err := e.client.SCard(ctx, key).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		members := make([]string, 0, limit)
		iter := e.client.SScan(ctx, key, 0, "", int64(limit)).Iterator()
		for iter.Next(ctx) {
			if len(members) >= limit {
				break
			}
			members = append(members, budget.NormalizeText(iter.Val()))
			if budget.Exhausted() {
				break
			}
		}
		if err := iter.Err(); err != nil {
			return result.RedisGetResult{}, err
		}
		res.Value = members
		budget.MarkElementCount(total, len(members))
	case "hash":
		total, err := e.client.HLen(ctx, key).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		m := make(map[string]any, limit)
		returned := 0
		iter := e.client.HScan(ctx, key, 0, "", int64(limit)).Iterator()
		for iter.Next(ctx) {
			field := iter.Val()
			if !iter.Next(ctx) {
				break
			}
			value := iter.Val()
			if len(m) >= limit {
				break
			}
			field = budget.NormalizeText(field)
			m[field] = result.NormalizeRedisValue(value, budget)
			returned++
			if budget.Exhausted() {
				break
			}
		}
		if err := iter.Err(); err != nil {
			return result.RedisGetResult{}, err
		}
		res.Value = m
		budget.MarkElementCount(total, returned)
	case "zset":
		total, err := e.client.ZCard(ctx, key).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		z, err := e.client.ZRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
		if err != nil {
			return result.RedisGetResult{}, err
		}
		members := make([]map[string]any, 0, len(z))
		for _, item := range z {
			members = append(members, map[string]any{"member": result.NormalizeRedisValue(item.Member, budget), "score": budget.AccountScalar(item.Score)})
			if budget.Exhausted() {
				break
			}
		}
		res.Value = members
		budget.MarkElementCount(total, len(members))
	default:
		res.Value = nil
	}
	budget.ApplyToRedisGetResult(&res)
	res.Exists = true
	return res, nil
}

func (e *redisEngine) Command(parent context.Context, argv []string) (result.RedisCommandResult, error) {
	if len(argv) == 0 {
		return result.RedisCommandResult{}, errors.New("command is required")
	}
	ctx, cancel := e.withTimeout(parent)
	defer cancel()

	args := make([]any, len(argv))
	for i, a := range argv {
		args[i] = a
	}
	out, err := e.client.Do(ctx, args...).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return result.RedisCommandResult{}, err
	}
	budget := result.NewBudget(e.limits)
	res := result.RedisCommandResult{
		Success: true,
		Command: budget.NormalizeText(strings.Join(argv, " ")),
		Result:  result.NormalizeRedisValue(out, budget),
	}
	budget.ApplyToRedisCommandResult(&res)
	return res, nil
}
