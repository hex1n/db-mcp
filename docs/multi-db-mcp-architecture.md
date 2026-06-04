# 多数据库 MCP 架构方案（ob-mcp → 多引擎）

> Mode: Plan · Depth: Standard+ · 本次已读源（均 verified）：`main.go`、`README.md`、`go.mod`、`examples/ob-mcp.toml.example`
> 范围约定：**只出架构方案，先不写代码**。下文代码片段仅为接口示意。

---

## TL;DR

根问题不是"加 Redis"，而是：**当前代码里 "datasource" ≡ "一条 MySQL 连接"**——MySQL 协议（`mysql.NewConfig()`）和 SQL 工具语义（`SHOW TABLES`/`execute_sql`）被焊死在核心里。

由此推出两条关键事实：

1. **MySQL 现在就已经能用**。OceanBase(MySQL 模式) 与 MySQL 共用 `go-sql-driver/mysql`，配置 `driver = "mysql"` 直连真 MySQL 即可。"支持 MySQL" ≈ 命名 + 文档 + driver 别名，**没有协议工作量**。
2. **Redis 才是真正的新引擎**：非 SQL、新客户端库、全新工具语义。它逼出唯一正确的动作——**先做一层"数据库引擎"抽象**，"等数据库"才能低成本接入。

**推荐架构**：能力化 `Engine` 抽象 + 每种 DB 专属工具 + **按"已配置的 kind"条件注册工具**。

---

## 行动计划（分片，每片可独立验证）

| 优先级 | 改动 | 工作量 | 风险 | 价值 |
|---|---|---:|---|---|
| P1 | **引擎抽取**：把现有 MySQL 逻辑包成 `sqlEngine`，7 个工具改走接口（行为不变） | ~0.5d | 低（可回归验证） | 高（一切的地基） |
| P2 | **driver 注册表 + 条件注册**：按配置里出现的 kind 注册工具集 | ~0.3d | 低 | 中（去歧义、控制工具面） |
| P3 | **Redis 引擎 + redis 工具 + 配置字段 + go-redis 依赖** | ~1d | 中（新依赖/新语义/需真实 redis 验证） | 高（用户主诉求） |
| P4 | **文档 + 改名决策 + 混合配置示例** | ~0.3d | 低（改名有兼容风险，靠 fallback 缓解） | 中 |
| P5 | （可选）**postgres 引擎** 验证抽象可扩展性 | ~0.5d | 低 | 低 |
| | **合计** | **~2.1d**（+0.5 可选） | | |

排序理由：P1 是行为不变的纯重构，是后面一切的地基，**最先做、最先验证**（绿色构建 + 原有 OB/MySQL 查询照常通过）。P3 是用户主诉求但依赖 P1/P2。P5 仅用于证明抽象没有 SQL/KV 之外的硬假设，可缓做。

### Phase 1 — 引擎抽取（行为不变）
- 新增 `engine.go`：定义 `Engine` 接口 + 能力子接口（见下）。
- 把 `db()` / `runQuery` / `runExec` / `listTables` / `describeTable` / `sampleTable` 的 MySQL 实现迁入 `sqlEngine`。
- `App.pools map[string]*sql.DB` → `App.engines map[string]Engine`。
- **验证**：`ob-mcp.exe --version` 通过；对现有 OB/MySQL 数据源跑 `select database(), now()` 与 `list_tables` 结果与改前一致。

### Phase 2 — driver 注册表 + 条件注册
- 引入 `factories map[string]func(DatasourceConfig) (Engine, error)`，按 `driver` 路由（`mysql`/`oceanbase`→sql；后续 `redis`/`postgres`）。
- 启动时扫描配置里出现的 kind 集合 → **只注册这些 kind 的工具** + 共享元工具。
- **验证**：纯 SQL 配置不出现 redis 工具；把错误 kind 的数据源传给某工具时返回清晰错误。

### Phase 3 — Redis 引擎
- 加依赖 `github.com/redis/go-redis/v9`。
- 新增 `redis.go`：`redisEngine` 实现 KV 能力接口。
- 新增工具：`redis_scan` / `redis_get` / `redis_type` / `redis_ttl` / `redis_command`。
- 配置新增 Redis 字段（见"配置变更"）。
- **验证**：对本地 redis 跑 scan/get/ttl/command；写命令（SET/DEL/FLUSH*）走与 `execute_sql` 一致的"写需确认"约束。

### Phase 4 — 文档 / 改名 / 示例
- README 改为多引擎叙述；补混合数据源（mysql + redis 同一配置）示例。
- 改名决策见下（非阻塞）。

