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
	focus        int             // probe node id, used to build the scoped REL arm
}

// selectQueries builds direction-sensitive queries with ground truth from edges.
// Probe symbols and every answer-set member are required to have a short name that
// is UNIQUE in the fixture: at 500 symbols the name list cycles (short names repeat),
// so without this guard a probe or answer would be ambiguous. Selection iterates ids
// in order for determinism (map ranges are randomized in Go).
func selectQueries(idQ map[int]string, edges []dedge) []edgeQuery {
	out := map[int][]dedge{}
	in := map[int][]dedge{}
	for _, e := range edges {
		out[e.src] = append(out[e.src], e)
		in[e.tgt] = append(in[e.tgt], e)
	}
	shortCount := map[string]int{}
	for _, qn := range idQ {
		shortCount[edgeShort(qn)]++
	}
	uniq := func(id int) bool { return shortCount[edgeShort(idQ[id])] == 1 }
	allUniq := func(es []dedge, pick func(dedge) int) bool {
		for _, e := range es {
			if shortCount[edgeShort(idQ[pick(e)])] != 1 {
				return false
			}
		}
		return true
	}
	set := func(es []dedge, pick func(dedge) int) map[string]bool {
		m := map[string]bool{}
		for _, e := range es {
			m[edgeShort(idQ[pick(e)])] = true
		}
		return m
	}
	maxID := len(idQ)
	var qs []edgeQuery
	// fwd: a pure-source node with outgoing calls, all endpoints unique-short
	for id := 0; id < maxID; id++ {
		calls := filterType(out[id], "calls")
		if len(calls) >= 1 && len(in[id]) == 0 && uniq(id) && allUniq(calls, func(e dedge) int { return e.tgt }) {
			qs = append(qs, edgeQuery{
				name:   "fwd_calls",
				prompt: fmt.Sprintf("Which symbols does %s call directly? List only their names.", edgeShort(idQ[id])),
				expect: set(calls, func(e dedge) int { return e.tgt }),
				focus:  id,
			})
			break
		}
	}
	// bwd: a pure-target node with incoming calls (the hard one)
	for id := 0; id < maxID; id++ {
		calls := filterType(in[id], "calls")
		if len(calls) >= 1 && len(out[id]) == 0 && uniq(id) && allUniq(calls, func(e dedge) int { return e.src }) {
			qs = append(qs, edgeQuery{
				name:   "bwd_callers",
				prompt: fmt.Sprintf("Which symbols call %s directly? List only their names.", edgeShort(idQ[id])),
				expect: set(calls, func(e dedge) int { return e.src }),
				focus:  id,
			})
			break
		}
	}
	// shared node: both in and out edges, all endpoints unique-short, in/out disjoint
	for id := 0; id < maxID; id++ {
		if len(out[id]) < 1 || len(in[id]) < 1 || !uniq(id) {
			continue
		}
		if !allUniq(out[id], func(e dedge) int { return e.tgt }) || !allUniq(in[id], func(e dedge) int { return e.src }) {
			continue
		}
		outSet := set(out[id], func(e dedge) int { return e.tgt })
		inSet := set(in[id], func(e dedge) int { return e.src })
		disjoint := true
		for n := range outSet {
			if inSet[n] {
				disjoint = false
				break
			}
		}
		if !disjoint {
			continue
		}
		sn := edgeShort(idQ[id])
		qs = append(qs,
			edgeQuery{
				name:     "shared_out",
				prompt:   fmt.Sprintf("List the symbols that are the TARGET of a relationship whose SOURCE is %s (i.e. the symbols %s calls or references). List only their names.", sn, sn),
				expect:   outSet,
				opposite: inSet,
				focus:    id,
			},
			edgeQuery{
				name:     "shared_in",
				prompt:   fmt.Sprintf("List the symbols that are the SOURCE of a relationship whose TARGET is %s (i.e. the symbols that call or reference %s). List only their names.", sn, sn),
				expect:   inSet,
				opposite: outSet,
				focus:    id,
			})
		break
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

var edgeFixtures = []edgeFixture{{"small", 20, 12}, {"med", 50, 30}, {"large", 500, 200}}

// activeEdgeFixtures filters by EVAL_EDGE_FIXTURES (comma list of names), e.g.
// EVAL_EDGE_FIXTURES=large to run only the 500-symbol rung. Default: all.
func activeEdgeFixtures() []edgeFixture {
	sel := os.Getenv("EVAL_EDGE_FIXTURES")
	if sel == "" {
		return edgeFixtures
	}
	want := map[string]bool{}
	for _, s := range strings.Split(sel, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []edgeFixture
	for _, f := range edgeFixtures {
		if want[f.name] {
			out = append(out, f)
		}
	}
	return out
}

func buildEdgeArms(f edgeFixture) (arms map[string]string, universe map[string]bool, queries []edgeQuery, idQ map[int]string, edges []dedge, nodeSection string) {
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
	nodeSection = strings.Join(lines[:es-1], "\n")
	return arms, universe, queries, idQ, edges, nodeSection
}

// nodeLinesByID maps id -> the full node line, from the canonical encoding.
func nodeLinesByID(encoded string) map[int]string {
	m := map[int]string{}
	for _, ln := range strings.Split(encoded, "\n") {
		if !strings.HasPrefix(ln, "@") || strings.Contains(ln, "<@") || strings.Contains(ln, "#") {
			continue
		}
		f := strings.Fields(ln)
		if len(f) >= 5 && scoreShaped(f[3]) {
			var id int
			fmt.Sscanf(f[0][1:], "%d", &id)
			m[id] = ln
		}
	}
	return m
}

// renderRelVariant builds a direction-explicit relationship arm scoped to the focus
// node's incident edges. Two levers: `labeled` (explicit source=/target=/type= vs bare
// SVO) and `scoped` (only the neighborhood's node lines vs the full node section).
func renderRelVariant(focus int, idQ map[int]string, edges []dedge, fullNodeSection string, nodeLines map[int]string, labeled, scoped bool) string {
	var inc []dedge
	nbr := map[int]bool{focus: true}
	for _, e := range edges {
		if e.src == focus || e.tgt == focus {
			inc = append(inc, e)
			nbr[e.src] = true
			nbr[e.tgt] = true
		}
	}
	nodeSec := fullNodeSection
	if scoped {
		var ls []string
		for id := 0; id < len(idQ)+len(edges); id++ {
			if nbr[id] {
				if ln, ok := nodeLines[id]; ok {
					ls = append(ls, ln)
				}
			}
		}
		nodeSec = fmt.Sprintf("GCF profile=graph tool=context symbols=%d\n## targets\n%s", len(ls), strings.Join(ls, "\n"))
	}
	var rl []string
	for _, e := range inc {
		if labeled {
			rl = append(rl, fmt.Sprintf("source=%s target=%s type=%s", idQ[e.src], idQ[e.tgt], e.typ))
		} else {
			rl = append(rl, fmt.Sprintf("%s %s %s", idQ[e.src], e.typ, idQ[e.tgt]))
		}
	}
	return nodeSec + fmt.Sprintf("\n## rel [%d]\n", len(rl)) + strings.Join(rl, "\n")
}

// renderRel is the original REL arm (bare SVO, full node section).
func renderRel(focus int, idQ map[int]string, edges []dedge, nodeSection string) string {
	return renderRelVariant(focus, idQ, edges, nodeSection, nil, false, false)
}

var relArmSpec = map[string][2]bool{ // arm -> {labeled, scoped}
	"REL": {false, false}, "RELL": {true, false}, "RELS": {false, true}, "RELSL": {true, true},
}

// edgeRunArms is the full arm set scored at run time. EVAL_EDGE_ARMS selects a subset.
var edgeRunArms = []string{"A", "B", "C", "ADJ", "JSON", "REL", "RELL", "RELS", "RELSL"}

func activeArms() []string {
	sel := os.Getenv("EVAL_EDGE_ARMS")
	if sel == "" {
		return edgeRunArms
	}
	want := map[string]bool{}
	for _, s := range strings.Split(sel, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []string
	for _, a := range edgeRunArms {
		if want[a] {
			out = append(out, a)
		}
	}
	return out
}

func TestEdgeProbeArtifacts(t *testing.T) {
	for _, f := range activeEdgeFixtures() {
		arms, universe, queries, idQ, edges, nodeSection := buildEdgeArms(f)
		if len(queries) == 0 {
			t.Fatalf("%s: no queries selected", f.name)
		}
		t.Logf("=== fixture %s (%d symbols, %d edges), %d unique names, %d queries ===",
			f.name, f.n, f.e, len(universe), len(queries))
		for _, q := range queries {
			t.Logf("  %-12s focus=@%d expect=%v opposite=%v", q.name, q.focus, keys(q.expect), keys(q.opposite))
		}
		// arm A must equal canonical; B/C/ADJ/JSON must differ and be non-empty.
		if arms["A"] == arms["B"] || arms["B"] == arms["C"] || len(arms["JSON"]) == 0 || len(arms["ADJ"]) == 0 {
			t.Errorf("%s: arm rendering degenerate", f.name)
		}
		// REL arm must build (scoped, non-empty rel section) for each query.
		for _, q := range queries {
			rel := renderRel(q.focus, idQ, edges, nodeSection)
			if !strings.Contains(rel, "## rel [") {
				t.Errorf("%s/%s: REL arm missing rel section", f.name, q.name)
			}
		}
		if f.name == "small" {
			for _, a := range edgeArms {
				t.Logf("--- arm %s ---\n%s", a, arms[a])
			}
			t.Logf("--- arm REL (scoped to %s) ---\n%s", queries[0].name,
				renderRel(queries[0].focus, idQ, edges, nodeSection))
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
	for _, a := range activeArms() {
		armTotals[a] = &acc{}
		armQuery[a] = map[string]*acc{}
	}

	for _, f := range activeEdgeFixtures() {
		arms, universe, queries, idQ, edges, nodeSection := buildEdgeArms(f)
		nodeLines := nodeLinesByID(arms["A"])
		logf("\n=== fixture %s (%d symbols, %d edges) ===", f.name, f.n, f.e)
		for _, a := range edgeArms {
			logf("  arm %-4s tokens(est) %d", a, len(arms[a])/4)
		}
		for _, q := range queries {
			for _, a := range activeArms() {
				if armQuery[a][q.name] == nil {
					armQuery[a][q.name] = &acc{}
				}
				content := arms[a]
				if spec, ok := relArmSpec[a]; ok {
					content = renderRelVariant(q.focus, idQ, edges, nodeSection, nodeLines, spec[0], spec[1])
				}
				pass := 0
				var sample string
				for r := 0; r < runs; r++ {
					prompt := fmt.Sprintf("Here is a code context payload:\n\n%s\n\nQuestion: %s\nAnswer concisely.",
						content, q.prompt)
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
	for _, a := range activeArms() {
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
		for _, a := range activeArms() {
			if c := armQuery[a][qn]; c != nil && c.total > 0 {
				row += fmt.Sprintf("  %s=%d/%d", a, c.correct, c.total)
			}
		}
		logf("  %s", row)
	}
}
