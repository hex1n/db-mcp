# db-mcp

[English](README.md)

面向 Codex 和 Claude Code 的多引擎数据库 MCP 服务器。它以单个快速 Go
二进制为载体，面向本地测试环境巡检和有边界的数据操作。支持的引擎：

- **OceanBase**（MySQL 协议）
- **MySQL**
- **Redis**

一个服务器可以同时服务多个不同类型的数据源。

## 功能

- 标准 MCP stdio 服务器。
- 懒连接，启动时不等待任何数据库。
- 项目级 TOML 数据源配置，支持每个项目配置多个数据源。
- 可插拔引擎注册表：新增数据库只需要一次注册和一个引擎实现。
- 工具按已配置的类型注册：没有 SQL 数据源的配置不会暴露 SQL 工具，反之亦然。
- 支持直接凭据、环境变量密码，或（SQL）基于 Java properties 的 JDBC 配置。
- 两种操作模式：`inspect` 用于代理的有边界读取，`operate` 用于本地明确允许的原生命令/写操作。
- 通过 `max_rows` 限制行数/元素数，通过 `max_value_bytes` 限制单值预览，通过 `max_result_bytes` 尽力限制 MCP 响应大小。

## 工具

共享工具（始终可用）：

- `list_datasources`
- `current_datasource`
- `get_current_time`

SQL 引擎（`mysql`、`oceanbase`）-- 存在 SQL 数据源时注册：

- `list_tables`
- `describe_table`
- `sample_table`
- `execute_sql`

Redis（`redis`）-- 存在 Redis 数据源时注册：

- `redis_scan`（有边界的 `SCAN`；优先于阻塞式 `KEYS`）
- `redis_get`（自动识别 string/list/set/hash/zset，并有边界返回）
- `redis_type`
- `redis_ttl`
- `redis_command`（以 argv 形式执行任意命令）

对 Redis 数据源调用 SQL 工具（或反过来）会返回清晰的能力错误。写入/破坏性语句（`execute_sql` 写入、`redis_command` 中的 `SET`/`DEL`/`FLUSHALL` 等）只应在用户明确确认后使用。

每个工具都带有 MCP annotations：读工具设置 `readOnlyHint`，`execute_sql` 和 `redis_command` 设置 `destructiveHint`；所有工具都设置 `openWorldHint=false`（表示封闭数据源，而不是开放互联网）。MCP 客户端可以用这些提示在视觉上区分安全读取和写入，并在破坏性调用前提示用户。

## 安装

该服务器支持跨平台。请在要运行 MCP 客户端的系统上构建。

Windows：

```powershell
go build -o "$env:USERPROFILE\bin\db-mcp.exe" ./cmd/db-mcp
```

macOS/Linux：

```bash
go build -o "$HOME/bin/db-mcp" ./cmd/db-mcp
```

有 tag release 后，也可以从 GitHub Releases 下载预编译二进制。发布产物覆盖：

- `linux-amd64`
- `linux-arm64`
- `darwin-amd64`（macOS Intel）
- `darwin-arm64`（macOS Apple Silicon）
- `windows-amd64`

每个 release 也会包含 `checksums.txt`。

仓库发布后，也可以从 GitHub 安装：

```bash
go install github.com/hex1n/db-mcp/cmd/db-mcp@latest
```

如果 GitHub owner 不是 `hex1n`，发布前请先更新 `go.mod` 中的 module path。

## 分发

推送版本 tag 会创建 GitHub Release，并上传跨平台二进制：

```bash
git tag v0.1.0
git push origin v0.1.0
```

release workflow 会用 `CGO_ENABLED=0` 构建，把 tag 注入到
`db-mcp --version`，打包 README 文件和 `examples/db-mcp.toml.example`，并上传校验和。当前发布目标是 Linux/macOS 的 amd64 和 arm64，以及 Windows amd64。

## 数据源配置

在目标项目根目录创建 `.db-mcp.toml`。如果文件包含密码，请不要提交它。

