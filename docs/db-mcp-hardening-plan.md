# db-mcp 安全硬化最佳改进方案

Mode: Plan
Depth: Standard+
Input sources read: `main.go`, `engine.go`, `redis.go`, `server_test.go`, `redis_test.go`, `engine_test.go`, `README.md`, `examples/db-mcp.toml.example`, `docs/multi-db-mcp-architecture.md`, `first-principles-planner/REFERENCE.md`

## TL;DR

根问题不是某一个 allowlist 漏洞，而是：db-mcp 把“给代理一个数据库操作能力”包装成 MCP 工具后，当前安全边界仍主要靠工具描述、行数上限和命令首词判断。对代理来说，这些边界太弱：合法调用也能触发错误数据源、无界响应、或只读模式下的大型读取。

最佳方案：新增一层轻量的“执行策略层”，在进入 SQL/Redis engine 前统一做三件事：

1. 目标策略：高风险工具必须显式确认 datasource，避免多数据源时省略参数落到错误库。
2. 能力策略：`read_only=true` 下收紧 `redis_command`，不再把“命令名是读命令”当成“结果有界”。
3. 资源策略：所有返回路径增加 value/result budget，`max_rows` 只负责行数，不再承担“整体有界”的职责。

不要优先引入复杂 SQL parser，也不要试图让 `redis_command` 保持“任意读命令都可用”。这两条会把问题变成协议解析军备竞赛，成本高且仍不可靠。

## 推荐切片

| Priority | Change | Likely files | Effort | Risk | Value |
|---|---|---|---:|---|---|
| P0 | 明确安全契约：`max_rows` 只限行数，`read_only` 是软保护，硬保护需要只读账号/ACL | `README.md`, `examples/db-mcp.toml.example` | 0.2d | 低 | 避免用户误判 |
| P1 | 高风险工具目标显式化：多数据源时 `execute_sql`/`redis_command` 必须传 `datasource` | `main.go`, `server_test.go`, `redis_test.go` | 0.4d | 中 | 防止写错库 |
| P2 | Redis read-only 策略重写：默认只允许 O(1)/小结果原始命令，SCAN/MGET/HRANDFIELD 等走专用工具或参数级 validator | `redis.go`, `redis_test.go`, `README.md` | 0.7d | 中 | 修掉只读模式无界读取 |
| P3 | 增加返回预算：SQL/Redis value 截断、总响应截断、`Truncated`/metadata 标记 | `engine.go`, `redis.go`, tests | 1.0d | 中 | 防止 MCP 响应爆炸 |
| P4 | 专用工具补齐资源语义：`redis_scan`/`redis_get` 明确 count/value 字节上限，`sample_table` 对大字段只给预览 | `redis.go`, `engine.go`, README | 0.6d | 中 | 让常用路径真正 bounded |
| P5 | 回归测试矩阵：错误 datasource、Redis 参数放大、SQL 大 cell、响应截断、文档契约 | `*_test.go` | 0.5d | 低 | 固化边界 |
| Total |  |  | 3.4d |  |  |

## 第一性原理拆解

### 期望结果

db-mcp 的目标用户是本地测试环境里的代理和开发者。它要让代理能快速查看数据库状态，但不能因为一次合法 MCP 调用造成：

- 写到意外数据源；
- 只读模式下拖垮 Redis/MCP/client；
- `max_rows=1` 仍返回超大 SQL/Redis payload；
- 文档给出比实现更强的安全暗示。

“解决”不是让工具绝对安全。数据库协议本身太宽，代理还能执行用户授意的危险操作。解决标准应该是：

- 省略参数不会导致高风险工具落到意外目标；
- 服务端对返回体有独立于行数的硬预算；
- read-only 模式下原始命令不允许无界读取；
- 文档和测试准确描述边界。

### Five Whys

1. 为什么 read-only 仍有风险？因为 `redis_command` 只看命令首词。
2. 为什么首词不够？因为 Redis 的读命令也可以由参数决定返回规模。
3. 为什么 `max_rows` 没兜住？因为 raw Redis command 和 SQL 单元格不受行数控制。
4. 为什么代理容易踩中？因为 MCP 工具描述说 bounded/read-only，模型会倾向相信工具语义。
5. 根因：能力入口缺少统一的目标、能力、资源策略，导致“工具看起来受控”，但执行路径仍是原始数据库能力。

## 约束拆分

### 真约束

