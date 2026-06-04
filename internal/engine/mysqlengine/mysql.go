package mysqlengine

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/result"
)

func Registrations() []engine.Registration {
	factory := func(ds config.DatasourceConfig, cfg config.Config) (engine.Engine, error) {
		return newSQLEngine(ds, cfg)
	}
	return []engine.Registration{
		{Name: "mysql", Spec: engine.Spec{Category: engine.CategorySQL, Factory: factory}},
		{Name: "oceanbase", Spec: engine.Spec{Category: engine.CategorySQL, Factory: factory}},
	}
}

type sqlEngine struct {
	kind         string
	db           *sql.DB
	queryTimeout time.Duration
	limits       result.Limits
}

func newSQLEngine(ds config.DatasourceConfig, cfg config.Config) (*sqlEngine, error) {
	cfg = config.ApplyDefaults(cfg)
	dsn := mysql.NewConfig()
	dsn.User = ds.Username
	dsn.Passwd = ds.Password
	dsn.Net = "tcp"
	dsn.Addr = fmt.Sprintf("%s:%d", ds.Host, ds.Port)
	dsn.DBName = ds.Database
	dsn.ParseTime = true
	dsn.Timeout = 10 * time.Second
	dsn.ReadTimeout = time.Duration(cfg.QueryTimeoutSeconds) * time.Second
	dsn.WriteTimeout = time.Duration(cfg.QueryTimeoutSeconds) * time.Second
	dsn.Params = map[string]string{"charset": "utf8mb4"}

	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)

	return &sqlEngine{
		kind:         config.DriverName(ds),
		db:           db,
		queryTimeout: time.Duration(cfg.QueryTimeoutSeconds) * time.Second,
		limits:       result.NewLimits(cfg.MaxRows, cfg.MaxValueBytes, cfg.MaxResultBytes),
	}, nil
}

func (e *sqlEngine) Kind() string { return e.kind }

func (e *sqlEngine) Close() error { return e.db.Close() }

func (e *sqlEngine) CurrentTime(ctx context.Context) (result.SQLResult, error) {
	return e.Query(ctx, "SELECT NOW() AS now", 1)
}

func (e *sqlEngine) ListTables(ctx context.Context, maxRows int) (result.SQLResult, error) {
	return e.Query(ctx, "SHOW TABLES", maxRows)
}

func (e *sqlEngine) DescribeTable(ctx context.Context, table string, maxRows int) (result.SQLResult, error) {
	quoted, err := quoteIdentifier(table)
	if err != nil {
		return result.SQLResult{}, err
	}
	return e.Query(ctx, "SHOW FULL COLUMNS FROM "+quoted, maxRows)
}

func (e *sqlEngine) SampleTable(ctx context.Context, table string, limit int) (result.SQLResult, error) {
	quoted, err := quoteIdentifier(table)
	if err != nil {
		return result.SQLResult{}, err
	}
	return e.Query(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT %d", quoted, limit), limit)
}

func (e *sqlEngine) Query(ctx context.Context, sqlText string, maxRows int) (result.SQLResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.queryTimeout)
	defer cancel()

	rows, err := e.db.QueryContext(ctx, sqlText)
	if err != nil {
		return result.SQLResult{}, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return result.SQLResult{}, err
	}

	budget := result.NewBudget(e.limits)
	res := result.SQLResult{SQL: budget.NormalizeText(sqlText), Success: true, Columns: columns}
	values := make([]any, len(columns))
	scanTargets := make([]any, len(columns))
	for i := range values {
		scanTargets[i] = &values[i]
	}

	for rows.Next() {
		if res.Rows >= maxRows {
			res.Truncated = true
			res.TruncationReason = "row_count"
			break
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return result.SQLResult{}, err
		}
		row := make([]any, len(columns))
		for i, value := range values {
			row[i] = result.NormalizeDBValue(value, budget)
		}
		res.Data = append(res.Data, row)
		res.Rows++
		if budget.Truncated() {
			res.Truncated = true
			res.TruncationReason = budget.Reason()
			break
		}
	}
	if err := rows.Err(); err != nil {
		return result.SQLResult{}, err
	}
	return res, nil
}

func (e *sqlEngine) Exec(ctx context.Context, sqlText string) (result.SQLResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.queryTimeout)
	defer cancel()

	res, err := e.db.ExecContext(ctx, sqlText)
	if err != nil {
		return result.SQLResult{}, err
	}
	rowsAffected, _ := res.RowsAffected()
	budget := result.NewBudget(e.limits)
	sqlPreview := budget.NormalizeText(sqlText)
	return result.SQLResult{
		SQL:              sqlPreview,
		Success:          true,
		RowsAffected:     rowsAffected,
		Truncated:        budget.Truncated(),
		TruncationReason: budget.Reason(),
	}, nil
}

var identPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func quoteIdentifier(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !identPattern.MatchString(name) {
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	return "`" + name + "`", nil
}
