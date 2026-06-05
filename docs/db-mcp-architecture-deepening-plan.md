# db-mcp 架构深化最佳改进方案

Status: architecture split implemented / follow-up deepening implemented in
`docs/plans/2026-06-05-db-mcp-architecture-improvement-plan.md`. Prefer current
source, README, tests, and the latest plan for active work.

Mode: Plan
Depth: Deep
Input sources read: `first-principles-planner/SKILL.md`, `first-principles-planner/REFERENCE.md`, `README.md`, `docs/db-mcp-hardening-plan.md`, `docs/multi-db-mcp-architecture.md`, architecture review HTML report, repository file listing, `go.mod`, `.gitignore`, `examples/db-mcp.toml.example`, function/test outline from `main.go`, `engine.go`, `redis.go`, `limits.go`, `*_test.go`

## TL;DR

根问题不是“Go 文件都在根目录”这一件事，而是：db-mcp 已经从单一 OceanBase/MySQL 小工具演进成多 datasource、多 engine、多安全策略的 MCP server，但代码组织仍像一个小型单文件 CLI。高杠杆接口没有被物理模块表达出来，导致 MCP tool surface、datasource resolution、engine registry、read-only policy、result preview 等不同变化原因被压在同一个 package/目录里。

最佳方案：先做“低风险目录化 + MCP tool surface 深化”的垂直切片，再按依赖方向拆 datasource/config、result/policy、engine adapters。不要先做纯机械搬目录，也不要一次性上完整 clean architecture。

推荐目标结构：

```text
cmd/db-mcp/                  # CLI entrypoint only
internal/config/             # TOML/env/properties/JDBC/defaults
internal/result/             # SQL/Redis response DTOs + preview budgets
internal/policy/             # datasource target policy + read-only policies
internal/engine/             # engine interfaces + explicit registry
internal/engine/mysqlengine/ # MySQL/OceanBase adapter
internal/engine/redisengine/ # Redis adapter
internal/app/                # use-case layer: datasource lookup + engine calls
internal/mcpserver/          # MCP SDK, tool catalog, schemas, annotations
```

第一步不要试图把所有包一次性做完。先完成可验证切片：

1. 根目录只保留项目元文件、README、docs、examples、`go.mod`/`go.sum`。
2. `cmd/db-mcp/main.go` 只负责 flags、config path、启动 stdio server。
3. `internal/mcpserver` 作为 deep Module 统一管理 tool catalog、schemas、annotations、handler binding。
4. `internal/app` 暴露 MCP 无关的 application Interface，承接 datasource 和 engine 调用。
5. `go test ./...` 与 `go vet ./...` 保持通过，README build command 改为 `go build ./cmd/db-mcp`。

## 推荐行动计划

| Priority | Change | Likely files | Effort | Risk | Value |
|---|---|---|---:|---|---|
| P0 | 固化目标依赖方向和目录契约 | new doc section or this plan only | 0.2d | 低 | 防止后续拆包走偏 |
| P1 | Command root split：新增 `cmd/db-mcp/main.go`，根 package main 变薄 | `main.go`, `cmd/db-mcp/main.go`, README | 0.4d | 中 | 立刻解决根目录平铺的入口问题 |
| P2 | Deepen MCP tool surface：抽 `internal/mcpserver` 管 tool catalog/schema/annotations/binding | `main.go`, `server_test.go`, `redis_test.go` | 0.8d | 中 | 最高杠杆，回应架构审查 top recommendation |
| P3 | 抽 `internal/app`：MCP SDK 与业务 use-case 解耦 | handler code, app tests | 0.7d | 中 | 测试可绕过 MCP SDK，handler 变薄 |
| P4 | 抽 `internal/config` 和 `internal/policy`：配置解析、datasource policy、read-only policy 独立 | config/readonly helpers, tests | 0.8d | 中 | 把安全策略从工具 glue 中移出 |
| P5 | 抽 `internal/result`：结果 DTO + preview budget 一处拥有截断语义 | `limits.go`, `engine.go`, `redis.go`, tests | 0.6d | 中 | SQL/Redis 共享资源语义 |
| P6 | 抽 `internal/engine/*`：显式 registry + adapter packages | `engine.go`, `redis.go`, tests | 1.0d | 中高 | 为下一种 engine 保持 locality |
| P7 | 清理根目录 artifacts：确认 `.gitignore` 覆盖 exe/dist，删除不应提交的 build outputs | root files | 0.2d | 低 | repo 结构可读、可发布 |
| Total |  |  | 4.7d |  |  |

