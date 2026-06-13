// Generic-profile generation eval: Can LLMs produce valid output in each format?
//
// Describes order data in natural language, asks the LLM to encode it,
// then validates through the real decoder for each format.
//
// Run (cold, no primers):
//   GOWORK=off EVAL_BACKEND=cli EVAL_MODEL=claude-haiku-4-5-20251001 \
//     EVAL_FORMATS=gcf,json,toon,ploon go test -run TestGenericGeneration -v -timeout 60m
//
// Run with primer:
//   EVAL_PRIMER=true EVAL_FORMATS=gcf ...
//
// Results written to eval/results/v3/generation/
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
	toon "github.com/toon-format/toon-go"
)

var genGenericSizes = []int{3, 5, 10, 20, 50}

const gcfV3Primer = `GCF generic profile example (2 orders):
GCF profile=generic
## orders [2]{orderId,customer,items,subtotal,total,status}
@0 ORD-001|^{id,name,email}|^|59.98|64.78|shipped
1|Alice|alice@test.com
.items [2]{sku,name,quantity,price}
    SKU-A|Widget|2|19.99
    SKU-B|Gadget|1|19.99
@1 ORD-002|^|^|29.99|32.39|pending
2|Bob|bob@test.com
.items [1]
    SKU-C|Gizmo|1|29.99

Rules:
- Header: GCF profile=generic
- Tabular arrays: ## name [count]{fields}
- Rows with nested objects use @index prefix and ^ for attachments
- First row declares inline schema: ^{field1,field2,...}
- Subsequent rows use bare ^ (reuses schema from first row)
- Inline attachment body: positional pipe-delimited values (no .fieldname prefix)
- Array attachments: .fieldname [count]{fields} on first row, .fieldname [count] on subsequent rows
- Scalars: numbers unquoted, strings unquoted unless containing |, commas, or special chars
- Null: -
- Output ONLY raw GCF. No explanation, no code fences. First line starts with "GCF profile=generic".`

func buildOrderDescriptions(orders []Order) string {
	var descs []string
	for _, o := range orders {
		var itemDescs []string
		for _, item := range o.Items {
			itemDescs = append(itemDescs, fmt.Sprintf("    - %s (SKU: %s, qty: %d, price: %.2f)", item.Name, item.SKU, item.Quantity, item.Price))
		}
		descs = append(descs, fmt.Sprintf(`- Order %s, status: %s, subtotal: %.2f, tax: %.2f, total: %.2f
  Customer: id=%d, name="%s", email=%s, tier=%s
  Items (%d):
%s`,
			o.OrderID, o.Status, o.Subtotal, o.Tax, o.Total,
			o.Customer.ID, o.Customer.Name, o.Customer.Email, o.Customer.Tier,
			len(o.Items), strings.Join(itemDescs, "\n")))
	}
	return strings.Join(descs, "\n")
}

const toonGenericPrimer = `TOON format example (2 orders):
orders[2]{orderId,status,subtotal,tax,total,customer_id,customer_name,customer_email,customer_tier,items}:
  ORD-001,shipped,59.98,4.80,64.78,1,Alice,alice@test.com,standard,"[{sku:SKU-A,name:Widget,quantity:2,price:19.99},{sku:SKU-B,name:Gadget,quantity:1,price:19.99}]"
  ORD-002,pending,29.99,2.40,32.39,2,Bob,bob@test.com,premium,"[{sku:SKU-C,name:Gizmo,quantity:1,price:29.99}]"

Rules:
- Header declares fields: name[count]{field1,field2,...}:
- Rows are comma-separated values matching header fields
- Nested objects are flattened into separate columns
- Output ONLY raw TOON. No explanation, no code fences.`

const ploonGenericPrimer = `PLOON format example (2 orders):
[orders#2](customer{email,id,name,tier},items#(name,price,quantity,sku),orderId,status,subtotal,tax,total)

1:1|ORD-001|shipped|59.98|4.80|64.78
2 |alice@test.com|1|Alice|standard
2:1|Widget|19.99|2|SKU-A
2:2|Gadget|19.99|1|SKU-B
1:2|ORD-002|pending|29.99|2.40|32.39
2 |bob@test.com|2|Bob|premium
2:1|Gizmo|29.99|1|SKU-C

Rules:
- Schema header: [name#count](fields) with nested objects as field{subfields} and arrays as field#(subfields)
- Array items: depth:index|values (e.g. 1:1, 2:1)
- Object values: depth |values (e.g. 2 )
- Pipe-separated values matching schema field order
- Output ONLY raw PLOON. No explanation, no code fences.`

const jsonGenericPrimer = `JSON example (1 order):
{"orders":[{"orderId":"ORD-001","customer":{"id":1,"name":"Alice","email":"alice@test.com","tier":"standard"},"items":[{"sku":"SKU-A","name":"Widget","quantity":2,"price":19.99}],"subtotal":19.99,"tax":1.60,"total":21.59,"status":"shipped"}]}

Output ONLY valid JSON. No explanation, no code fences.`

