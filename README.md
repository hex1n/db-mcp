# db-mcp

[简体中文](README.zh-CN.md)

Multi-engine database MCP server for Codex and Claude Code. It targets local
test-environment inspection and bounded data operations through a single fast Go
binary. Supported engines:

- **OceanBase** (MySQL protocol)
- **MySQL**
- **Redis**

One server can serve several datasources of different kinds at the same time.

## Features

- Standard MCP stdio server.
- Lazy connections, so startup does not wait for any database.
- Project-scoped TOML datasource config, multiple datasources per project.
- Pluggable engine registry — adding a new database is one registration plus an
  engine implementation.
- Tools are registered per configured kind: a config without SQL datasources
  will not expose SQL tools, and vice versa.
- Direct credentials, environment-backed passwords, or (for SQL) Java
  properties-backed JDBC config.
- Bounded row/element counts via `max_rows`, single-value previews via `max_value_bytes`, and best-effort MCP response caps via `max_result_bytes`.

## Tools

Shared (always available):

- `list_datasources`
- `current_datasource`
- `get_current_time`

SQL engines (`mysql`, `oceanbase`) — registered when a SQL datasource exists:

- `list_tables`
- `describe_table`
- `sample_table`
- `execute_sql`

Redis (`redis`) — registered when a Redis datasource exists:

- `redis_scan` (bounded `SCAN`; preferred over the blocking `KEYS`)
- `redis_get` (auto-detects string/list/set/hash/zset, bounded)
- `redis_type`
- `redis_ttl`
- `redis_command` (arbitrary command in argv form)

Calling a SQL tool against a Redis datasource (or vice versa) returns a clear
capability error. Write/destructive statements (`execute_sql` writes,
`redis_command` `SET`/`DEL`/`FLUSHALL`...) should only be used after explicit
user confirmation.

Every tool carries MCP annotations: read tools set `readOnlyHint`, while
`execute_sql` and `redis_command` set `destructiveHint`; all set
`openWorldHint=false` (a closed datasource, not the open internet). MCP clients
can use these hints to visually separate safe reads from writes and to prompt
before destructive calls.

## Install

The server is cross-platform. Build it on the OS where you will run the MCP
client.

Windows:

```powershell
go build -o "$env:USERPROFILE\bin\db-mcp.exe" ./cmd/db-mcp
```

macOS/Linux:

```bash
go build -o "$HOME/bin/db-mcp" ./cmd/db-mcp
```

Download prebuilt binaries from GitHub Releases after a tagged release exists.
Release assets are published for:

- `linux-amd64`
- `linux-arm64`
- `darwin-amd64` (macOS Intel)
- `darwin-arm64` (macOS Apple Silicon)
- `windows-amd64`

Each release also includes `checksums.txt`.

Or install from GitHub after the repository is published:

```bash
go install github.com/hex1n/db-mcp/cmd/db-mcp@latest
```

If the GitHub owner is not `hex1n`, update the module path in `go.mod` before
publishing.

## Distribution

Pushing a version tag creates a GitHub Release with cross-platform binaries:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow builds with `CGO_ENABLED=0`, injects the tag into
`db-mcp --version`, packages README files and `examples/db-mcp.toml.example`,
and uploads checksums. Supported release targets are Linux/macOS on amd64 and
arm64, plus Windows on amd64.

## Datasource Config

Create `.db-mcp.toml` in the target project root. Do not commit this file when
it contains passwords.

```toml
default = "project_test"
max_rows = 500
max_value_bytes = 65536
max_result_bytes = 1048576
query_timeout_seconds = 30
# read_only = true rejects writes server-side (see "Read-only mode" below).
read_only = false

# OceanBase (MySQL protocol) with an env-backed password.
[datasources.project_test]
driver = "oceanbase"
host = "oceanbase.example.internal"
port = 3306
database = "your_database"
username = "your_user@tenant#cluster"
password_from = "env:DB_MCP_PASSWORD"

# Plain MySQL.
[datasources.mysql_local]
driver = "mysql"
host = "127.0.0.1"
port = 3306
database = "app"
username = "root"
password = "secret"

# Redis. Uses redis_db (logical DB index) instead of a SQL database name.
[datasources.cache_local]
driver = "redis"
host = "127.0.0.1"
port = 6379
redis_db = 0
# password_from = "env:REDIS_PASSWORD"
```

General notes:

- `default` is required when more than one datasource is configured. With a single
  datasource, it may be omitted and resolves to that datasource.
- `max_rows` limits rows or collection elements, not bytes. `max_value_bytes` caps
  each returned value preview, and `max_result_bytes` caps the returned MCP payload
  on a best-effort basis. A `truncated` response is a preview, not full data.
