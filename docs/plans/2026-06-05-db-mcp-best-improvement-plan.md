# db-mcp 最佳改进计划

Mode: Plan
Depth: Deep
Input sources read this session: `first-principles-planner/SKILL.md`, `first-principles-planner/REFERENCE.md`, `docs/research/2026-06-05-db-mcp-project-deep-analysis.md`, `.github/workflows/ci.yml`, local `rg` search over `.github/`, `docs/`, `examples/`, `internal/`, `cmd/`, `README.md`, `README.zh-CN.md`.

## TL;DR

db-mcp 当前的问题不是“还缺一轮大架构重构”。深度分析显示，主要分层已经落地，离线测试、vet、build 也通过；真正的根问题是：**这个多引擎数据库 MCP server 已经具备操作真实数据源的能力，但验证、风险开关、资源边界和文档权威性还没有跟上这个能力级别**。

最佳路线是“证据优先的安全硬化”：

1. 先把真实 MySQL/Redis smoke 放进 CI，关掉最大的不确定性。
2. 再把 raw SQL/Redis 能力产品化为明确的操作模式，而不是只靠工具描述和客户端提示。
3. 补强结果预算的可预测性，尤其是 UTF-8 截断、raw command 大结果和测试矩阵。
4. 小步修正抽象泄漏和 identifier 能力，不做大重构。
5. 标记历史方案文档状态，避免后续维护者按过期 plan 行动。

## Implementation Progress

Status: implemented in the current worktree on 2026-06-05. The later architecture follow-up in `docs/plans/2026-06-05-db-mcp-architecture-improvement-plan.md` moved live smoke ownership from `internal/mcpserver` to engine adapter packages.

| Priority | Status | Evidence |
|---|---|---|
| P0 | Done | `docs/README.md` defines current vs historical docs; historical docs have status blocks. |
| P1 | Done | `.github/workflows/ci.yml` has a Linux integration job with MySQL/Redis service containers; live smoke tests now run under `internal/engine/mysqlengine` and `internal/engine/redisengine`. |
| P2 | Done | `internal/config` supports `mode = "inspect" | "operate"` and rejects `mode` plus `read_only`; tool descriptions reflect mode. |
| P3 | Done | `internal/result` uses UTF-8-safe truncation and has result-budget tests for text, arrays, and budget application. |
| P4 | Done | `engine.CurrentTime` returns `result.TimeResult`; SQL and Redis adapters return the same neutral result shape. |
| P5 | Done | MySQL identifier quoting supports `schema.table` and rejects expressions. |
| P6 | Done | README, README.zh-CN, examples, CI, and docs index describe verification, boundaries, and current docs. |

Final review and verification on 2026-06-05:

- The architecture follow-up plan completed the remaining ownership cleanup without changing the broad package topology.
- MySQL/Redis live smoke ownership now sits in engine adapter packages, and the MySQL smoke includes a startup retry to avoid service-container races.
- Passed `go test ./...`, `go vet ./...`, `go build -o /tmp/db-mcp-arch-build ./cmd/db-mcp`, `git diff --check HEAD`, coverage collection, and local Docker MySQL 8.4 + Redis 7 adapter live smoke.

## 推荐行动计划

