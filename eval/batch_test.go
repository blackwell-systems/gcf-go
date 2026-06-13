package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestGenerateBatch creates an OpenAI Batch API JSONL file for the comprehension eval.
// Usage:
//   EVAL_MODEL=gpt-4o EVAL_NUM_ORDERS=500 EVAL_FORMATS=gcf,json,toon GOWORK=off go test -run TestGenerateBatch -v
//
// Then submit:
//   export OPENAI_API_KEY=...
//   FILE_ID=$(curl -s https://api.openai.com/v1/files -H "Authorization: Bearer $OPENAI_API_KEY" -F purpose=batch -F file=@batch_gpt-4o_500.jsonl | jq -r .id)
//   curl -s https://api.openai.com/v1/batches -H "Authorization: Bearer $OPENAI_API_KEY" -H "Content-Type: application/json" -d "{\"input_file_id\":\"$FILE_ID\",\"endpoint\":\"/v1/chat/completions\",\"completion_window\":\"24h\"}" | jq .
//
// Check status:
//   curl -s https://api.openai.com/v1/batches/BATCH_ID -H "Authorization: Bearer $OPENAI_API_KEY" | jq .status
//
// Download results:
//   curl -s https://api.openai.com/v1/files/OUTPUT_FILE_ID/content -H "Authorization: Bearer $OPENAI_API_KEY" > batch_results.jsonl
func TestGenerateBatch(t *testing.T) {
	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	numOrders := 500
	if n := os.Getenv("EVAL_NUM_ORDERS"); n != "" {
		if parsed, err := fmt.Sscanf(n, "%d", &numOrders); err != nil || parsed != 1 {
			numOrders = 500
		}
	}

	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "gcf,json,toon"
	}
	formatList := strings.Split(formatsEnv, ",")
	for i := range formatList {
		formatList[i] = strings.TrimSpace(formatList[i])
	}

	// Generate fixture (same as real eval).
	orders := buildGenericFixture(numOrders)
	questions := buildGenericQuestions(numOrders)

	// Encode in all formats.
	encoded := map[string]string{}
	for _, f := range formatList {
		content, err := encodeGenericOrders(orders, f)
		if err != nil {
			t.Fatalf("encode %s: %v", f, err)
		}
		encoded[f] = content
		t.Logf("%-8s %6d bytes, ~%d tokens", f, len(content), len(content)/4)
	}

	// Generate JSONL.
	outName := fmt.Sprintf("batch_%s_%d.jsonl", model, numOrders)
	outFile, err := os.Create(outName)
	if err != nil {
		t.Fatalf("create %s: %v", outName, err)
	}
	defer outFile.Close()

	count := 0
	for _, q := range questions {
		for _, f := range formatList {
			content := encoded[f]
			prompt := fmt.Sprintf("Here is order data in %s format:\n\n%s\n\nQuestion: %s",
				strings.ToUpper(f), content, q.Question)

			tokenKey := "max_tokens"
			if strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "o") {
				tokenKey = "max_completion_tokens"
			}

			request := map[string]any{
				"custom_id": fmt.Sprintf("%s_%s", q.Name, f),
				"method":    "POST",
				"url":       "/v1/chat/completions",
				"body": map[string]any{
					"model":  model,
					tokenKey: 200,
					"messages": []map[string]string{
						{"role": "user", "content": prompt},
					},
				},
			}
			line, _ := json.Marshal(request)
			outFile.Write(line)
			outFile.WriteString("\n")
			count++
		}
	}

	t.Logf("\nGenerated %s (%d requests)", outName, count)
	t.Logf("\nSubmit with:")
	t.Logf("  export OPENAI_API_KEY=sk-...")
	t.Logf("  FILE_ID=$(curl -s https://api.openai.com/v1/files -H \"Authorization: Bearer $OPENAI_API_KEY\" -F purpose=batch -F file=@%s | jq -r .id)", outName)
	t.Logf("  curl -s https://api.openai.com/v1/batches -H \"Authorization: Bearer $OPENAI_API_KEY\" -H \"Content-Type: application/json\" -d \"{\\\"input_file_id\\\":\\\"$FILE_ID\\\",\\\"endpoint\\\":\\\"/v1/chat/completions\\\",\\\"completion_window\\\":\\\"24h\\\"}\" | jq .")
}

// TestDumpFixture writes the 500-order fixture as JSON for use in token benchmarks.
func TestDumpFixture(t *testing.T) {
	numOrders := 500
	if n := os.Getenv("EVAL_NUM_ORDERS"); n != "" {
		if parsed, err := fmt.Sscanf(n, "%d", &numOrders); err != nil || parsed != 1 {
			numOrders = 500
		}
	}
	orders := buildGenericFixture(numOrders)
	wrapper := map[string]any{"orders": ordersToAny(orders)}
	b, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	outPath := fmt.Sprintf("fixture_%d_orders.json", numOrders)
	if err := os.WriteFile(outPath, b, 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("Written %s (%d bytes)", outPath, len(b))
}
