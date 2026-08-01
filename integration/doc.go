// Package integration contains integration tests that exercise the sink
// adapters against real database servers. All test files carry the
// "integration" build tag, so a plain `go test ./...` never compiles or
// runs them. Run them with:
//
//	go test -tags integration ./integration/...
//
// Each test reads its connection parameters from environment variables and
// calls t.Skip when the <PREFIX>_HOST variable is unset. The contract per
// sink (prefix POSTGRES, TIMESCALEDB, MYSQL, CLICKHOUSE):
//
//	<PREFIX>_HOST      required, enables the test
//	<PREFIX>_PORT      optional, defaults to the sink's standard port
//	<PREFIX>_USER      optional, defaults to "meter" ("default" for ClickHouse)
//	<PREFIX>_PASSWORD  optional, defaults to "meterpass" ("" for ClickHouse)
//	<PREFIX>_DB        optional, defaults to "meterlogger"
//
// TDEngine and QuestDB are deliberately not covered here: neither has a
// reliable official service-container setup for CI.
package integration