- MCP 工具可被代理反复、自动调用，服务端不能依赖“代理会节制”。
- SQL 和 Redis 协议都允许大结果，行数不是资源上限。
- `execute_sql` 和 `redis_command` 是高风险能力，工具 annotation 只能提示客户端，不能替代服务端校验。
- 项目目标是轻量 Go binary，不适合引入重型代理网关或完整数据库代理层。

### 可改约定

- 省略 datasource 时默认使用配置 default。
- `redis_command` 在 read-only 下仍提供较宽的读命令直通。
- `max_rows` 被文档表达成 bounded results 的主要机制。
- SQL/Redis engine 各自做结果归一化，没有共享预算模型。

### 未验证假设

| # | Assumption | Type | If wrong | Verification |
|---|---|---|---|---|
| 1 | 主要使用场景是本地/测试环境，不是生产审计网关 | unverified | 需要更强 ACL、审计、租户隔离 | README 当前写 test-environment，发布前确认 |
| 2 | 破坏性写操作可以要求更显式的 datasource | convention | 老用法会多一次参数填写 | 加兼容开关或只在多数据源时启用 |
| 3 | SQL 大单元格无法仅靠 Go 层完全避免传输内存压力 | technical | 若 driver 支持硬 packet/result limit，可更强 | 实现前核对 go-sql-driver/mysql 当前能力 |
| 4 | Redis raw command 的便利性低于安全边界价值 | product | power user 会觉得 read-only 下受限 | 保留非 read-only 的 raw command，但加响应预算 |

## 方案对比

### A. 执行策略层加硬边界（推荐）

机制：保留现有工具和 engine 抽象，在 handler 进入 engine 前增加 policy：

- `resolveToolDatasource(tool, input)` 决定是否允许省略 datasource；
- `validateRedisCommand(argv, mode, limits)` 做命令和参数级校验；
- `ResultBudget` 约束 SQL/Redis 返回的单值、元素数、总字节数。

优点：

- 改动贴近风险入口，和现有架构兼容；
- 不需要重写 MCP 工具体系；
- 能用单元测试直接覆盖；
- 后续加 PostgreSQL 或其他 engine 时也能复用预算模型。

失败模式：

- SQL 单个超大 cell 在 driver 层可能已经被读取到内存，Go 层截断只能避免继续放大到 MCP 响应。
- 如果用户真的需要 raw Redis 大读取，read-only 模式会变得更保守。

### B. 禁用 raw 工具，只保留专用工具

机制：移除或默认隐藏 `execute_sql` 写能力和 `redis_command`，只提供 `list/sample/get/scan` 这类专用工具。

优点：

- 安全面最小；
- 每个工具都容易做预算；
- 代理误用概率最低。

缺点：

- db-mcp 作为测试排查工具会损失大量灵活性；
- SQL 临时诊断能力几乎被砍掉；
- 用户仍可能要求把 raw 能力加回来。

适合条件：如果目标从“本地测试辅助”转为“默认连生产的受控只读查询器”，这个方案更合理。

### C. 完整 SQL/Redis 语义解析

机制：引入 SQL parser、Redis command schema，精确识别只读、结果规模和危险操作。

优点：

- 理论上能给出更细粒度的准入判断；
- 可以支持更多原始命令。

缺点：

- MySQL/OceanBase 方言和 Redis 命令族持续变化，维护成本高；
- SQL side-effect function、存储过程、权限差异仍绕不开；
- 与轻量 MCP server 的定位不匹配。

适合条件：只有当项目明确升级为数据库安全代理或企业策略网关时才值得做。

## 具体设计

### 1. 目标策略

新增内部 helper：

```go
func (a *App) datasourceNameForTool(tool string, inputName string) (string, error)
```

建议规则：

- 读工具继续允许省略 datasource，使用 default。
- `execute_sql` 和 `redis_command` 在 `len(datasources) > 1` 时要求显式 datasource。
- 单数据源场景保留现有便利性。
- 如果 `default` 为空且多数据源，配置加载阶段报错；不要按字典序隐式选择。

这解决“合法调用省略 datasource 写错库”的根因，而不是依赖工具描述提醒模型。

### 2. Redis read-only 策略

把当前 `map[string]bool` 改为显式 policy：

```go
type redisCommandPolicy struct {
    readOnly bool
    validate func(argv []string, limits Limits) error
}
```

第一版建议：

