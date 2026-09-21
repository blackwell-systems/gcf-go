// Throwaway prototype probe for the graph `## loc` section (SPEC v3.6.0, §6b).
//
// This is NOT the production codec. It reuses the comprehension fixture and the
// real gcf.Encode output, then bolts positions on three ways to answer one
// question before we pay the spec-first, 7-SDK implementation cost:
//
//	Does the id-keyed `## loc` side section cost comprehension versus putting
//	position inline on the node line, and does either regress against JSON?
//
// Arms (all carry identical positions; one symbol is deliberately omitted from
// every arm to test that the model says "unknown" instead of hallucinating):
//
//	json-loc    - positions as natural JSON fields (the no-regress baseline)
//	gcf-loc     - real gcf.Encode output + an appended `## loc` section (§6b)
//	gcf-inline  - real gcf.Encode output with `file line col` appended to each
//	              node line (the rejected alternative; the join-cost baseline)
//
// Run (cheap early signal; defaults to the local codex CLI):
//
//	EVAL_LOC=1 GOWORK=off go test -run TestLocComprehension -v -timeout 30m
//
// Backend defaults to codex; override with EVAL_BACKEND=cli|api|openai|google
// (shared setupBackend). Writes a self-contained log to
// results/comprehension/loc-probe-<backend>-<model>-<timestamp>.log.
package eval

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
)

// posFor derives a deterministic 1-indexed position from a qualified name.
// File is the package path plus a same-named .go file, so symbols in one package
// share a file (realistic); line/column come from an FNV hash of the qname.
func posFor(qn string) (file string, line, col int) {
	rest := qn
	if i := strings.Index(qn, "project/"); i >= 0 {
		rest = qn[i+len("project/"):]
	}
	pkgPath := rest
	if dot := strings.Index(rest, "."); dot >= 0 {
		pkgPath = rest[:dot] // e.g. "internal/auth"
	}
	base := pkgPath
	if slash := strings.LastIndex(pkgPath, "/"); slash >= 0 {
		base = pkgPath[slash+1:]
	}
	file = pkgPath + "/" + base + ".go"
	h := fnv.New32a()
	_, _ = h.Write([]byte(qn))
	v := h.Sum32()
	line = int(v%1000) + 1
	col = int((v/1000)%80) + 1
	return file, line, col
}

