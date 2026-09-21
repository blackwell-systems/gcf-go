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
	"regexp"
	"strconv"
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

var posRe = regexp.MustCompile(`([A-Za-z0-9_./-]+\.go)[:\s]+([0-9]+)`)

// extractPos pulls the first "<file>.go<sep><line>" position out of a response,
// so an answer that echoes payload syntax (e.g. "@0 internal/x/x.go 42 5") still
// yields its position for scoring instead of being counted a formatting failure.
func extractPos(resp string) (file string, line int, ok bool) {
	m := posRe.FindStringSubmatch(resp)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// classifyLoc buckets a location answer: "correct" (expected file+line),
// "wrong" (a position, but not the expected one), or "none" (no position found).
func classifyLoc(expFile string, expLine int, resp string) string {
	f, l, ok := extractPos(resp)
	if !ok {
		return "none"
	}
	if f == expFile && l == expLine {
		return "correct"
	}
	return "wrong"
}

// classifyAbsent buckets an answer for a symbol with no position in the payload:
// "declined" (correct), "hallucinated" (invented a position), or "none".
func classifyAbsent(resp string) string {
	if _, _, ok := extractPos(resp); ok {
		return "hallucinated"
	}
	low := strings.ToLower(resp)
	for _, kw := range []string{"unknown", "not given", "not provided", "no location", "not available", "n/a", "not specified", "cannot", "no position"} {
		if strings.Contains(low, kw) {
			return "declined"
		}
	}
	return "none"
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
	rawCall, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}
	// Retry any error (incl. transient TLS/network) so one flaky call does not
	// zero a whole cell; callOpenAI only retries 429/503 on its own.
	callLLM := func(prompt string) (string, error) {
		var lastErr error
		for attempt := 0; attempt < 4; attempt++ {
			resp, cerr := rawCall(prompt)
			if cerr == nil {
				return resp, nil
			}
			lastErr = cerr
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
		return "", lastErr
	}

	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	resultsDir := filepath.Join("results", "comprehension")
	os.MkdirAll(resultsDir, 0755)
	safeModel := strings.ReplaceAll(model, "/", "_")
	logPath := filepath.Join(resultsDir, fmt.Sprintf("loc-probe-%s-%s-%s.log",
		backendName, safeModel, time.Now().Format("2006-01-02-150405")))
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

	// Probe symbols (all qnames are unique in this fixture). Two single-hop
	// location lookups plus an edit-target lookup isolate loc retrieval. The
	// earlier edge-direction "caller_location" question was dropped: all arms
	// (including json-loc) failed it equally, so it measured edge-arrow
	// reasoning, not the loc layout under test.
	loc1 := fixture.Symbols[137].QualifiedName
	loc2 := fixture.Symbols[288].QualifiedName
	editTarget := fixture.Symbols[311].QualifiedName
	absent := fixture.Symbols[42].QualifiedName // omitted from every arm

	locSection := buildLocSection(nodes, absent)

	arms := []struct{ name, label, content string }{
		{"json-loc", "JSON", jsonWithPos(fixture, absent)},
		{"gcf-loc", "GCF", gcfOut + "\n" + locSection},
		{"gcf-inline", "GCF", inlineLoc(gcfOut, absent)},
	}

	f1, l1, _ := posFor(loc1)
	f2, l2, _ := posFor(loc2)
	ef, el, _ := posFor(editTarget)

	const ansFmt = " Reply with ONLY the source file path and line number, formatted exactly as path:line (for example internal/foo/foo.go:42). Do not copy identifiers, ids, or scores from the payload."

	type qn struct {
		name, prompt, passBucket string
		classify                 func(resp string) string
	}
	questions := []qn{
		{
			"symbol_location", fmt.Sprintf("What source file and line is the symbol %q defined at?", loc1) + ansFmt, "correct",
			func(r string) string { return classifyLoc(f1, l1, r) },
		},
		{
			"symbol_location2", fmt.Sprintf("What source file and line is the symbol %q defined at?", loc2) + ansFmt, "correct",
			func(r string) string { return classifyLoc(f2, l2, r) },
		},
		{
			"edit_target", fmt.Sprintf("You need to modify the behavior of the symbol %q. What source file and line should you edit?", editTarget) + ansFmt, "correct",
			func(r string) string { return classifyLoc(ef, el, r) },
		},
		{
			"absent_location", fmt.Sprintf("What source file and line is the symbol %q defined at? If its location is not given anywhere in the context, reply with exactly the word: unknown", absent), "declined",
			func(r string) string { return classifyAbsent(r) },
		},
	}

	logf("Backend: %s", backendLabel)
	logf("Fixture: %d symbols, %d edges | absent probe: %s", len(fixture.Symbols), len(fixture.Edges), absent)
	for _, a := range arms {
		logf("  %-11s tokens (est): %d", a.name, len(a.content)/4)
	}
	logf("")

	type tally struct{ correct, wrong, none, hallucinated, declined, total int }
	scores := map[string]*tally{}
	for _, a := range arms {
		scores[a.name] = &tally{}
	}

	type ev struct {
		q, arm, bucket, got string
		pass                bool
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
				bucket := q.classify(resp)
				ch <- ev{q: q.name, arm: a.name, bucket: bucket, pass: bucket == q.passBucket, got: strings.TrimSpace(resp)}
			}(a)
		}
		for range arms {
			r := <-ch
			if r.err != nil {
				logf("  SKIP %-16s %-11s error: %v", r.q, r.arm, r.err)
				continue
			}
			s := scores[r.arm]
			s.total++
			switch r.bucket {
			case "correct":
				s.correct++
			case "wrong":
				s.wrong++
			case "none":
				s.none++
			case "hallucinated":
				s.hallucinated++
			case "declined":
				s.declined++
			}
			mark := "FAIL"
			if r.pass {
				mark = "PASS"
			}
			got := r.got
			if len(got) > 60 {
				got = got[:60]
			}
			logf("  %s %-16s %-11s [%s] got=%q", mark, r.q, r.arm, r.bucket, got)
		}
	}

	logf("")
	logf("=== Loc probe summary (%s) ===", backendLabel)
	logf("%-11s %9s   %-11s  %s", "Arm", "Accuracy", "Est Tokens", "buckets correct/wrong/none/halluc/declined")
	for _, a := range arms {
		s := scores[a.name]
		pass := s.correct + s.declined
		acc := 0.0
		if s.total > 0 {
			acc = 100.0 * float64(pass) / float64(s.total)
		}
		logf("%-11s %8.1f%%   %-11d  %d/%d/%d/%d/%d", a.name, acc, len(a.content)/4,
			s.correct, s.wrong, s.none, s.hallucinated, s.declined)
	}
}