func buildGenPromptForFormat(format string, numOrders int, usePrimer bool) (string, []Order) {
	orders := buildGenericFixture(numOrders)
	desc := buildOrderDescriptions(orders)

	var prompt string
	switch format {
	case "gcf":
		if usePrimer {
			prompt = fmt.Sprintf("%s\n\nNow encode this order data as GCF generic profile:\n%d orders:\n%s", gcfV3Primer, numOrders, desc)
		} else {
			prompt = fmt.Sprintf("Encode this order data as GCF (Graph Compact Format) generic profile. Output ONLY the encoded data, no explanation.\n\n%d orders:\n%s", numOrders, desc)
		}
	case "json":
		if usePrimer {
			prompt = fmt.Sprintf("%s\n\nNow encode this order data as JSON:\n%d orders:\n%s", jsonGenericPrimer, numOrders, desc)
		} else {
			prompt = fmt.Sprintf("Encode this order data as JSON. Output ONLY valid JSON, no explanation.\n\n%d orders:\n%s", numOrders, desc)
		}
	case "toon":
		if usePrimer {
			prompt = fmt.Sprintf("%s\n\nNow encode this order data as TOON:\n%d orders:\n%s", toonGenericPrimer, numOrders, desc)
		} else {
			prompt = fmt.Sprintf("Encode this order data as TOON (Token-Oriented Object Notation). Output ONLY the encoded data, no explanation.\n\n%d orders:\n%s", numOrders, desc)
		}
	case "ploon":
		if usePrimer {
			prompt = fmt.Sprintf("%s\n\nNow encode this order data as PLOON:\n%d orders:\n%s", ploonGenericPrimer, numOrders, desc)
		} else {
			prompt = fmt.Sprintf("Encode this order data as PLOON (Path-Level Object Oriented Notation). Output ONLY the encoded data, no explanation.\n\n%d orders:\n%s", numOrders, desc)
		}
	}
	return prompt, orders
}

// validateGenOutput validates LLM output for a given format.
// Returns (decoded value, output bytes, error).
func validateGenOutput(format string, output string) (any, int, error) {
	switch format {
	case "gcf":
		text := stripToGCFGeneric(output)
		decoded, err := gcf.DecodeGeneric(text)
		return decoded, len(text), err
	case "json":
		text := stripToJSONGen(output)
		var decoded any
		err := json.Unmarshal([]byte(text), &decoded)
		return decoded, len(text), err
	case "toon":
		text := stripToTOONGen(output)
		var decoded map[string]any
		err := toon.UnmarshalString(text, &decoded)
		return decoded, len(text), err
	case "ploon":
		text := stripToPLOON(output)
		decoded, err := decodePLOON(text)
		return decoded, len(text), err
	}
	return nil, 0, fmt.Errorf("unknown format: %s", format)
}

func stripToGCFGeneric(s string) string {
	s = strings.ReplaceAll(s, "```gcf\n", "")
	s = strings.ReplaceAll(s, "```gcf", "")
	s = strings.ReplaceAll(s, "```\n", "")
	s = strings.ReplaceAll(s, "```", "")
	if idx := strings.Index(s, "GCF profile=generic"); idx >= 0 {
		s = s[idx:]
	}
	return strings.TrimSpace(s)
}

func stripToJSONGen(s string) string {
	s = strings.ReplaceAll(s, "```json\n", "")
	s = strings.ReplaceAll(s, "```json", "")
	s = strings.ReplaceAll(s, "```\n", "")
	s = strings.ReplaceAll(s, "```", "")
	// Find first { or [
	for i, c := range s {
		if c == '{' || c == '[' {
			s = s[i:]
			break
		}
	}
	return strings.TrimSpace(s)
}

func stripToTOONGen(s string) string {
	s = strings.ReplaceAll(s, "```toon\n", "")
	s = strings.ReplaceAll(s, "```toml\n", "")
	s = strings.ReplaceAll(s, "```\n", "")
	s = strings.ReplaceAll(s, "```", "")
	return strings.TrimSpace(s)
}

func stripToPLOON(s string) string {
	s = strings.ReplaceAll(s, "```ploon\n", "")
	s = strings.ReplaceAll(s, "```\n", "")
	s = strings.ReplaceAll(s, "```", "")
	if idx := strings.Index(s, "["); idx >= 0 {
		s = s[idx:]
	}
	return strings.TrimSpace(s)
}

