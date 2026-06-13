// Generic-profile comprehension eval: tests LLM comprehension of nested
// structured data (orders, customers, items) across formats.
//
// Unlike the graph-profile eval (comprehension_test.go) which tests code
// intelligence payloads, this tests real-world nested data that most MCP
// tool responses contain.
//
// Run a single format:
//   GOWORK=off EVAL_FORMATS=gcf-v3 go test -run TestGenericComprehension -v -timeout 15m
//
// Run all formats:
//   GOWORK=off EVAL_FORMATS=gcf-v2,gcf-v3,json,toon,ploon go test -run TestGenericComprehension -v -timeout 60m
//
// Backends: EVAL_BACKEND=api (default haiku), openai, google
// Models: EVAL_MODEL=claude-haiku-4-5-20251001
//
// Results are written to eval/results/v3/comprehension/ as they complete.
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
	toon "github.com/toon-format/toon-go"
)

// Order represents a single e-commerce order with nested customer and items.
type Order struct {
	OrderID  string   `json:"orderId"`
	Customer Customer `json:"customer"`
	Items    []Item   `json:"items"`
	Subtotal float64  `json:"subtotal"`
	Tax      float64  `json:"tax"`
	Total    float64  `json:"total"`
	Status   string   `json:"status"`
}

type Customer struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Tier  string `json:"tier"`
}

type Item struct {
	SKU      string  `json:"sku"`
	Name     string  `json:"name"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

// buildGenericFixture generates deterministic nested order data.
func buildGenericFixture(numOrders int) []Order {
	names := []string{
		"Alice Chen", "Bob Martinez", "Carol Kim", "David Patel", "Eva Johansson",
		"Frank Okafor", "Grace Liu", "Henry Nakamura", "Iris Dubois", "James Kowalski",
		"Karen Singh", "Leo Fernandez", "Mia Thompson", "Noah Schmidt", "Olivia Reyes",
		"Peter Chang", "Quinn Murphy", "Rachel Sato", "Sam Okonkwo", "Tanya Volkov",
	}
	tiers := []string{"standard", "premium", "enterprise", "standard", "premium"}
	statuses := []string{"shipped", "pending", "processing", "delivered", "cancelled"}
	products := []struct {
		name  string
		sku   string
		price float64
	}{
		{"Wireless Mouse", "SKU-WM01", 29.99},
		{"USB-C Cable", "SKU-UC02", 14.99},
		{"Laptop Stand", "SKU-LS03", 49.99},
		{"Mechanical Keyboard", "SKU-MK04", 89.99},
		{"Monitor Arm", "SKU-MA05", 39.99},
		{"Webcam HD", "SKU-WC06", 59.99},
		{"Headset Pro", "SKU-HP07", 79.99},
		{"Desk Pad", "SKU-DP08", 24.99},
		{"USB Hub", "SKU-UH09", 34.99},
		{"Cable Clips", "SKU-CC10", 9.99},
	}

	orders := make([]Order, numOrders)
	for i := 0; i < numOrders; i++ {
		custIdx := i % len(names)
		numItems := (i % 4) + 1 // 1-4 items per order

		items := make([]Item, numItems)
		subtotal := 0.0
		for j := 0; j < numItems; j++ {
			p := products[(i*3+j)%len(products)]
			qty := (j % 3) + 1
			items[j] = Item{
				SKU:      p.sku,
				Name:     p.name,
				Quantity: qty,
				Price:    p.price,
			}
			subtotal += p.price * float64(qty)
		}
		subtotal = math.Round(subtotal*100) / 100
		tax := math.Round(subtotal*0.08*100) / 100
		total := math.Round((subtotal+tax)*100) / 100

		orders[i] = Order{
			OrderID: fmt.Sprintf("ORD-%04d", i+1),
			Customer: Customer{
				ID:    i + 1,
				Name:  names[custIdx],
				Email: strings.ToLower(strings.ReplaceAll(names[custIdx], " ", ".")) + "@example.com",
				Tier:  tiers[i%len(tiers)],
			},
			Items:    items,
			Subtotal: subtotal,
			Tax:      tax,
			Total:    total,
			Status:   statuses[i%len(statuses)],
		}
	}
	return orders
}

type genericQuestion struct {
	Name     string
	Question string
	Expected func(orders []Order) string
	Verify   func(expected, response string) (bool, string)
}

func numericVerify(expected, resp string) (bool, string) {
	resp = strings.TrimSpace(resp)
	resp = strings.ReplaceAll(resp, ",", "") // strip thousand separators
	resp = strings.ReplaceAll(resp, "$", "")
	if resp == expected {
		return true, "exact"
	}
	// Allow float rounding: 1234.56 vs 1234.6
	expF, err1 := strconv.ParseFloat(expected, 64)
	gotF, err2 := strconv.ParseFloat(resp, 64)
	if err1 == nil && err2 == nil && math.Abs(expF-gotF) < 0.1 {
		return true, "close"
	}
	if strings.Contains(resp, expected) {
		return true, "contains"
	}
	return false, fmt.Sprintf("got %q", resp)
}

func stringVerify(expected, resp string) (bool, string) {
	resp = strings.TrimSpace(resp)
	resp = strings.Trim(resp, "`\"")
	if strings.EqualFold(resp, expected) {
		return true, "exact"
	}
	if strings.Contains(strings.ToLower(resp), strings.ToLower(expected)) {
		return true, "contains"
	}
	return false, fmt.Sprintf("got %q", resp)
}