最小可验证切片建议先做 P1-P3，总计约 1.9d。完成后再判断是否继续 P4-P6。

## 根问题重述

db-mcp 的实际复杂度已经不再是“一个 main package 里几个工具函数”：

- MCP 工具面有共享工具、SQL 工具、Redis 工具、read/write annotations、multi-datasource required schema。
- datasource resolution 同时处理 default、driver category、properties file、JDBC URL、env secret、path resolution。
- engine 层同时包含 SQL engine、Redis engine、registry、capability interfaces。
- safety 层同时包含 SQL read-only、Redis command policy、result limits、target datasource policy。
- 测试已经覆盖 tool registration、annotations、wrong kind、read-only、config defaults、budget 等多条变化轴。

现在这些变化轴主要靠文件名分隔，而不是靠 Go package boundary 分隔。平铺目录只是症状；真正的问题是 Module/Interface 没有提供足够 leverage。

解决标准：

- 新人看目录就能判断“改 MCP schema 去哪里、改 Redis adapter 去哪里、改预算语义去哪里”。
- MCP SDK 依赖集中在 `internal/mcpserver`，不会漏到 engine/config/policy。
- engine adapter 不知道 MCP tool request，也不承担 datasource selection policy。
- safety policy 可以被单测直接调用，不需要构造 MCP server。
- 添加一个新 engine 时，主要新增一个 adapter package 和少量 tool catalog wiring，而不是修改半个 `main.go`。

## 约束拆分

### 真约束

- Go 的目录即 package boundary；如果要解决物理平铺，就必须接受 package API 设计成本。
- 当前 module 是 CLI/binary 项目，根目录需要保留 `go.mod`、README、docs、examples 等项目入口资产。
- MCP SDK tool registration 需要具体 schema/input/result 类型，不能完全抽象成纯配置。
- 现有行为已经有测试覆盖，重构必须保持 `go test ./...` 可作为主要回归信号。
- 项目目标仍是轻量 MCP server，不应引入框架式依赖注入或过度分层。

### 可改约定

- `go build .` 从根目录构建二进制；可以改为 `go build ./cmd/db-mcp`。
- 所有 production Go 文件都在根目录。
- `main.go` 同时承载 config、server、handlers、datasource resolution、policy helper。
- engine registry 使用同 package `init` 注册；拆包后可以改成显式 registry。

### 未验证假设

| # | Assumption | Type | If wrong... | Verification |
|---|---|---|---|---|
| 1 | 用户接受 build path 从 `.` 改为 `./cmd/db-mcp` | convention | 需要保留 root command wrapper | 更新 README 后本地构建验证 |
| 2 | 近期 engine 数量仍是 MySQL/OceanBase + Redis | scope | 需要更强 adapter/plugin 边界 | 添加新 engine 前复核 |
| 3 | 目前无外部 package import 当前 root package API | unverified | package move 会破坏外部使用者 | 项目未发布迹象较强，但发布前确认 |
| 4 | 先拆 tool/app 比先拆 engine 更划算 | design | 如果马上要加 Postgres，engine 先拆可能更急 | 以当前架构审查 top recommendation 为依据 |

## 机制对比

### A. 纯机械搬目录

机制：把 `main.go`、`engine.go`、`redis.go`、`limits.go` 按名字搬到若干目录，尽量少改代码。

优点：

- 初始 diff 看起来简单。
- 能快速减少根目录文件数量。

失败模式：

- Go package boundary 会迫使导出大量本不该导出的类型和 helper。
- 如果没有先设计依赖方向，很容易出现 import cycle。
- 只是把平铺从根目录转移到 `internal/`，没有解决 tool surface shallow 的根因。

结论：不推荐作为主方案。只能作为 P1 的小范围过渡，不能停在这里。

### B. 一次性全量 clean architecture

机制：一次提交把 config/app/domain/ports/adapters/server 全部拆完。

优点：

- 结构最完整，边界最清晰。
- package 会强制依赖方向。

失败模式：

- 对当前代码规模偏重，容易产生大量 exported DTO 和接口。
- review 难，回归定位难。
- 很可能在没有新 engine 需求时提前设计过多 extension points。

结论：最终形态可以借鉴，但不要一次性落地。

### C. 垂直切片式深化模块（推荐）

机制：按变化原因和依赖方向拆模块，每个切片都保持可构建、可测试。第一刀同时解决根目录入口和平铺问题，并优先深化架构审查推荐的 MCP tool surface Module。

