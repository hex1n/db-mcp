# db-mcp 架构改进最佳方案

Mode: Plan
Depth: Deep
Input sources read this session: `first-principles-planner/SKILL.md`, `first-principles-planner/REFERENCE.md`, previous `improve-codebase-architecture` findings, `README.md`, `docs/README.md`, `docs/research/2026-06-05-db-mcp-project-deep-analysis.md`, `docs/plans/2026-06-05-db-mcp-best-improvement-plan.md`, `docs/db-mcp-architecture-deepening-plan.md`, `.github/workflows/ci.yml`, `cmd/db-mcp/main.go`, `internal/app/app.go`, `internal/mcpserver/server.go`, `internal/mcpserver/server_test.go`, `internal/config/config.go`, `internal/policy/policy.go`, `internal/result/result.go`, `internal/engine/engine.go`, `internal/engine/mysqlengine/mysql.go`, `internal/engine/redisengine/redis.go`.

No root `CONTEXT.md` or `docs/adr/` was found. Domain vocabulary is inferred from current README and code: datasource, engine, registry, MCP tool, operation mode, result budget, SQL/Redis live smoke.

## TL;DR

db-mcp does **not** need another broad package split. The earlier architecture split is already mostly implemented: `cmd`, `config`, `app`, `mcpserver`, `policy`, `result`, and `engine` packages are in place. The current architecture problem is subtler:

> The code has the right coarse packages, but several important modules are still shallow. Their interfaces do not hide enough behavior, so tool schema rules, datasource resolution rules, result budget semantics, engine capability routing, and test responsibilities are still spread across call sites.

The best path is a sequence of small deepening slices:

1. Deepen the MCP tool surface module and split protocol catalog tests from behavior tests.
2. Separate App use-case inputs from MCP schema inputs, then add direct App tests.
3. Deepen datasource resolution so SQL/Redis/credential details stop accumulating in one function.
4. Centralize result budget application so engines share one truncation contract.
5. Move live smoke closer to engine adapters while keeping MCP protocol smoke thin.
6. Only then revisit capability routing if adding a third engine category makes the current App seam hurt.

This plan optimizes for locality, not abstraction count. Every phase should delete or shrink duplicated knowledge before adding a new seam.

## Implementation Progress

Status: implemented in current worktree on 2026-06-05, with P6 intentionally deferred by this plan.

| Priority | Status | Evidence |
|---|---|---|
| P0 | Done | `docs/README.md` lists current planning artifacts; this section records module vocabulary and plan ownership. |
| P1 | Done | `internal/mcpserver/catalog.go` owns tool catalog/schema/annotation decisions; `internal/mcpserver/catalog_test.go` tests catalog behavior without MCP sessions. |
| P2 | Done | `internal/app/app.go` use-case inputs no longer carry MCP schema tags; `internal/mcpserver/inputs.go` owns schema structs; `internal/app/app_test.go` directly tests App behavior. |
| P3 | Done | `internal/config/config.go` keeps `ResolveDatasource` stable while splitting lookup, properties, secrets, defaults, and validation stages; config tests cover those stages. |
| P4 | Done | `internal/result/result.go` applies budget truncation to result types; SQL/Redis adapters delegate truncation status to result helpers. |
| P5 | Done | MySQL/Redis live smoke tests live in `internal/engine/mysqlengine` and `internal/engine/redisengine`; CI and README commands point to adapter packages. |
| P6 | Deferred | No third engine category exists yet; capability routing remains intentionally unchanged until real variation justifies the seam. |

Final review and verification on 2026-06-05:

- Reviewed the architecture diff against P0-P5 and left P6 deferred.
- Fixed a Redis collection-count edge case so `MarkElementCount(total, returned)` uses the actual returned element count.
- Added MySQL live-smoke retry around initial container startup to reduce CI flake risk.
- Passed `go test ./...`, `go vet ./...`, `go build -o /tmp/db-mcp-arch-build ./cmd/db-mcp`, `git diff --check HEAD`, coverage collection, and local Docker MySQL 8.4 + Redis 7 adapter live smoke.

## Recommended Action Plan