func buildGenericQuestions(numOrders int) []genericQuestion {
	return []genericQuestion{
		{
			Name:     "order_count",
			Question: "How many orders are in this data? Reply with ONLY a number.",
			Expected: func(orders []Order) string { return fmt.Sprintf("%d", len(orders)) },
			Verify:   numericVerify,
		},
		{
			Name:     "first_customer_name",
			Question: "What is the customer name on the first order? Reply with ONLY the name.",
			Expected: func(orders []Order) string { return orders[0].Customer.Name },
			Verify:   stringVerify,
		},
		{
			Name:     "last_order_status",
			Question: "What is the status of the last order? Reply with ONLY the status.",
			Expected: func(orders []Order) string { return orders[len(orders)-1].Status },
			Verify:   stringVerify,
		},
		{
			Name:     "total_items_first_order",
			Question: "How many line items are in the first order? Reply with ONLY a number.",
			Expected: func(orders []Order) string { return fmt.Sprintf("%d", len(orders[0].Items)) },
			Verify:   numericVerify,
		},
		{
			Name:     "customer_email_order5",
			Question: "What is the customer email on order ORD-0005? Reply with ONLY the email address.",
			Expected: func(orders []Order) string {
				for _, o := range orders {
					if o.OrderID == "ORD-0005" {
						return o.Customer.Email
					}
				}
				return ""
			},
			Verify: stringVerify,
		},
		{
			Name:     "count_shipped",
			Question: "How many orders have status 'shipped'? Reply with ONLY a number.",
			Expected: func(orders []Order) string {
				count := 0
				for _, o := range orders {
					if o.Status == "shipped" {
						count++
					}
				}
				return fmt.Sprintf("%d", count)
			},
			Verify: numericVerify,
		},
		{
			Name:     "count_premium_customers",
			Question: "How many orders have a customer with tier 'premium'? Reply with ONLY a number.",
			Expected: func(orders []Order) string {
				count := 0
				for _, o := range orders {
					if o.Customer.Tier == "premium" {
						count++
					}
				}
				return fmt.Sprintf("%d", count)
			},
			Verify: numericVerify,
		},
		{
			Name:     "highest_total",
			Question: "What is the highest order total? Reply with ONLY the number (e.g. 123.45).",
			Expected: func(orders []Order) string {
				max := 0.0
				for _, o := range orders {
					if o.Total > max {
						max = o.Total
					}
				}
				return fmt.Sprintf("%.2f", max)
			},
			Verify: numericVerify,
		},
		{
			Name:     "sku_first_item_order3",
			Question: "What is the SKU of the first item in order ORD-0003? Reply with ONLY the SKU.",
			Expected: func(orders []Order) string {
				for _, o := range orders {
					if o.OrderID == "ORD-0003" && len(o.Items) > 0 {
						return o.Items[0].SKU
					}
				}
				return ""
			},
			Verify: stringVerify,
		},
		{
			Name:     "total_revenue_shipped",
			Question: "What is the sum of all order totals where status is 'shipped'? Reply with ONLY the number (e.g. 1234.56).",
			Expected: func(orders []Order) string {
				sum := 0.0
				for _, o := range orders {
					if o.Status == "shipped" {
						sum += o.Total
					}
				}
				return fmt.Sprintf("%.2f", sum)
			},
			Verify: numericVerify,
		},
		{
			Name:     "unique_statuses",
			Question: "List all unique order statuses, comma-separated, alphabetically. Reply with ONLY the list.",
			Expected: func(orders []Order) string {
				seen := map[string]bool{}
				for _, o := range orders {
					seen[o.Status] = true
				}
				sorted := make([]string, 0, len(seen))
				for s := range seen {
					sorted = append(sorted, s)
				}
				for i := 0; i < len(sorted); i++ {
					for j := i + 1; j < len(sorted); j++ {
						if sorted[j] < sorted[i] {
							sorted[i], sorted[j] = sorted[j], sorted[i]
						}
					}
				}
				return strings.Join(sorted, ", ")
			},
			Verify: func(expected, resp string) (bool, string) {
				normalize := func(s string) string {
					s = strings.ToLower(strings.TrimSpace(s))
					parts := strings.Split(s, ",")
					for i, p := range parts {
						parts[i] = strings.TrimSpace(p)
					}
					return strings.Join(parts, ", ")
				}
				if normalize(resp) == normalize(expected) {
					return true, "exact"
				}
				for _, t := range strings.Split(expected, ", ") {
					if !strings.Contains(strings.ToLower(resp), t) {
						return false, fmt.Sprintf("missing %q", t)
					}
				}
				return true, "all present"
			},
		},
		{
			Name:     "count_orders_with_3plus_items",
			Question: "How many orders have 3 or more line items? Reply with ONLY a number.",
			Expected: func(orders []Order) string {
				count := 0
				for _, o := range orders {
					if len(o.Items) >= 3 {
						count++
					}
				}
				return fmt.Sprintf("%d", count)
			},
			Verify: numericVerify,
		},
		{
			Name:     "customer_tier_last_order",
			Question: "What is the customer tier on the last order? Reply with ONLY the tier.",
			Expected: func(orders []Order) string { return orders[len(orders)-1].Customer.Tier },
			Verify:   stringVerify,
		},
	}
}