| Priority | Change | Likely files | Effort | Risk | Value |
|---|---|---|---:|---|---|
| P0 | 固化当前基线：新增 plan/research 引用、标记历史 docs 状态，不改行为 | `docs/*.md`, `README.md` 或 `docs/README.md` | 0.4d | 低 | 防止重复规划和误读 |
| P1 | 加 Linux integration CI：用 MySQL/Redis service containers 跑现有 live smoke | `.github/workflows/ci.yml`, `internal/mcpserver/server_test.go` | 0.8d | 中 | 关闭最大验证缺口 |
| P2 | 引入操作模式：把 raw/write 能力从“描述提醒”升级为服务端可配置策略 | `internal/config`, `internal/policy`, `internal/app`, `internal/mcpserver`, README/tests | 1.2d | 中 | 降低代理误操作风险 |
| P3 | 强化结果预算语义：UTF-8 安全截断、raw 大结果测试、预算原因一致性 | `internal/result`, `internal/engine/*`, tests | 0.8d | 中 | 让 bounded contract 更可信 |
| P4 | 拆出 engine-neutral meta result：消除 `CurrentTime` 返回 `SQLResult` 的抽象泄漏 | `internal/result`, `internal/engine`, `internal/app`, `internal/mcpserver`, tests | 0.5d | 低中 | 为后续非 SQL/KV engine 留空间 |
| P5 | 支持安全的 SQL qualified identifier，至少支持 `schema.table` | `internal/engine/mysqlengine`, tests, README | 0.5d | 中 | 提升真实数据库可用性 |
| P6 | 发布前质量门：补 release checklist、示例 smoke 指南、版本兼容边界 | README, `examples/`, `.github/workflows/*` | 0.4d | 低 | 降低发布和用户接入成本 |
| Total |  |  | 4.6d |  |  |

推荐先做 P0-P1，合计约 1.2d。P1 通过后再做 P2-P3，因为 raw 能力和预算改动必须有真实 Redis/MySQL 回归信号。

## 第一性原理问题重述

不好的问题表述是：“下一步要不要继续重构/继续加功能？”

正确的问题是：**db-mcp 如何在保持轻量测试环境工具定位的同时，让代理访问真实数据库这件事变得可验证、可控、可维护？**

“解决”应满足：

- 每个发布前都能自动证明 MySQL 和 Redis 的真实基本链路可用。
- 用户能一眼分清 inspect/read-only、bounded read、raw/write 的能力边界。
- 服务端策略能阻止配置外的危险路径，而不是只依赖 MCP annotations 和文案。
- 结果预算的行为有测试，且不会产生明显破损输出。
- 新维护者不会被历史计划文档误导。

## 约束拆分

### 真约束

- MCP client 可能是代理自动调用工具，服务端不能只依赖“模型会谨慎”。
- db-mcp 定位是本地/测试环境数据库工具，不是生产级数据库安全网关。
- SQL/Redis 都能产生大结果；Go 层预算无法完全阻止 driver/client 先分配内存。
- 当前代码已有较好分层和测试矩阵，重构必须用 `go test ./...`、`go vet ./...`、`go build ./cmd/db-mcp` 保持绿色。
- CI 目前跨平台跑 gofmt/vet/test/build，但没有 MySQL/Redis service 容器；live smoke 目前只靠本地环境变量触发。

### 可改约定

- `read_only = false` 出现在 example 中，不代表必须永远作为推荐安全姿态。
- `redis_command` 必须“任意命令永远可用”只是便利性选择，不是真约束。
- `get_current_time` 返回 `SQLResult` 是实现便利，不是领域模型要求。
- 历史 plan 文档可以保留，但应标明 superseded/current-status。
- SQL table 参数只支持 `[A-Za-z0-9_]+` 是保守策略，不是协议限制。

### 未验证假设

| # | Assumption | Type | If wrong... | Verification |
|---|---|---|---|---|
| 1 | GitHub Actions Linux service containers 足以稳定跑 MySQL/Redis smoke | unverified | P1 需要退化为 Docker compose 或 nightly/manual workflow | P1 先只加 Linux integration job，观察稳定性 |
| 2 | OceanBase 不适合立即放入普通 CI service matrix | unverified | 若有轻量官方镜像，可加入 integration job | 单独调研 OceanBase CI 成本；不阻塞 MySQL/Redis |
| 3 | 用户仍需要 raw SQL/Redis escape hatch | product assumption | 可更激进地默认禁用 raw/write 工具 | P2 设计为可配置策略，保留 opt-in |
| 4 | UTF-8 安全截断比“按字节最小实现”更重要 | quality assumption | 可以只记录为已知限制 | P3 加测试，确认改动复杂度低再落地 |
| 5 | 支持 `schema.table` 比支持任意 quoted identifier 更有价值 | usage assumption | 需要更完整 identifier parser | P5 先做 `schema.table` 纵向切片 |

