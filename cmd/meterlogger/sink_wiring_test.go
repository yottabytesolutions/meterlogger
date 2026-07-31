package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestBuildSinksFunctionsWireEveryKnownSink statically verifies that every
// build<Source>Sinks function in cmd/meterlogger references every known sink
// identifier (QuestDB, stdout, postgres, mysql, timescaledb, clickhouse, tdengine).
// CLAUDE.md's "Adding a
// new sink" checklist requires wiring a new sink into all four source_*.go
// files by hand; nothing fails to compile if a contributor wires it into some
// sources and forgets another, the sink is just silently unavailable there.
// This is a cheap stand-in for a sink registry: it won't stop broken wiring,
// but it will fail the build the moment a sink is wired into some sources and
// missing from another.
func TestBuildSinksFunctionsWireEveryKnownSink(t *testing.T) {
	//nolint:goconst // source names repeat across main.go and this table; not worth a shared const for 4 fixed values
	sourceWirings := []struct {
		source, fileName, funcName string
	}{
		{"heat", "source_heat.go", "buildHeatSinks"},
		{"grid", "source_grid.go", "buildGridSinks"},
		{"solar", "source_solar.go", "buildSolarSinks"},
		{"ventilation", "source_ventilation.go", "buildVentilationSinks"},
	}
	requiredSinkIdentifiers := []string{
		"QuestDB", "stdout", "postgres", "mysql", "timescaledb", "clickhouse", "tdengine",
	}

	for _, w := range sourceWirings {
		fn := findFuncDecl(t, w.fileName, w.funcName)
		if fn == nil {
			t.Errorf("source %q: function %s not found in %s", w.source, w.funcName, w.fileName)
			continue
		}

		seen := identifiersIn(fn.Body)
		for _, want := range requiredSinkIdentifiers {
			if !seen[want] {
				t.Errorf(
					"source %q: %s in %s does not reference %q; this sink looks unwired for this source",
					w.source, w.funcName, w.fileName, want,
				)
			}
		}
	}
}

// findFuncDecl parses fileName and returns the top-level function declaration
// named funcName, or nil if it isn't found.
func findFuncDecl(t *testing.T, fileName, funcName string) *ast.FuncDecl {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", fileName, err)
	}

	for _, decl := range file.Decls {
		if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Recv == nil && fn.Name.Name == funcName {
			return fn
		}
	}
	return nil
}

// identifiersIn collects every identifier name referenced anywhere in node.
func identifiersIn(node ast.Node) map[string]bool {
	seen := make(map[string]bool)
	ast.Inspect(node, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			seen[ident.Name] = true
		}
		return true
	})
	return seen
}