| Priority | Change | Likely files/modules | Effort | Risk | Value |
|---|---|---|---:|---|---|
| P0 | Add architecture vocabulary and ownership baseline for current modules | `docs/plans/*` or later `CONTEXT.md` if explicitly chosen | 0.3d | Low | Prevents future reviews from re-proposing completed broad splits |
| P1 | Deepen MCP tool surface: make catalog/schema/annotation decisions testable without full MCP sessions | `internal/mcpserver/server.go`, new mcpserver catalog helper/tests, `internal/mcpserver/server_test.go` | 0.8d | Medium | Highest immediate locality gain; shrinks 724-line protocol test pressure |
| P2 | Separate App use-case inputs from MCP schema shape and add direct App tests | `internal/app/app.go`, `internal/app/app_test.go`, `internal/mcpserver/server.go` | 0.9d | Medium | Moves datasource/mode/engine behavior tests to the real use-case seam |
| P3 | Deepen datasource resolution into a resolver module with driver/credential stages | `internal/config/config.go`, `internal/config/*_test.go` | 0.8d | Medium | Makes future TLS/PostgreSQL/Redis connection options local |
| P4 | Centralize result budget application beyond primitive `Budget` counters | `internal/result`, `internal/engine/mysqlengine`, `internal/engine/redisengine`, tests | 1.0d | Medium-high | Hardest but most valuable safety-contract cleanup |
| P5 | Move live smoke ownership from MCP protocol tests toward engine adapter tests | `internal/engine/*`, `internal/mcpserver/server_test.go`, `.github/workflows/ci.yml` | 0.7d | Medium | CI failures point to the adapter that broke |
| P6 | Revisit capability routing only when a new engine category appears | `internal/engine`, `internal/app`, new adapter package | 0.6d deferred | Medium | Avoids speculative seams until there are enough adapters to justify them |
| Total | P0-P5 recommended now; P6 deferred |  | 4.5d now + 0.6d deferred |  |  |

Recommended first vertical slice: **P1 + the minimum part of P2**. The acceptance test is not line-count vanity; it is this:

- MCP tool catalog/schema/annotation behavior can be tested without invoking handlers.
- App behavior can be tested without starting an MCP client session.
- Existing `go test ./...`, `go vet ./...`, and live MySQL/Redis smoke still pass.

## Root Problem

Bad framing: "Should we keep refactoring the architecture?"

Better framing:

> db-mcp is now a multi-engine database MCP server that can operate real datasources. Maintainers need to change tool surface, safety policy, result bounding, and engine adapters without relearning the whole request path or proving behavior only through MCP transport tests.

Solved means:

- A maintainer can answer "where does this rule live?" from the module name.
- A change to MCP schema does not require touching App use-case shapes.
- A change to App datasource policy can be tested without the MCP SDK.
- A change to result budget semantics is verified once and reused by SQL and Redis adapters.
- A live MySQL/Redis failure points to an engine adapter, not a general MCP test blob.
- Adding another engine mostly changes one adapter and one explicit tool-surface registration point.

## Problem Archaeology

| Trace | Observation | Root classification |
|---|---|---|
| User outcome | db-mcp is used by agents against local/test datasources; mistakes can read too much, write wrong places, or expose confusing tools. | True constraint: service-side behavior must be explicit and testable. |
| Technical | The broad package split exists, but `mcpserver`, `app`, `config`, and `result` still contain several rules each. | Current root: modules exist, but some interfaces are shallow. |
| Historical | Historical docs still describe migrations that are now implemented; recent plan already handled CI/mode/budget/time result. | Convention: do not restart the old "split packages" plan. |
| Operational | CI now has MySQL/Redis integration wiring, but remote Actions stability is still proven only after push. | True constraint: architecture changes must keep the integration lane green. |
| Testing | `internal/mcpserver/server_test.go` is the dominant test harness; App has 0% package coverage in coverage output because behavior is exercised through MCP tests. | Root testability issue: behavior is tested through the wrong seam. |

## Constraint Split

### True constraints

