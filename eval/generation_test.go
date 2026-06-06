// LLM generation benchmark: Can models produce valid GCF?
//
// Generates a prompt describing symbols and edges in natural language,
// asks the LLM to encode it as GCF, then validates the output through
// the real gcf.Decode() decoder.
//
// Run:
//
//	GOWORK=off go test -run TestGeneration -v -timeout 0
//
// Backends: same as TestComprehension (cli, api, openai, google, xai)
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
	toon "github.com/toon-format/toon-go"
)

var genSizes = []struct {
	symbols int
	edges   int
}{
	{5, 3},
	{10, 6},
	{20, 12},
	{50, 25},
	{100, 50},
}

const gcfPrimer = `GCF format example (3 symbols, 2 edges):
GCF tool=example budget=5000 tokens=100 symbols=3 edges=2
## targets
@0 fn pkg.Foo 0.90 lsp_resolved
@1 type pkg.Bar 0.70 ast_inferred
## related
@2 method pkg.Baz 0.50 structural
## edges [2]
@1<@0 calls
@2<@1 imports

Rules:
- Edge syntax: @target<@source edgeType (source->target direction)
- Kind abbreviations: function=fn, type=type, method=method, interface=iface
- Distance 0 = ## targets, 1 = ## related, 2 = ## extended
- Output ONLY raw GCF. No explanation, no code fences. First line starts with "GCF tool=".`

func buildGenPrompt(nSymbols, nEdges int) (string, *gcf.Payload) {
	packages := []string{
		"api", "auth", "store", "service", "middleware",
		"handler", "model", "config", "cache", "worker",
	}
	names := []string{
		"Handle", "Process", "Validate", "Create", "Update", "Delete",
		"Get", "Set", "Check", "Build", "Parse", "Format", "Encode",
		"Decode", "Transform", "Load", "Save", "Init", "Close", "Open",
	}
	suffixes := []string{
		"Request", "Response", "Config", "Handler", "Manager",
		"Service", "Store", "Client", "Factory", "Builder",
	}
	kinds := []string{"function", "type", "method", "interface"}
	provs := []string{"lsp_resolved", "ast_inferred", "structural"}
	edgeTypes := []string{"calls", "imports", "implements", "references"}

	p := &gcf.Payload{
		Tool:        "context_for_task",
		TokenBudget: 50000,
		TokensUsed:  nSymbols * 35,
	}

	var symDescs []string
	for i := 0; i < nSymbols; i++ {
		pkg := packages[i%len(packages)]
		name := names[i%len(names)] + suffixes[i%len(suffixes)]
		kind := kinds[i%len(kinds)]
		prov := provs[i%len(provs)]
		score := 0.95 - float64(i)*(0.85/float64(max(nSymbols-1, 1)))
		if score < 0.10 {
			score = 0.10
		}
		distance := 0
		if i >= nSymbols/3 {
			distance = 1
		}
		if i >= 2*nSymbols/3 {
			distance = 2
		}

		qn := fmt.Sprintf("pkg/%s.%s", pkg, name)
		p.Symbols = append(p.Symbols, gcf.Symbol{
			QualifiedName: qn,
			Kind:          kind,
			Score:         score,
			Provenance:    prov,
			Distance:      distance,
		})
		distLabel := []string{"target", "related", "extended"}[distance]
		symDescs = append(symDescs, fmt.Sprintf("- %s (%s, score %.2f, %s, %s)", qn, kind, score, prov, distLabel))
	}

	var edgeDescs []string
	for i := 0; i < nEdges && i+1 < nSymbols; i++ {
		srcIdx := (i*3 + 1) % nSymbols
		tgtIdx := (i * 3) % nSymbols
		et := edgeTypes[i%len(edgeTypes)]
		p.Edges = append(p.Edges, gcf.Edge{
			Source:   p.Symbols[srcIdx].QualifiedName,
			Target:   p.Symbols[tgtIdx].QualifiedName,
			EdgeType: et,
		})
		edgeDescs = append(edgeDescs, fmt.Sprintf("- %s %s %s", p.Symbols[srcIdx].QualifiedName, et, p.Symbols[tgtIdx].QualifiedName))
	}

	prompt := fmt.Sprintf(`%s

Now encode this data as GCF:
Tool: context_for_task, Budget: 50000, Tokens: %d
Symbols (%d):
%s
Edges (%d):
%s`,
		gcfPrimer,
		nSymbols*35, nSymbols, strings.Join(symDescs, "\n"),
		nEdges, strings.Join(edgeDescs, "\n"))

	return prompt, p
}

