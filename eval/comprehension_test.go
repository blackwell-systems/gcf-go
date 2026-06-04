// LLM comprehension benchmark: GCF vs TOON vs JSON at 500 symbols.
//
// Generates a realistic 500-symbol, 200-edge payload, encodes it in all three
// formats using the official libraries, sends each to an LLM, and measures
// accuracy on 6 structured extraction questions.
//
// Run:
//
//	GOWORK=off go test -run TestComprehension -v -timeout 15m
//
// Backends:
//
//	EVAL_BACKEND=cli  (default) - uses `claude -p`
//	EVAL_BACKEND=api            - uses Anthropic API (requires ANTHROPIC_API_KEY)
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	gcf "github.com/blackwell-systems/gcf-go"
	toon "github.com/toon-format/toon-go"
)

// buildFixture generates a realistic payload with the given symbol/edge count.
func buildFixture(numSymbols, numEdges int) *gcf.Payload {
	packages := []string{
		"internal/auth", "internal/server", "internal/store",
		"internal/cache", "internal/config", "internal/middleware",
		"internal/handler", "internal/model", "internal/service",
		"internal/repository",
	}
	kinds := []string{"function", "type", "method", "interface"}
	provenances := []string{"lsp_resolved", "ast_inferred", "lsp_resolved", "structural"}
	names := []string{
		"Handle", "Process", "Validate", "Create", "Update", "Delete",
		"Get", "Set", "Check", "Build", "Parse", "Format", "Encode",
		"Decode", "Transform", "Convert", "Load", "Save", "Init",
		"Close", "Open", "Read", "Write", "Flush", "Reset", "Clear",
		"Register", "Dispatch", "Execute", "Invoke", "Resolve", "Lookup",
		"Filter", "Sort", "Merge", "Split", "Join", "Map", "Reduce",
		"Scan", "Walk", "Visit", "Collect", "Emit", "Notify", "Subscribe",
		"Publish", "Connect", "Disconnect", "Authenticate", "Authorize",
		"Encrypt", "Decrypt", "Hash", "Sign", "Verify", "Compress",
		"Decompress", "Cache", "Evict", "Refresh",
	}
	suffixes := []string{
		"Request", "Response", "Config", "Options", "Result",
		"Handler", "Manager", "Service", "Store", "Client",
		"Factory", "Builder", "Provider", "Resolver", "Adapter",
	}

	p := &gcf.Payload{
		Tool:        "context_for_task",
		TokenBudget: 50000,
		PackRoot:    "e7a3f1b2c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1",
	}

	for i := 0; i < numSymbols; i++ {
		pkg := packages[i%len(packages)]
		kind := kinds[i%len(kinds)]
		name := names[i%len(names)]
		suffix := suffixes[i%len(suffixes)]
		prov := provenances[i%len(provenances)]
		score := 0.95 - float64(i)*0.012
		if score < 0.10 {
			score = 0.10
		}
		distance := 0
		if i >= numSymbols/3 {
			distance = 1
		}
		if i >= 2*numSymbols/3 {
			distance = 2
		}

		qn := fmt.Sprintf("github.com/org/project/%s.%s%s", pkg, name, suffix)
		if kind == "method" {
			typeName := suffixes[i%len(suffixes)]
			qn = fmt.Sprintf("github.com/org/project/%s.%s.%s", pkg, typeName, name)
		}

		p.Symbols = append(p.Symbols, gcf.Symbol{
			QualifiedName: qn,
			Kind:          kind,
			Score:         score,
			Provenance:    prov,
			Distance:      distance,
		})
	}

	p.TokensUsed = len(p.Symbols) * 35

	edgeTypes := []string{"calls", "imports", "implements", "references"}
	for i := 0; i < numEdges && i+1 < len(p.Symbols); i++ {
		src := p.Symbols[(i*3+1)%len(p.Symbols)]
		tgt := p.Symbols[(i*3)%len(p.Symbols)]
		et := edgeTypes[i%len(edgeTypes)]
		p.Edges = append(p.Edges, gcf.Edge{
			Source:   src.QualifiedName,
			Target:   tgt.QualifiedName,
			EdgeType: et,
		})
	}

	return p
}