- MCP clients see a static tool surface at server startup, so tool registration and schema rules need to be deterministic.
- The MCP SDK dependency should stay concentrated in `internal/mcpserver`.
- Datasource selection and operation mode are safety-relevant; they should not be only documentation or client-side hints.
- SQL and Redis result budgets are best-effort after driver/client data is available; docs must not imply a hard memory boundary.
- Existing public config compatibility matters: omitted `mode` defaults to `operate`; legacy `read_only=true` remains accepted as inspect.
- Go package changes must stay small enough to preserve `go test ./...`, `go vet ./...`, `go build ./cmd/db-mcp`, and integration smoke.

### Conventions that can change

- App input structs currently carry JSON/schema tags because it was convenient for MCP binding.
- MCP tests currently cover many non-MCP behaviors because that was the easiest end-to-end seam.
- `ResolveDatasource` currently owns all datasource defaults, credential resolution, JDBC parsing, and driver validation.
- Result budget is currently a shared helper rather than the owner of complete truncation behavior.

### Load-bearing assumptions

| # | Assumption | Type | If wrong... | Verification |
|---|---|---|---|---|
| 1 | Future changes are more likely to add datasource/engine capabilities than rewrite the MCP SDK adapter. | Unverified product assumption | P1/P2 may be less valuable than CLI/config hardening | Check issue backlog or next requested feature before implementation. |
| 2 | Keeping App MCP-free is worth a small translation layer. | Architecture assumption | Extra adapter code could be more ceremony than leverage | Prototype one tool path only, then apply deletion test. |
| 3 | Result budget semantics will continue to be shared across SQL and Redis. | Strong code evidence, still assumption for future engines | If future engines have unrelated result shapes, P4 should stay narrow | Try P4 only around existing SQL/Redis bounded-preview behavior. |
| 4 | Engine-level live smoke can preserve the same coverage as MCP-level smoke. | Test design assumption | Moving tests could accidentally stop verifying MCP wiring | Keep one thin MCP live smoke or protocol smoke after relocation. |
| 5 | Capability routing only becomes painful with another category beyond SQL/KV. | Current-code inference | If current wrong-kind behavior changes often, P6 should move earlier | Track churn around `ConfiguredCategories`, `sqlEngineFor`, `kvEngineFor`. |

## Option Tournament

### Option A: Broad architecture split again

Mechanism: introduce more packages and formalize more interfaces now.

Why it loses:

- The package-level architecture is already largely split.
- It risks adding seams before there are two adapters or real variation.
- It does not directly improve the biggest current friction: MCP tests carrying App/engine behavior.

Use this only if db-mcp is about to add several unrelated datasource families at once.

### Option B: Test-only cleanup

Mechanism: keep architecture as-is, move some tests out of `server_test.go`.

Why it loses:

- It improves symptoms but leaves the same shallow interfaces.
- App inputs would still be partly MCP-shaped.
- Result budget rules would still be scattered across engines.

Use this if there is no appetite for any production-code refactor before a release.

### Option C: Deepen existing modules through vertical slices

Mechanism: keep current package topology, but make the MCP tool surface, App use-case seam, datasource resolver, and result budget modules deeper one at a time.

Why it wins:

- It targets the actual friction from the architecture review.
- It creates better test seams before changing behavior.
- It avoids speculative interfaces; each slice must pass the deletion test.
- It keeps the existing public behavior and safety improvements intact.

Strongest failure mode: the team could over-design internal helper modules and make the code harder to navigate.

Mitigation: every slice must remove duplicated knowledge or shorten a caller. If no deletion happens, stop.

## Detailed Plan

### P0: Establish Current Architecture Baseline

Goal: stop future work from replaying old architecture plans.

Actions:

- Keep `docs/README.md` as the doc index for historical vs current plans.
- Optionally create a root `CONTEXT.md` only if the team wants durable domain language; do not do it silently.
- Record current architecture vocabulary:
  - datasource: configured target.
  - engine: runtime adapter for a datasource driver.
  - registry: driver-to-engine factory table.
  - MCP tool surface: tool catalog, schema, annotation, and handler binding.
  - operation mode: inspect/operate service-side behavior switch.
  - result budget: best-effort output preview contract.