func TestGeneration(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "cli"
	}

	var callLLM func(prompt string) (string, error)
	var backendLabel string

	switch backendName {
	case "cli":
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not on PATH")
		}
		cliModel := os.Getenv("EVAL_MODEL")
		if cliModel != "" {
			backendLabel = fmt.Sprintf("cli (claude -p --model %s)", cliModel)
		} else {
			backendLabel = "cli (claude -p)"
		}
		callLLM = func(prompt string) (string, error) {
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
		}
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=openai requires OPENAI_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		backendLabel = fmt.Sprintf("openai (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callOpenAIGen(apiKey, model, prompt)
		}
	case "google":
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=google requires GOOGLE_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gemini-2.5-flash"
		}
		backendLabel = fmt.Sprintf("google (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callGoogleGen(apiKey, model, prompt)
		}
	case "api":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=api requires ANTHROPIC_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "claude-haiku-4-5-20251001"
		}
		backendLabel = fmt.Sprintf("api (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callAPIGen(apiKey, model, prompt)
		}
	default:
		t.Fatalf("unknown EVAL_BACKEND %q", backendName)
	}

	t.Logf("Backend: %s", backendLabel)
	t.Logf("Tests: %d sizes (5 to 100 symbols)", len(genSizes))
	t.Log("")

	type result struct {
		symbols    int
		edges      int
		valid      bool
		gcfBytes   int
		jsonBytes  int
		savings    float64
		err        string
		parsedSyms int
		parsedEdgs int
	}

	var results []result
	for _, sz := range genSizes {
		prompt, expectedPayload := buildGenPrompt(sz.symbols, sz.edges)
		t.Logf("Generating %d symbols, %d edges...", sz.symbols, sz.edges)

		output, err := callLLM(prompt)
		if err != nil {
			t.Logf("  ERROR: %v", err)
			results = append(results, result{symbols: sz.symbols, edges: sz.edges, err: err.Error()})
			continue
		}

		// Strip code fences and find GCF start.
		gcfText := stripToGCF(output)

		// Validate through real decoder.
		parsed, decErr := gcf.Decode(gcfText)
		if decErr != nil {
			t.Logf("  INVALID: %v", decErr)
			t.Logf("  Output (first 500 chars): %s", truncate(gcfText, 500))
			results = append(results, result{symbols: sz.symbols, edges: sz.edges, err: decErr.Error(), gcfBytes: len(gcfText)})
			continue
		}

		gcfBytes := len(gcfText)
		// Compare with JSON encoding of expected payload.
		jsonOut, _ := json.Marshal(expectedPayload)
		jsonBytes := len(jsonOut)
		savings := 100.0 * (1.0 - float64(gcfBytes)/float64(jsonBytes))

		t.Logf("  VALID  %d symbols, %d edges  (%d B GCF, %d B JSON, %.0f%% savings)",
			len(parsed.Symbols), len(parsed.Edges), gcfBytes, jsonBytes, savings)

		results = append(results, result{
			symbols: sz.symbols, edges: sz.edges, valid: true,
			gcfBytes: gcfBytes, jsonBytes: jsonBytes, savings: savings,
			parsedSyms: len(parsed.Symbols), parsedEdgs: len(parsed.Edges),
		})

		_ = expectedPayload
	}

	// Summary.
	t.Log("")
	t.Log("=== Summary ===")
	t.Logf("%-8s %-6s %-6s %-10s %-10s %-8s", "Symbols", "Edges", "Valid", "GCF bytes", "JSON bytes", "Savings")
	validCount := 0
	for _, r := range results {
		valid := "NO"
		if r.valid {
			valid = "YES"
			validCount++
		}
		t.Logf("%-8d %-6d %-6s %-10d %-10d %-8.0f%%", r.symbols, r.edges, valid, r.gcfBytes, r.jsonBytes, r.savings)
	}
	t.Logf("\n%d/%d valid.", validCount, len(results))
}