## 方案对比

### A. 继续架构重构优先

机制：继续拆包、清理接口、抽更细的 ports/adapters。

优点：

- 代码结构会更规整。
- 长期扩展新 engine 可能更舒服。

失败模式：

- 深度分析已经证明主要分层落地，继续重构不能解决 live smoke 缺口。
- 没有真实 DB CI 时，重构越多，越难确认真实行为没退化。
- 容易重复执行历史 plan 中已完成的迁移。

结论：不是当前最佳第一步。

### B. 直接扩展新数据库或新能力

机制：加 PostgreSQL、TLS、Redis Sentinel/Cluster、更多 SQL/Redis 工具。

优点：

- 用户可见功能增长快。
- 能验证当前 registry/engine 设计是否真的可扩展。

失败模式：

- 当前 MySQL/Redis 真实链路尚未进入 CI，新增能力会扩大未验证面积。
- raw/write 和预算边界还不够产品化，功能越多风险越分散。

结论：等 P1-P3 后再考虑。

### C. 禁用 raw/write，变成强只读工具

机制：默认移除或隐藏 `execute_sql` 写入和 `redis_command`，只保留专用 bounded read 工具。

优点：

- 安全面最小。
- 代理误操作概率最低。

失败模式：

- 违背当前“本地测试环境排查工具”的灵活性定位。
- 用户需要临时诊断时会绕过 db-mcp，反而失去统一边界。

结论：适合生产只读网关，不适合当前默认路线。

### D. 证据优先的安全硬化（推荐）

机制：先把真实服务验证自动化，再用配置策略表达 raw/write 能力边界，随后补预算质量和小抽象债。

优点：

- 直接解决深度分析里的最大不确定性：live smoke 未运行。
- 每一步都是可验证纵向切片，不需要大爆炸重构。
- 保留测试环境工具的灵活性，同时给用户清晰的安全姿态。
- 与当前模块结构相容，改动能落在已有 `config/policy/result/app/mcpserver/engine` 边界内。

失败模式：

- 如果 CI service containers 不稳定，P1 可能带来 flaky 成本。
- 如果用户明确只想本地手动使用，P2 的策略配置会显得偏重。

结论：当前最佳方案。

## 具体切片

### P0: 固化当前基线和文档状态

目标：让后续改进从当前事实出发，而不是从过期计划出发。

动作：

- 在 `docs/` 下增加一个短索引或在历史 plan 顶部加状态块：
  - `docs/multi-db-mcp-architecture.md`: `Status: historical / partly superseded by current code`
  - `docs/db-mcp-hardening-plan.md`: `Status: mostly implemented / remaining risks tracked by latest plan`
  - `docs/db-mcp-architecture-deepening-plan.md`: `Status: architecture split implemented / remaining small debts only`
- 在 README 的开发/验证区域引用新的 research 和 plan，或者新增 `docs/README.md` 说明文档权威顺序：README + source/tests > current plan > historical plans。
- 不改行为，不改 public API。

验收：

- 新维护者能通过 docs 入口判断哪些文档是当前计划，哪些是历史。
- `go test ./...` 可选运行，理论上不受影响。

### P1: MySQL/Redis integration CI

目标：每个 PR 自动验证真实 MySQL 和 Redis 基础链路。

动作：

- 在 `.github/workflows/ci.yml` 增加 Linux-only integration job，保留现有跨平台 unit job。
- 使用 GitHub Actions service containers 启动 MySQL 和 Redis。
- 给现有 live tests 注入：
  - `DB_MCP_TEST_MYSQL=127.0.0.1:3306/app`
  - `DB_MCP_TEST_MYSQL_USER=root`
  - `DB_MCP_TEST_MYSQL_PASSWORD=...`
  - `DB_MCP_TEST_REDIS=127.0.0.1:6379`