Acceptance:

- A new maintainer can identify which docs are current without reading historical migration plans first.

### P1: Deepen MCP Tool Surface

Goal: make `internal/mcpserver` a deeper module whose interface hides catalog/schema/annotation decisions.

Current friction:

- `New` reads categories and config, constructs descriptions, chooses input structs, and registers handlers in one flow.
- Tool catalog tests require opening an in-memory MCP session.

Actions:

- Extract a private tool catalog representation inside `internal/mcpserver`.
- Keep MCP SDK types at the outer adapter edge.
- Add tests that inspect the catalog decision directly:
  - SQL-only tool set.
  - Redis-only tool set.
  - mixed datasource high-risk tools require datasource.
  - inspect/operate descriptions.
  - annotations.
- Keep a smaller protocol test proving the catalog is actually registered into MCP.

Do not:

- Do not introduce a cross-package public tool catalog unless another package needs it.
- Do not change tool names or public schema behavior in this slice.

Acceptance:

- Most tool metadata assertions no longer need a client/server in-memory session.
- `server.go` becomes mostly adaptation and registration, not policy derivation.

### P2: Separate App Use-Case Inputs From MCP Schema Inputs

Goal: make App a use-case module, not an MCP schema carrier.

Current friction:

- `app.ExecuteSQLInput`, `TableInput`, `Redis*Input` include schema tags.
- `mcpserver` defines extra required-datasource input structs to satisfy schema differences.
- App behavior lacks direct package tests.

Actions:

- Remove or stop relying on JSON schema tags from App use-case inputs.
- Let MCP adapter own schema-specific structs and translate into App inputs.
- Add `internal/app/app_test.go` with fake engines/registry to directly cover:
  - datasource defaulting.
  - high-risk explicit datasource rule.
  - inspect-mode SQL/Redis refusal.
  - wrong-kind operation error.
  - engine cache behavior if useful.

Do not:

- Do not make App depend on MCP SDK.
- Do not duplicate policy logic inside mcpserver; translation only.

Acceptance:

- App tests cover the same behavioral rules now mostly asserted through `server_test.go`.
- MCP tests only need to prove schema/transport adaptation.

### P3: Deepen Datasource Resolution

Goal: make datasource resolution easier to extend without growing one function.

Current friction:

- `ResolveDatasource` owns properties file loading, JDBC parsing, env secret lookup, default ports, SQL required fields, Redis required fields, and driver defaults.

Actions:

- Keep public `config.ResolveDatasource(cfg, configPath, name)` stable.
- Internally split resolution into stages:
  - lookup datasource by name.
  - enrich from properties file where supported.
  - resolve secrets.
  - apply driver defaults.
  - validate required fields by driver category.
- Add scenario tests for each stage and driver category.

Do not:

- Do not add a plugin system for config resolvers yet.
- Do not move driver-specific config into engine packages unless there is a real second adapter needing it.

Acceptance:

- Adding a PostgreSQL or TLS option has an obvious local place.
- Errors remain at least as clear as current errors.

### P4: Centralize Result Budget Application

Goal: make bounded-preview behavior a deep result module, not repeated engine loop logic.

Current friction:

- SQL and Redis engines each manually set `Truncated` and `TruncationReason`.
- Redis string/list/set/hash/zset paths each apply budget in slightly different loops.

Actions:

- Identify common budget application patterns before adding abstractions:
  - scalar/string normalization.
  - ordered collection truncation.
  - key/value map truncation.
  - SQL row/cell truncation.
  - command preview truncation.
- Move only proven repeated behavior into result helpers.
- Keep engine-specific IO loops in engine adapters.
- Add tests at result-module level for all repeated patterns.

Do not:

- Do not hide database IO behind result helpers.
- Do not try to create one generic shape for SQL rows and Redis values.

Acceptance:

- At least two duplicated truncation branches disappear from engine adapters.
- Budget reasons are tested once for common shapes.
- Existing SQL/Redis output JSON remains compatible unless intentionally versioned.

### P5: Move Live Smoke Closer To Engine Adapters

Goal: make integration failures point to the broken adapter.

Current friction:

