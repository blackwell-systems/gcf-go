package gcf

import (
	"bytes"
	"strings"
	"testing"
)

// TestStreamingHeaderCarriesProfile locks the fix for the streaming graph
// encoder omitting the required profile discriminator (SPEC 3.1 / 16.1).
func TestStreamingHeaderCarriesProfile(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, "context_for_task", StreamOptions{TokenBudget: 5000})
	enc.WriteSymbol(Symbol{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.95, Provenance: "lsp", Distance: 0})
	enc.WriteEdge(Edge{Source: "pkg.Auth", Target: "pkg.Auth", EdgeType: "calls"})
	enc.Close()

	got := buf.String()
	if !strings.HasPrefix(got, "GCF profile=graph tool=context_for_task") {
		t.Fatalf("streaming header must begin with 'GCF profile=graph tool=...', got:\n%s", got)
	}
	// The streamed payload must decode cleanly through the strict graph decoder.
	if _, err := Decode(got); err != nil {
		t.Fatalf("streaming output failed to decode: %v\n%s", err, got)
	}
}

// TestGraphDecodeRequiresProfileGraph enforces SPEC 16.3: the graph decoder
// must reject a header that omits or misstates profile=graph.
func TestGraphDecodeRequiresProfileGraph(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"missing profile", "GCF tool=test\n## targets\n@0 fn pkg.Foo 0.78 lsp\n", true},
		{"wrong profile", "GCF profile=generic\n=42\n", true},
		{"valid graph", "GCF profile=graph tool=test symbols=1\n## targets\n@0 fn pkg.Foo 0.78 lsp\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got success for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected success, got error %v for %q", err, tc.input)
			}
		})
	}
}