- `execute_sql` and `redis_command` require an explicit `datasource` when multiple
  datasources are configured.

Per-driver notes:

- `driver` defaults to `mysql` when omitted. `oceanbase` is an alias of the
  MySQL driver.
- SQL datasources require `host`, `database`, and `username`. Default port 3306.
- Redis datasources require only `host`; `username`/`password` are optional and
  `redis_db` selects the logical DB (default 0). Default port 6379.
- `password_from = "env:NAME"` reads the password from an environment variable.

A SQL datasource can also be backed by a Java properties file with keys like:

```properties
db.project.url=jdbc:mysql://oceanbase.example.internal:3306/your_database
db.project.username=your_user@tenant#cluster
db.project.password=your_password
```

```toml
[datasources.from_properties]
driver = "mysql"
properties_file = "app/bootstrap/src/main/resources/config/application-test.properties"
properties_prefix = "db.project"
```

`properties_file` is SQL-only.

## Read-only mode

Set `read_only = true` at the top level to refuse mutations server-side:

- `execute_sql` accepts only `SELECT`/`SHOW`/`DESC`/`DESCRIBE`/`EXPLAIN`. `WITH`/
  CTE statements are rejected (a CTE can wrap `DELETE`/`UPDATE`), as is
  `SELECT ... INTO OUTFILE`/`DUMPFILE`.
- `redis_command` accepts only small-result metadata reads or argument-bounded
  reads (`PING`, `TIME`, `TYPE`, `TTL`, `EXISTS`, `STRLEN`, `GETRANGE`, `HLEN`,
  `LLEN`, `SCARD`, `ZCARD`, ...). Writes and raw commands whose result can be
  enlarged by data or arguments (`SET`/`DEL`/`FLUSHALL`, `KEYS`, `SCAN`, `MGET`,
  `HGETALL`, `SMEMBERS`, `LRANGE`, `ZRANGE`, `HSCAN`, ...) are refused; use
  `redis_get`/`redis_scan` for bounded data reads.
- The read-only built-ins (`list_tables`, `redis_get`, ...) are unaffected.
- The write tools keep their `destructiveHint` even in this mode, so clients
  still prompt before running them — read-only is a guard, not a guarantee.

This is a best-effort statement-level guard — it does not parse SQL, so a
side-effecting function inside a `SELECT` is not caught. Result budgets also run
after the database client has produced values, so very large SQL cells may still
consume driver memory before they are truncated for MCP output. For a hard
guarantee, also use a read-only database account or a Redis ACL/read-only
replica.

Config lookup order when `--config` is omitted:

1. `DB_MCP_CONFIG`
2. `$CLAUDE_PROJECT_DIR/.db-mcp.toml`
3. `.db-mcp.toml` in the current working directory

## Codex

Project `.codex/config.toml`:

Windows:

```toml
[mcp_servers.db_mcp_project_test]
command = "C:\\Users\\you\\bin\\db-mcp.exe"
args = ["--config", "C:\\path\\to\\your-project\\.db-mcp.toml"]
```

macOS/Linux:

```toml
[mcp_servers.db_mcp_project_test]
command = "/path/to/db-mcp"
args = ["--config", "/path/to/your-project/.db-mcp.toml"]
```

If `db-mcp` is on `PATH`, `command = "db-mcp"` is also fine.

Restart or reload the Codex session after changing MCP server config.

## Claude Code

Project `.mcp.json`:

Windows:

```json
{
  "mcpServers": {
    "db-mcp-project-test": {
      "command": "${USERPROFILE}/bin/db-mcp.exe",
      "args": [
        "--config",
        "${CLAUDE_PROJECT_DIR:-.}/.db-mcp.toml"
      ]
    }
  }
}
```

macOS/Linux:

```json
{
  "mcpServers": {
    "db-mcp-project-test": {
      "command": "${HOME}/bin/db-mcp",
      "args": [
        "--config",
        "${CLAUDE_PROJECT_DIR:-.}/.db-mcp.toml"
      ]
    }
  }
}
```

Claude Code supports environment-variable expansion in `.mcp.json`, including
`${VAR}` and `${VAR:-default}`.

## Local Verification

Windows:

```powershell
db-mcp.exe --version
```

macOS/Linux:

```bash
db-mcp --version
```

For a SQL datasource, ask the MCP client to run:

```sql
select database() as db_name, now() as db_time
```

For a Redis datasource, ask it to run `redis_command` with `["PING"]`, or
`redis_scan` with pattern `*`.