- MySQL/Redis live smoke tests live in `internal/mcpserver/server_test.go`, alongside protocol tests.
- Engine adapter packages have low offline coverage and no direct live smoke ownership.

Actions:

- Add adapter-level live smoke tests under:
  - `internal/engine/mysqlengine`
  - `internal/engine/redisengine`
- Keep environment variable names stable if possible.
- Update CI integration job to run adapter live smoke plus one thin MCP smoke if needed.
- Leave `internal/mcpserver` with protocol-level assertions.

Do not:

- Do not remove MCP-level confidence entirely; tool wiring still needs one smoke path.
- Do not require OceanBase in normal CI unless a stable lightweight test instance exists.

Acceptance:

- A MySQL driver regression fails in `mysqlengine`.
- A Redis client/budget regression fails in `redisengine`.
- MCP server tests shrink and become more about MCP behavior.

### P6: Defer Capability Routing Until There Is More Variation

Goal: avoid speculative seams.

Current friction:

- App uses `sqlEngineFor` and `kvEngineFor` type assertions.

Recommendation:

- Do not prioritize this now.
- Revisit when adding a third category, such as document store, search, vector, or PostgreSQL-specific tooling that does not fit the current SQL/KV split.

Trigger:

- If a new engine requires conditional tool registration, wrong-kind errors, and App dispatch rules beyond SQL/KV, then create a deeper capability routing module.

Acceptance if triggered:

- New capability routing removes category decisions from at least two callers.
- There are at least two real adapters or categories behind the seam.

## Sequencing

Recommended sequence:

1. P1: MCP tool surface deepening.
2. P2: App use-case seam and direct App tests.
3. P3: Datasource resolution internal stages.
4. P5: Move live smoke closer to engines.
5. P4: Result budget application, after tests are better localized.
6. P6 only when new engine capability pressure appears.

Why P4 after P5? Result-budget refactors touch safety behavior and engine implementations. Better test locality first lowers the risk of silently weakening bounded-preview behavior.

## Verification Plan

Minimum verification for each phase:

```bash
go test ./...
go vet ./...
go build ./cmd/db-mcp
git diff --check
```

Integration verification when touching engine adapters or result budget:

```bash
DB_MCP_TEST_MYSQL=127.0.0.1:3306/app \
DB_MCP_TEST_MYSQL_USER=root \
DB_MCP_TEST_MYSQL_PASSWORD=dbmcp \
DB_MCP_TEST_REDIS=127.0.0.1:6379 \
go test -run 'Test(MySQL|Redis).*Smoke|Test.*LiveSmoke' ./...
```

CI verification after push:

- Linux/macOS/Windows unit lane green.
- Linux integration lane green for MySQL/Redis service containers.
- OceanBase remains optional unless a stable test target is available.

## What Would Change The Recommendation

- If the next roadmap item is only a one-off release with no new datasource work, do P1/P2 test split only and defer P3-P5.
- If the next roadmap item is PostgreSQL, do P3 before P1 so config resolution does not become harder.
- If Redis Cluster/Sentinel/TLS is the next item, do P3 and P5 before P1.
- If GitHub Actions integration becomes flaky, split live smoke into nightly/manual before doing broad engine refactors.

## Non-Goals

- No broad clean architecture rewrite.
- No new public extension/plugin framework.
- No full SQL parser or production security gateway work.
- No PostgreSQL, Redis Cluster/Sentinel, TLS, RBAC, or audit workflow in this architecture phase.
- No ADR/CONTEXT durable capture unless explicitly requested.

## Final Recommendation

Start with P1 + P2 as one vertical slice. It is the smallest path that directly improves locality and testability without re-litigating the package split already completed. It also creates the test surface needed to safely do P3-P5 later.

The first implementation PR should be judged by deletion:

- Did `server_test.go` lose protocol-heavy behavioral assertions?
- Did App gain direct tests for datasource/mode/engine behavior?
- Did MCP SDK knowledge stay inside `internal/mcpserver`?
- Did public tool behavior remain unchanged?

If those answers are not yes, the slice added ceremony instead of depth and should be stopped or simplified.