- 若 MySQL service 初始化库名不稳定，CI job 显式创建 `app` database 后再跑 `go test -run 'Test(MySQL|Redis)LiveSmoke' ./internal/mcpserver`。
- 把 Redis smoke key 从 `obmcp:smoke` 改成 `dbmcp:smoke`，避免命名历史残留。
- OceanBase 保留 env-triggered manual smoke，不放入第一版 CI。

验收：

- 普通 `go test ./...` 仍通过。
- Linux integration job 能稳定跑 MySQL + Redis live smoke。
- CI 总时长仍可接受；若超过预期，把 integration job 设置为只在 PR/main 跑，不在所有 matrix OS 跑。

### P2: 操作模式和 raw/write 策略

目标：把“可写/原生命令能力”从说明文字变成服务端可配置策略。

推荐设计：

```toml
# inspect: safest default for agents; raw writes disabled, bounded reads only.
# operate: existing flexible behavior; raw/write tools enabled with explicit datasource rules.
mode = "inspect"
```

兼容路径：

- 第一版可以保留 `read_only`，新增 `mode` 时做等价映射：
  - `mode = "inspect"` -> read-only guard + raw command restricted
  - `mode = "operate"` -> 当前行为
- 如果同时配置 `read_only` 和 `mode`，配置加载阶段给出明确错误或定义优先级；推荐错误，避免双源真相。
- README 示例默认展示 `mode = "inspect"`；另给“需要写入测试库时”的 `mode = "operate"` 示例。

策略规则：

- `inspect`:
  - `execute_sql` 只允许现有 read-only SQL 判定。
  - `redis_command` 只允许现有 read-only allowlist。
  - 专用 read tools 不受影响。
- `operate`:
  - 保留当前写能力。
  - 多数据源高风险工具仍必须显式 datasource。
  - tool description 明确这是 raw/write mode。

验收：

- 单元测试覆盖 `mode` 与 `read_only` 的配置冲突。
- MCP tool description 能反映当前 mode。
- `inspect` 模式下现有 read-only SQL/Redis 拒绝测试继续通过。
- `operate` 模式下现有 raw live smoke 可用。

### P3: 预算语义强化

目标：让 bounded output contract 更稳定、更不容易产生破损结果。

动作：

- `NormalizeText` 改为 UTF-8 安全截断；避免按字节切断多字节字符。
- 增加测试：
  - 中文/emoji 字符在 `max_value_bytes` 和 `max_result_bytes` 边界下仍是 valid UTF-8。
  - SQL `[]byte`/string 截断原因稳定为 `value_bytes`。
  - Redis nested array/map 达到 result budget 后原因稳定为 `result_bytes`。
  - `redis_command` 返回大数组时会截断并标记 `truncated=true`。
- 在 README 中继续明确 `max_result_bytes` 是 best-effort，不承诺 JSON 精确字节数或 driver 内存硬上限。

验收：

- `go test ./internal/result ./internal/engine/redisengine ./internal/mcpserver` 通过。
- `go test ./...` 通过。
- 截断后的字符串通过 `utf8.ValidString`。

### P4: engine-neutral meta result

目标：修掉基础 `Engine` 接口把 `CurrentTime` 绑定到 `SQLResult` 的抽象泄漏。

动作：

- 新增 `result.TimeResult` 或 `result.MetaResult`：

```go
type TimeResult struct {
    Datasource string `json:"datasource"`
    Success bool `json:"success"`
    Now string `json:"now"`
}
```

- `engine.Engine.CurrentTime` 返回 `result.TimeResult`。
- MySQL engine 内部仍可用 SQL 查询实现，但对外返回 neutral DTO。
- Redis engine 用 `TIME` 后返回同样 DTO。
- `get_current_time` MCP schema/response 更新；测试断言新 response shape。