// encodeTOON encodes a Payload using the official toon-go library.
func encodeTOON(p *gcf.Payload) (string, error) {
	type toonSymbol struct {
		Name       string  `toon:"name"`
		Kind       string  `toon:"kind"`
		Score      float64 `toon:"score"`
		Provenance string  `toon:"provenance"`
		Distance   int     `toon:"distance"`
	}
	type toonEdge struct {
		Source   string `toon:"source"`
		Target   string `toon:"target"`
		EdgeType string `toon:"type"`
	}
	type toonPayload struct {
		Tool        string       `toon:"tool"`
		TokensUsed  int          `toon:"tokens_used"`
		TokenBudget int          `toon:"token_budget"`
		Symbols     []toonSymbol `toon:"symbols"`
		Edges       []toonEdge   `toon:"edges,omitempty"`
	}

	tp := toonPayload{
		Tool:        p.Tool,
		TokensUsed:  p.TokensUsed,
		TokenBudget: p.TokenBudget,
		Symbols:     make([]toonSymbol, len(p.Symbols)),
		Edges:       make([]toonEdge, len(p.Edges)),
	}
	for i, s := range p.Symbols {
		prov := s.Provenance
		if prov == "" {
			prov = "-"
		}
		tp.Symbols[i] = toonSymbol{Name: s.QualifiedName, Kind: s.Kind, Score: s.Score, Provenance: prov, Distance: s.Distance}
	}
	for i, e := range p.Edges {
		tp.Edges[i] = toonEdge{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType}
	}
	return toon.MarshalString(tp)
}

type question struct {
	Name     string
	Question string
	Expected func(p *gcf.Payload) string
	Verify   func(expected, response string) (bool, string)
}

func exactOrContains(expected, resp string) (bool, string) {
	resp = strings.TrimSpace(resp)
	if resp == expected {
		return true, "exact"
	}
	if strings.Contains(resp, expected) {
		return true, "contains"
	}
	return false, fmt.Sprintf("got %q", resp)
}