```toml
default = "project_test"
max_rows = 500
max_value_bytes = 65536
max_result_bytes = 1048576
query_timeout_seconds = 30
# inspect 是推荐给代理使用的默认姿态。只有在本地测试库明确允许
# 原生命令/写操作时，才使用 mode = "operate"。
mode = "inspect"

# 使用环境变量密码的 OceanBase（MySQL 协议）。
[datasources.project_test]
driver = "oceanbase"
host = "oceanbase.example.internal"
port = 3306
database = "your_database"
username = "your_user@tenant#cluster"
password_from = "env:DB_MCP_PASSWORD"

# 普通 MySQL。
[datasources.mysql_local]
driver = "mysql"
host = "127.0.0.1"
port = 3306
database = "app"
username = "root"
password = "secret"

# Redis。使用 redis_db（逻辑 DB 索引），而不是 SQL database 名称。
[datasources.cache_local]
driver = "redis"
host = "127.0.0.1"
port = 6379
redis_db = 0
# password_from = "env:REDIS_PASSWORD"
```

通用说明：

- 配置多个数据源时必须设置 `default`。只有一个数据源时可以省略，此时会解析为该数据源。
- `max_rows` 限制行数或集合元素数，不限制字节数。`max_value_bytes` 限制每个返回值的预览，`max_result_bytes` 尽力限制返回的 MCP payload。带 `truncated` 的响应是预览，不是完整数据。
- `mode = "inspect"` 会把 `execute_sql` 和 `redis_command` 限制为有边界读取。`mode = "operate"` 保留原生命令/写操作能力。为了向后兼容，省略 `mode` 时 db-mcp 使用 `operate`。
- 旧的 `read_only = true` 仍作为 `mode = "inspect"` 的兼容写法被接受，但不要同时配置 `mode` 和 `read_only`。
- 配置多个数据源时，`execute_sql` 和 `redis_command` 必须显式传入 `datasource`。

各 driver 说明：

- 省略 `driver` 时默认使用 `mysql`。`oceanbase` 是 MySQL driver 的别名。
- SQL 数据源需要 `host`、`database` 和 `username`。默认端口为 3306。
- Redis 数据源只需要 `host`；`username`/`password` 可选，`redis_db` 选择逻辑 DB（默认 0）。默认端口为 6379。
- `password_from = "env:NAME"` 会从环境变量读取密码。

SQL 数据源也可以由 Java properties 文件提供，示例 keys：

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

`properties_file` 仅支持 SQL。

## 操作模式

在顶层设置 `mode = "inspect"`，即可在服务端拒绝变更：

- `execute_sql` 只接受读语句：`SELECT`/`SHOW`/`DESC`/`DESCRIBE`/`EXPLAIN` 以及只读的 `WITH` CTE（包裹 `INSERT`/`UPDATE`/`DELETE`/`REPLACE` 的 CTE 会被拒绝）。`EXPLAIN ANALYZE`（会真正执行语句）和 `SELECT ... INTO OUTFILE`/`DUMPFILE` 也会被拒绝。
- 对 SQL 引擎，inspect 模式还会以只读会话（`transaction_read_only`）建立连接，因此即使有语句绕过分类器（例如带副作用的函数），数据库本身也会拒绝写入。
- `redis_command` 只接受小结果的元数据读取，或由参数限定结果大小的读取（`PING`、`TIME`、`TYPE`、`TTL`、`EXISTS`、`STRLEN`、`GETRANGE`、`HLEN`、`LLEN`、`SCARD`、`ZCARD` 等）。写命令以及结果可能被数据或参数放大的原生命令（`SET`/`DEL`/`FLUSHALL`、`KEYS`、`SCAN`、`MGET`、`HGETALL`、`SMEMBERS`、`LRANGE`、`ZRANGE`、`HSCAN` 等）会被拒绝；请使用 `redis_get`/`redis_scan` 进行有边界的数据读取。
- 内置只读工具（`list_tables`、`redis_get` 等）不受影响。
- 写工具即使在此模式下仍保留 `destructiveHint`，所以客户端仍会在运行前提示。inspect 模式是防护，不是保证。

