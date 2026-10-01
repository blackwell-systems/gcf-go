package eval

// Column-ordering comprehension eval (gated EVAL_COLORDER). Novel optimization: in a GCF
// generic tabular array the field order is declared ONCE in the header and every row is
// positional, so reading a value means mapping a column position back to the header by
// counting. Hypothesis: a weak model counts less reliably the farther the queried column
// sits from the anchor (the id in column 1), so RELOCATING a frequently-queried column to
// sit adjacent to the anchor should improve retrieval at no token cost (same bytes, just a
// permuted header + rows). This is a pure presentation change, lossless, decoder-agnostic.
//
// A/B: one field ("tier") is moved from the LAST column (baseline, far from id) to the
// SECOND column (salient_first, adjacent to id). Everything else held constant. Questions
// target tier (the relocated field, should improve), plus a control field that does not move
// much (name) and a far field that stays far in both (score), to isolate the tier effect.
//
//   GOWORK=off EVAL_COLORDER=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//     OPENAI_API_KEY=... EVAL_MODEL=... EVAL_TEMPERATURE=0.2 \
//     EVAL_FORMATS=baseline,salient_first,json go test -run TestColumnOrdering -v -timeout 40m

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var coDepts = []string{"Sales", "Engineering", "Operations", "Support", "Finance"}
var coStatuses = []string{"active", "pending", "suspended", "closed", "trial"}
var coRegions = []string{"us-east", "us-west", "eu-central", "apac", "latam"}
var coTiers = []string{"bronze", "silver", "gold", "platinum", "diamond"}

func coTierOf(i int) string  { return coTiers[i%len(coTiers)] }
func coScoreOf(i int) int    { return (i*7)%100 + 1 }
func coDeptOf(i int) string  { return coDepts[i%len(coDepts)] }
func coStatOf(i int) string  { return coStatuses[i%len(coStatuses)] }
func coRegOf(i int) string   { return coRegions[i%len(coRegions)] }
func coLevelOf(i int) int    { return (i % 5) + 1 }

func buildColOrderFixture(n int) []map[string]any {
	arr := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		arr[i] = map[string]any{
			"id":       fmt.Sprintf("u%04d", i+1),
			"name":     fmt.Sprintf("Member %04d", i+1),
			"dept":     coDeptOf(i),
			"level":    coLevelOf(i),
			"status":   coStatOf(i),
			"region":   coRegOf(i),
			"email":    fmt.Sprintf("member%04d@example.com", i+1),
			"joined":   fmt.Sprintf("2024-%02d-%02d", (i%12)+1, (i%27)+1),
			"lastSeen": fmt.Sprintf("2026-%02d-%02d", (i%9)+1, (i%27)+1),
			"plan":     coPlanOf(i),
			"score":    coScoreOf(i),
			"tier":     coTierOf(i),
		}
	}
	return arr
}

var coPlans = []string{"monthly", "annual", "quarterly", "lifetime", "trial"}

func coPlanOf(i int) string { return coPlans[i%len(coPlans)] }

// field orders over 12 columns. baseline = tier LAST (col 12, far from id anchor);
// salient_first = tier at col 2 (adjacent). score stays far in both (distance control).
var coBaselineOrder = []string{"id", "name", "dept", "level", "status", "region", "email", "joined", "lastSeen", "plan", "score", "tier"}
var coSalientOrder = []string{"id", "tier", "name", "dept", "level", "status", "region", "email", "joined", "lastSeen", "plan", "score"}