优点：

- 直接回应当前最痛点：root flat + `main.go` 承载过多。
- 每个阶段都有 `go test ./...` 验证。
- 不为了抽象而抽象，package 边界来自已有变化轴。
- 后续 P4-P6 可以根据实际需求暂停或继续。

失败模式：

- 中间态会存在 `internal/app` 仍偏大，直到 P4-P6 完成。
- 包路径迁移会让测试文件移动较多，短期 diff 不小。
- 若下一步马上新增第三种数据库，可能需要提前做 P6。

结论：当前最佳方案。

## 推荐目标依赖方向

```text
cmd/db-mcp
  -> internal/config
  -> internal/app
  -> internal/mcpserver

internal/mcpserver
  -> internal/app
  -> internal/result
  -> github.com/modelcontextprotocol/go-sdk

internal/app
  -> internal/config
  -> internal/engine
  -> internal/policy
  -> internal/result

internal/engine
  -> internal/config
  -> internal/result

internal/engine/mysqlengine
  -> internal/engine
  -> internal/config
  -> internal/result

internal/engine/redisengine
  -> internal/engine
  -> internal/config
  -> internal/result

internal/policy
  -> internal/config
  -> internal/result
```

禁止方向：

- `internal/engine/*` 不能 import `internal/mcpserver`。
- `internal/config` 不能 import `internal/app` 或 `internal/engine`。
- `internal/result` 不能 import MCP SDK 或 concrete database clients。
- `cmd/db-mcp` 不承载业务逻辑。

## 目标模块职责

### `cmd/db-mcp`

只做 CLI composition root：

- parse flags；
- resolve config path；
- call `config.Load`；
- construct `app.App`；
- pass app to `mcpserver.New`；
- run stdio server；
- defer close。

### `internal/mcpserver`

这是第一优先级的 deep Module：

- tool catalog；
- JSON schema/input structs；
- annotations；
- conditional registration by configured category；
- high-risk tool datasource required schema；
- MCP request/response adaptation；
- all MCP SDK imports。

理想 Interface：

```go
func New(a *app.App) *mcp.Server
```

外部不需要知道每个工具怎么注册、哪些 schema required、annotations 如何拼。

### `internal/app`

MCP 无关的 use-case layer：

- owns engine cache；
- owns datasource selection；
- exposes methods like `ListDatasources`, `ExecuteSQL`, `RedisCommand`；
- calls policy before engine；
- returns result DTO/error。

这让 handler 测试可以变成 app-level tests 或 mcpserver adapter tests。

### `internal/config`

拥有配置不变量：

- defaults；
- single vs multi datasource default；
- env secret；
- properties file；
- JDBC URL parsing；
- path resolution。

目标是 engine adapter 永远不需要自己补齐 raw config。

### `internal/policy`

拥有所有“是否允许”的规则：

- high-risk tool datasource policy；
- SQL read-only statement policy；
- Redis read-only command policy。

这样安全 hardening 的后续改动不会继续扩大 `main.go`/handler。

### `internal/result`

拥有响应形状和截断语义：

- `SQLResult`；
- Redis result DTOs；
- `ResultLimits`；
- preview budget；
- truncation reason merge。

这是架构审查里的 result preview candidate。先抽包，后续再决定是否把 `resultBudget` Interface 进一步缩小。

### `internal/engine`

拥有 engine ports 和 explicit registry：

- `Engine`；
- `SQLEngine`；
- `KVEngine`；
- `Registry`；
- category/kind metadata。

不要继续依赖跨包 `init` 隐式注册。推荐 composition root 显式装配：

```go
registry := engine.NewRegistry(
    mysqlengine.Factory(),
    redisengine.Factory(),
)
```

## 迁移顺序

### Phase 1: Command root split

目标：根目录不再承载所有 production code。

动作：

- 新增 `cmd/db-mcp/main.go`。
- 将当前 `main()` 中 CLI/startup 逻辑迁到 `cmd/db-mcp`。
- 临时把其余逻辑移动到 `internal/dbmcp` 或直接进入 `internal/app`，以保持可编译。
- README build command 改为 `go build -o ... ./cmd/db-mcp`。