const toonPrimer = `TOON format example (3 symbols, 2 edges):
tool: example
symbols[3]{name,kind,score,provenance,distance}:
  pkg.Foo,function,0.9,lsp_resolved,0
  pkg.Bar,type,0.7,ast_inferred,1
  pkg.Baz,method,0.5,structural,2
edges[2]{source,target,type}:
  pkg.Foo,pkg.Bar,calls
  pkg.Bar,pkg.Baz,imports

Rules:
- Header format: arrayName[count]{field1,field2,...}:
- Each row indented with 2 spaces, fields comma-separated
- Key-value pairs use "key: value" syntax
- Output ONLY raw TOON. No explanation, no code fences. First line starts with "tool:".`

func buildToonGenPrompt(nSymbols, nEdges int, payload *gcf.Payload) string {
	var symDescs []string
	for _, s := range payload.Symbols {
		distLabel := []string{"target", "related", "extended"}[s.Distance]
		symDescs = append(symDescs, fmt.Sprintf("- %s (%s, score %.2f, %s, %s)", s.QualifiedName, s.Kind, s.Score, s.Provenance, distLabel))
	}
	var edgeDescs []string
	for _, e := range payload.Edges {
		edgeDescs = append(edgeDescs, fmt.Sprintf("- %s %s %s", e.Source, e.EdgeType, e.Target))
	}

	return fmt.Sprintf(`%s

Now encode this data as TOON:
Tool: context_for_task
Symbols (%d):
%s
Edges (%d):
%s`,
		toonPrimer,
		nSymbols, strings.Join(symDescs, "\n"),
		nEdges, strings.Join(edgeDescs, "\n"))
}

func TestGenerationTOON(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "cli"
	}

	var callLLM func(prompt string) (string, error)
	var backendLabel string

	switch backendName {
	case "cli":
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not on PATH")
		}
		cliModel := os.Getenv("EVAL_MODEL")
		if cliModel != "" {
			backendLabel = fmt.Sprintf("cli (claude -p --model %s)", cliModel)
		} else {
			backendLabel = "cli (claude -p)"
		}
		callLLM = func(prompt string) (string, error) {
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
		}
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=openai requires OPENAI_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		backendLabel = fmt.Sprintf("openai (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callOpenAIGen(apiKey, model, prompt)
		}
	case "google":
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=google requires GOOGLE_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gemini-2.5-flash"
		}
		backendLabel = fmt.Sprintf("google (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callGoogleGen(apiKey, model, prompt)
		}
	case "api":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=api requires ANTHROPIC_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "claude-haiku-4-5-20251001"
		}
		backendLabel = fmt.Sprintf("api (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callAPIGen(apiKey, model, prompt)
		}
	default:
		t.Fatalf("unknown EVAL_BACKEND %q", backendName)
	}

	t.Logf("Backend: %s", backendLabel)
	t.Logf("Tests: %d sizes (5 to 100 symbols)", len(genSizes))
	t.Log("")

	type result struct {
		symbols   int
		edges     int
		valid     bool
		toonBytes int
		jsonBytes int
		savings   float64
		err       string
	}

	var results []result
	for _, sz := range genSizes {
		_, payload := buildGenPrompt(sz.symbols, sz.edges)
		prompt := buildToonGenPrompt(sz.symbols, sz.edges, payload)
		t.Logf("Generating %d symbols, %d edges...", sz.symbols, sz.edges)

		output, err := callLLM(prompt)
		if err != nil {
			t.Logf("  ERROR: %v", err)
			results = append(results, result{symbols: sz.symbols, edges: sz.edges, err: err.Error()})
			continue
		}

		// Strip code fences.
		toonText := stripToTOON(output)

		// Validate: try to unmarshal with toon-go.
		type toonSymbol struct {
			Name       string  `toon:"name"`
			Kind       string  `toon:"kind"`
			Score      float64 `toon:"score"`
			Provenance string  `toon:"provenance"`
			Distance   int     `toon:"distance"`
		}
		type toonEdge struct {
			Source string `toon:"source"`
			Target string `toon:"target"`
			Type   string `toon:"type"`
		}
		type toonPayload struct {
			Tool    string       `toon:"tool"`
			Symbols []toonSymbol `toon:"symbols"`
			Edges   []toonEdge   `toon:"edges"`
		}

		var parsed toonPayload
		decErr := toon.UnmarshalString(toonText, &parsed)
		if decErr != nil {
			t.Logf("  INVALID: %v", decErr)
			t.Logf("  Output (first 500 chars): %s", truncate(toonText, 500))
			results = append(results, result{symbols: sz.symbols, edges: sz.edges, err: decErr.Error(), toonBytes: len(toonText)})
			continue
		}

		toonBytes := len(toonText)
		jsonOut, _ := json.Marshal(payload)
		jsonBytes := len(jsonOut)
		savings := 100.0 * (1.0 - float64(toonBytes)/float64(jsonBytes))

		t.Logf("  VALID  %d symbols, %d edges  (%d B TOON, %d B JSON, %.0f%% savings)",
			len(parsed.Symbols), len(parsed.Edges), toonBytes, jsonBytes, savings)

		results = append(results, result{
			symbols: sz.symbols, edges: sz.edges, valid: true,
			toonBytes: toonBytes, jsonBytes: jsonBytes, savings: savings,
		})
	}

	// Summary.
	t.Log("")
	t.Log("=== Summary ===")
	t.Logf("%-8s %-6s %-6s %-10s %-10s %-8s", "Symbols", "Edges", "Valid", "TOON bytes", "JSON bytes", "Savings")
	validCount := 0
	for _, r := range results {
		valid := "NO"
		if r.valid {
			valid = "YES"
			validCount++
		}
		t.Logf("%-8d %-6d %-6s %-10d %-10d %-8.0f%%", r.symbols, r.edges, valid, r.toonBytes, r.jsonBytes, r.savings)
	}
	t.Logf("\n%d/%d valid.", validCount, len(results))
}