// scoreShaped reports whether s matches the node-line score grammar [-]D.DD.
func scoreShaped(s string) bool {
	s = strings.TrimPrefix(s, "-")
	dot := strings.IndexByte(s, '.')
	if dot < 1 || len(s)-dot-1 != 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == dot {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseNodeLines returns (id, qname) for every node line in a graph payload,
// isolating node lines by shape: "@id kind qname score prov", where the 4th
// field is a two-decimal score (edges contain "<@", refs contain "#").
func parseNodeLines(gcfOut string) [][2]string {
	var out [][2]string
	for _, line := range strings.Split(gcfOut, "\n") {
		if !strings.HasPrefix(line, "@") || strings.Contains(line, "<@") || strings.Contains(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 || !scoreShaped(f[3]) {
			continue
		}
		out = append(out, [2]string{f[0][1:], f[2]})
	}
	return out
}

// buildLocSection builds the §6b `## loc [N]` section, omitting one qname.
func buildLocSection(nodes [][2]string, omit string) string {
	var b strings.Builder
	var lines []string
	for _, idq := range nodes {
		id, qn := idq[0], idq[1]
		if qn == omit {
			continue
		}
		file, line, col := posFor(qn)
		lines = append(lines, fmt.Sprintf("@%s %s %d %d", id, file, line, col))
	}
	fmt.Fprintf(&b, "## loc [%d]\n", len(lines))
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// inlineLoc appends "file line col" to each node line (omitting one qname),
// leaving group headers, edges, and refs untouched.
func inlineLoc(gcfOut, omit string) string {
	var out []string
	for _, line := range strings.Split(gcfOut, "\n") {
		if strings.HasPrefix(line, "@") && !strings.Contains(line, "<@") && !strings.Contains(line, "#") {
			f := strings.Fields(line)
			if len(f) >= 5 && scoreShaped(f[3]) && f[2] != omit {
				file, ln, col := posFor(f[2])
				line = fmt.Sprintf("%s %s %d %d", line, file, ln, col)
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// jsonWithPos renders the payload as JSON with position fields per symbol
// (omitting them for one qname), the natural JSON baseline for the same data.
func jsonWithPos(p *gcf.Payload, omit string) string {
	type sym struct {
		QualifiedName string  `json:"qualified_name"`
		Kind          string  `json:"kind"`
		Score         float64 `json:"score"`
		Provenance    string  `json:"provenance"`
		Distance      int     `json:"distance"`
		File          string  `json:"file,omitempty"`
		Line          int     `json:"line,omitempty"`
		Column        int     `json:"column,omitempty"`
	}
	type edge struct {
		Source, Target, Type string
	}
	doc := struct {
		Tool    string `json:"tool"`
		Symbols []sym  `json:"symbols"`
		Edges   []edge `json:"edges"`
	}{Tool: p.Tool}
	for _, s := range p.Symbols {
		e := sym{QualifiedName: s.QualifiedName, Kind: s.Kind, Score: s.Score, Provenance: s.Provenance, Distance: s.Distance}
		if s.QualifiedName != omit {
			e.File, e.Line, e.Column = posFor(s.QualifiedName)
		}
		doc.Symbols = append(doc.Symbols, e)
	}
	for _, ed := range p.Edges {
		doc.Edges = append(doc.Edges, edge{ed.Source, ed.Target, ed.EdgeType})
	}
	out, _ := json.MarshalIndent(doc, "", "  ")
	return string(out)
}

func containsFileLine(expectFile string, expectLine int, resp string) (bool, string) {
	hasFile := strings.Contains(resp, expectFile)
	hasLine := strings.Contains(resp, fmt.Sprintf("%d", expectLine))
	if hasFile && hasLine {
		return true, "file+line"
	}
	return false, fmt.Sprintf("file=%v line=%v", hasFile, hasLine)
}

// TestLocProbeArtifacts verifies the three arms are built correctly without any
// LLM calls: node parsing, loc-section count, and that the "absent" probe symbol
// carries no position in any arm (so absent_location is a fair test).
func TestLocProbeArtifacts(t *testing.T) {
	fixture := buildFixture(500, 200)
	gcfOut := gcf.Encode(fixture)
	nodes := parseNodeLines(gcfOut)
	if len(nodes) != len(fixture.Symbols) {
		t.Fatalf("parsed %d node lines, want %d", len(nodes), len(fixture.Symbols))
	}
	absent := fixture.Symbols[42].QualifiedName

	// gcf-loc: header count and one omission.
	loc := buildLocSection(nodes, absent)
	if !strings.HasPrefix(loc, fmt.Sprintf("## loc [%d]\n", len(nodes)-1)) {
		t.Errorf("loc header wrong: %q", strings.SplitN(loc, "\n", 2)[0])
	}
	if got := strings.Count(loc, "\n@"); got != len(nodes)-1 {
		t.Errorf("loc lines = %d, want %d", got, len(nodes)-1)
	}

	// gcf-inline: the absent symbol's node line must remain 5 fields (no position),
	// while a present symbol's line must gain 3 (file line col) => 8 fields.
	inline := inlineLoc(gcfOut, absent)
	var absentFields, presentFields int
	for _, line := range strings.Split(inline, "\n") {
		if !strings.HasPrefix(line, "@") || strings.Contains(line, "<@") || strings.Contains(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 3 && f[2] == absent {
			absentFields = len(f)
		}
		if len(f) >= 3 && f[2] == fixture.Symbols[137].QualifiedName {
			presentFields = len(f)
		}
	}
	if absentFields != 5 {
		t.Errorf("inline absent line has %d fields, want 5 (no position)", absentFields)
	}
	if presentFields != 8 {
		t.Errorf("inline present line has %d fields, want 8 (5 + file line col)", presentFields)
	}

	// json-loc: absent symbol must have no line/column fields.
	js := jsonWithPos(fixture, absent)
	var doc struct {
		Symbols []map[string]any `json:"symbols"`
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatalf("json-loc invalid: %v", err)
	}
	for _, s := range doc.Symbols {
		if s["qualified_name"] == absent {
			if _, ok := s["line"]; ok {
				t.Errorf("json-loc absent symbol still has a line field")
			}
		}
	}
	t.Logf("arms ok: %d nodes, loc omits 1, inline absent=5 present=8 fields, json-loc absent has no position", len(nodes))

	// Sample wire for eyeballing.
	locHead := strings.Join(strings.Split(loc, "\n")[:4], "\n")
	t.Logf("gcf-loc section (head):\n%s", locHead)
	for _, line := range strings.Split(inline, "\n") {
		f := strings.Fields(line)
		if strings.HasPrefix(line, "@") && !strings.Contains(line, "<@") && !strings.Contains(line, "#") && len(f) == 8 {
			t.Logf("gcf-inline node line: %s", line)
			break
		}
	}
}

func TestLocComprehension(t *testing.T) {
	if os.Getenv("EVAL_LOC") == "" {
		t.Skip("set EVAL_LOC=1 to run the graph loc-section comprehension probe")
	}

	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "codex"
	}
	callLLM, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}

	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	resultsDir := filepath.Join("results", "comprehension")
	os.MkdirAll(resultsDir, 0755)
	logPath := filepath.Join(resultsDir, fmt.Sprintf("loc-probe-%s-%s-%s.log",
		backendName, model, time.Now().Format("2006-01-02-150405")))
	logFile, ferr := os.Create(logPath)
	if ferr != nil {
		t.Fatalf("create log: %v", ferr)
	}
	defer logFile.Close()
	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		logFile.WriteString(line + "\n")
		logFile.Sync()
	}
	logf("Log: %s", logPath)

	fixture := buildFixture(500, 200)
	gcfOut := gcf.Encode(fixture)
	nodes := parseNodeLines(gcfOut)
	if len(nodes) != len(fixture.Symbols) {
		t.Fatalf("parsed %d node lines, expected %d", len(nodes), len(fixture.Symbols))
	}

	// Probe symbols (all qnames are unique in this fixture).
	locTarget := fixture.Symbols[137].QualifiedName // symbol_location, edit_target
	editTarget := fixture.Symbols[311].QualifiedName
	callee := fixture.Symbols[0].QualifiedName // edges[0]: Symbols[1] calls Symbols[0]
	caller := fixture.Symbols[1].QualifiedName
	absent := fixture.Symbols[42].QualifiedName // omitted from every arm

	locSection := buildLocSection(nodes, absent)

	arms := []struct{ name, label, content string }{
		{"json-loc", "JSON", jsonWithPos(fixture, absent)},
		{"gcf-loc", "GCF", gcfOut + "\n" + locSection},
		{"gcf-inline", "GCF", inlineLoc(gcfOut, absent)},
	}

	lf, ll, _ := posFor(locTarget)
	ef, el, _ := posFor(editTarget)
	cf, cl, _ := posFor(caller)

	type qn struct {
		name, prompt string
		verify       func(resp string) (bool, string)
	}
	questions := []qn{
		{
			"symbol_location",
			fmt.Sprintf("What source file and line is the symbol %q defined at? Reply as file:line, nothing else.", locTarget),
			func(r string) (bool, string) { return containsFileLine(lf, ll, r) },
		},
		{
			"caller_location",
			fmt.Sprintf("According to the relationships in this context, one symbol calls %q. At what source file and line is that calling symbol defined? Reply as file:line, nothing else.", callee),
			func(r string) (bool, string) { return containsFileLine(cf, cl, r) },
		},
		{
			"edit_target",
			fmt.Sprintf("You need to modify the behavior of the symbol %q. What source file and line should you edit? Reply as file:line, nothing else.", editTarget),
			func(r string) (bool, string) { return containsFileLine(ef, el, r) },
		},
		{
			"absent_location",
			fmt.Sprintf("What source file and line is the symbol %q defined at? If the location is not given in the context, reply exactly \"unknown\". Reply with file:line or unknown, nothing else.", absent),
			func(r string) (bool, string) {
				low := strings.ToLower(strings.TrimSpace(r))
				af, _, _ := posFor(absent)
				if strings.Contains(low, "unknown") || strings.Contains(low, "not given") || strings.Contains(low, "not provided") || strings.Contains(low, "no location") {
					return true, "declined"
				}
				if strings.Contains(r, af) {
					return false, "hallucinated file"
				}
				return false, "did not decline"
			},
		},
	}

	logf("Backend: %s", backendLabel)
	logf("Fixture: %d symbols, %d edges | absent probe: %s", len(fixture.Symbols), len(fixture.Edges), absent)
	for _, a := range arms {
		logf("  %-11s tokens (est): %d", a.name, len(a.content)/4)
	}
	logf("")

	type res struct{ correct, total int }
	scores := map[string]*res{}
	for _, a := range arms {
		scores[a.name] = &res{}
	}

	type ev struct {
		q, arm, detail, got string
		ok                  bool
		err                 error
	}
	for _, q := range questions {
		ch := make(chan ev, len(arms))
		for _, a := range arms {
			go func(a struct{ name, label, content string }) {
				prompt := fmt.Sprintf("Here is a code context payload in %s format:\n\n%s\n\nQuestion: %s",
					a.label, a.content, q.prompt)
				resp, err := callLLM(prompt)
				if err != nil {
					ch <- ev{q: q.name, arm: a.name, err: err}
					return
				}
				ok, detail := q.verify(resp)
				ch <- ev{q: q.name, arm: a.name, ok: ok, detail: detail, got: strings.TrimSpace(resp)}
			}(a)
		}
		for range arms {
			r := <-ch
			if r.err != nil {
				logf("  SKIP %-16s %-11s error: %v", r.q, r.arm, r.err)
				continue
			}
			scores[r.arm].total++
			mark := "FAIL"
			if r.ok {
				mark = "PASS"
				scores[r.arm].correct++
			}
			got := r.got
			if len(got) > 60 {
				got = got[:60]
			}
			logf("  %s %-16s %-11s [%s] got=%q", mark, r.q, r.arm, r.detail, got)
		}
	}

	logf("")
	logf("=== Loc probe summary ===")
	logf("%-11s %8s %10s", "Arm", "Accuracy", "Est Tokens")
	for _, a := range arms {
		s := scores[a.name]
		acc := 0.0
		if s.total > 0 {
			acc = 100.0 * float64(s.correct) / float64(s.total)
		}
		logf("%-11s %7.1f%% %10d", a.name, acc, len(a.content)/4)
	}
}