验收：

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go build ./cmd/db-mcp` 通过。
- 根目录不再有 production `.go` 文件，或只保留极薄 compatibility wrapper。

### Phase 2: Tool surface Module

目标：让 MCP tool surface 成为 deep Module。

动作：

- 新增 `internal/mcpserver`。
- 搬迁 `newServer`、annotations helper、tool schema/input structs、tool registration。
- 用 tool catalog 表达 shared/sql/redis tool groups。
- `internal/mcpserver` 只调用 `app.App` 方法，不直接做 engine selection。

验收：

- `TestToolsRegistered`、`TestConditionalToolRegistrationNoSQL`、annotation tests 迁入/保留并通过。
- 新增或保留多 datasource high-risk schema 测试。
- `internal/mcpserver` 是唯一 import MCP SDK 的内部 package。

### Phase 3: App use-case seam

目标：handler glue 和业务执行分离。

动作：

- 新增 `internal/app`。
- `App` 保留 config、engine registry/cache、close。
- 工具方法从 MCP handler signature 改为普通方法，例如：

```go
func (a *App) ExecuteSQL(ctx context.Context, in ExecuteSQLInput) (result.SQLResult, error)
```

- `mcpserver` 做薄 adapter：decode input -> call app -> encode result。

验收：

- app tests 不依赖 MCP request object。
- mcpserver tests 只覆盖 registration/schema/adapter。

### Phase 4: Config and policy extraction

目标：配置和安全策略具备 locality。

动作：

- `loadConfig`, `resolveSecret`, `loadProperties`, `parseJDBCMySQLURL`, `resolvePath` 进入 `internal/config`。
- `isReadOnlyStatement`, `ensureRedisReadOnly`, datasource explicit-target policy 进入 `internal/policy`。
- policy package 不 import MCP SDK。

验收：

- config tests 在 `internal/config`。
- read-only tests 在 `internal/policy`。
- 修改 Redis allowlist 不需要打开 mcpserver/app handler。

### Phase 5: Result extraction

目标：结果预算/预览语义成为独立 Module。

动作：

- `limits.go` 进入 `internal/result`。
- SQL/Redis result structs 进入 `internal/result`。
- `normalizeDBValue` 和 `normalizeRedisValue` 的公共预算逻辑共用 `result.Previewer` 或保持 package-private helper。

验收：

- budget tests 在 `internal/result`。
- engine adapters 只依赖 `result.Limits` 和 result DTO，不知道 MCP。

### Phase 6: Engine adapters

目标：engine Interface 和具体数据库实现分离。

动作：

- `Engine`, `SQLEngine`, `KVEngine`, registry 进入 `internal/engine`。
- SQL implementation 进入 `internal/engine/mysqlengine`。
- Redis implementation 进入 `internal/engine/redisengine`。
- registry 改为显式装配，避免 blank import 或 `init` 魔法。

验收：

- 添加一个 dummy/fake engine factory 的测试不需要 Redis/MySQL client。
- wrong-kind tests 仍通过。
- 新增 engine 时无需改 `internal/mcpserver` 的共享逻辑，只补 tool group 或 adapter wiring。

## 反演测试

推荐方案什么时候会变成坏选择？

- 如果项目不会继续增长，永远只维护 4 个 Go 文件，目录化收益低于包边界成本。
- 如果用户强烈要求 `go build .` 必须保持不变，那么 `cmd/db-mcp` 会带来兼容成本，需要保留 root wrapper 或调整发布脚本。
- 如果马上要做一个完全不同的 datasource family，例如 Elasticsearch/document store，先抽 engine adapter 可能比先抽 tool surface 更紧急。
- 如果外部已有代码 import 当前 module 的 root package API，移动 package 会破坏外部用户；需要先查发布状态和 tags。

这些条件目前没有推翻推荐。当前代码已经有多 engine、安全策略、测试矩阵，且架构审查最高推荐就是 tool surface Module。目录平铺问题让这个推荐更急，而不是更独立。

## 下一步可验证切片

先做 P1-P3，不做 P4-P6：

1. 建 `cmd/db-mcp`、`internal/app`、`internal/mcpserver`。
2. 让 root 不再承载 production implementation。
3. 保持现有工具行为不变。
4. 跑 `go test ./...`、`go vet ./...`、`go build ./cmd/db-mcp`。
5. 对比 `Select-String -Path '**/*.go' -Pattern 'modelcontextprotocol/go-sdk'`，确认 MCP SDK 只在 `internal/mcpserver` 和 command wiring 出现。

如果这个切片通过，再继续 P4-P6。若 P1-P3 出现大量导出类型扩散，暂停并收紧 `internal/result`/input DTO 的归属，而不是继续硬拆。
