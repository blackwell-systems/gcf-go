// LLM format comprehension benchmark for GCF.
//
// Sends a GCF-encoded payload and a JSON-encoded version of the same data
// to an LLM, then verifies it can answer structured extraction questions.
// Validates that GCF's compact format doesn't sacrifice comprehension.
//
// Two backends:
//
//	EVAL_BACKEND=cli  (default) - shells out to `claude -p "..."`.
//	EVAL_BACKEND=api            - calls Anthropic Messages API.
//	                              Requires ANTHROPIC_API_KEY.
//
// Run:
//
//	GOWORK=off go test -run TestGCFComprehension -v -timeout 10m
package gcf

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
)

// fixture is a static payload for comprehension testing.
var comprehensionFixture = &Payload{
	Tool:        "context_for_task",
	TokenBudget: 5000,
	TokensUsed:  1847,
	PackRoot:    "a1b2c3d4e5f6",
	Symbols: []Symbol{
		{QualifiedName: "github.com/org/repo/internal/auth.AuthMiddleware", Kind: "function", Score: 0.92, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "github.com/org/repo/internal/auth.ValidateToken", Kind: "function", Score: 0.85, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "github.com/org/repo/internal/auth.SessionStore", Kind: "type", Score: 0.78, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "github.com/org/repo/internal/auth.TokenConfig", Kind: "type", Score: 0.71, Provenance: "ast_inferred", Distance: 1},
		{QualifiedName: "github.com/org/repo/internal/server.HandleRequest", Kind: "function", Score: 0.65, Provenance: "lsp_resolved", Distance: 1},
		{QualifiedName: "github.com/org/repo/internal/server.Router", Kind: "type", Score: 0.58, Provenance: "lsp_resolved", Distance: 1},
		{QualifiedName: "github.com/org/repo/internal/store.UserDB", Kind: "interface", Score: 0.52, Provenance: "ast_inferred", Distance: 2},
		{QualifiedName: "github.com/org/repo/internal/store.SQLiteStore", Kind: "type", Score: 0.45, Provenance: "lsp_resolved", Distance: 2},
	},
	Edges: []Edge{
		{Source: "github.com/org/repo/internal/server.HandleRequest", Target: "github.com/org/repo/internal/auth.AuthMiddleware", EdgeType: "calls"},
		{Source: "github.com/org/repo/internal/auth.AuthMiddleware", Target: "github.com/org/repo/internal/auth.ValidateToken", EdgeType: "calls"},
		{Source: "github.com/org/repo/internal/auth.AuthMiddleware", Target: "github.com/org/repo/internal/auth.SessionStore", EdgeType: "references"},
		{Source: "github.com/org/repo/internal/store.SQLiteStore", Target: "github.com/org/repo/internal/store.UserDB", EdgeType: "implements"},
	},
}

type question struct {
	Name     string
	Question string
	Expected string
	Verify   func(expected, response string) (bool, string)
}

var questions = []question{
	{
		Name:     "top_symbol",
		Question: "What is the qualified name of the highest-scored symbol? Reply with ONLY the qualified name, nothing else.",
		Expected: "github.com/org/repo/internal/auth.AuthMiddleware",
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(resp)
			if strings.Contains(resp, expected) {
				return true, "match"
			}
			if strings.Contains(resp, "AuthMiddleware") {
				return true, "short match"
			}
			return false, fmt.Sprintf("got %q", resp)
		},
	},
	{
		Name:     "symbol_count",
		Question: "How many symbols are in the context? Reply with ONLY a number, nothing else.",
		Expected: "8",
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(resp)
			if resp == expected || strings.Contains(resp, expected) {
				return true, "match"
			}
			return false, fmt.Sprintf("got %q", resp)
		},
	},
	{
		Name:     "edge_count",
		Question: "How many edges (relationships) are in the context? Reply with ONLY a number, nothing else.",
		Expected: "4",
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(resp)
			if resp == expected || strings.Contains(resp, expected) {
				return true, "match"
			}
			return false, fmt.Sprintf("got %q", resp)
		},
	},
	{
		Name:     "top_kind",
		Question: "What is the kind of the highest-scored symbol? Reply with ONLY the kind (e.g. function, type, method), nothing else.",
		Expected: "function",
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(strings.ToLower(resp))
			if resp == expected || resp == "fn" {
				return true, "match"
			}
			return false, fmt.Sprintf("got %q", resp)
		},
	},
	{
		Name:     "edge_types",
		Question: "List all unique edge types in the context, comma-separated, alphabetically. Reply with ONLY the list, nothing else.",
		Expected: "calls, implements, references",
		Verify: func(expected, resp string) (bool, string) {
			resp = strings.TrimSpace(strings.ToLower(resp))
			resp = strings.ReplaceAll(resp, "`", "")
			expected = strings.ToLower(expected)
			if resp == expected {
				return true, "exact"
			}
			for _, t := range []string{"calls", "implements", "references"} {
				if !strings.Contains(resp, t) {
					return false, fmt.Sprintf("missing %q in %q", t, resp)
				}
			}
			return true, "all present"
		},
	},
}

func TestGCFComprehension(t *testing.T) {
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
			return callAnthropicAPI(apiKey, model, prompt)
		}
	default:
		t.Fatalf("unknown EVAL_BACKEND %q (use cli or api)", backendName)
	}

	gcfOutput := Encode(comprehensionFixture)
	jsonOutput, _ := json.MarshalIndent(comprehensionFixture, "", "  ")

	t.Logf("Backend: %s", backendLabel)
	t.Logf("GCF tokens (est): %d", len(gcfOutput)/4)
	t.Logf("JSON tokens (est): %d", len(jsonOutput)/4)
	t.Log("")

	formats := map[string]string{
		"gcf":  gcfOutput,
		"json": string(jsonOutput),
	}

	type result struct {
		format  string
		correct int
		total   int
		tokens  int
	}
	results := make(map[string]*result)
	for f, content := range formats {
		results[f] = &result{format: f, tokens: len(content) / 4}
	}

	for _, q := range questions {
		for _, f := range []string{"gcf", "json"} {
			content := formats[f]
			prompt := fmt.Sprintf("Here is a code context payload in %s format:\n\n%s\n\nQuestion: %s",
				strings.ToUpper(f), content, q.Question)

			resp, err := callLLM(prompt)
			if err != nil {
				t.Logf("  SKIP %-15s %-5s api error: %v", q.Name, f, err)
				continue
			}

			ok, detail := q.Verify(q.Expected, resp)
			results[f].total++
			if ok {
				results[f].correct++
			}

			mark := "PASS"
			if !ok {
				mark = "FAIL"
			}
			t.Logf("  %s %-15s %-5s [%s] expected=%q got=%q",
				mark, q.Name, f, detail, q.Expected, strings.TrimSpace(resp))
		}
	}

	t.Log("")
	t.Log("=== Summary ===")
	t.Logf("%-6s %8s %10s", "Format", "Accuracy", "Est Tokens")
	for _, f := range []string{"gcf", "json"} {
		r := results[f]
		acc := 0.0
		if r.total > 0 {
			acc = 100.0 * float64(r.correct) / float64(r.total)
		}
		t.Logf("%-6s %7.1f%% %10d", f, acc, r.tokens)
	}
}

func callAnthropicAPI(apiKey, model, prompt string) (string, error) {
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
		Content []struct{ Text string `json:"text"` } `json:"content"`
	}
	json.Unmarshal(respBody, &result)
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return result.Content[0].Text, nil
}
