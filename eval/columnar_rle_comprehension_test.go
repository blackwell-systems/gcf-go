package eval

// Columnar RLE / value-grouping comprehension eval (gated EVAL_RLE). Roadmap item
// "value-grouping for low-cardinality columns": group rows under a low-card column, emit the
// value once as a group subheader, list members bare. Token-measured before; comprehension
// untested. Risk: grouping is a hierarchy the model must track upward (which group is a row
// under?), a cousin of reference indirection. The "dept of member X" question is the
// discriminator: grouped form requires scanning to the member's group header.
//
//   GOWORK=off EVAL_RLE=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//     OPENAI_API_KEY=... EVAL_MODEL=... EVAL_TEMPERATURE=0.2 \
//     EVAL_FORMATS=flat,grouped,json go test -run TestColumnarRLE -v -timeout 40m

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

var rleDepts = []string{"Sales", "Engineering", "Operations", "Support", "Finance"}

func rleDeptOf(i int) string { return rleDepts[i%len(rleDepts)] }

func buildRLEFixture(n int) []any {
	arr := make([]any, n)
	for i := 0; i < n; i++ {
		arr[i] = map[string]any{
			"id":    fmt.Sprintf("u%04d", i+1),
			"name":  fmt.Sprintf("Member %04d", i+1),
			"dept":  rleDeptOf(i),
			"level": (i % 5) + 1,
		}
	}
	return arr
}

func encodeRLE(arr []any, format string) (string, error) {
	switch format {
	case "flat":
		return gcf.EncodeGeneric(map[string]any{"members": arr}), nil
	case "grouped":
		// group by dept (in canonical dept order), emit dept once as a subheader, rows bare
		byDept := map[string][]map[string]any{}
		for _, item := range arr {
			rec := item.(map[string]any)
			d := rec["dept"].(string)
			byDept[d] = append(byDept[d], rec)
		}
		var b strings.Builder
		b.WriteString("GCF profile=generic\n")
		b.WriteString("## members (grouped by dept)\n")
		for _, d := range rleDepts {
			grp := byDept[d]
			if len(grp) == 0 {
				continue
			}
			fmt.Fprintf(&b, "dept=%s [%d]{id,name,level}\n", d, len(grp))
			for _, rec := range grp {
				fmt.Fprintf(&b, "%s|%s|%d\n", rec["id"], rec["name"], rec["level"].(int))
			}
		}
		return b.String(), nil
	case "json":
		bb, err := json.MarshalIndent(map[string]any{"members": arr}, "", "  ")
		return string(bb), err
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

func TestColumnarRLE(t *testing.T) {
	if os.Getenv("EVAL_RLE") == "" {
		t.Skip("set EVAL_RLE=1 to run")
	}
	n := 60
	if v := os.Getenv("EVAL_RLE_N"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			n = p
		}
	}
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "flat,grouped,json"
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

	arr := buildRLEFixture(n)
	id := func(i int) string { return fmt.Sprintf("u%04d", i+1) }

	type q struct {
		name, question, expected, kind string
		verify                         func(string, string) (bool, string)
	}
	countDept := func(d string) string {
		c := 0
		for i := 0; i < n; i++ {
			if rleDeptOf(i) == d {
				c++
			}
		}
		return fmt.Sprintf("%d", c)
	}
	questions := []q{
		// discriminator: dept-of-member (grouped = scan up to group header)
		{"dept_first", "What is the dept of member " + id(2) + "? Reply with ONLY the value.", rleDeptOf(2), "g", stringVerify},
		{"dept_mid", "What is the dept of member " + id(n/2) + "? Reply with ONLY the value.", rleDeptOf(n / 2), "g", stringVerify},
		{"dept_deep", "What is the dept of member " + id(n-1) + "? Reply with ONLY the value.", rleDeptOf(n - 1), "g", stringVerify},
		// grouped should make group-count trivial (header), flat must tally
		{"count_eng", "How many members have dept 'Engineering'? Reply with ONLY a number.", countDept("Engineering"), "c", numericVerify},
		// controls
		{"level_mid", "What is the level of member " + id(n/2) + "? Reply with ONLY a number.", fmt.Sprintf("%d", (n/2)%5+1), "x", numericVerify},
		{"count", "How many members are in this data? Reply with ONLY a number.", fmt.Sprintf("%d", n), "x", numericVerify},
	}

	type fdat struct{ name, content string }
	var formats []fdat
	for _, f := range formatList {
		s, err := encodeRLE(arr, f)
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
	logPath := filepath.Join(resultsDir, fmt.Sprintf("rle-%dmembers-%s-%s-%s.log",
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

	logf("Columnar RLE (value-grouping) Comprehension Eval")
	logf("Backend: %s", backendLabel)
	logf("Members: %d, Questions: %d, Formats: %s", n, len(questions), strings.Join(formatList, ", "))
	for _, f := range formats {
		logf("%-10s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	logf("")

	type res struct{ correct, total, grpCorrect, grpTotal int }
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
				logf("  SKIP %-14s %-8s error: %v", qq.name, f.name, err)
				continue
			}
			ok, detail := qq.verify(qq.expected, resp)
			r := results[f.name]
			r.total++
			if ok {
				r.correct++
			}
			if qq.kind == "g" {
				r.grpTotal++
				if ok {
					r.grpCorrect++
				}
			}
			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-14s %-8s [%s] expected=%q got=%q",
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
		logf("%-10s %.1f%% %d/%d  (dept-of-member %d/%d)", f.name, acc, r.correct, r.total, r.grpCorrect, r.grpTotal)
	}
	logf("Log: %s", logPath)
}
