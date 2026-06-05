// Package gcf implements the GCF (Graph Compact Format) encoder and decoder.
//
// GCF is a compact, text-only, graph-native wire format designed for MCP tool
// responses. It exploits referential identity (local IDs), graph topology
// (edges as references), and hierarchical grouping (distance-based sections)
// to achieve 84% token savings over JSON while remaining human-readable.
//
// Specification: https://github.com/blackwell-systems/gcf
//
// Encode a payload:
//
//	out := gcf.Encode(&gcf.Payload{
//	    Tool: "context_for_task",
//	    Symbols: []gcf.Symbol{{QualifiedName: "pkg.Func", Kind: "function", Score: 0.9, Provenance: "lsp_resolved"}},
//	})
//
// Decode a payload:
//
//	p, err := gcf.Decode(input)
//
// Session deduplication (previously-transmitted symbols as bare references):
//
//	sess := gcf.NewSession()
//	out1 := gcf.EncodeWithSession(&payload1, sess) // full declarations
//	out2 := gcf.EncodeWithSession(&payload2, sess) // reused symbols as @N refs
//
// Delta encoding (only added/removed symbols):
//
//	out := gcf.EncodeDelta(&gcf.DeltaPayload{...})
package gcf

import (
	"fmt"
	"strings"
)

// Symbol represents a node in a GCF payload.
type Symbol struct {
	QualifiedName string     // fully qualified identifier (e.g., "pkg/auth.Middleware")
	Kind          string     // node type: "function", "type", "method", etc.
	Score         float64    // relevance score (0.0 to 1.0)
	Provenance    string     // discovery method: "lsp_resolved", "ast_inferred", etc.
	Distance      int        // hops from query center (0=target, 1=related, 2+=extended)
	Signature     string     // optional: function/method signature
	Components    Components // optional: score breakdown
}

// Components holds the score breakdown for a symbol.
type Components struct {
	BlastRadius float64 // number of callers (normalized)
	Confidence  float64 // edge provenance confidence
	Recency     float64 // git recency signal
	Distance    float64 // graph distance penalty
}

// Edge represents a directed relationship in a GCF payload.
type Edge struct {
	Source   string // qualified name of source symbol
	Target   string // qualified name of target symbol
	EdgeType string
	Status   string // optional: "added", "removed", "unchanged" (for diff responses)
}

// Payload is the input/output structure for GCF encoding/decoding.
type Payload struct {
	Tool        string   // producing tool name (e.g., "context_for_task")
	TokensUsed  int      // actual tokens consumed by this payload
	TokenBudget int      // token budget requested by the consumer
	PackRoot    string   // content-addressed identity (hex SHA-256), enables delta encoding
	Symbols     []Symbol // ordered by score descending within each distance group
	Edges       []Edge   // directed relationships between symbols
}

// KindAbbrev maps full kind names to short GCF abbreviations.
var KindAbbrev = map[string]string{
	"function":      "fn",
	"type":          "type",
	"method":        "method",
	"interface":     "iface",
	"var":           "var",
	"const":         "const",
	"resource":      "resource",
	"table":         "table",
	"class":         "class",
	"selector":      "selector",
	"field":         "field",
	"route_handler": "route",
	"external":      "ext",
	"file":          "file",
	"package":       "pkg",
	"service":       "svc",
}

// KindExpand is the reverse of KindAbbrev.
var KindExpand = map[string]string{
	"fn":       "function",
	"type":     "type",
	"method":   "method",
	"iface":    "interface",
	"var":      "var",
	"const":    "const",
	"resource": "resource",
	"table":    "table",
	"class":    "class",
	"selector": "selector",
	"field":    "field",
	"route":    "route_handler",
	"ext":      "external",
	"file":     "file",
	"pkg":      "package",
	"svc":      "service",
}

// Encode serializes a Payload into GCF text format.
func Encode(p *Payload) string {
	var b strings.Builder

	// Header line.
	b.WriteString(fmt.Sprintf("GCF tool=%s budget=%d tokens=%d symbols=%d edges=%d",
		p.Tool, p.TokenBudget, p.TokensUsed, len(p.Symbols), len(p.Edges)))
	if p.PackRoot != "" {
		b.WriteString(fmt.Sprintf(" pack_root=%s", p.PackRoot))
	}
	b.WriteByte('\n')

	// Build symbol index for edge references.
	symIndex := make(map[string]int, len(p.Symbols))
	for i, s := range p.Symbols {
		symIndex[s.QualifiedName] = i
	}

	// Group symbols by distance.
	groups := groupByDistance(p.Symbols)
	groupNames := []string{"targets", "related", "extended"}

	for _, g := range groups {
		if len(g.symbols) == 0 {
			continue
		}
		name := "targets"
		if g.distance < len(groupNames) {
			name = groupNames[g.distance]
		} else {
			name = fmt.Sprintf("distance_%d", g.distance)
		}
		b.WriteString("## ")
		b.WriteString(name)
		b.WriteByte('\n')

		for _, s := range g.symbols {
			idx := symIndex[s.QualifiedName]
			kind := KindAbbrev[s.Kind]
			if kind == "" {
				kind = s.Kind
			}
			b.WriteString(fmt.Sprintf("@%d %s %s %.2f %s",
				idx, kind, s.QualifiedName, s.Score, s.Provenance))
			b.WriteByte('\n')
		}
	}

	// Edges section.
	if len(p.Edges) > 0 {
		b.WriteString(fmt.Sprintf("## edges [%d]\n", len(p.Edges)))
		for _, e := range p.Edges {
			srcIdx, srcOk := symIndex[e.Source]
			tgtIdx, tgtOk := symIndex[e.Target]
			if !srcOk || !tgtOk {
				continue
			}
			line := fmt.Sprintf("@%d<@%d %s", tgtIdx, srcIdx, e.EdgeType)
			if e.Status != "" && e.Status != "unchanged" {
				line += " " + e.Status
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	return b.String()
}

type distanceGroup struct {
	distance int
	symbols  []Symbol
}

func groupByDistance(symbols []Symbol) []distanceGroup {
	if len(symbols) == 0 {
		return nil
	}
	var groups []distanceGroup
	var current *distanceGroup
	for _, s := range symbols {
		if current == nil || current.distance != s.Distance {
			groups = append(groups, distanceGroup{distance: s.Distance})
			current = &groups[len(groups)-1]
		}
		current.symbols = append(current.symbols, s)
	}
	return groups
}
