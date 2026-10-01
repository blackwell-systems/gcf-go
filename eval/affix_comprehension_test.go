package eval

// Affix / template-factoring comprehension eval (gated EVAL_AFFIX). For a high-cardinality column
// whose values share a common prefix and/or suffix (emails, URLs, paths, SKUs), affix factoring
// declares the shared affix ONCE in the header and carries only the varying middle per row, e.g.
//
//   ## members [N]{id,name,email} email=affix("acct-","@team.example.com")
//   u0001|Member 0001|xqk30            -> email reconstructs to "acct-xqk30@team.example.com"
//
// This is a big lossless token win (the affix is written once, not N times) and beats whole-value
// interning (bpp) because the values are DISTINCT, so nothing is interned. The open question is
// comprehension: affix factoring is classed as reference/reconstruction indirection in
// ENCODE-AUTO-DESIGN.md (the model must reassemble prefix+middle+suffix), so it was gated
// frontier-only BY ANALOGY to keyed-map/dictionary, never directly measured. This harness measures
// it: flat (full value in the cell) vs affix (header affix + middle-only cell) vs json.
//
// The varying middle is a deterministic per-row token NOT derivable from the id, so the model must
// actually fetch the middle and wrap it; returning the bare middle is a FAIL (no reconstruction).
//
//   GOWORK=off EVAL_AFFIX=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//     OPENAI_API_KEY=... EVAL_MODEL=... EVAL_TEMPERATURE=0.2 \
//     EVAL_FORMATS=flat,affix,json go test -run TestAffix -v -timeout 40m

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

const affixPrefix = "acct-"
const affixSuffix = "@team.example.com"

var afDepts = []string{"Sales", "Engineering", "Operations", "Support", "Finance"}

func afDeptOf(i int) string { return afDepts[i%len(afDepts)] }
func afScoreOf(i int) int   { return (i*7)%100 + 1 }

// afMid returns a deterministic 5-char middle token that is NOT derivable from the id, so the
// model must read it from the row and reassemble the full value around the declared affix.
func afMid(i int) string {
	const cs = "bcdfghjklmnpqrstvwxz"
	h := (i*2654435761 + 1013904223) & 0x7fffffff
	return fmt.Sprintf("%c%c%c%02d", cs[h%20], cs[(h/20)%20], cs[(h/400)%20], i%100)
}

func afEmail(i int) string { return affixPrefix + afMid(i) + affixSuffix }

func buildAffixFixture(n int) []map[string]any {
	arr := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		arr[i] = map[string]any{
			"id":    fmt.Sprintf("u%04d", i+1),
			"name":  fmt.Sprintf("Member %04d", i+1),
			"dept":  afDeptOf(i),
			"email": afEmail(i),
			"score": afScoreOf(i),
		}
	}
	return arr
}

func encodeAffix(arr []map[string]any, format string) (string, error) {
	order := []string{"id", "name", "dept", "email", "score"}
	switch format {
	case "flat":
		var b strings.Builder
		b.WriteString("GCF profile=generic\n")
		fmt.Fprintf(&b, "## members [%d]{%s}\n", len(arr), strings.Join(order, ","))
		for _, rec := range arr {
			fmt.Fprintf(&b, "%s|%s|%s|%s|%d\n", rec["id"], rec["name"], rec["dept"], rec["email"], rec["score"].(int))
		}
		return b.String(), nil
	case "affix":
		var b strings.Builder
		b.WriteString("GCF profile=generic\n")
		// declare the affix once; the email cell carries only the varying middle, which
		// reconstructs as prefix + <cell> + suffix.
		fmt.Fprintf(&b, "## members [%d]{%s} email=affix(%q,%q)\n",
			len(arr), strings.Join(order, ","), affixPrefix, affixSuffix)
		for i, rec := range arr {
			fmt.Fprintf(&b, "%s|%s|%s|%s|%d\n", rec["id"], rec["name"], rec["dept"], afMid(i), rec["score"].(int))
		}
		return b.String(), nil
	case "json":
		bb, err := json.MarshalIndent(map[string]any{"members": arr}, "", "  ")
		return string(bb), err
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

func TestAffix(t *testing.T) {
	if os.Getenv("EVAL_AFFIX") == "" {
		t.Skip("set EVAL_AFFIX=1 to run")
	}
	n := 60
	if v := os.Getenv("EVAL_AFFIX_N"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			n = p
		}
	}
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "flat,affix,json"
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

	arr := buildAffixFixture(n)
	id := func(i int) string { return fmt.Sprintf("u%04d", i+1) }

	type q struct {
		name, question, expected, kind string
		verify                         func(string, string) (bool, string)
	}
	questions := []q{
		// the affix-factored field (email): model must reassemble prefix + middle + suffix.
		{"email_early", "What is the email of member " + id(0) + "? Reply with ONLY the value.", afEmail(0), "e", stringVerify},
		{"email_mid", "What is the email of member " + id(n/2) + "? Reply with ONLY the value.", afEmail(n / 2), "e", stringVerify},
		{"email_deep", "What is the email of member " + id(n-1) + "? Reply with ONLY the value.", afEmail(n - 1), "e", stringVerify},
		// control field (flat in all arms)
		{"name_ctrl", "What is the name of member " + id(n/2) + "? Reply with ONLY the value.", fmt.Sprintf("Member %04d", n/2+1), "n", stringVerify},
		// trivial control
		{"count", "How many members are in this data? Reply with ONLY a number.", fmt.Sprintf("%d", n), "x", numericVerify},
	}

	type fdat struct{ name, content string }
	var formats []fdat
	for _, f := range formatList {
		s, err := encodeAffix(arr, f)
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
	logPath := filepath.Join(resultsDir, fmt.Sprintf("affix-%dmembers-%s-%s-%s.log",
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

	logf("Affix / Template-Factoring Comprehension Eval")
	logf("Backend: %s", backendLabel)
	logf("Members: %d, Questions: %d, Formats: %s", n, len(questions), strings.Join(formatList, ", "))
	logf("affix: email = %q + <middle> + %q", affixPrefix, affixSuffix)
	for _, f := range formats {
		logf("%-8s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	logf("")

	type res struct{ correct, total, emailCorrect, emailTotal int }
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
				logf("  SKIP %-12s %-6s error: %v", qq.name, f.name, err)
				continue
			}
			ok, detail := qq.verify(qq.expected, resp)
			r := results[f.name]
			r.total++
			if ok {
				r.correct++
			}
			if qq.kind == "e" {
				r.emailTotal++
				if ok {
					r.emailCorrect++
				}
			}
			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-12s %-6s [%s] expected=%q got=%q",
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
		logf("%-8s %.1f%% %d/%d  (affix-field email %d/%d)", f.name, acc, r.correct, r.total, r.emailCorrect, r.emailTotal)
	}
	logf("Log: %s", logPath)
}
