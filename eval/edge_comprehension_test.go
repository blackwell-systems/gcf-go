// Staged pilot for the edge-direction comprehension study (scope note:
// gcf-integration-work/gcf-graph-symbol-position-scope.md, "edge arrow" finding).
//
// Question: is direction-comprehension a SYNTAX problem or a PRESENTATION problem?
// Arms (same graph, five renderings of the edges):
//
//	A  target<source   @1<@0 calls      (current wire)
//	B  source>target   @0>@1 calls      (source-first arrow)
//	C  natural SVO      @0 calls @1
//	ADJ adjacency       per-node out=/in= lists (direction pre-resolved)
//	JSON control        {source,target,type} objects
//
// Direction-sensitive queries with deterministic ground truth; bucketed scoring
// (correct / wrong / none), and "wrong" specifically fires on the opposite
// direction (reversed edge) so reversal is measured, not hidden.
//
// Pilot run (staged; a few hundred calls on cheap models):
//
//	EVAL_EDGE=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//	  OPENAI_API_KEY=... EVAL_MODEL=<id> EVAL_TEMPERATURE=0.2 \
//	  GOWORK=off go test -run TestEdgeComprehension -v -timeout 30m
//
// TestEdgeProbeArtifacts runs with no LLM and verifies arm construction + probes.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
)

type dedge struct {
	src, tgt int // node ids
	typ      string
}

var edgeLineRe = regexp.MustCompile(`^@(\d+)<@(\d+)\s+(\S+)`)

// edgeShort is the last dot-segment of a qualified name.
func edgeShort(qn string) string {
	if i := strings.LastIndex(qn, "."); i >= 0 {
		return qn[i+1:]
	}
	return qn
}

// parseGraph returns id->qname, the edge list, and the edges-section line span
// [start,end) within the encoded lines, from a gcf.Encode graph payload.
func parseGraph(encoded string) (idQ map[int]string, edges []dedge, lines []string, edgeStart, edgeEnd int) {
	lines = strings.Split(encoded, "\n")
	idQ = map[int]string{}
	for _, idq := range parseNodeLines(encoded) {
		var id int
		fmt.Sscanf(idq[0], "%d", &id)
		idQ[id] = idq[1]
	}
	edgeStart, edgeEnd = -1, -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "## edges") {
			edgeStart = i + 1
			edgeEnd = len(lines)
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(lines[j], "## ") || strings.TrimSpace(lines[j]) == "" {
					edgeEnd = j
					break
				}
			}
			for j := edgeStart; j < edgeEnd; j++ {
				m := edgeLineRe.FindStringSubmatch(lines[j])
				if m == nil {
					continue
				}
				var t, s int
				fmt.Sscanf(m[1], "%d", &t)
				fmt.Sscanf(m[2], "%d", &s)
				edges = append(edges, dedge{src: s, tgt: t, typ: m[3]})
			}
			break
		}
	}
	return idQ, edges, lines, edgeStart, edgeEnd
}

// renderArm produces one arm's payload from the canonical encoding.
func renderArm(arm, encoded string, idQ map[int]string, edges []dedge, lines []string, es, ee int) string {
	head := strings.Join(lines[:es-1], "\n") // node section incl. "## edges" header removed below
	// head currently excludes the "## edges" header line (lines[es-1]); rebuild per arm.
	nodeSection := strings.Join(lines[:es-1], "\n")
	_ = head
	switch arm {
	case "A":
		return encoded
	case "B":
		var b strings.Builder
		b.WriteString(nodeSection)
		b.WriteString("\n## edges\n")
		for _, e := range edges {
			b.WriteString(fmt.Sprintf("@%d>@%d %s\n", e.src, e.tgt, e.typ))
		}
		return strings.TrimRight(b.String(), "\n")
	case "C":
		var b strings.Builder
		b.WriteString(nodeSection)
		b.WriteString("\n## edges\n")
		for _, e := range edges {
			b.WriteString(fmt.Sprintf("@%d %s @%d\n", e.src, e.typ, e.tgt))
		}
		return strings.TrimRight(b.String(), "\n")
	case "ADJ":
		out := map[int][]string{}
		in := map[int][]string{}
		for _, e := range edges {
			out[e.src] = append(out[e.src], fmt.Sprintf("%s %s", e.typ, edgeShort(idQ[e.tgt])))
			in[e.tgt] = append(in[e.tgt], fmt.Sprintf("%s %s", e.typ, edgeShort(idQ[e.src])))
		}
		var b strings.Builder
		b.WriteString(nodeSection)
		b.WriteString("\n## adjacency\n")
		// stable id order
		for id := 0; id < len(idQ)+len(edges); id++ {
			qn, ok := idQ[id]
			if !ok {
				continue
			}
			if len(out[id]) == 0 && len(in[id]) == 0 {
				continue
			}
			o := "(none)"
			if len(out[id]) > 0 {
				o = strings.Join(out[id], ", ")
			}
			inc := "(none)"
			if len(in[id]) > 0 {
				inc = strings.Join(in[id], ", ")
			}
			b.WriteString(fmt.Sprintf("@%d %s: out=[%s]; in=[%s]\n", id, edgeShort(qn), o, inc))
		}
		return strings.TrimRight(b.String(), "\n")
	case "JSON":
		type js struct {
			Symbols []map[string]any `json:"symbols"`
			Edges   []map[string]any `json:"edges"`
		}
		var d js
		for id := 0; id < len(idQ)+len(edges); id++ {
			if qn, ok := idQ[id]; ok {
				d.Symbols = append(d.Symbols, map[string]any{"name": edgeShort(qn), "qualified_name": qn})
			}
		}
		for _, e := range edges {
			d.Edges = append(d.Edges, map[string]any{
				"source": edgeShort(idQ[e.src]), "target": edgeShort(idQ[e.tgt]), "type": e.typ})
		}
		out, _ := json.MarshalIndent(d, "", "  ")
		return string(out)
	}
	return encoded
}

