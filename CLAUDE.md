# MeterLogger - Claude Instructions

## After every code change

Run these checks and fix all issues before considering any task done:

```sh
golangci-lint run ./... # must report 0 issues
go test ./... # all tests must pass
```

If lint issues remain, fix them - do not skip or defer. Use `//nolint:rulename // reason` only
when suppression is genuinely the right call and always include a short justification.

## Architecture - non-negotiable rules

- **Ports and Adapters** pattern: separate concerns into distinct layers (ports, adapters, domain).
- **Services depend only on `internal/domain` interfaces.** Never import an adapter package from
  `internal/service/`. This is the core invariant of the clean architecture.
- **One container per source** is the deployment model. The `--source` flag selects which meter
  type runs. Config uses `Enabled: true` flags to document which sources exist.
- **All enabled sinks receive every write concurrently** via `internal/adapters/sink/multisink/`.
  At least one sink must be enabled.
- **Fail-fast error handling.** On a non-recoverable error, send `SIGTERM` (via `processKiller()`)
  and return. Do not swallow errors and keep running.

## Adding a new sink

For a SQL database that fits an existing wire protocol (Postgres- or MySQL-compatible):

1. Add a dialect in `internal/adapters/sink/sqlsink/dialect.go` and a thin wrapper package
   `internal/adapters/sink/<name>/` with `common.go` (Config, DSN builder, driver import,
   delegating constructors), modeled on `internal/adapters/sink/postgres/`.
2. Register the connection in `cmd/meterlogger/config.go` and `db.go` (`dbConnections` plus
   its `sql()`, `closers()`, `checkers()` methods and `initDBs`). The `source_*.go` files need
   no changes; `buildSourceSinks` picks up every open `sqlsink.DB` automatically.

For a sink with its own storage model (like ClickHouse or QuestDB):

1. Create `internal/adapters/sink/<name>/` implementing the four repository interfaces.
2. Use `internal/adapters/schemastore/` for migrations (`NewSQLMigrator`,
   `NewClickHouseMigrator`, or `NewTDEngineMigrator`).
3. Wire it in `cmd/meterlogger/sinks.go` (`buildSourceSinks`); the compiler forces every
   source to provide a constructor for it.

Either way, add the sink to the sink table in `documentation/README.md` and config examples
in `documentation/configuration.md`.

## Adding a new source

Follow the same pattern as existing sources (`gridmeter`, `enphase`, `serialmbus`, `ducobox`):

1. Define reader and repository interfaces in `internal/domain/`.
2. Implement the reader in `internal/adapters/source/<name>/`.
3. Implement store methods in each sink package.
4. Add a `multisink` wrapper in `internal/adapters/sink/multisink/`.
5. Wire it in `cmd/meterlogger/source_<name>.go` and register the `--source` flag value.

## Code style

- **No global variables** except `processKiller` in `internal/service/kill.go` (test seam).
- **No `log` package** outside `cmd/meterlogger/`. Use `log/slog` everywhere else.
- **Constructor injection only.** All structs are created via `NewXxx(...)`. No init-time side
  effects outside `main`.
- **Context flows everywhere.** Every repository call accepts and propagates `context.Context`.
- **One package doc comment per package.** Put it in `common.go` (sinks) or the primary file.
  Remove it from all other files in the same package.
- **Line length limit is 120 characters** (enforced by `golines`).
- **`net.JoinHostPort`** for constructing host:port strings in DSNs - never `fmt.Sprintf`.
- **`strconv.Itoa`** instead of `fmt.Sprintf("%d", n)` for integer-to-string conversion.

## Testing

- Keep total test coverage at or above **80%**.
- Use `github.com/DATA-DOG/go-sqlmock` for database tests - no real connections required.
- Hardware-dependent code paths (serial ports, real network) are acceptable untested exceptions;
  document them in the test file with a comment.
- Service error paths: replace `processKiller` with a no-op in tests to avoid sending signals.
- Table-driven tests are preferred for functions with many input variants.

## Documentation

Breaking changes must be documented. When you change a config key name, remove a default, or
change a CLI flag:

1. Add a "Breaking change" callout in the relevant `documentation/` file.
2. Update all config examples in `documentation/configuration.md` and `documentation/deployment.md`.

Do not create new markdown files unless if not needed. Prefer updating existing ones. Keep them concise.
When adding new files make sure they are linked in appropriate places in other markdown files.
Keep indices up to date.

## Build

```sh
make build # produces out/meterlogger-linux-amd64, out/meterlogger-linux-arm64, out/meterlogger-darwin-arm64
make clean # removes out/
```

The Docker image is a two-stage scratch build. It contains only `/meterlogger`, plus
`/etc/passwd`, `/etc/group`, and `/etc/ssl/certs/ca-certificates.crt`. Timezone data is
embedded in the binary via the `time/tzdata` import. The `HEALTHCHECK` instruction runs
`meterlogger healthcheck`, which probes `/readyz`.