func encodeColOrder(arr []map[string]any, format string) (string, error) {
	emit := func(order []string) string {
		var b strings.Builder
		b.WriteString("GCF profile=generic\n")
		fmt.Fprintf(&b, "## members [%d]{%s}\n", len(arr), strings.Join(order, ","))
		for _, rec := range arr {
			cells := make([]string, len(order))
			for j, f := range order {
				cells[j] = fmt.Sprintf("%v", rec[f])
			}
			b.WriteString(strings.Join(cells, "|") + "\n")
		}
		return b.String()
	}
	switch format {
	case "baseline":
		return emit(coBaselineOrder), nil
	case "salient_first":
		return emit(coSalientOrder), nil
	case "json":
		bb, err := json.MarshalIndent(map[string]any{"members": arr}, "", "  ")
		return string(bb), err
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

func TestColumnOrdering(t *testing.T) {
	if os.Getenv("EVAL_COLORDER") == "" {
		t.Skip("set EVAL_COLORDER=1 to run")
	}
	n := 60
	if v := os.Getenv("EVAL_COLORDER_N"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			n = p
		}
	}
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "baseline,salient_first,json"
	}
	formatList := strings.Split(formatsEnv, ",")
	for i := range formatList {
		formatList[i] = strings.TrimSpace(formatList[i])
	}

	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "api"
	}
	callLLM, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}

	arr := buildColOrderFixture(n)
	id := func(i int) string { return fmt.Sprintf("u%04d", i+1) }

	type q struct {
		name, question, expected, kind string
		verify                         func(string, string) (bool, string)
	}
	questions := []q{
		// the relocated field (tier): far in baseline, adjacent to anchor in salient_first.
		{"tier_early", "What is the tier of member " + id(0) + "? Reply with ONLY the value.", coTierOf(0), "t", stringVerify},
		{"tier_mid", "What is the tier of member " + id(n/2) + "? Reply with ONLY the value.", coTierOf(n / 2), "t", stringVerify},
		{"tier_deep", "What is the tier of member " + id(n-1) + "? Reply with ONLY the value.", coTierOf(n - 1), "t", stringVerify},
		// control field that barely moves (name: col 2 baseline, col 3 salient).
		{"name_ctrl", "What is the name of member " + id(n/2) + "? Reply with ONLY the value.", fmt.Sprintf("Member %04d", n/2+1), "n", stringVerify},
		// far field in BOTH orders (score: col 7 baseline, col 8 salient) - distance control.
		{"score_ctrl", "What is the score of member " + id(n/2) + "? Reply with ONLY a number.", fmt.Sprintf("%d", coScoreOf(n/2)), "s", numericVerify},
		// trivial control
		{"count", "How many members are in this data? Reply with ONLY a number.", fmt.Sprintf("%d", n), "x", numericVerify},
	}

	type fdat struct{ name, content string }
	var formats []fdat
	for _, f := range formatList {
		s, err := encodeColOrder(arr, f)
		if err != nil {
			t.Fatalf("encode %s: %v", f, err)
		}
		formats = append(formats, fdat{f, s})
	}

	resultsDir := filepath.Join("results", "comprehension")
	os.MkdirAll(resultsDir, 0755)
	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	safeModel := strings.ReplaceAll(model, "/", "_")
	logPath := filepath.Join(resultsDir, fmt.Sprintf("colorder-%dmembers-%s-%s-%s.log",
		n, backendName, safeModel, time.Now().Format("2006-01-02-150405")))
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer logFile.Close()
	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		logFile.WriteString(line + "\n")
		logFile.Sync()
	}

	logf("Column-Ordering Comprehension Eval")
	logf("Backend: %s", backendLabel)
	logf("Members: %d, Questions: %d, Formats: %s", n, len(questions), strings.Join(formatList, ", "))
	logf("baseline order:      %s", strings.Join(coBaselineOrder, ","))
	logf("salient_first order: %s", strings.Join(coSalientOrder, ","))
	for _, f := range formats {
		logf("%-14s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	logf("")

	type res struct{ correct, total, tierCorrect, tierTotal int }
	results := map[string]*res{}
	for _, f := range formats {
		results[f.name] = &res{}
	}
	for _, qq := range questions {
		for _, f := range formats {
			prompt := fmt.Sprintf("Here is member data in %s format:\n\n%s\n\nQuestion: %s",
				strings.ToUpper(f.name), f.content, qq.question)
			resp, err := callLLM(prompt)
			if err != nil {
				logf("  SKIP %-14s %-14s error: %v", qq.name, f.name, err)
				continue
			}
			ok, detail := qq.verify(qq.expected, resp)
			r := results[f.name]
			r.total++
			if ok {
				r.correct++
			}
			if qq.kind == "t" {
				r.tierTotal++
				if ok {
					r.tierCorrect++
				}
			}
			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-14s %-14s [%s] expected=%q got=%q",
				mark, qq.name, f.name, detail, qq.expected, strings.TrimSpace(resp))
		}
	}
	logf("")
	logf("=== Summary ===")
	for _, f := range formats {
		r := results[f.name]
		acc := 0.0
		if r.total > 0 {
			acc = 100 * float64(r.correct) / float64(r.total)
		}
		logf("%-14s %.1f%% %d/%d  (relocated-field tier %d/%d)", f.name, acc, r.correct, r.total, r.tierCorrect, r.tierTotal)
	}
	logf("Log: %s", logPath)
}