type edgeQuery struct {
	name, prompt string
	expect       map[string]bool // short names that MUST appear
	opposite     map[string]bool // short names whose presence = reversed direction (wrong)
}

// selectQueries builds direction-sensitive queries with ground truth from edges.
func selectQueries(idQ map[int]string, edges []dedge) []edgeQuery {
	out := map[int][]dedge{}
	in := map[int][]dedge{}
	for _, e := range edges {
		out[e.src] = append(out[e.src], e)
		in[e.tgt] = append(in[e.tgt], e)
	}
	set := func(es []dedge, pick func(dedge) int) map[string]bool {
		m := map[string]bool{}
		for _, e := range es {
			m[edgeShort(idQ[pick(e)])] = true
		}
		return m
	}
	var qs []edgeQuery
	// fwd: a node with outgoing calls
	for id, es := range out {
		calls := filterType(es, "calls")
		if len(calls) >= 1 && len(in[id]) == 0 { // pure source, unambiguous
			qs = append(qs, edgeQuery{
				name:   "fwd_calls",
				prompt: fmt.Sprintf("Which symbols does %s call directly? List only their names.", edgeShort(idQ[id])),
				expect: set(calls, func(e dedge) int { return e.tgt }),
			})
			break
		}
	}
	// bwd: a node with incoming calls (the hard one)
	for id, es := range in {
		calls := filterType(es, "calls")
		if len(calls) >= 1 && len(out[id]) == 0 { // pure target
			qs = append(qs, edgeQuery{
				name:   "bwd_callers",
				prompt: fmt.Sprintf("Which symbols call %s directly? List only their names.", edgeShort(idQ[id])),
				expect: set(calls, func(e dedge) int { return e.src }),
			})
			break
		}
	}
	// shared node: both in and out edges
	for id := 0; id < len(idQ)+len(edges); id++ {
		if len(out[id]) >= 1 && len(in[id]) >= 1 {
			outSet := set(out[id], func(e dedge) int { return e.tgt })
			inSet := set(in[id], func(e dedge) int { return e.src })
			qs = append(qs,
				edgeQuery{
					name:     "shared_out",
					prompt:   fmt.Sprintf("Which symbols does %s point to (its outgoing relationships)? List only their names.", edgeShort(idQ[id])),
					expect:   outSet,
					opposite: inSet,
				},
				edgeQuery{
					name:     "shared_in",
					prompt:   fmt.Sprintf("Which symbols point to %s (its incoming relationships)? List only their names.", edgeShort(idQ[id])),
					expect:   inSet,
					opposite: outSet,
				})
			break
		}
	}
	return qs
}

func filterType(es []dedge, t string) []dedge {
	var out []dedge
	for _, e := range es {
		if e.typ == t {
			out = append(out, e)
		}
	}
	return out
}