验收：

- SQL/KV engine 不再为了 meta tool 构造 SQL columns/data。
- `get_current_time` 对 SQL/Redis 都返回相同 neutral shape。
- 现有 read tool registration 测试通过。

### P5: 安全支持 `schema.table`

目标：提高 SQL 工具对真实库结构的可用性，同时不放开 identifier 注入面。

推荐机制：

- 不接受任意 raw table expression。
- 支持一段或两段 identifier：
  - `table`
  - `schema.table`
- 每段仍只允许 `[A-Za-z0-9_]+`，分别 quote 后拼接：`` `schema`.`table` ``。
- 明确不支持用户手写反引号、函数、join、子查询。

验收：

- `quoteIdentifier("users") == "`users`"`。
- `quoteIdentifier("app.users") == "`app`.`users`"`。
- 拒绝 `app.users where 1=1`、`app.*`、`` `app`.`users` ``、`a.b.c`。
- `describe_table` 和 `sample_table` 测试覆盖 qualified name。

### P6: 发布前质量门

目标：把“能发版”变成清晰 checklist。

动作：

- README 增加 Development Verification：
  - local: `go test ./...`, `go vet ./...`, `go build ./cmd/db-mcp`
  - integration: MySQL/Redis smoke command
  - optional: OceanBase smoke
- examples 分出 inspect/operate 或在单个 example 中标注两种模式。
- release workflow 前置依赖 CI 绿色；必要时增加 `workflow_run` 或文档化“tag 前必须 main CI green”。
- 记录不支持范围：production security gateway、full SQL parsing、Redis cluster/sentinel、PostgreSQL。

验收：

- 新用户能从 README 完成安装、配置、验证。
- 维护者能知道发 tag 前需要哪些检查。

## 推荐执行顺序

1. **P0 + P1 一起做**：先建立文档基线和真实 DB CI。
   这一步不会改变用户行为，但会显著提高后续改动置信度。

2. **P2 单独做**：操作模式是行为设计，必须独立 review。
   如果担心兼容性，第一版可先加 `mode` 但保持默认等价当前行为，同时 README 推荐 inspect。

3. **P3 做在 P2 之后**：预算强化需要同时覆盖 inspect/operate 两种模式。
   这一步应该是纯质量提升，不改变工具集合。

4. **P4/P5 小步做**：分别是架构债和可用性债。
   它们不应和 P2/P3 混在一个 diff 里。

5. **P6 收尾**：发布前把文档、examples、CI 叙述统一。

## 反演测试

推荐方案什么时候会变成坏选择？

- 如果项目永远只在单人本地手动使用，P1/P2 的工程成本可能偏高。
- 如果用户明确要把 db-mcp 变成生产安全网关，本计划不够强，需要 RBAC、审计、审批、数据库侧权限和更完整解析。
- 如果短期唯一目标是新增 PostgreSQL，P4/P5 的优先级应下降，先做新 engine 的最小纵向切片。
- 如果 GitHub Actions service containers 对 MySQL/Redis 持续 flaky，P1 应改为 nightly/manual integration workflow，而不是阻塞所有 PR。

这些情况目前没有推翻推荐。深度分析的最大事实缺口是 live smoke 未运行，而当前代码已经具备多引擎、raw/write 和预算能力；先补验证，再收紧边界，是最短的可靠路径。

## 下一步可验证切片

先执行 P0-P1：

1. 更新 docs 状态，标明哪些文档是 historical/superseded。
2. 在 CI 新增 Linux integration job，只跑 MySQL/Redis service smoke。
3. 清理 Redis smoke key 的 `obmcp` 残留命名。
4. 运行：

```bash
go test ./...
go vet ./...
go build ./cmd/db-mcp
```

成功标准：本地离线检查全绿，GitHub Actions 上普通 matrix job 和 Linux integration job 都稳定通过。
