package gcf

import (
	"strings"
	"testing"
)

func TestPackRoot_Basic(t *testing.T) {
	symbols := []Symbol{
		{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.78, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "pkg.Server", Kind: "function", Score: 0.54, Provenance: "lsp_resolved", Distance: 1},
	}
	edges := []Edge{
		{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"},
	}

	got := PackRoot(symbols, edges)
	expected := "sha256:8e6d32973b4005c604399a14faa32799d54f427800008312a3357c349d41e572"
	if got != expected {
		t.Errorf("PackRoot mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

func TestPackRoot_Empty(t *testing.T) {
	got := PackRoot(nil, nil)
	expected := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != expected {
		t.Errorf("PackRoot empty mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

func TestPackRoot_SingleSymbol(t *testing.T) {
	symbols := []Symbol{
		{QualifiedName: "pkg.Main", Kind: "function", Score: 1.0, Provenance: "lsp_resolved", Distance: 0},
	}

	got := PackRoot(symbols, nil)
	expected := "sha256:da39c1fb07913659a75865cf4afaa971683448625d19ac92c0303e90f678585f"
	if got != expected {
		t.Errorf("PackRoot single mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

func TestPackRoot_SortOrder(t *testing.T) {
	symbols := []Symbol{
		{QualifiedName: "z.Foo", Kind: "function", Score: 0.9, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "a.Bar", Kind: "type", Score: 0.5, Provenance: "ast_inferred", Distance: 1},
	}
	edges := []Edge{
		{Source: "z.Foo", Target: "a.Bar", EdgeType: "calls"},
		{Source: "a.Bar", Target: "z.Foo", EdgeType: "imports"},
	}

	got := PackRoot(symbols, edges)
	expected := "sha256:fa9ffe4a5d09fc21e1e122000b269ac9098fbec8d21eee5665ed33711be8af94"
	if got != expected {
		t.Errorf("PackRoot sort order mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}

	// Verify order independence: reversed input should produce same hash.
	symbolsReversed := []Symbol{symbols[1], symbols[0]}
	edgesReversed := []Edge{edges[1], edges[0]}
	got2 := PackRoot(symbolsReversed, edgesReversed)
	if got2 != expected {
		t.Errorf("PackRoot not order-independent:\n  got:      %s\n  expected: %s", got2, expected)
	}
}

func TestVerifyDelta_Success(t *testing.T) {
	baseSymbols := []Symbol{
		{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.78, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "pkg.Server", Kind: "function", Score: 0.54, Provenance: "lsp_resolved", Distance: 1},
	}
	baseEdges := []Edge{
		{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"},
	}

	removedSymbols := []Symbol{
		{Kind: "function", QualifiedName: "pkg.Server"},
	}
	addedSymbols := []Symbol{
		{QualifiedName: "pkg.Handler", Kind: "function", Score: 0.85, Provenance: "lsp_resolved", Distance: 1},
	}
	removedEdges := []Edge{
		{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"},
	}
	addedEdges := []Edge{
		{Source: "pkg.Handler", Target: "pkg.Auth", EdgeType: "calls"},
	}

	expectedRoot := "sha256:209fa026484529cc94ab00667a30bfa2951cb66e4bfeb237bbd528a9c640513d"

	resultSyms, resultEdges, err := VerifyDelta(
		baseSymbols, baseEdges,
		removedSymbols, addedSymbols,
		removedEdges, addedEdges,
		expectedRoot,
	)
	if err != nil {
		t.Fatalf("VerifyDelta failed: %v", err)
	}
	if len(resultSyms) != 2 {
		t.Errorf("expected 2 result symbols, got %d", len(resultSyms))
	}
	if len(resultEdges) != 1 {
		t.Errorf("expected 1 result edge, got %d", len(resultEdges))
	}
}

func TestVerifyDelta_RootMismatch(t *testing.T) {
	baseSymbols := []Symbol{
		{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.78, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "pkg.Server", Kind: "function", Score: 0.54, Provenance: "lsp_resolved", Distance: 1},
	}
	baseEdges := []Edge{
		{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"},
	}

	removedSymbols := []Symbol{{Kind: "function", QualifiedName: "pkg.Server"}}
	addedSymbols := []Symbol{{QualifiedName: "pkg.Handler", Kind: "function", Score: 0.85, Provenance: "lsp_resolved", Distance: 1}}
	removedEdges := []Edge{{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"}}
	addedEdges := []Edge{{Source: "pkg.Handler", Target: "pkg.Auth", EdgeType: "calls"}}

	wrongRoot := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	_, _, err := VerifyDelta(baseSymbols, baseEdges, removedSymbols, addedSymbols, removedEdges, addedEdges, wrongRoot)
	if err == nil {
		t.Fatal("expected root_mismatch error, got success")
	}
	if !strings.Contains(err.Error(), "root_mismatch") {
		t.Errorf("expected root_mismatch, got: %v", err)
	}
}

func TestVerifyDelta_RemoveNonexistent(t *testing.T) {
	baseSymbols := []Symbol{
		{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.78, Provenance: "lsp_resolved", Distance: 0},
	}

	removedSymbols := []Symbol{{Kind: "function", QualifiedName: "pkg.DoesNotExist"}}

	_, _, err := VerifyDelta(baseSymbols, nil, removedSymbols, nil, nil, nil, "")
	if err == nil {
		t.Fatal("expected delta_invalid error")
	}
	if !strings.Contains(err.Error(), "delta_invalid") {
		t.Errorf("expected delta_invalid, got: %v", err)
	}
}
