package gcf

import (
	"fmt"
	"strings"
)

// DeltaPayload represents the diff between a prior context pack and the
// current result. Used for incremental context delivery.
type DeltaPayload struct {
	Tool         string
	BaseRoot     string // pack_root the consumer has
	NewRoot      string // pack_root of the current result
	Removed      []Symbol
	Added        []Symbol
	RemovedEdges []Edge
	AddedEdges   []Edge
	DeltaTokens  int
	FullTokens   int
}

// EncodeDelta serializes a DeltaPayload into GCF delta format.
func EncodeDelta(d *DeltaPayload) string {
	var b strings.Builder

	// Header.
	savings := 0.0
	if d.FullTokens > 0 {
		savings = 100.0 * (1.0 - float64(d.DeltaTokens)/float64(d.FullTokens))
	}
	b.WriteString(fmt.Sprintf("GCF tool=%s delta=true base_root=%s new_root=%s tokens=%d savings=%.0f%%\n",
		d.Tool, d.BaseRoot, d.NewRoot, d.DeltaTokens, savings))

	// Removed symbols: short references (consumer already has the full declaration).
	if len(d.Removed) > 0 {
		b.WriteString("## removed\n")
		for _, s := range d.Removed {
			kind := KindAbbrev[s.Kind]
			if kind == "" {
				kind = s.Kind
			}
			b.WriteString(fmt.Sprintf("%s %s\n", kind, s.QualifiedName))
		}
	}

	// Added symbols: full declarations (consumer doesn't have these).
	if len(d.Added) > 0 {
		b.WriteString("## added\n")
		for i, s := range d.Added {
			kind := KindAbbrev[s.Kind]
			if kind == "" {
				kind = s.Kind
			}
			b.WriteString(fmt.Sprintf("@%d %s %s %.2f %s\n",
				i, kind, s.QualifiedName, s.Score, s.Provenance))
		}
	}

	// Removed edges.
	if len(d.RemovedEdges) > 0 {
		b.WriteString("## edges_removed\n")
		for _, e := range d.RemovedEdges {
			b.WriteString(fmt.Sprintf("%s -> %s %s\n", e.Source, e.Target, e.EdgeType))
		}
	}

	// Added edges.
	if len(d.AddedEdges) > 0 {
		b.WriteString("## edges_added\n")
		for _, e := range d.AddedEdges {
			b.WriteString(fmt.Sprintf("%s -> %s %s\n", e.Source, e.Target, e.EdgeType))
		}
	}

	return b.String()
}