- read-only 下保留：`PING`, `TIME`, `TYPE`, `TTL`, `PTTL`, `EXISTS`, `STRLEN`, `HLEN`, `LLEN`, `SCARD`, `ZCARD`, 单 key `GET` 但受 value bytes 限制。
- read-only 下移除 raw `SCAN`, `MGET`, `HSCAN`, `SSCAN`, `ZSCAN`, `HRANDFIELD`, `SRANDMEMBER`, `ZRANDMEMBER`, `INFO`, `COMMAND`, `OBJECT`，除非单独写参数 validator。
- `redis_scan` 和 `redis_get` 作为推荐读取路径，因为它们有明确 count/value 截断语义。

如果想保留 `MGET`，必须加参数数量上限，例如 `len(keys) <= maxRows`，且总响应预算生效。不要再只靠首词 allowlist。

### 3. 统一资源预算

新增 limits，默认值保守：

```toml
max_rows = 500
max_value_bytes = 65536
max_result_bytes = 1048576
```

语义：

- `max_rows`：行数或集合元素数。
- `max_value_bytes`：单个 SQL cell、Redis string、Redis bulk value 的最大返回字节。
- `max_result_bytes`：单次 MCP tool response 的近似最大 payload。

实现原则：

- SQL：`normalizeDBValue` 改为预算感知，长字符串/bytes 截断并返回 truncation metadata。
- Redis：`normalizeRedisValue` 改为预算感知，递归数组/map 时达到元素或字节预算就停止。
- 所有 result 保留 `Truncated`，必要时增加 `TruncationReason` 或 `Limits` 字段，便于代理知道不是完整数据。

注意：Go 层预算不能保证数据库 driver 在读取超大单元格前不分配内存。实现前应核对 MySQL driver 是否能设置更低 packet/result limit；若不能，文档必须把 SQL 大 cell 标成剩余风险，并建议只读账号加数据库侧限制。

### 4. 文档契约

README 中把“Bounded results via `max_rows`”改成更精确的表述：

- `max_rows` limits row/element count。
- `max_value_bytes` limits each returned value preview。
- `max_result_bytes` limits total MCP response payload best-effort。
- `read_only` rejects common write paths but is not a privilege boundary。
- Hard safety requires database read-only account or Redis ACL/read-only replica。

这不是文档粉饰，而是避免用户用错误安全模型部署。

### 5. 测试矩阵

必须补这些单元测试：

- 多数据源下 `execute_sql` 省略 datasource 返回错误。
- 多数据源下 `redis_command` 省略 datasource 返回错误。
- 单数据源下高风险工具仍可省略 datasource，避免破坏最小用法。
- `read_only=true` 拒绝 raw `SCAN COUNT 1000000`、大 `MGET`、随机采样大 count。
- `redis_get` 对大 string 返回前缀并 `Truncated=true`。
- `normalizeDBValue` 对大 `[]byte`/string 截断。
- Redis nested array/map 响应达到总预算后截断。
- README 示例字段和默认值与 `loadConfig` 保持一致。

## 推荐执行顺序

1. 先做 P1：多数据源高风险工具必须显式 datasource。这是最小、最确定、最能防真实事故的切片。
2. 再做 P2：重写 Redis read-only policy。这个直接修上轮最高风险发现。
3. 再做 P3/P4：补资源预算。先覆盖 Redis，因为 Redis raw response 现在最容易无界；再覆盖 SQL。
4. 最后做 P0/P5：文档和测试随每个切片同步更新，完成后集中检查 README 契约。

## 反演测试

推荐方案什么时候最差？

- 如果用户明确只在一次性本地脚本中使用，且永远只有一个测试库，P1 的显式 datasource 价值较低。
- 如果用户主要依赖 `redis_command` 做复杂排查，P2 会减少 read-only 下的便利性。
- 如果目标是生产级安全网关，当前方案不够，需要 ACL、审计日志、RBAC、查询审批和真实协议代理。

这些条件目前都不推翻推荐方案，因为 README 的定位是本地测试环境，且上轮风险来自合法 MCP 调用的资源/目标边界，而不是缺少企业治理功能。

## 下一步可验证切片

第一步只实现 P1：

- 修改 `main.go` 的 datasource 解析逻辑；
- 增加 3 个测试：多数据源 SQL 省略失败、多数据源 Redis 省略失败、单数据源仍兼容；
- 跑 `go test ./...`。

这个切片不碰 Redis 命令语义，也不引入新配置，回归面最小。通过后再进入 Redis policy 和预算层。
