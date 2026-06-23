# db-mcp Agent Guide

## Project Context

- Go MCP server for bounded local/test database inspection and explicitly
  approved local operations.
- Supported engines: OceanBase/MySQL via MySQL protocol and Redis.
- Read `README.md`, `README.zh-CN.md`, and relevant files under `docs/` before
  changing public behavior, configuration, safety boundaries, or setup docs.

## Safety Boundary

- Prefer `mode = "inspect"` for agent use.
- Treat `execute_sql` and `redis_command` as potentially destructive surfaces.
- Do not run write/destructive database operations, live datasource smoke tests,
  or commands against business systems without explicit user approval.
- Do not commit datasource configs containing passwords or private hosts.

## Verification

Use the narrowest relevant check first.

- Focused Go package test: `go test ./<package>`
- Standard checks: `go test ./...`, `go vet ./...`, `go build ./cmd/db-mcp`
- Optional live smoke tests require explicit local/test datasource env vars and
  approval for the target.
- Docs-only: inspect Markdown and run `git diff --check`

## Final Response

End with changed files, verification commands/results, unverified gaps, and
residual risks. Do not commit or push unless explicitly asked.
