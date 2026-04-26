# Contributing to MeterLogger

Thanks for taking an interest. Pull requests are welcome.

This project is published under the [Yottabyte Solutions Source Available License](LICENSE). It is not open source. You can read the code, fork it for the purpose of preparing a pull request, and submit changes back. You cannot use the code in your own products or services.

## Before you start

For anything larger than a typo, a doc clarification, or a one-line bug fix, open an issue first. Describe what you want to change and why. This avoids wasted work on changes that do not fit the project direction.

A change is in scope if it:

- Fixes a defect against documented behaviour.
- Improves documentation, tests, or build tooling.
- Adds a new sink or source that follows the patterns described in [documentation/architecture.md](documentation/architecture.md) and the project's `CLAUDE.md`.
- Improves observability, performance, or security without breaking existing behaviour.

A change is likely out of scope if it:

- Changes the architectural rules listed in `CLAUDE.md` (clean architecture, one container per source, fail-fast, etc.).
- Adds optional features behind feature flags.
- Vendors large external dependencies.
- Renames public configuration keys without a clear migration path.

## Contributor terms

By submitting a pull request, you agree to the contribution terms in section 3 of the [LICENSE](LICENSE):

- You are the author of the change, or you have the right to submit it.
- You grant Yottabyte Solutions a perpetual, worldwide, royalty-free, sublicensable license to use and relicense your contribution.
- You provide the contribution as is, without warranty.

A separate CLA is not required at this time.

## Development setup

Requirements:

- Go (version pinned in `go.mod`).
- `golangci-lint` (version pinned in `.github/workflows/ci.yml`).
- `make`.
- Docker, only if you want to build container images.

Common commands:

```sh
make build              # build binaries into out/
go test ./...           # run all tests
golangci-lint run ./... # run the linter
```

The lint and test suites must both pass with zero issues before a pull request can be merged.

## Code style

The full set of project rules lives in `CLAUDE.md`. Highlights:

- Services depend only on interfaces in `internal/domain/`. Do not import an adapter package from `internal/service/`.
- Use `log/slog`, not the standard `log` package, outside `cmd/meterlogger/`.
- Use constructor injection. No globals except the `processKiller` test seam.
- Every repository call accepts and propagates `context.Context`.
- Line length is 120 characters, enforced by `golines` through `golangci-lint`.
- Use `net.JoinHostPort` for DSN host/port composition. Use `strconv.Itoa` for integer-to-string conversion.
- Do not use em dashes anywhere in code, comments, or documentation.

## Tests

- Keep total coverage at or above 80 percent.
- Use `github.com/DATA-DOG/go-sqlmock` for database tests; do not require a real database.
- Hardware-dependent paths (real serial ports, real network endpoints) may stay untested. Add a comment in the test file explaining why.
- Service error paths replace `processKiller` with a no-op to avoid signalling the process during tests.
- Prefer table-driven tests for functions with several input variants.

## Adding a sink or source

Follow the patterns documented in `CLAUDE.md` and `documentation/architecture.md`. In short:

- A new sink lives under `internal/adapters/sink/<name>/` and registers in `cmd/meterlogger/config.go`, `db.go`, and the four `source_*.go` files. Add it to the sink table in `documentation/README.md` and the config examples in `documentation/configuration.md`.
- A new source lives under `internal/adapters/source/<name>/`, defines its reader and repository interfaces in `internal/domain/`, ships per-sink store methods, has a `multisink` wrapper, and is wired through a new `cmd/meterlogger/source_<name>.go`.

## Commits and pull requests

- Keep commits focused. One logical change per commit.
- Write a short, factual subject line. Describe the change in the body if needed.
- Rebase your branch on `master` before opening the PR.
- Reference any issue the change closes.
- The PR description must include a clear summary, a test plan, and any breaking change notes.
- Do not push generated artefacts (`out/`, `bin/`, `.env`, IDE files). The `.gitignore` covers these.

## Reporting bugs

Open an issue with:

- The version (`meterlogger --help` prints the build version), or the commit SHA.
- The source you ran (`heat`, `grid`, `solar`, `ventilation`).
- The sinks you had enabled.
- A minimal config that reproduces the problem, with secrets redacted.
- The relevant log output at debug level (`--debug`).

## Reporting security issues

Do not file a public issue for a security problem. See [SECURITY.md](SECURITY.md) for the disclosure process.
