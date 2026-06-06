package gcf

import (
	"bytes"
	"strings"
	"testing"
)

func TestStreamEncoder_Basic(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "context_for_task", StreamOptions{TokenBudget: 5000})

	enc.WriteSymbol(Symbol{
		QualifiedName: "pkg.Auth",
		Kind:          "function",
		Score:         0.78,
		Provenance:    "lsp_resolved",
		Distance:      0,
	})
	enc.WriteSymbol(Symbol{
		QualifiedName: "pkg.Server",
		Kind:          "function",
		Score:         0.54,
		Provenance:    "lsp_resolved",
		Distance:      1,
	})
	enc.WriteEdge(Edge{
		Source:   "pkg.Server",
		Target:   "pkg.Auth",
		EdgeType: "calls",
	})
	enc.Close()

	output := buf.String()

	// Header line (first line) has no symbols= or edges= (streaming mode).
	headerLine := strings.Split(output, "\n")[0]
	if strings.Contains(headerLine, "symbols=") {
		t.Errorf("streaming header should not contain symbols=, got: %s", headerLine)
	}
	if strings.Contains(headerLine, "edges=") {
		t.Errorf("streaming header should not contain edges=, got: %s", headerLine)
	}

	// Has deferred count marker.
	if !strings.Contains(output, "## edges [?]") {
		t.Errorf("expected ## edges [?], got:\n%s", output)
	}

	// Has trailer summary.
	if !strings.Contains(output, "## _summary symbols=2 edges=1") {
		t.Errorf("expected summary with symbols=2 edges=1, got:\n%s", output)
	}

	// Has correct structure.
	if !strings.Contains(output, "## targets") {
		t.Error("missing ## targets")
	}
	if !strings.Contains(output, "## related") {
		t.Error("missing ## related")
	}
	if !strings.Contains(output, "@0 fn pkg.Auth 0.78 lsp_resolved") {
		t.Error("missing first symbol")
	}
	if !strings.Contains(output, "@1 fn pkg.Server 0.54 lsp_resolved") {
		t.Error("missing second symbol")
	}
	if !strings.Contains(output, "@0<@1 calls") {
		t.Error("missing edge")
	}
}

func TestStreamEncoder_RoundTrip(t *testing.T) {
	// Encode with streaming, decode with standard decoder.
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "blast_radius", StreamOptions{TokenBudget: 10000})

	enc.WriteSymbol(Symbol{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.95, Provenance: "lsp", Distance: 0})
	enc.WriteSymbol(Symbol{QualifiedName: "pkg.Config", Kind: "type", Score: 0.80, Provenance: "ast", Distance: 0})
	enc.WriteSymbol(Symbol{QualifiedName: "pkg.Server", Kind: "function", Score: 0.60, Provenance: "lsp", Distance: 1})
	enc.WriteEdge(Edge{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"})
	enc.WriteEdge(Edge{Source: "pkg.Auth", Target: "pkg.Config", EdgeType: "references"})
	enc.Close()

	// Decode should succeed.
	p, err := Decode(buf.String())
	if err != nil {
		t.Fatalf("decode failed: %v\noutput:\n%s", err, buf.String())
	}

	if p.Tool != "blast_radius" {
		t.Errorf("tool: got %q, want %q", p.Tool, "blast_radius")
	}
	if len(p.Symbols) != 3 {
		t.Errorf("symbols: got %d, want 3", len(p.Symbols))
	}
	if len(p.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(p.Edges))
	}
}

func TestStreamEncoder_NoEdges(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "test", StreamOptions{})

	enc.WriteSymbol(Symbol{QualifiedName: "a.A", Kind: "function", Score: 0.9, Provenance: "x", Distance: 0})
	enc.Close()

	output := buf.String()

	// No edges section.
	if strings.Contains(output, "## edges") {
		t.Errorf("should not emit edges section when no edges written:\n%s", output)
	}

	// Summary shows edges=0.
	if !strings.Contains(output, "edges=0") {
		t.Errorf("summary should show edges=0:\n%s", output)
	}
}

func TestStreamEncoder_MultipleDistanceGroups(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "test", StreamOptions{})

	enc.WriteSymbol(Symbol{QualifiedName: "a", Kind: "function", Score: 1.0, Provenance: "x", Distance: 0})
	enc.WriteSymbol(Symbol{QualifiedName: "b", Kind: "function", Score: 0.8, Provenance: "x", Distance: 1})
	enc.WriteSymbol(Symbol{QualifiedName: "c", Kind: "function", Score: 0.6, Provenance: "x", Distance: 2})
	enc.WriteSymbol(Symbol{QualifiedName: "d", Kind: "function", Score: 0.4, Provenance: "x", Distance: 5})
	enc.Close()

	output := buf.String()

	if !strings.Contains(output, "## targets") {
		t.Error("missing ## targets")
	}
	if !strings.Contains(output, "## related") {
		t.Error("missing ## related")
	}
	if !strings.Contains(output, "## extended") {
		t.Error("missing ## extended")
	}
	if !strings.Contains(output, "## distance_5") {
		t.Error("missing ## distance_5")
	}
	if !strings.Contains(output, "sections=targets:1,related:1,extended:1,distance_5:1") {
		t.Errorf("wrong sections in summary:\n%s", output)
	}
}

func TestStreamEncoder_SkipsUnknownEdgeRefs(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "test", StreamOptions{})

	enc.WriteSymbol(Symbol{QualifiedName: "a.A", Kind: "function", Score: 0.9, Provenance: "x", Distance: 0})
	enc.WriteEdge(Edge{Source: "unknown.B", Target: "a.A", EdgeType: "calls"})
	enc.Close()

	output := buf.String()

	// Edge should be skipped (unknown source).
	if strings.Contains(output, "calls") {
		t.Errorf("should skip edge with unknown source:\n%s", output)
	}
	if !strings.Contains(output, "edges=0") {
		t.Errorf("summary should show edges=0:\n%s", output)
	}
}

func TestStreamEncoder_Incremental(t *testing.T) {
	// Verify that output is written incrementally (not buffered).
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "test", StreamOptions{})

	// After header, buffer should already have content.
	if buf.Len() == 0 {
		t.Error("header should be written immediately")
	}

	headerLen := buf.Len()
	enc.WriteSymbol(Symbol{QualifiedName: "a.A", Kind: "function", Score: 0.9, Provenance: "x", Distance: 0})

	// After first symbol, buffer should have grown.
	if buf.Len() <= headerLen {
		t.Error("symbol should be written immediately (not buffered)")
	}

	preEdgeLen := buf.Len()
	enc.WriteEdge(Edge{Source: "a.A", Target: "a.A", EdgeType: "self"})

	// After edge, buffer should have grown.
	if buf.Len() <= preEdgeLen {
		t.Error("edge should be written immediately (not buffered)")
	}

	enc.Close()
}
