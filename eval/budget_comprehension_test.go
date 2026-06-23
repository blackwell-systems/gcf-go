// Fixed-budget comprehension eval: holds token budget constant, varies data quantity.
//
// Instead of sending the same data in different formats and comparing tokens,
// this sends as much data as fits within a fixed token budget in each format
// and tests whether the model can answer questions about data that only fits
// in compact formats.
//
// Run:
//   GOWORK=off EVAL_BACKEND=codex go test -run TestBudgetComprehension -v -timeout 0
//
// Results written to eval/results/v3/comprehension/budget-experiment/
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
	toon "github.com/toon-format/toon-go"
)

// tokenEstimate returns a rough token count (chars/4 for English text).
// Good enough for budget gating; exact count isn't critical.
func tokenEstimate(s string) int {
	return len(s) / 4
}

type budgetQuestion struct {
	Name     string
	Question string
	// TargetIdx is the order index that holds the answer.
	// If the format can't fit this many orders, it should fail.
	TargetIdx int
	Expected  func(orders []Order) string
	Verify    func(expected, response string) (bool, string)
}

func TestBudgetComprehension(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "codex"
	}
	callLLM, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}

	resultsDir := filepath.Join("results", "v3", "comprehension", "budget-experiment")
	os.MkdirAll(resultsDir, 0755)

	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	logName := fmt.Sprintf("budget-%s-%s-%s.log",
		backendName, model, time.Now().Format("2006-01-02-150405"))
	logPath := filepath.Join(resultsDir, logName)
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

	// Generate a large dataset (500 orders).
	allOrders := buildGenericFixture(500)

	// Plant a specific "needle" at a known position.
	// Order at index 299 gets a uniquely high total.
	allOrders[299].Total = 99999.99
	allOrders[299].Customer.Name = "Needle McFindme"
	allOrders[299].Customer.Email = "needle@findme.com"
	allOrders[299].Customer.Tier = "diamond"
	allOrders[299].Status = "escalated"

	// Questions that require seeing the needle order.
	questions := []budgetQuestion{
		{
			Name:      "highest_total",
			Question:  "What is the highest order total in this data? Reply with ONLY the number.",
			TargetIdx: 299,
			Expected:  func(orders []Order) string { return "99999.99" },
			Verify:    numericVerify,
		},
		{
			Name:      "needle_customer",
			Question:  "What is the customer name on the order with the highest total? Reply with ONLY the name.",
			TargetIdx: 299,
			Expected:  func(orders []Order) string { return "Needle McFindme" },
			Verify:    stringVerify,
		},
		{
			Name:      "needle_status",
			Question:  "What is the status of the order with total 99999.99? Reply with ONLY the status.",
			TargetIdx: 299,
			Expected:  func(orders []Order) string { return "escalated" },
			Verify:    stringVerify,
		},
		{
			Name:      "needle_tier",
			Question:  "What is the customer tier on the order with customer email needle@findme.com? Reply with ONLY the tier.",
			TargetIdx: 299,
			Expected:  func(orders []Order) string { return "diamond" },
			Verify:    stringVerify,
		},
	}

	// Token budgets to test.
	budgets := []int{4000, 8000, 16000, 32000}

	// Formats to test.
	type formatEncoder struct {
		name   string
		encode func(orders []Order) string
	}
	formats := []formatEncoder{
		{"json", func(orders []Order) string {
			wrapper := map[string]any{"orders": ordersToAny(orders)}
			b, _ := json.Marshal(wrapper)
			return string(b)
		}},
		{"gcf", func(orders []Order) string {
			wrapper := map[string]any{"orders": ordersToAny(orders)}
			return gcf.EncodeGeneric(wrapper)
		}},
		{"gcf-flat", func(orders []Order) string {
			wrapper := map[string]any{"orders": ordersToAny(orders)}
			jsonBytes, _ := json.Marshal(wrapper)
			cmd := exec.Command("/opt/homebrew/bin/node", "--input-type=module", "-e", `
				import { encodeGenericFlat } from '/Users/dayna.blackwell/code/gcf/eval/encode-flat-prototype.mjs';
				let input = '';
				process.stdin.on('data', d => input += d);
				process.stdin.on('end', () => {
					const data = JSON.parse(input);
					process.stdout.write(encodeGenericFlat(data));
				});
			`)
			cmd.Stdin = bytes.NewReader(jsonBytes)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Run()
			return out.String()
		}},
		{"toon", func(orders []Order) string {
			wrapper := map[string]any{"orders": ordersToAny(orders)}
			s, _ := toon.MarshalString(wrapper)
			return s
		}},
	}

	logf("=== Fixed-Budget Comprehension Eval ===")
	logf("Backend: %s", backendLabel)
	logf("Total orders: 500, Needle at index: 299")
	logf("Budgets: %v tokens", budgets)
	logf("Formats: json, gcf, gcf-flat, toon")
	logf("")

	for _, budget := range budgets {
		logf("--- Budget: %d tokens ---", budget)

		for _, f := range formats {
			// Binary search for how many orders fit within the budget.
			lo, hi := 1, 500
			maxOrders := 0
			for lo <= hi {
				mid := (lo + hi) / 2
				encoded := f.encode(allOrders[:mid])
				tokens := tokenEstimate(encoded)
				if tokens <= budget {
					maxOrders = mid
					lo = mid + 1
				} else {
					hi = mid - 1
				}
			}

			// Check if the needle (index 299) is within range.
			needleVisible := maxOrders > 299

			encoded := ""
			if maxOrders > 0 {
				ordersToSend := int(math.Min(float64(maxOrders), 500))
				encoded = f.encode(allOrders[:ordersToSend])
			}

			logf("  %-10s fits %d orders (%d tokens), needle visible: %v",
				f.name, maxOrders, tokenEstimate(encoded), needleVisible)

			if !needleVisible {
				logf("    SKIP: needle at index 299 not reachable within budget")
				for _, q := range questions {
					logf("    SKIP %-20s (data truncated)", q.Name)
				}
				continue
			}

			// Run questions.
			for _, q := range questions {
				expected := q.Expected(allOrders)
				prompt := fmt.Sprintf("Here is order data in %s format:\n\n%s\n\nQuestion: %s",
					strings.ToUpper(f.name), encoded, q.Question)

				resp, err := callLLM(prompt)
				if err != nil {
					logf("    SKIP %-20s error: %v", q.Name, err)
					continue
				}

				ok, detail := q.Verify(expected, resp)
				mark := "PASS"
				if !ok {
					mark = "FAIL"
				}
				logf("    %s %-20s [%s] expected=%q got=%q",
					mark, q.Name, detail, expected, strings.TrimSpace(resp))
			}
		}
		logf("")
	}

	logf("Log: %s", logPath)
}