SQL 语句分类器是一道快速的第一层防护，并不完整解析 SQL。SQL 引擎上由只读 DB 会话兜底，但这依赖引擎遵守 `transaction_read_only`（MySQL 保证；OceanBase MySQL 模式接受该变量）。`redis_command` 没有等价的服务端兜底，其允许清单即是防线。结果预算也发生在数据库客户端产出值之后，所以非常大的 SQL 单元格仍可能在被截断成 MCP 输出前消耗 driver 内存。若需要硬保证，请同时使用只读数据库账号，或 Redis ACL/只读副本。

只有在本地测试库明确允许原生命令/写操作时才使用 `mode = "operate"`。operate 模式下，`execute_sql` 和 `redis_command` 在配置多个数据源时仍必须显式传入 `datasource`，且两个工具仍保留 `destructiveHint`。

旧的 `read_only = true` 仍作为 `mode = "inspect"` 的兼容写法被支持。新配置应使用 `mode`。

省略 `--config` 时，配置查找顺序为：

1. `DB_MCP_CONFIG`
2. `$CLAUDE_PROJECT_DIR/.db-mcp.toml`
3. 当前工作目录下的 `.db-mcp.toml`

## Codex

项目 `.codex/config.toml`：

Windows：

```toml
[mcp_servers.db_mcp_project_test]
command = "C:\\Users\\you\\bin\\db-mcp.exe"
args = ["--config", "C:\\path\\to\\your-project\\.db-mcp.toml"]
```

macOS/Linux：

```toml
[mcp_servers.db_mcp_project_test]
command = "/path/to/db-mcp"
args = ["--config", "/path/to/your-project/.db-mcp.toml"]
```

如果 `db-mcp` 已经在 `PATH` 中，也可以使用 `command = "db-mcp"`。

修改 MCP server 配置后，请重启或重新加载 Codex 会话。

## Claude Code

项目 `.mcp.json`：

Windows：

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

macOS/Linux：

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

Claude Code 的 `.mcp.json` 支持环境变量展开，包括 `${VAR}` 和 `${VAR:-default}`。

## 开发验证

本地检查：

```bash
go test ./...
go vet ./...
go build ./cmd/db-mcp
```

安装或构建二进制后，验证 CLI：

```bash
db-mcp --version
```

对于 SQL 数据源，请让 MCP 客户端运行：

```sql
select database() as db_name, now() as db_time
```

对于 Redis 数据源，请让客户端以 `["PING"]` 运行 `redis_command`，或以 pattern `*` 运行 `redis_scan`。

可选的真实连接 smoke tests 默认跳过；只有设置对应环境变量时才会运行：

```bash
DB_MCP_TEST_MYSQL=127.0.0.1:3306/app \
DB_MCP_TEST_MYSQL_USER=root \
DB_MCP_TEST_MYSQL_PASSWORD=secret \
go test -run TestMySQLLiveSmoke ./internal/engine/mysqlengine

DB_MCP_TEST_OCEANBASE=oceanbase.example.internal:3306/app \
DB_MCP_TEST_OCEANBASE_USER='user@tenant#cluster' \
DB_MCP_TEST_OCEANBASE_PASSWORD=secret \
go test -run TestOceanBaseLiveSmoke ./internal/engine/mysqlengine

DB_MCP_TEST_REDIS=127.0.0.1:6379 \
go test -run TestRedisLiveSmoke ./internal/engine/redisengine
```

CI 会在 Linux、macOS 和 Windows 上运行本地检查，并在 Linux service containers 上运行 MySQL 与 Redis 真实连接 smoke tests。OceanBase 仍保留为环境变量触发的 smoke test，因为它需要兼容的外部测试实例。

发布 tag 前，请确认 `main` 的 CI 为绿色。release workflow 会打包上文同一个 `./cmd/db-mcp` 二进制路径。

## 边界

db-mcp 不是生产数据库安全网关。它不提供完整 SQL 解析、RBAC、审计审批流、Redis Cluster/Sentinel 支持或 PostgreSQL 支持。需要硬安全边界时，请使用数据库侧只读账号、Redis ACL 和网络隔离。

## 文档

当前文档索引和历史计划状态见 `docs/README.md`。
