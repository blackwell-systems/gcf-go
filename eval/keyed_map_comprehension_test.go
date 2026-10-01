package eval

// Keyed-map comprehension eval (gated EVAL_KEYEDMAP). Closes the Appendix B gap in
// gcf/ENCODE-AUTO-DESIGN.md. GCF's keyed-map body is byte-identical to the tabular body;
// only the header marker differs ([N:]{key,...} vs name [N]{id,...}). This measures
// whether the keyed header reads as accurately as the tabular one, across model tiers.
// Mirrors TestGenericComprehension: same backend, same scoring, logs to results/comprehension/.
//
//   GOWORK=off EVAL_KEYEDMAP=1 EVAL_BACKEND=openai OPENAI_BASE_URL=https://openrouter.ai/api/v1 \
//     OPENAI_API_KEY=... EVAL_MODEL=google/gemini-2.5-flash EVAL_TEMPERATURE=0.2 \
//     EVAL_FORMATS=keyed-map,generic,json go test -run TestKeyedMapComprehension -v -timeout 40m

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

func buildKeyedMapFixture(n int) ([]string, map[string]any) {
	depts := []string{"Sales", "Engineering", "Operations", "Support", "Finance"}
	regions := []string{"us-east", "us-west", "eu-central", "ap-south"}
	ids := make([]string, n)
	m := map[string]any{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("u%04d", i+1)
		ids[i] = id
		m[id] = map[string]any{
			"name":   fmt.Sprintf("Member %04d", i+1),
			"dept":   depts[i%len(depts)],
			"level":  (i % 5) + 1,
			"active": i%2 == 0,
			"region": regions[i%len(regions)],
		}
	}
	return ids, m
}

func encodeKeyedMap(ids []string, m map[string]any, format string) (string, error) {
	switch format {
	case "keyed-map":
		return gcf.EncodeGeneric(m), nil
	case "generic":
		arr := make([]any, len(ids))
		for i, id := range ids {
			rec := m[id].(map[string]any)
			row := map[string]any{"id": id}
			for k, v := range rec {
				row[k] = v
			}
			arr[i] = row
		}
		return gcf.EncodeGeneric(map[string]any{"members": arr}), nil
	case "json":
		b, err := json.MarshalIndent(m, "", "  ")
		return string(b), err
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

type kmQ struct {
	name     string
	question string
	expected func(ids []string, m map[string]any) string
	verify   func(expected, resp string) (bool, string)
}

func buildKeyedMapQuestions(ids []string, m map[string]any) []kmQ {
	field := func(id, f string) string { return fmt.Sprintf("%v", m[id].(map[string]any)[f]) }
	mid := ids[len(ids)/2]
	deep := ids[len(ids)-1]
	countDept := func(d string) string {
		c := 0
		for _, id := range ids {
			if m[id].(map[string]any)["dept"] == d {
				c++
			}
		}
		return fmt.Sprintf("%d", c)
	}
	return []kmQ{
		{"count", "How many members are in this data? Reply with ONLY a number.",
			func(ids []string, m map[string]any) string { return fmt.Sprintf("%d", len(ids)) }, numericVerify},
		{"dept_first", "What is the dept of member " + ids[0] + "? Reply with ONLY the value.",
			func(ids []string, m map[string]any) string { return field(ids[0], "dept") }, stringVerify},
		{"level_mid", "What is the level of member " + mid + "? Reply with ONLY a number.",
			func(ids []string, m map[string]any) string { return field(mid, "level") }, numericVerify},
		{"region_deep", "What is the region of member " + deep + "? Reply with ONLY the value.",
			func(ids []string, m map[string]any) string { return field(deep, "region") }, stringVerify},
		{"name_mid", "What is the name of member " + mid + "? Reply with ONLY the value.",
			func(ids []string, m map[string]any) string { return field(mid, "name") }, stringVerify},
		{"count_eng", "How many members have dept 'Engineering'? Reply with ONLY a number.",
			func(ids []string, m map[string]any) string { return countDept("Engineering") }, numericVerify},
		{"count_active", "How many members are active? Reply with ONLY a number.",
			func(ids []string, m map[string]any) string {
				c := 0
				for _, id := range ids {
					if m[id].(map[string]any)["active"] == true {
						c++
					}
				}
				return fmt.Sprintf("%d", c)
			}, numericVerify},
		{"level_deep", "What is the level of member " + deep + "? Reply with ONLY a number.",
			func(ids []string, m map[string]any) string { return field(deep, "level") }, numericVerify},
	}
}

func TestKeyedMapComprehension(t *testing.T) {
	if os.Getenv("EVAL_KEYEDMAP") == "" {
		t.Skip("set EVAL_KEYEDMAP=1 to run")
	}
	n := 60
	if v := os.Getenv("EVAL_KM_N"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "keyed-map,generic,json"
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

	ids, m := buildKeyedMapFixture(n)
	questions := buildKeyedMapQuestions(ids, m)

	type fd struct{ name, content string }
	var formats []fd
	for _, f := range formatList {
		s, err := encodeKeyedMap(ids, m, f)
		if err != nil {
			t.Fatalf("encode %s: %v", f, err)
		}
		formats = append(formats, fd{f, s})
	}

	resultsDir := filepath.Join("results", "comprehension")
	os.MkdirAll(resultsDir, 0755)
	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	safeModel := strings.ReplaceAll(model, "/", "_")
	logPath := filepath.Join(resultsDir, fmt.Sprintf("keyedmap-%dmembers-%s-%s-%s.log",
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

	logf("Keyed-Map Comprehension Eval")
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

	for _, q := range questions {
		expected := q.expected(ids, m)
		for _, f := range formats {
			prompt := fmt.Sprintf("Here is member data in %s format:\n\n%s\n\nQuestion: %s",
				strings.ToUpper(f.name), f.content, q.question)
			resp, err := callLLM(prompt)
			if err != nil {
				logf("  SKIP %-20s %-10s error: %v", q.name, f.name, err)
				continue
			}
			ok, detail := q.verify(expected, resp)
			results[f.name].total++
			if ok {
				results[f.name].correct++
			}
			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-20s %-10s [%s] expected=%q got=%q",
				mark, q.name, f.name, detail, expected, strings.TrimSpace(resp))
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