### Phase 5 —（可选）postgres
- 复用 `sqlEngine` 思路新增 `postgres` driver（`pgx`），验证 SQL 子接口对非 MySQL 方言也成立（identifier 引用、`SHOW TABLES` 等价物需方言化）。

---

## ⛳ 待你拍板的 1 个决定（Phase 3 动工前唯一阻塞项）

**Redis 等非 SQL 库的工具暴露形态**：

| 选项 | 形态 | 取舍 |
|---|---|---|
| **A. 每种 DB 专属工具（推荐）** | 保留 SQL 工具，另加 `redis_scan/get/type/ttl/command` | 语义最贴各自心智模型、参数歧义最低；代价是工具数量增长，**但用 P2 的条件注册抵消** |
| B. 统一通用工具 | 单个 `execute` 自适应 | 工具最少；但 SQL（字符串）与 Redis（argv 数组）参数形态本就不同，强行合一会增加模型用错的概率 |
| C. 混合 | 通用 `execute_command` + 各 DB 少量便捷工具 | 折中；适合"重原始命令、轻发现"的用法 |

**我的推荐：A**。第一性原理：MCP 客户端是 LLM，它最需要的是「低歧义 + 可发现」。专属工具名（`redis_scan` 而非泛化 `list_objects`）让模型按各存储的自然心智操作，错误率最低。A 的唯一缺点（工具变多）被 **条件注册**（只注册已配置 kind 的工具）干净地解决——纯 MySQL 项目根本看不到 redis 工具。
**何时该选 B/C**：若你主要当"原始命令直通管道"用、且刻意要极小工具面，则收敛到 `execute` + `list_datasources`。

---

## 关键设计

### Engine 抽象（能力化，避免 SQL/KV 硬塞同一接口）

```go
// 示意，非最终代码
type Engine interface {
    Kind() string                       // "mysql" / "oceanbase" / "redis"
    Ping(ctx context.Context) (any, error)
    Close() error
}

// SQL 类（mysql/oceanbase/postgres）
type SQLEngine interface {
    Engine
    Query(ctx, sql string, maxRows int) (SQLResult, error)
    Exec(ctx, sql string) (SQLResult, error)
    ListTables(ctx) (SQLResult, error)
    DescribeTable(ctx, table string) (SQLResult, error)
    SampleTable(ctx, table string, limit int) (SQLResult, error)
}

// KV 类（redis）
type KVEngine interface {
    Engine
    Scan(ctx, pattern string, count int) (KVResult, error)
    Get(ctx, key string) (KVResult, error)   // 内部按 type 走 string/hash/list/set/zset
    Type(ctx, key string) (KVResult, error)
    TTL(ctx, key string) (KVResult, error)
    Command(ctx, argv []string) (KVResult, error)
}
```
工具实现里对取到的 `Engine` 做能力断言：`if e, ok := eng.(SQLEngine); ok { ... } else { return errWrongKind }`。

### 工具分层
- **共享元工具**（与 kind 无关）：`list_datasources`、`current_datasource`、`get_current_time`（SQL→`SELECT NOW()`；Redis→`TIME`）、可加 `ping`。
- **SQL 工具**：`list_tables`、`describe_table`、`sample_table`、`execute_sql`（沿用现有查询/写分流 + 写需确认）。
- **Redis 工具**：`redis_scan`（基于 SCAN，**禁用阻塞的 KEYS**）、`redis_get`、`redis_type`、`redis_ttl`、`redis_command`（写命令同样"需确认"，对 `FLUSHALL/FLUSHDB/KEYS` 给出告警）。

### 连接管理
`App.engines map[string]Engine` 懒加载；`engineFor(name)` = 解析配置 → 按 driver 查工厂 → 建引擎 → 缓存。`close()` 遍历 `Engine.Close()`。

### 配置变更（`DatasourceConfig`）
- `driver` 成为判别字段：`mysql` / `oceanbase`(别名→mysql) / `redis` /（将来）`postgres`。
- Host/Port/Username/Password/PasswordFrom **复用**（Redis 6+ ACL 用 username，旧版仅 password）。
- Redis 的逻辑库是数字索引，与 SQL 的 `database`(schema) 语义冲突 → **新增显式字段 `redis_db int`**（0–15，默认 0）。更长远可改为 `[datasources.x.options]` map 容纳各 kind 私有项；当前 2–3 种库下显式字段更直观。

```toml
[datasources.cache_test]
driver   = "redis"
host     = "127.0.0.1"
port     = 6379
redis_db = 0
password_from = "env:REDIS_PW"
```

