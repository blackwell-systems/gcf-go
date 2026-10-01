package eval

// Near-constant-column WITH EXCEPTIONS eval (gated EVAL_NEARCONST). The constant-column study
// proved a FULLY constant column factors safely. This tests the real "near-constant" case: a
// column that is mostly one value with a few per-row overrides. Factored form declares the
// default once plus a sparse exceptions list; rows drop the column. The risk: an exception is a
// partial override (default unless listed), which is a mini reference lookup, so a model might
// blindly return the default for an override row. The override question is the discriminator.
//
//   GOWORK=off EVAL_NEARCONST=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//     OPENAI_API_KEY=... EVAL_MODEL=... EVAL_TEMPERATURE=0.2 \
//     EVAL_FORMATS=repeated,factored,json go test -run TestNearConstantColumn -v -timeout 40m

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
)

const ncDefault = "us-east"

var ncOverrides = []string{"eu-west", "ap-south"}

// regionFor returns (value, isException) for row index i (0-based). Every 10th row overrides.
func regionFor(i int) (string, bool) {
	if (i+1)%10 == 0 {
		excIdx := (i+1)/10 - 1
		return ncOverrides[excIdx%len(ncOverrides)], true
	}
	return ncDefault, false
}

func buildNearConstFixture(n int) []any {
	depts := []string{"Sales", "Engineering", "Operations", "Support", "Finance"}
	arr := make([]any, n)
	for i := 0; i < n; i++ {
		region, _ := regionFor(i)
		arr[i] = map[string]any{
			"id":     fmt.Sprintf("u%04d", i+1),
			"name":   fmt.Sprintf("Member %04d", i+1),
			"dept":   depts[i%len(depts)],
			"level":  (i % 5) + 1,
			"region": region,
		}
	}
	return arr
}

func encodeNearConst(arr []any, format string) (string, error) {
	switch format {
	case "repeated":
		return gcf.EncodeGeneric(map[string]any{"members": arr}), nil
	case "factored":
		stripped := make([]any, len(arr))
		var exc []string
		for i, item := range arr {
			rec := item.(map[string]any)
			row := map[string]any{}
			for k, v := range rec {
				if k == "region" {
					continue
				}
				row[k] = v
			}
			stripped[i] = row
			if _, isExc := regionFor(i); isExc {
				exc = append(exc, fmt.Sprintf("%s=%s", rec["id"], rec["region"]))
			}
		}
		s := gcf.EncodeGeneric(map[string]any{"members": stripped})
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "## members ") {
				decl := fmt.Sprintf("region=%s (default; exceptions: %s)", ncDefault, strings.Join(exc, ", "))
				lines = append(lines[:i+1], append([]string{decl}, lines[i+1:]...)...)
				break
			}
		}
		return strings.Join(lines, "\n"), nil
	case "json":
		b, err := json.MarshalIndent(map[string]any{"members": arr}, "", "  ")
		return string(b), err
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

func TestNearConstantColumn(t *testing.T) {
	if os.Getenv("EVAL_NEARCONST") == "" {
		t.Skip("set EVAL_NEARCONST=1 to run")
	}
	n := 60
	if v := os.Getenv("EVAL_NC_N"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			n = p
		}
	}
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "repeated,factored,json"
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

	arr := buildNearConstFixture(n)
	id := func(i int) string { return fmt.Sprintf("u%04d", i+1) }

	type q struct {
		name, question, expected, kind string
		verify                         func(string, string) (bool, string)
	}
	// defaults: rows not divisible by 10; exceptions: rows divisible by 10
	questions := []q{
		{"region_default_a", "What is the region of member " + id(4) + "? Reply with ONLY the value.", mustRegion(4), "d", stringVerify},
		{"region_default_b", "What is the region of member " + id(n-2) + "? Reply with ONLY the value.", mustRegion(n - 2), "d", stringVerify},
		{"region_EXC_a", "What is the region of member " + id(9) + "? Reply with ONLY the value.", mustRegion(9), "e", stringVerify},  // u0010
		{"region_EXC_b", "What is the region of member " + id(19) + "? Reply with ONLY the value.", mustRegion(19), "e", stringVerify}, // u0020
		{"region_EXC_c", "What is the region of member " + id(39) + "? Reply with ONLY the value.", mustRegion(39), "e", stringVerify}, // u0040
		{"count", "How many members are in this data? Reply with ONLY a number.", fmt.Sprintf("%d", n), "c", numericVerify},
	}

	type fdat struct{ name, content string }
	var formats []fdat
	for _, f := range formatList {
		s, err := encodeNearConst(arr, f)
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
	logPath := filepath.Join(resultsDir, fmt.Sprintf("nearconst-%dmembers-%s-%s-%s.log",
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

	logf("Near-Constant-Column (with exceptions) Comprehension Eval")
	logf("Backend: %s", backendLabel)
	logf("Members: %d, Questions: %d, Formats: %s", n, len(questions), strings.Join(formatList, ", "))
	for _, f := range formats {
		logf("%-10s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	logf("")

	type res struct{ correct, total, excCorrect, excTotal int }
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
				logf("  SKIP %-18s %-10s error: %v", qq.name, f.name, err)
				continue
			}
			ok, detail := qq.verify(qq.expected, resp)
			r := results[f.name]
			r.total++
			if ok {
				r.correct++
			}
			if qq.kind == "e" {
				r.excTotal++
				if ok {
					r.excCorrect++
				}
			}
			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-18s %-10s [%s] expected=%q got=%q",
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
		logf("%-10s %.1f%% %d/%d  (exceptions %d/%d)", f.name, acc, r.correct, r.total, r.excCorrect, r.excTotal)
	}
	logf("Log: %s", logPath)
}

func mustRegion(i int) string {
	v, _ := regionFor(i)
	return v
}