// encodeGenericOrders encodes the order data in the specified format.
func encodeGenericOrders(orders []Order, format string) (string, error) {
	wrapper := map[string]any{"orders": ordersToAny(orders)}

	switch format {
	case "gcf-v2":
		return gcf.EncodeGeneric(wrapper), nil
	case "gcf-v3":
		return gcf.EncodeGenericV3(wrapper), nil
	case "json":
		b, err := json.MarshalIndent(wrapper, "", "  ")
		return string(b), err
	case "toon":
		return toon.MarshalString(wrapper)
	case "ploon":
		jsonBytes, err := json.Marshal(wrapper)
		if err != nil {
			return "", err
		}
		cmd := exec.Command("/opt/homebrew/bin/node", "-e", `
			const {stringify} = require('/Users/dayna.blackwell/code/toon-benchmark/node_modules/ploon');
			let input = '';
			process.stdin.on('data', d => input += d);
			process.stdin.on('end', () => {
				const data = JSON.parse(input);
				process.stdout.write(stringify(data));
			});
		`)
		cmd.Stdin = bytes.NewReader(jsonBytes)
		var out bytes.Buffer
		cmd.Stdout = &out
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("ploon encode failed: %w\nstderr: %s", err, stderr.String())
		}
		return out.String(), nil
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

func ordersToAny(orders []Order) []any {
	result := make([]any, len(orders))
	for i, o := range orders {
		items := make([]any, len(o.Items))
		for j, item := range o.Items {
			items[j] = map[string]any{
				"sku": item.SKU, "name": item.Name,
				"quantity": item.Quantity, "price": item.Price,
			}
		}
		result[i] = map[string]any{
			"orderId":  o.OrderID,
			"customer": map[string]any{"id": o.Customer.ID, "name": o.Customer.Name, "email": o.Customer.Email, "tier": o.Customer.Tier},
			"items":    items,
			"subtotal": o.Subtotal,
			"tax":      o.Tax,
			"total":    o.Total,
			"status":   o.Status,
		}
	}
	return result
}

func TestGenericComprehension(t *testing.T) {
	// Parse which formats to test.
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "gcf-v3" // default: just test v3
	}
	formatList := strings.Split(formatsEnv, ",")
	for i := range formatList {
		formatList[i] = strings.TrimSpace(formatList[i])
	}

	numOrders := 100
	if n := os.Getenv("EVAL_NUM_ORDERS"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil {
			numOrders = parsed
		}
	}

	// Set up LLM backend (reuse from comprehension_test.go).
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "api"
	}
	callLLM, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}

	// Generate fixture.
	orders := buildGenericFixture(numOrders)
	questions := buildGenericQuestions(numOrders)

	// Encode in all requested formats.
	type formatData struct {
		name    string
		content string
	}
	var formats []formatData
	for _, f := range formatList {
		encoded, err := encodeGenericOrders(orders, f)
		if err != nil {
			t.Fatalf("encode %s: %v", f, err)
		}
		formats = append(formats, formatData{name: f, content: encoded})
	}

	// Header.
	t.Logf("=== Generic-Profile Comprehension Eval ===")
	t.Logf("Backend:    %s", backendLabel)
	t.Logf("Orders:     %d", numOrders)
	t.Logf("Questions:  %d", len(questions))
	t.Logf("Formats:    %s", strings.Join(formatList, ", "))
	t.Log("")
	for _, f := range formats {
		t.Logf("%-8s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	t.Log("")

	// Results directory.
	resultsDir := filepath.Join("results", "v3", "comprehension")
	os.MkdirAll(resultsDir, 0755)

	// Open log file for streaming results.
	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	logName := fmt.Sprintf("generic-%dorders-%s-%s-%s.log",
		numOrders, backendName, model, time.Now().Format("2006-01-02-150405"))
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

	logf("Generic-Profile Comprehension Eval")
	logf("Backend: %s", backendLabel)
	logf("Orders: %d, Questions: %d, Formats: %s", numOrders, len(questions), strings.Join(formatList, ", "))
	for _, f := range formats {
		logf("%-8s %6d bytes, ~%d tokens", f.name, len(f.content), len(f.content)/4)
	}
	logf("")

	// Track results per format.
	type result struct {
		correct int
		total   int
	}
	results := make(map[string]*result)
	for _, f := range formats {
		results[f.name] = &result{}
	}

	// Run eval.
	for _, q := range questions {
		expected := q.Expected(orders)

		for _, f := range formats {
			prompt := fmt.Sprintf("Here is order data in %s format:\n\n%s\n\nQuestion: %s",
				strings.ToUpper(f.name), f.content, q.Question)

			resp, err := callLLM(prompt)
			if err != nil {
				logf("  SKIP %-25s %-8s error: %v", q.Name, f.name, err)
				continue
			}

			ok, detail := q.Verify(expected, resp)
			results[f.name].total++
			if ok {
				results[f.name].correct++
			}

			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			logf("  %s %-25s %-8s [%s] expected=%q got=%q",
				mark, q.Name, f.name, detail, expected, strings.TrimSpace(resp))
		}
	}

	// Summary.
	logf("")
	logf("=== Summary ===")
	logf("%-10s %8s %6s/%s", "Format", "Accuracy", "Pass", "Total")
	for _, f := range formats {
		r := results[f.name]
		acc := 0.0
		if r.total > 0 {
			acc = 100.0 * float64(r.correct) / float64(r.total)
		}
		logf("%-10s %7.1f%% %6d/%d", f.name, acc, r.correct, r.total)
	}
	logf("")
	logf("Log: %s", logPath)
}

// setupBackend initializes the LLM backend. Returns callLLM func, label, error.
func setupBackend(t *testing.T, backendName string) (func(string) (string, error), string, error) {
	switch backendName {
	case "cli":
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not on PATH")
		}
		cliModel := os.Getenv("EVAL_MODEL")
		label := "cli (claude -p)"
		if cliModel != "" {
			label = fmt.Sprintf("cli (claude -p --model %s)", cliModel)
		}
		return func(prompt string) (string, error) {
			args := []string{"-p", prompt}
			if cliModel != "" {
				args = []string{"-p", "--model", cliModel, prompt}
			}
			cmd := exec.Command("claude", args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("claude -p failed: %w\nstderr: %s", err, stderr.String())
			}
			return stdout.String(), nil
		}, label, nil
	case "api":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=api requires ANTHROPIC_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "claude-haiku-4-5-20251001"
		}
		label := fmt.Sprintf("api (%s)", model)
		return func(prompt string) (string, error) {
			return callAPI(apiKey, model, prompt)
		}, label, nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=openai requires OPENAI_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		label := fmt.Sprintf("openai (%s)", model)
		return func(prompt string) (string, error) {
			return callOpenAI(apiKey, model, prompt)
		}, label, nil
	case "google":
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=google requires GOOGLE_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gemini-2.0-flash"
		}
		label := fmt.Sprintf("google (%s)", model)
		return func(prompt string) (string, error) {
			return callGoogle(apiKey, model, prompt)
		}, label, nil
	default:
		return nil, "", fmt.Errorf("unknown backend: %s", backendName)
	}
}