func TestGenerationJSON(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "cli"
	}

	var callLLM func(prompt string) (string, error)
	var backendLabel string

	switch backendName {
	case "cli":
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not on PATH")
		}
		cliModel := os.Getenv("EVAL_MODEL")
		if cliModel != "" {
			backendLabel = fmt.Sprintf("cli (claude -p --model %s)", cliModel)
		} else {
			backendLabel = "cli (claude -p)"
		}
		callLLM = func(prompt string) (string, error) {
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
		}
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=openai requires OPENAI_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		backendLabel = fmt.Sprintf("openai (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callOpenAIGen(apiKey, model, prompt)
		}
	case "google":
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=google requires GOOGLE_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "gemini-2.5-flash"
		}
		backendLabel = fmt.Sprintf("google (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callGoogleGen(apiKey, model, prompt)
		}
	case "api":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("EVAL_BACKEND=api requires ANTHROPIC_API_KEY")
		}
		model := os.Getenv("EVAL_MODEL")
		if model == "" {
			model = "claude-haiku-4-5-20251001"
		}
		backendLabel = fmt.Sprintf("api (%s)", model)
		callLLM = func(prompt string) (string, error) {
			return callAPIGen(apiKey, model, prompt)
		}
	default:
		t.Fatalf("unknown EVAL_BACKEND %q", backendName)
	}

	t.Logf("Backend: %s", backendLabel)
	t.Logf("Tests: %d sizes (5 to 100 symbols)", len(genSizes))
	t.Log("")

	type result struct {
		symbols   int
		edges     int
		valid     bool
		jsonBytes int
		err       string
	}

	var results []result
	for _, sz := range genSizes {
		_, payload := buildGenPrompt(sz.symbols, sz.edges)
		t.Logf("Generating %d symbols, %d edges...", sz.symbols, sz.edges)

		var symDescs []string
		for _, s := range payload.Symbols {
			symDescs = append(symDescs, fmt.Sprintf("- %s (%s, score %.2f, %s, distance %d)", s.QualifiedName, s.Kind, s.Score, s.Provenance, s.Distance))
		}
		var edgeDescs []string
		for _, e := range payload.Edges {
			edgeDescs = append(edgeDescs, fmt.Sprintf("- %s %s %s", e.Source, e.EdgeType, e.Target))
		}

		prompt := fmt.Sprintf(`Output ONLY valid JSON. No explanation, no code fences.

Encode this data as a JSON object with "tool", "symbols" (array of objects with qualifiedName, kind, score, provenance, distance), and "edges" (array of objects with source, target, edgeType):

Tool: context_for_task
Symbols (%d):
%s
Edges (%d):
%s`,
			sz.symbols, strings.Join(symDescs, "\n"),
			sz.edges, strings.Join(edgeDescs, "\n"))

		output, err := callLLM(prompt)
		if err != nil {
			t.Logf("  ERROR: %v", err)
			results = append(results, result{symbols: sz.symbols, edges: sz.edges, err: err.Error()})
			continue
		}

		// Strip code fences and find JSON start.
		jsonText := stripToJSON(output)

		// Validate: try to parse as JSON.
		var parsed any
		decErr := json.Unmarshal([]byte(jsonText), &parsed)
		if decErr != nil {
			t.Logf("  INVALID: %v", decErr)
			t.Logf("  Output (first 500 chars): %s", truncate(jsonText, 500))
			results = append(results, result{symbols: sz.symbols, edges: sz.edges, err: decErr.Error(), jsonBytes: len(jsonText)})
			continue
		}

		t.Logf("  VALID  (%d B JSON)", len(jsonText))
		results = append(results, result{symbols: sz.symbols, edges: sz.edges, valid: true, jsonBytes: len(jsonText)})
	}

	// Summary.
	t.Log("")
	t.Log("=== Summary ===")
	t.Logf("%-8s %-6s %-6s %-10s", "Symbols", "Edges", "Valid", "JSON bytes")
	validCount := 0
	for _, r := range results {
		valid := "NO"
		if r.valid {
			valid = "YES"
			validCount++
		}
		t.Logf("%-8d %-6d %-6s %-10d", r.symbols, r.edges, valid, r.jsonBytes)
	}
	t.Logf("\n%d/%d valid.", validCount, len(results))
}