// classifyEdge buckets a response against expected / opposite name sets.
func classifyEdge(resp string, universe, expect, opposite map[string]bool) string {
	got := map[string]bool{}
	low := strings.ToLower(resp)
	for name := range universe {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`).MatchString(low) {
			got[name] = true
		}
	}
	if len(got) == 0 {
		return "none"
	}
	for name := range opposite {
		if got[name] {
			return "wrong" // reversed direction
		}
	}
	for name := range expect {
		if !got[name] {
			return "wrong"
		}
	}
	return "correct"
}

var edgeArms = []string{"A", "B", "C", "ADJ", "JSON"}

type edgeFixture struct {
	name string
	n, e int
}

var edgeFixtures = []edgeFixture{{"small", 20, 12}, {"med", 50, 30}}

func buildEdgeArms(f edgeFixture) (arms map[string]string, universe map[string]bool, queries []edgeQuery) {
	fx := buildFixture(f.n, f.e)
	encoded := gcf.Encode(fx)
	idQ, edges, lines, es, ee := parseGraph(encoded)
	arms = map[string]string{}
	for _, a := range edgeArms {
		arms[a] = renderArm(a, encoded, idQ, edges, lines, es, ee)
	}
	universe = map[string]bool{}
	for _, qn := range idQ {
		universe[edgeShort(qn)] = true
	}
	queries = selectQueries(idQ, edges)
	return arms, universe, queries
}

func TestEdgeProbeArtifacts(t *testing.T) {
	for _, f := range edgeFixtures {
		arms, universe, queries := buildEdgeArms(f)
		if len(queries) == 0 {
			t.Fatalf("%s: no queries selected", f.name)
		}
		t.Logf("=== fixture %s (%d symbols, %d edges), %d unique names, %d queries ===",
			f.name, f.n, f.e, len(universe), len(queries))
		for _, q := range queries {
			t.Logf("  %-12s expect=%v opposite=%v", q.name, keys(q.expect), keys(q.opposite))
		}
		// arm A must equal canonical; B/C/ADJ/JSON must differ and be non-empty.
		if arms["A"] == arms["B"] || arms["B"] == arms["C"] || len(arms["JSON"]) == 0 || len(arms["ADJ"]) == 0 {
			t.Errorf("%s: arm rendering degenerate", f.name)
		}
		if f.name == "small" {
			for _, a := range edgeArms {
				t.Logf("--- arm %s ---\n%s", a, arms[a])
			}
		}
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestEdgeComprehension(t *testing.T) {
	if os.Getenv("EVAL_EDGE") == "" {
		t.Skip("set EVAL_EDGE=1 to run the edge-direction comprehension pilot")
	}
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "openai"
	}
	rawCall, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}
	callLLM := func(prompt string) (string, error) {
		var last error
		for a := 0; a < 4; a++ {
			r, e := rawCall(prompt)
			if e == nil {
				return r, nil
			}
			last = e
			time.Sleep(time.Duration(1<<a) * time.Second)
		}
		return "", last
	}
	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	runs := 3

	resultsDir := filepath.Join("results", "comprehension")
	os.MkdirAll(resultsDir, 0755)
	logPath := filepath.Join(resultsDir, fmt.Sprintf("edge-probe-%s-%s.log",
		strings.ReplaceAll(model, "/", "_"), time.Now().Format("2006-01-02-150405")))
	lf, _ := os.Create(logPath)
	defer lf.Close()
	logf := func(format string, a ...any) {
		s := fmt.Sprintf(format, a...)
		t.Log(s)
		lf.WriteString(s + "\n")
		lf.Sync()
	}
	logf("Backend: %s | model: %s | runs/cell: %d", backendLabel, model, runs)

	// arm -> {correct,total}, and arm -> query -> correct count
	type acc struct{ correct, total int }
	armTotals := map[string]*acc{}
	armQuery := map[string]map[string]*acc{}
	for _, a := range edgeArms {
		armTotals[a] = &acc{}
		armQuery[a] = map[string]*acc{}
	}

	for _, f := range edgeFixtures {
		arms, universe, queries := buildEdgeArms(f)
		logf("\n=== fixture %s (%d symbols, %d edges) ===", f.name, f.n, f.e)
		for _, a := range edgeArms {
			logf("  arm %-4s tokens(est) %d", a, len(arms[a])/4)
		}
		for _, q := range queries {
			for _, a := range edgeArms {
				if armQuery[a][q.name] == nil {
					armQuery[a][q.name] = &acc{}
				}
				pass := 0
				var sample string
				for r := 0; r < runs; r++ {
					prompt := fmt.Sprintf("Here is a code context payload:\n\n%s\n\nQuestion: %s\nAnswer concisely.",
						arms[a], q.prompt)
					resp, cerr := callLLM(prompt)
					if cerr != nil {
						continue
					}
					b := classifyEdge(resp, universe, q.expect, q.opposite)
					if b == "correct" {
						pass++
					}
					if r == 0 {
						sample = strings.TrimSpace(strings.ReplaceAll(resp, "\n", " "))
						if len(sample) > 55 {
							sample = sample[:55]
						}
					}
				}
				armTotals[a].correct += pass
				armTotals[a].total += runs
				armQuery[a][q.name].correct += pass
				armQuery[a][q.name].total += runs
				logf("  %-4s %-12s %d/%d  e.g.=%q", a, q.name, pass, runs, sample)
			}
		}
	}

	logf("\n=== EDGE PILOT SUMMARY (%s) ===", model)
	logf("%-4s %10s", "arm", "accuracy")
	for _, a := range edgeArms {
		t := armTotals[a]
		if t.total == 0 {
			continue
		}
		logf("%-4s %8.0f%%   (%d/%d)", a, 100*float64(t.correct)/float64(t.total), t.correct, t.total)
	}
	logf("\nby query type (correct/total):")
	qnames := []string{"fwd_calls", "bwd_callers", "shared_out", "shared_in"}
	for _, qn := range qnames {
		row := qn + ":"
		for _, a := range edgeArms {
			if c := armQuery[a][qn]; c != nil && c.total > 0 {
				row += fmt.Sprintf("  %s=%d/%d", a, c.correct, c.total)
			}
		}
		logf("  %s", row)
	}
}