var questions = []question{
	{
		Name:     "symbol_count",
		Question: "How many symbols are in the context? Reply with ONLY a number, nothing else.",
		Expected: func(p *gcf.Payload) string { return fmt.Sprintf("%d", len(p.Symbols)) },
		Verify:   exactOrContains,
	},
	{
		Name:     "edge_count",
		Question: "How many edges (relationships between symbols) are in the context? Reply with ONLY a number, nothing else.",
		Expected: func(p *gcf.Payload) string { return fmt.Sprintf("%d", len(p.Edges)) },
		Verify:   exactOrContains,
	},
	{
		Name:     "top_symbol",
		Question: "What is the short name (last component after the final dot) of the highest-scored symbol? Reply with ONLY the name, nothing else.",
		Expected: func(p *gcf.Payload) string {
			qn := p.Symbols[0].QualifiedName
			if dot := strings.LastIndex(qn, "."); dot >= 0 {
				return qn[dot+1:]
			}
			return qn
		},
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(resp)
			resp = strings.Trim(resp, "`")
			if strings.EqualFold(resp, expected) || strings.Contains(resp, expected) {
				return true, "match"
			}
			return false, fmt.Sprintf("got %q", resp)
		},
	},
	{
		Name:     "top_kind",
		Question: "What is the kind of the highest-scored symbol? Reply with ONLY the kind (e.g. function, type, method), nothing else.",
		Expected: func(p *gcf.Payload) string { return p.Symbols[0].Kind },
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(strings.ToLower(resp))
			if resp == expected || resp == gcf.KindAbbrev[expected] {
				return true, "match"
			}
			return false, fmt.Sprintf("got %q", resp)
		},
	},
	{
		Name:     "target_count",
		Question: "How many symbols are in the 'targets' group (distance 0)? Reply with ONLY a number, nothing else.",
		Expected: func(p *gcf.Payload) string {
			count := 0
			for _, s := range p.Symbols {
				if s.Distance == 0 {
					count++
				}
			}
			return fmt.Sprintf("%d", count)
		},
		Verify: exactOrContains,
	},
	{
		Name:     "edge_types",
		Question: "List all unique edge types in the context, comma-separated, alphabetically. Reply with ONLY the list, nothing else.",
		Expected: func(p *gcf.Payload) string {
			types := make(map[string]bool)
			for _, e := range p.Edges {
				types[e.EdgeType] = true
			}
			sorted := make([]string, 0, len(types))
			for t := range types {
				sorted = append(sorted, t)
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
				s = strings.ReplaceAll(s, "`", "")
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
}

func TestComprehension(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "cli"
	}

	var callLLM func(prompt string) (string, error)
	var backendLabel string

	switch backendName {
	case "cli":
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not on PATH; install Claude Code or set EVAL_BACKEND=api with ANTHROPIC_API_KEY")
		}
		backendLabel = "cli (claude -p)"
		callLLM = func(prompt string) (string, error) {
			cmd := exec.Command("claude", "-p", prompt)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("claude -p failed: %w\nstderr: %s", err, stderr.String())
			}
			return stdout.String(), nil
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
			return callAPI(apiKey, model, prompt)
		}
	default:
		t.Fatalf("unknown EVAL_BACKEND %q (use cli or api)", backendName)
	}

	fixture := buildFixture(500, 200)

	gcfOutput := gcf.Encode(fixture)
	jsonOutput, _ := json.MarshalIndent(fixture, "", "  ")
	toonOutput, err := encodeTOON(fixture)
	if err != nil {
		t.Fatalf("TOON encode failed: %v", err)
	}

	formats := []struct {
		name    string
		content string
	}{
		{"gcf", gcfOutput},
		{"toon", toonOutput},
		{"json", string(jsonOutput)},
	}

	t.Logf("Backend: %s", backendLabel)
	t.Logf("Fixture: %d symbols, %d edges", len(fixture.Symbols), len(fixture.Edges))
	t.Log("")
	for _, f := range formats {
		t.Logf("%-5s tokens (est): %d", f.name, len(f.content)/4)
	}
	t.Logf("GCF vs JSON savings: %.0f%%", 100.0*(1.0-float64(len(gcfOutput))/float64(len(jsonOutput))))
	t.Logf("GCF vs TOON savings: %.0f%%", 100.0*(1.0-float64(len(gcfOutput))/float64(len(toonOutput))))
	t.Log("")

	type result struct {
		correct int
		total   int
		tokens  int
	}
	results := make(map[string]*result)
	for _, f := range formats {
		results[f.name] = &result{tokens: len(f.content) / 4}
	}

	for _, q := range questions {
		expected := q.Expected(fixture)
		for _, f := range formats {
			prompt := fmt.Sprintf("Here is a code context payload in %s format:\n\n%s\n\nQuestion: %s",
				strings.ToUpper(f.name), f.content, q.Question)

			resp, err := callLLM(prompt)
			if err != nil {
				t.Logf("  SKIP %-15s %-5s error: %v", q.Name, f.name, err)
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
			t.Logf("  %s %-15s %-5s [%s] expected=%q got=%q",
				mark, q.Name, f.name, detail, expected, strings.TrimSpace(resp))
		}
	}

	t.Log("")
	t.Log("=== Summary ===")
	t.Logf("%-6s %8s %10s %10s", "Format", "Accuracy", "Est Tokens", "vs JSON")
	jsonTokens := results["json"].tokens
	for _, f := range formats {
		r := results[f.name]
		acc := 0.0
		if r.total > 0 {
			acc = 100.0 * float64(r.correct) / float64(r.total)
		}
		vsJSON := "baseline"
		if f.name != "json" && jsonTokens > 0 {
			vsJSON = fmt.Sprintf("%.0f%%", 100.0*float64(r.tokens)/float64(jsonTokens))
		}
		t.Logf("%-6s %7.1f%% %10d %10s", f.name, acc, r.tokens, vsJSON)
	}
}

func callAPI(apiKey, model, prompt string) (string, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": 200,
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