func stripToJSON(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	// Find line starting with "{".
	for i, line := range cleaned {
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			return strings.Join(cleaned[i:], "\n")
		}
	}
	return strings.Join(cleaned, "\n")
}

func stripToTOON(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	// Find line starting with "tool:".
	for i, line := range cleaned {
		if strings.HasPrefix(strings.TrimSpace(line), "tool:") {
			return strings.Join(cleaned[i:], "\n")
		}
	}
	return strings.Join(cleaned, "\n")
}

func stripToGCF(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Remove code fence markers.
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	// Find line starting with "GCF ".
	for i, line := range cleaned {
		if strings.HasPrefix(strings.TrimSpace(line), "GCF ") {
			return strings.Join(cleaned[i:], "\n")
		}
	}
	return strings.Join(cleaned, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Generation-specific API callers with higher max_tokens.

func callOpenAIGen(apiKey, model, prompt string) (string, error) {
	tokenKey := "max_tokens"
	if strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "o") {
		tokenKey = "max_completion_tokens"
	}
	body := map[string]any{
		"model":    model,
		tokenKey:   4096,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	if t := os.Getenv("EVAL_TEMPERATURE"); t != "" {
		if temp, err := strconv.ParseFloat(t, 64); err == nil {
			body["temperature"] = temp
		}
	}
	bodyBytes, _ := json.Marshal(body)

	url := "https://api.openai.com/v1/chat/completions"

	maxRetries := 5
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 && attempt < maxRetries {
			wait := time.Duration(1<<uint(attempt)) * 5 * time.Second
			time.Sleep(wait)
			continue
		}
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("OpenAI API %d: %s", resp.StatusCode, string(respBody))
		}
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		json.Unmarshal(respBody, &result)
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("empty response")
		}
		return result.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("max retries exceeded")
}

func callGoogleGen(apiKey, model, prompt string) (string, error) {
	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]any{"maxOutputTokens": 4096},
	}
	if t := os.Getenv("EVAL_TEMPERATURE"); t != "" {
		if temp, err := strconv.ParseFloat(t, 64); err == nil {
			body["generationConfig"] = map[string]any{"temperature": temp, "maxOutputTokens": 4096}
		}
	}
	bodyBytes, _ := json.Marshal(body)

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	maxRetries := 8
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 && attempt < maxRetries {
			wait := time.Duration(10+attempt*5) * time.Second
			time.Sleep(wait)
			continue
		}
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("Google API %d: %s", resp.StatusCode, string(respBody))
		}
		var result struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		json.Unmarshal(respBody, &result)
		if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
			return "", fmt.Errorf("empty response")
		}
		return result.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("max retries exceeded")
}

func callAPIGen(apiKey, model, prompt string) (string, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	bodyBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(respBody, &result)
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return result.Content[0].Text, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
