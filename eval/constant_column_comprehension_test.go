package eval

// Near-constant-column safe-factoring eval (gated EVAL_CONSTCOL). Tests #3(b) from the
// EncodeAuto work: a column that holds one value across every row can be factored to a single
// declaration (lossless). Unlike dictionary/affix techniques (reference indirection), this is
// a single global fact, so it should be comprehension-safe. Question: does a model read the
// constant column as accurately when it is stated once (factored) as when repeated per row?
//
//   GOWORK=off EVAL_CONSTCOL=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//     OPENAI_API_KEY=... EVAL_MODEL=... EVAL_TEMPERATURE=0.2 \
//     EVAL_FORMATS=repeated,factored,json go test -run TestConstantColumnComprehension -v -timeout 40m

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

const constColValue = "us-east"

func buildConstColFixture(n int) []any {
	depts := []string{"Sales", "Engineering", "Operations", "Support", "Finance"}
	arr := make([]any, n)
	for i := 0; i < n; i++ {
		arr[i] = map[string]any{
			"id":     fmt.Sprintf("u%04d", i+1),
			"name":   fmt.Sprintf("Member %04d", i+1),
			"dept":   depts[i%len(depts)],
			"level":  (i % 5) + 1,
			"region": constColValue, // constant across every row
		}
	}
	return arr
}

func encodeConstCol(arr []any, format string) (string, error) {
	switch format {
	case "repeated":
		// standard GCF tabular: region column repeated on every row
		return gcf.EncodeGeneric(map[string]any{"members": arr}), nil
	case "factored":
		// region removed from rows, declared once after the header line
		stripped := make([]any, len(arr))
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
		}
		s := gcf.EncodeGeneric(map[string]any{"members": stripped})
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "## members ") {
				decl := fmt.Sprintf("region=%s (constant, applies to all members)", constColValue)
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

func TestConstantColumnComprehension(t *testing.T) {
	if os.Getenv("EVAL_CONSTCOL") == "" {
		t.Skip("set EVAL_CONSTCOL=1 to run")
	}
	n := 60
	if v := os.Getenv("EVAL_CC_N"); v != "" {
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

	arr := buildConstColFixture(n)
	ids := make([]string, n)
	for i, item := range arr {
		ids[i] = item.(map[string]any)["id"].(string)
	}
	mid := ids[n/2]
	deep := ids[n-1]
	deptOf := func(id string) string {
		for _, item := range arr {
			rec := item.(map[string]any)
			if rec["id"] == id {
				return rec["dept"].(string)
			}
		}
		return ""
	}

	type q struct {
		name     string
		question string
		expected string
		verify   func(string, string) (bool, string)
	}
	questions := []q{
		// the discriminators: the constant column, read where it is only stated once in factored
		{"region_mid", "What is the region of member " + mid + "? Reply with ONLY the value.", constColValue, stringVerify},
		{"region_deep", "What is the region of member " + deep + "? Reply with ONLY the value.", constColValue, stringVerify},
		{"region_first", "What is the region of member " + ids[0] + "? Reply with ONLY the value.", constColValue, stringVerify},
		// controls
		{"count", "How many members are in this data? Reply with ONLY a number.", fmt.Sprintf("%d", n), numericVerify},
		{"dept_mid", "What is the dept of member " + mid + "? Reply with ONLY the value.", deptOf(mid), stringVerify},
		{"dept_deep", "What is the dept of member " + deep + "? Reply with ONLY the value.", deptOf(deep), stringVerify},
	}

	type fdat struct{ name, content string }
	var formats []fdat
	for _, f := range formatList {
		s, err := encodeConstCol(arr, f)
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
	logPath := filepath.Join(resultsDir, fmt.Sprintf("constcol-%dmembers-%s-%s-%s.log",
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

	logf("Constant-Column Factoring Comprehension Eval")
	logf("Backend: %s", backendLabel)
	logf("Members: %d, Questions: %d, Formats: %s", n, len(questions), strings.Join(formatList, ", "))
	for _, f := range formats {
		logf("%-10s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	logf("")

	type res struct{ correct, total int }
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
				logf("  SKIP %-20s %-10s error: %v", qq.name, f.name, err)
				continue
			}
			ok, detail := qq.verify(qq.expected, resp)
			results[f.name].total++
			if ok {
				results[f.name].correct++
			}
			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-20s %-10s [%s] expected=%q got=%q",
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
		logf("%-10s %.1f%% %d/%d", f.name, acc, r.correct, r.total)
	}
	logf("Log: %s", logPath)
}