// decodePLOON decodes PLOON output via node.
func decodePLOON(text string) (any, error) {
	cmd := exec.Command("/opt/homebrew/bin/node", "-e", `
		const {parse} = require('/Users/dayna.blackwell/code/toon-benchmark/node_modules/ploon');
		let input = '';
		process.stdin.on('data', d => input += d);
		process.stdin.on('end', () => {
			try {
				const data = parse(input);
				process.stdout.write(JSON.stringify(data));
			} catch(e) {
				process.stderr.write(e.message);
				process.exit(1);
			}
		});
	`)
	cmd.Stdin = strings.NewReader(text)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s", stderr.String())
	}
	var decoded any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func TestGenericGeneration(t *testing.T) {
	formatsEnv := os.Getenv("EVAL_FORMATS")
	if formatsEnv == "" {
		formatsEnv = "gcf"
	}
	formatList := strings.Split(formatsEnv, ",")
	for i := range formatList {
		formatList[i] = strings.TrimSpace(formatList[i])
	}

	usePrimer := os.Getenv("EVAL_PRIMER") == "true"

	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "cli"
	}
	callLLM, backendLabel, err := setupBackend(t, backendName)
	if err != nil {
		t.Fatal(err)
	}

	resultsDir := filepath.Join("results", "v3", "generation")
	os.MkdirAll(resultsDir, 0755)

	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = "default"
	}
	primerLabel := "cold"
	if usePrimer {
		primerLabel = "primer"
	}
	logName := fmt.Sprintf("generic-gen-%s-%s-%s-%s.log", primerLabel, backendName, model, time.Now().Format("2006-01-02-150405"))
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

	logf("=== Generic-Profile Generation Eval ===")
	logf("Backend: %s", backendLabel)
	logf("Primer: %s", primerLabel)
	logf("Sizes: %v orders", genGenericSizes)
	logf("Formats: %s", strings.Join(formatList, ", "))
	logf("")

	type genResult struct {
		format    string
		numOrders int
		valid     bool
		roundTrip bool
		outBytes  int
		jsonBytes int
		savings   float64
		errMsg    string
	}

	var results []genResult

	for _, numOrders := range genGenericSizes {
		for _, format := range formatList {
			prompt, expectedOrders := buildGenPromptForFormat(format, numOrders, usePrimer)

			logf("Generating %d orders as %s...", numOrders, format)

			output, err := callLLM(prompt)
			if err != nil {
				logf("  ERROR: %v", err)
				results = append(results, genResult{format: format, numOrders: numOrders, errMsg: err.Error()})
				continue
			}

			decoded, outBytes, decErr := validateGenOutput(format, output)
			if decErr != nil {
				logf("  INVALID: %v", decErr)
				preview := output
				if len(preview) > 500 {
					preview = preview[:500]
				}
				logf("  Output (first 500): %s", preview)
				results = append(results, genResult{format: format, numOrders: numOrders, errMsg: decErr.Error(), outBytes: outBytes})
				continue
			}

			// JSON size for comparison.
			wrapper := map[string]any{"orders": ordersToAny(expectedOrders)}
			jsonBytesRef := 0
			if jb, err := json.Marshal(wrapper); err == nil {
				jsonBytesRef = len(jb)
			}
			savings := 0.0
			if jsonBytesRef > 0 {
				savings = 100.0 * (1.0 - float64(outBytes)/float64(jsonBytesRef))
			}

			// Round-trip check (format-specific).
			roundTrip := false
			if format == "gcf" && decoded != nil {
				reEncoded := gcf.EncodeGeneric(decoded)
				reDecoded, reErr := gcf.DecodeGeneric(reEncoded)
				if reErr == nil {
					d1, _ := json.Marshal(decoded)
					d2, _ := json.Marshal(reDecoded)
					var n1, n2 any
					json.Unmarshal(d1, &n1)
					json.Unmarshal(d2, &n2)
					roundTrip = reflect.DeepEqual(n1, n2)
				}
			} else if format == "json" && decoded != nil {
				// JSON always round-trips through itself.
				roundTrip = true
			} else {
				// TOON/PLOON: skip round-trip (not our format).
				roundTrip = false
			}

			rtLabel := "n/a"
			if format == "gcf" || format == "json" {
				rtLabel = fmt.Sprintf("%v", roundTrip)
			}

			logf("  VALID  %d bytes, %d bytes JSON ref, %.0f%% savings, round-trip=%s",
				outBytes, jsonBytesRef, savings, rtLabel)

			results = append(results, genResult{
				format: format, numOrders: numOrders, valid: true, roundTrip: roundTrip,
				outBytes: outBytes, jsonBytes: jsonBytesRef, savings: savings,
			})
		}
	}

	// Summary.
	logf("")
	logf("=== Summary ===")
	logf("%-8s %-8s %-6s %-10s %-10s %-10s %-8s", "Format", "Orders", "Valid", "RoundTrip", "Out bytes", "JSON ref", "Savings")

	formatValid := make(map[string]int)
	formatTotal := make(map[string]int)
	for _, r := range results {
		formatTotal[r.format]++
		valid := "NO"
		if r.valid {
			valid = "YES"
			formatValid[r.format]++
		}
		rt := "n/a"
		if r.format == "gcf" || r.format == "json" {
			rt = "NO"
			if r.roundTrip {
				rt = "YES"
			}
		}
		logf("%-8s %-8d %-6s %-10s %-10d %-10d %-8.0f%%", r.format, r.numOrders, valid, rt, r.outBytes, r.jsonBytes, r.savings)
	}

	logf("")
	for _, f := range formatList {
		logf("%s: %d/%d valid", f, formatValid[f], formatTotal[f])
	}
	logf("")
	logf("Log: %s", logPath)
}
