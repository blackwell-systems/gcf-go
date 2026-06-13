package gcf

import (
	"strings"
	"testing"
)

func TestEncode_Basic(t *testing.T) {
	p := &Payload{
		Tool:        "test",
		TokenBudget: 5000,
		TokensUsed:  100,
		Symbols: []Symbol{
			{QualifiedName: "pkg.Func", Kind: "function", Score: 0.9, Provenance: "lsp_resolved", Distance: 0},
			{QualifiedName: "pkg.Type", Kind: "type", Score: 0.7, Provenance: "ast_inferred", Distance: 1},
		},
		Edges: []Edge{
			{Source: "pkg.Type", Target: "pkg.Func", EdgeType: "calls"},
		},
	}

	out := Encode(p)

	if !strings.HasPrefix(out, "GCF profile=graph tool=test") {
		t.Errorf("missing GCF header, got: %s", out)
	}
	if !strings.Contains(out, "@0 fn pkg.Func 0.90 lsp_resolved") {
		t.Errorf("missing symbol @0, got:\n%s", out)
	}
	if !strings.Contains(out, "@1 type pkg.Type 0.70 ast_inferred") {
		t.Errorf("missing symbol @1, got:\n%s", out)
	}
	if !strings.Contains(out, "## targets") {
		t.Error("missing ## targets group")
	}
	if !strings.Contains(out, "## related") {
		t.Error("missing ## related group")
	}
	if !strings.Contains(out, "@0<@1 calls") {
		t.Errorf("missing edge, got:\n%s", out)
	}
}

func TestRoundTrip(t *testing.T) {
	p := &Payload{
		Tool:        "context_for_task",
		TokenBudget: 5000,
		TokensUsed:  1847,
		PackRoot:    "abc123",
		Symbols: []Symbol{
			{QualifiedName: "pkg.A", Kind: "function", Score: 0.9, Provenance: "lsp_resolved", Distance: 0},
			{QualifiedName: "pkg.B", Kind: "method", Score: 0.7, Provenance: "ast_inferred", Distance: 1},
			{QualifiedName: "pkg.C", Kind: "interface", Score: 0.5, Provenance: "structural", Distance: 2},
		},
		Edges: []Edge{
			{Source: "pkg.B", Target: "pkg.A", EdgeType: "calls"},
			{Source: "pkg.C", Target: "pkg.B", EdgeType: "implements"},
		},
	}

	encoded := Encode(p)
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded.Tool != p.Tool {
		t.Errorf("tool: got %q, want %q", decoded.Tool, p.Tool)
	}
	if decoded.TokenBudget != p.TokenBudget {
		t.Errorf("budget: got %d, want %d", decoded.TokenBudget, p.TokenBudget)
	}
	if decoded.TokensUsed != p.TokensUsed {
		t.Errorf("tokens: got %d, want %d", decoded.TokensUsed, p.TokensUsed)
	}
	if decoded.PackRoot != p.PackRoot {
		t.Errorf("pack_root: got %q, want %q", decoded.PackRoot, p.PackRoot)
	}
	if len(decoded.Symbols) != len(p.Symbols) {
		t.Fatalf("symbols: got %d, want %d", len(decoded.Symbols), len(p.Symbols))
	}
	for i, s := range decoded.Symbols {
		if s.QualifiedName != p.Symbols[i].QualifiedName {
			t.Errorf("symbol %d qname: got %q, want %q", i, s.QualifiedName, p.Symbols[i].QualifiedName)
		}
		if s.Kind != p.Symbols[i].Kind {
			t.Errorf("symbol %d kind: got %q, want %q", i, s.Kind, p.Symbols[i].Kind)
		}
		if s.Distance != p.Symbols[i].Distance {
			t.Errorf("symbol %d distance: got %d, want %d", i, s.Distance, p.Symbols[i].Distance)
		}
	}
	if len(decoded.Edges) != len(p.Edges) {
		t.Fatalf("edges: got %d, want %d", len(decoded.Edges), len(p.Edges))
	}
	for i, e := range decoded.Edges {
		if e.EdgeType != p.Edges[i].EdgeType {
			t.Errorf("edge %d type: got %q, want %q", i, e.EdgeType, p.Edges[i].EdgeType)
		}
	}
}

func TestSession_Dedup(t *testing.T) {
	sess := NewSession()
	p := &Payload{
		Tool: "test",
		Symbols: []Symbol{
			{QualifiedName: "pkg.Func", Kind: "function", Score: 0.9, Provenance: "lsp_resolved"},
		},
	}

	// First call: full declaration.
	out1 := EncodeWithSession(p, sess)
	if !strings.Contains(out1, "fn pkg.Func") {
		t.Error("first call should have full declaration")
	}
	if sess.Size() != 1 {
		t.Errorf("session should have 1 symbol, got %d", sess.Size())
	}

	// Second call: bare reference.
	out2 := EncodeWithSession(p, sess)
	if !strings.Contains(out2, "# previously transmitted") {
		t.Error("second call should have bare reference")
	}
	if strings.Contains(out2, "fn pkg.Func 0.90") {
		t.Error("second call should NOT have full declaration")
	}
}

func TestEncodeDelta_Basic(t *testing.T) {
	d := &DeltaPayload{
		Tool:        "test",
		BaseRoot:    "aaa",
		NewRoot:     "bbb",
		Removed:     []Symbol{{QualifiedName: "pkg.Old", Kind: "function"}},
		Added:       []Symbol{{QualifiedName: "pkg.New", Kind: "function", Score: 0.8, Provenance: "rwr"}},
		DeltaTokens: 20,
		FullTokens:  100,
	}

	out := EncodeDelta(d)

	if !strings.Contains(out, "delta=true") {
		t.Error("missing delta=true")
	}
	if !strings.Contains(out, "base_root=aaa") {
		t.Error("missing base_root")
	}
	if !strings.Contains(out, "## removed") {
		t.Error("missing removed section")
	}
	if !strings.Contains(out, "fn pkg.Old") {
		t.Error("missing removed symbol")
	}
	if !strings.Contains(out, "## added") {
		t.Error("missing added section")
	}
	if !strings.Contains(out, "fn pkg.New 0.80 rwr") {
		t.Error("missing added symbol")
	}
}

func TestDecode_Empty(t *testing.T) {
	_, err := Decode("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestDecode_InvalidHeader(t *testing.T) {
	_, err := Decode("NOT_GCF foo=bar")
	if err == nil {
		t.Error("expected error on invalid header")
	}
}

func TestKindAbbrev_RoundTrip(t *testing.T) {
	for full, abbrev := range KindAbbrev {
		expanded, ok := KindExpand[abbrev]
		if !ok {
			t.Errorf("KindExpand missing entry for %q", abbrev)
			continue
		}
		if expanded != full {
			t.Errorf("KindExpand[%q] = %q, want %q", abbrev, expanded, full)
		}
	}
}