### 安全姿态（沿用并扩展现有约束）
- `max_rows` → 同时作为 Redis scan/list 的上限（如 `redis_scan count` 上限、聚合类型读取条数上限）。
- 写需确认：`execute_sql` 现状 + `redis_command` 的写命令。
- **新增风险点**：`KEYS *`、`FLUSHALL/FLUSHDB` 在类生产实例上是 O(N)/破坏性 → 便捷工具一律走 `SCAN`，`redis_command` 命中这些命令时告警。

### 项目身份（约定，非约束）—— 已决策（Phase 4）
原名 `ob-mcp` / `.ob-mcp.toml` / `OB_MCP_CONFIG` / module `github.com/hexin/ob-mcp` 在多库语境下名不副实。
**决定：改名为 `db-mcp`，且不保留 fallback**（项目尚未发布，一次性干净替换）。已落地：module path、二进制名、server name、`--version` 输出、配置文件名（`.db-mcp.toml`）、环境变量（`DB_MCP_CONFIG`）、README/示例、`.gitignore`、测试用 `DB_MCP_TEST_REDIS` 全部改为新名；旧的 `.ob-mcp.toml` / `OB_MCP_CONFIG` **不再被识别**，本地 `.mcp.json` / `.codex` 里的 exe 路径需手动改为 `db-mcp.exe`。

---

## 问题考古

### 根因（Five Whys）
```
诉求: "还要支持 mysql redis 等"
Why? -> 同一套 MCP 想跨多个用不同存储的测试项目做数据巡检
Why 现状挡路? -> 工具与连接全是 SQL/MySQL 语义
Why 对 Redis 不成立? -> Redis 非 SQL，database/sql 与现有工具套不上
根: 缺少 "datasource-kind" 抽象——当前 "datasource" 被定义成了 "一条 MySQL 连接"
```

### 约束三分
- **真约束**：MCP 工具在启动时静态注册；`database/sql` 只适配 SQL；Redis 需独立客户端；stdio 单进程；配置为项目级 TOML；测试环境 + 有界结果 + 写需确认的安全基线。
- **约定（可改）**：项目名/配置名/env 名；现有 7 工具命名；"7 工具集"本身。
- **待验证假设**：见下表。

### 假设审计
| # | 假设 | 类型 | 若错 | 验证 |
|---|---|---|---|---|
| 1 | "等数据库" 近期≈MySQL+Redis(+或许 PG) | 范围 | 抽象需更通用(文档/列存) | 跟你确认目标清单 |
| 2 | 同一进程需并存多种 kind | 技术 | 可退化为按 kind 分进程 | 配置已支持多 datasource，成立概率高 |
| 3 | 用户偏好低歧义工具优于最小工具面 | 偏好 | 改走 B/C 方案 | 即上面"待拍板的决定" |
| 4 | Redis 凭据可复用 host/port/user/pass | 技术 | 需加 TLS/sentinel/cluster 字段 | go-redis 单点连接成立；集群/哨兵留作扩展 |

---

## 方案对比（机制层面，≥2 种）

1. **能力化 Engine 抽象 + 专属工具 + 条件注册（推荐）**
   机制：把"连接管理 + 操作语义"抽到 `Engine`/能力子接口，工具按已配置 kind 注册。
   有利条件：库种数有限(2–5)、重视模型低歧义、要 DRY 可扩展。
   失败模式：库种暴增到 10+ 时子接口可能需再分类(如 DocEngine)；由注册表吸收，可控。

2. **通用命令直通（单 `execute`）**
   机制：一个工具，按 kind 把入参解释为 SQL 或 Redis 命令。
   有利条件：极简工具面、power-user 直接打命令。
   失败模式：参数形态被迫合一，模型易把 SQL 发给 redis 源、或弄错 argv；丢掉发现/便捷层。

3. **并行 bolt-on（不抽象，Redis 另写一套）**
   机制：保留 SQL 硬编码，旁边复制一套 Redis 代码与工具。
   有利条件：只想最快加一个 Redis、不打算再扩。
   失败模式：第三种库到来时重复劳动翻倍，连接/超时/安全逻辑多处漂移——把根问题(缺抽象)留给未来。**因与"等数据库"诉求直接冲突而否决。**

### 反演测试（对推荐项）
"专属工具 + 条件注册" 何时最差？——当你刻意要极小工具面、且几乎只用原始命令、不要任何发现性工具时。此时方案 2 更优。否则推荐项在低歧义、可扩展、安全复用三方面都占优。

---

## 下一步（可独立验证）
Phase 1（引擎抽取，行为不变）是最小且能拆掉全部风险的切片：完成后构建通过 + 现有 OB/MySQL 工具回归一致即算验证成功。待你 ① 确认上面"工具暴露形态"的拍板、② 给我开工许可后，从 Phase 1 起做。
