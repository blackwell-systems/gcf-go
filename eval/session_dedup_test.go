// Session dedup comprehension eval: tests whether LLMs correctly resolve
// bare references (@N # previously transmitted) to their original declarations
// from earlier in a multi-call conversation.
//
// Simulates a 3-call agent session with increasing session dedup depth.
// Each call includes comprehension questions with deterministic ground truth.
// Compares session dedup vs full retransmission vs JSON.
//
// Run:
//
//	EVAL_BACKEND=google GOOGLE_API_KEY=... EVAL_MODEL=gemini-2.5-flash GOWORK=off go test -run TestSessionDedup -v -timeout 30m
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
)

// sessionCall represents one tool response in a multi-call session.
type sessionCall struct {
	CallNum     int
	GCFSession  string
	GCFFull     string
	JSON        string
	Questions   []sessionQuestion
	SymbolCount int
	EdgeCount   int
	NewSymbols  int
	BareRefs    int
}

type sessionQuestion struct {
	Name         string
	Question     string
	Expected     string
	NeedsBareRef bool
}

func buildSessionCalls() []*sessionCall {
	packages := []string{
		"network/bgp", "network/ospf", "network/fabric",
		"network/mpls", "network/vxlan", "network/policy",
		"network/acl", "network/qos", "network/monitor",
		"network/telemetry",
	}
	names := []string{
		"HandlePeer", "ProcessRoute", "ValidateConfig", "CreateSession",
		"UpdateTopology", "DeleteNeighbor", "GetStatus", "SetPolicy",
		"CheckHealth", "BuildGraph", "ParseUpdate", "FormatOutput",
		"EncodePacket", "DecodeFrame", "TransformPath", "ConvertAddress",
		"LoadState", "SaveCheckpoint", "InitProtocol", "CloseSession",
		"OpenConnection", "ReadTelemetry", "WriteLog", "FlushBuffer",
		"ResetCounters", "ClearCache", "RegisterHandler", "DispatchEvent",
		"ExecuteAction", "InvokeCallback",
	}
	kinds := []string{"function", "type", "method", "interface"}
	provenances := []string{"lsp_resolved", "ast_inferred", "structural"}
	edgeTypes := []string{"calls", "imports", "implements", "references"}

	makePayload := func(numSym, numEdge int) *gcf.Payload {
		p := &gcf.Payload{Tool: "network_topology", TokenBudget: 50000}
		for i := 0; i < numSym; i++ {
			pkg := packages[i%len(packages)]
			name := names[i%len(names)]
			kind := kinds[i%len(kinds)]
			prov := provenances[i%len(provenances)]
			score := 0.95 - float64(i)*0.015
			if score < 0.10 {
				score = 0.10
			}
			distance := 0
			if i >= numSym/3 {
				distance = 1
			}
			if i >= 2*numSym/3 {
				distance = 2
			}
			qn := fmt.Sprintf("github.com/netclaw/%s.%s", pkg, name)
			p.Symbols = append(p.Symbols, gcf.Symbol{
				QualifiedName: qn, Kind: kind, Score: score,
				Provenance: prov, Distance: distance,
			})
		}
		p.TokensUsed = len(p.Symbols) * 35
		for i := 0; i < numEdge && i+1 < len(p.Symbols); i++ {
			src := p.Symbols[(i*3+1)%len(p.Symbols)]
			tgt := p.Symbols[(i*3)%len(p.Symbols)]
			p.Edges = append(p.Edges, gcf.Edge{
				Source: src.QualifiedName, Target: tgt.QualifiedName,
				EdgeType: edgeTypes[i%len(edgeTypes)],
			})
		}
		return p
	}

	// Call 1: 50 symbols, 80 edges
	p1 := makePayload(50, 80)

	// Call 2: same 50 + 10 new = 60 symbols, 100 edges
	p2 := makePayload(60, 100)

	// Call 3: same 60 + 5 new = 65 symbols, 110 edges
	p3 := makePayload(65, 110)

	// Target symbol for bare-ref questions (symbol 0, declared in call 1)
	targetQN := p1.Symbols[0].QualifiedName
	targetShort := targetQN
	if dot := strings.LastIndex(targetShort, "."); dot >= 0 {
		targetShort = targetShort[dot+1:]
	}
	targetKind := p1.Symbols[0].Kind

	countKind := func(p *gcf.Payload, k string) int {
		n := 0
		for _, s := range p.Symbols {
			if s.Kind == k {
				n++
			}
		}
		return n
	}

	// Encode
	sess := gcf.NewSession()
	gcfFull1 := gcf.Encode(p1)
	gcfSess1 := gcf.EncodeWithSession(p1, sess)
	json1, _ := json.MarshalIndent(p1, "", "  ")

	gcfFull2 := gcf.Encode(p2)
	gcfSess2 := gcf.EncodeWithSession(p2, sess)
	json2, _ := json.MarshalIndent(p2, "", "  ")

	gcfFull3 := gcf.Encode(p3)
	gcfSess3 := gcf.EncodeWithSession(p3, sess)
	json3, _ := json.MarshalIndent(p3, "", "  ")

	// Actual edge counts (capped by symbols-1 due to loop guard)
	edgeCount1 := len(p1.Edges)
	edgeCount2 := len(p2.Edges)
	edgeCount3 := len(p3.Edges)

	return []*sessionCall{
		{
			CallNum: 1, GCFSession: gcfSess1, GCFFull: gcfFull1, JSON: string(json1),
			SymbolCount: 50, EdgeCount: edgeCount1, NewSymbols: 50, BareRefs: 0,
			Questions: []sessionQuestion{
				{"symbol_count", "In the MOST RECENT tool response only (not previous ones), how many symbols are listed? Reply with ONLY a number.", "50", false},
				{"edge_count", "In the MOST RECENT tool response only, how many edges are listed? Reply with ONLY a number.", fmt.Sprintf("%d", edgeCount1), false},
				{"target_kind", fmt.Sprintf("What is the kind of the symbol %s? Reply with ONLY the kind (function, type, method, or interface).", targetShort), targetKind, false},
				{"function_count", "In the MOST RECENT tool response only, how many symbols have kind 'function' (or 'fn')? Reply with ONLY a number.", fmt.Sprintf("%d", countKind(p1, "function")), false},
			},
		},
		{
			CallNum: 2, GCFSession: gcfSess2, GCFFull: gcfFull2, JSON: string(json2),
			SymbolCount: 60, EdgeCount: edgeCount2, NewSymbols: 10, BareRefs: 50,
			Questions: []sessionQuestion{
				{"symbol_count", "In the MOST RECENT tool response (the last one, not earlier ones), how many symbols are listed? Count both fully declared symbols and 'previously transmitted' references. Reply with ONLY a number.", fmt.Sprintf("%d", len(p2.Symbols)), true},
				{"edge_count", "In the MOST RECENT tool response only, how many edges are listed? Reply with ONLY a number.", fmt.Sprintf("%d", edgeCount2), false},
				{"target_kind", fmt.Sprintf("What is the kind of the symbol %s? It may have been fully declared in an earlier tool response and appear as 'previously transmitted' in the latest one. Reply with ONLY the kind (function, type, method, or interface).", targetShort), targetKind, true},
				{"function_count", "In the MOST RECENT tool response, how many symbols have kind 'function' (or 'fn')? For 'previously transmitted' symbols, look up their kind from the earlier response where they were fully declared. Reply with ONLY a number.", fmt.Sprintf("%d", countKind(p2, "function")), true},
			},
		},
		{
			CallNum: 3, GCFSession: gcfSess3, GCFFull: gcfFull3, JSON: string(json3),
			SymbolCount: 65, EdgeCount: edgeCount3, NewSymbols: 5, BareRefs: 60,
			Questions: []sessionQuestion{
				{"symbol_count", "In the MOST RECENT tool response (the last one only), how many symbols are listed? Count both fully declared symbols and 'previously transmitted' references. Reply with ONLY a number.", fmt.Sprintf("%d", len(p3.Symbols)), true},
				{"edge_count", "In the MOST RECENT tool response only, how many edges are listed? Reply with ONLY a number.", fmt.Sprintf("%d", edgeCount3), false},
				{"target_kind", fmt.Sprintf("What is the kind of the symbol %s? Look it up from whichever tool response declared it fully. Reply with ONLY the kind (function, type, method, or interface).", targetShort), targetKind, true},
				{"function_count", "In the MOST RECENT tool response, how many symbols have kind 'function' (or 'fn')? For 'previously transmitted' symbols, look up their kind from earlier responses. Reply with ONLY a number.", fmt.Sprintf("%d", countKind(p3, "function")), true},
			},
		},
	}
}

// callGoogleMultiTurn sends a multi-turn conversation to Gemini.
func callGoogleMultiTurn(apiKey, model string, messages []map[string]any) (string, error) {
	body := map[string]any{"contents": messages}
	if t := os.Getenv("EVAL_TEMPERATURE"); t != "" {
		if temp, err := strconv.ParseFloat(t, 64); err == nil {
			body["generationConfig"] = map[string]any{"temperature": temp}
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
			return "", fmt.Errorf("empty response: %s", string(respBody))
		}
		return result.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("max retries exceeded")
}

// callOpenAIMultiTurn sends a multi-turn conversation to OpenAI-compatible API.
func callOpenAIMultiTurn(apiKey, model string, messages []map[string]string) (string, error) {
	tokenKey := "max_tokens"
	if strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "o") {
		tokenKey = "max_completion_tokens"
	}
	body := map[string]any{
		"model":    model,
		tokenKey:   200,
		"messages": messages,
	}
	bodyBytes, _ := json.Marshal(body)

	url := "https://api.openai.com/v1/chat/completions"
	if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
		url = strings.TrimRight(baseURL, "/") + "/chat/completions"
	}

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

func TestSessionDedup(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		t.Skip("Session dedup eval requires multi-turn API. Set EVAL_BACKEND=google or openai.")
	}

	model := os.Getenv("EVAL_MODEL")
	calls := buildSessionCalls()

	type formatDef struct {
		name       string
		getContent func(c *sessionCall) string
	}
	formats := []formatDef{
		{"gcf_session", func(c *sessionCall) string { return c.GCFSession }},
		{"gcf_full", func(c *sessionCall) string { return c.GCFFull }},
		{"json", func(c *sessionCall) string { return c.JSON }},
	}

	type scorecard struct {
		correct      int
		total        int
		bareRefOK    int
		bareRefTotal int
	}
	scores := map[string]*scorecard{}
	for _, f := range formats {
		scores[f.name] = &scorecard{}
	}

	t.Logf("=== Session Dedup Eval ===")
	t.Logf("Backend: %s, Model: %s", backendName, model)
	t.Logf("Calls: %d, Formats: %d", len(calls), len(formats))
	t.Logf("")

	for _, f := range formats {
		t.Logf("--- %s ---", f.name)

		for _, call := range calls {
			content := f.getContent(call)
			t.Logf("  Call %d: %d sym (%d new, %d bare), %d edges, ~%d tok",
				call.CallNum, call.SymbolCount, call.NewSymbols, call.BareRefs,
				call.EdgeCount, len(content)/4)

			for _, q := range call.Questions {
				// Build the prompt as a single turn with conversation context
				// (simulates multi-turn by including prior calls in the prompt)
				var promptParts []string
				for _, prior := range calls[:call.CallNum] {
					priorContent := f.getContent(prior)
					promptParts = append(promptParts, fmt.Sprintf("Tool response (call %d):\n\n%s", prior.CallNum, priorContent))
				}
				promptParts = append(promptParts, fmt.Sprintf("Question about the data across all tool responses above: %s", q.Question))
				prompt := strings.Join(promptParts, "\n\n---\n\n")

				var resp string
				var err error

				switch backendName {
				case "google":
					apiKey := os.Getenv("GOOGLE_API_KEY")
					if apiKey == "" {
						t.Skip("GOOGLE_API_KEY required")
					}
					if model == "" {
						model = "gemini-2.5-flash"
					}
					contents := []map[string]any{
						{"role": "user", "parts": []map[string]string{{"text": prompt}}},
					}
					resp, err = callGoogleMultiTurn(apiKey, model, contents)
				case "openai":
					apiKey := os.Getenv("OPENAI_API_KEY")
					if apiKey == "" {
						t.Skip("OPENAI_API_KEY required")
					}
					if model == "" {
						model = "gpt-4o"
					}
					msgs := []map[string]string{{"role": "user", "content": prompt}}
					resp, err = callOpenAIMultiTurn(apiKey, model, msgs)
				default:
					t.Fatalf("Unsupported backend for session eval: %s", backendName)
				}

				if err != nil {
					t.Logf("    %s: ERROR %v", q.Name, err)
					scores[f.name].total++
					if q.NeedsBareRef {
						scores[f.name].bareRefTotal++
					}
					continue
				}

				resp = strings.TrimSpace(resp)
				ok := verifySessionAnswer(q.Expected, resp)

				scores[f.name].total++
				if q.NeedsBareRef {
					scores[f.name].bareRefTotal++
				}

				if ok {
					scores[f.name].correct++
					if q.NeedsBareRef {
						scores[f.name].bareRefOK++
					}
					t.Logf("    %s: PASS expected=%s got=%s", q.Name, q.Expected, resp)
				} else {
					t.Logf("    %s: FAIL expected=%s got=%s", q.Name, q.Expected, resp)
				}

				time.Sleep(1 * time.Second)
			}
		}
		t.Logf("")
	}

	// Summary
	t.Logf("=== SUMMARY ===")
	t.Logf("%-15s %8s %12s", "Format", "Accuracy", "BareRef Acc")
	t.Logf("%s", strings.Repeat("-", 40))
	for _, f := range formats {
		s := scores[f.name]
		acc := 0.0
		if s.total > 0 {
			acc = float64(s.correct) / float64(s.total) * 100
		}
		brAcc := 0.0
		if s.bareRefTotal > 0 {
			brAcc = float64(s.bareRefOK) / float64(s.bareRefTotal) * 100
		}
		t.Logf("%-15s %4d/%-3d %4d/%-3d",
			f.name, s.correct, s.total, s.bareRefOK, s.bareRefTotal)
		_ = acc
		_ = brAcc
	}

	// Token savings
	t.Logf("")
	t.Logf("Token estimates:")
	for _, call := range calls {
		st := len(call.GCFSession) / 4
		ft := len(call.GCFFull) / 4
		jt := len(call.JSON) / 4
		sv := 0.0
		if jt > 0 {
			sv = (1.0 - float64(st)/float64(jt)) * 100
		}
		t.Logf("  Call %d: session=%d full=%d json=%d (session saves %.1f%%)",
			call.CallNum, st, ft, jt, sv)
	}
}

func verifySessionAnswer(expected, resp string) bool {
	resp = strings.TrimSpace(strings.ToLower(resp))
	expected = strings.TrimSpace(strings.ToLower(expected))
	resp = strings.Trim(resp, "`*_\"'")

	if resp == expected {
		return true
	}
	if strings.Contains(resp, expected) {
		return true
	}

	// Kind aliases
	aliases := map[string]string{
		"fn": "function", "function": "function",
		"type": "type", "method": "method",
		"iface": "interface", "interface": "interface",
	}
	if aliases[resp] == expected || aliases[expected] == resp {
		return true
	}

	return false
}

// TestSessionDedupResolve tests the one thing that matters in production:
// can the model resolve a bare reference to its original declaration?
// 5 symbols declared in call 1, all become bare refs in call 2.
// Each question asks about one specific bare-ref symbol.
func TestSessionDedupResolve(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		t.Skip("Set EVAL_BACKEND=google or openai")
	}
	model := os.Getenv("EVAL_MODEL")

	// 5 symbols with distinct, memorable attributes
	p1 := &gcf.Payload{Tool: "blast_radius", TokenBudget: 5000}
	p1.Symbols = []gcf.Symbol{
		{QualifiedName: "pkg/auth.ValidateToken", Kind: "function", Score: 0.95, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "pkg/server.HTTPHandler", Kind: "type", Score: 0.88, Provenance: "ast_inferred", Distance: 0},
		{QualifiedName: "pkg/db.Connect", Kind: "method", Score: 0.72, Provenance: "structural", Distance: 1},
		{QualifiedName: "pkg/cache.Invalidate", Kind: "function", Score: 0.65, Provenance: "lsp_resolved", Distance: 1},
		{QualifiedName: "pkg/config.LoadYAML", Kind: "interface", Score: 0.50, Provenance: "ast_inferred", Distance: 2},
	}
	p1.Edges = []gcf.Edge{
		{Source: "pkg/server.HTTPHandler", Target: "pkg/auth.ValidateToken", EdgeType: "calls"},
		{Source: "pkg/auth.ValidateToken", Target: "pkg/db.Connect", EdgeType: "calls"},
		{Source: "pkg/server.HTTPHandler", Target: "pkg/cache.Invalidate", EdgeType: "references"},
		{Source: "pkg/config.LoadYAML", Target: "pkg/server.HTTPHandler", EdgeType: "implements"},
	}
	p1.TokensUsed = 200

	// Call 2: same 5 symbols + 1 new. All 5 originals become bare refs.
	p2 := &gcf.Payload{Tool: "blast_radius", TokenBudget: 5000}
	p2.Symbols = append(p2.Symbols, p1.Symbols...)
	p2.Symbols = append(p2.Symbols, gcf.Symbol{
		QualifiedName: "pkg/metrics.RecordLatency", Kind: "function", Score: 0.45, Provenance: "structural", Distance: 2,
	})
	p2.Edges = append(p2.Edges, p1.Edges...)
	p2.Edges = append(p2.Edges, gcf.Edge{
		Source: "pkg/server.HTTPHandler", Target: "pkg/metrics.RecordLatency", EdgeType: "calls",
	})
	p2.TokensUsed = 250

	sess := gcf.NewSession()
	gcfSess1 := gcf.EncodeWithSession(p1, sess)
	gcfSess2 := gcf.EncodeWithSession(p2, sess)

	t.Logf("=== Bare Reference Resolution Test ===")
	t.Logf("Backend: %s, Model: %s", backendName, model)
	t.Logf("Call 1: 5 symbols fully declared (%d tokens)", len(gcfSess1)/4)
	t.Logf("Call 2: 5 bare refs + 1 new (%d tokens)", len(gcfSess2)/4)
	t.Logf("")
	t.Logf("Call 1 payload:")
	t.Logf("%s", gcfSess1)
	t.Logf("")
	t.Logf("Call 2 payload:")
	t.Logf("%s", gcfSess2)
	t.Logf("")

	// Each question resolves one attribute of one bare-ref symbol
	questions := []struct {
		name     string
		question string
		expected string
	}{
		{"resolve_kind_0", "What is the kind of the symbol ValidateToken? Reply with ONLY the kind.", "function"},
		{"resolve_kind_1", "What is the kind of the symbol HTTPHandler? Reply with ONLY the kind.", "type"},
		{"resolve_kind_2", "What is the kind of the symbol Connect? Reply with ONLY the kind.", "method"},
		{"resolve_kind_3", "What is the kind of the symbol Invalidate? Reply with ONLY the kind.", "function"},
		{"resolve_kind_4", "What is the kind of the symbol LoadYAML? Reply with ONLY the kind.", "interface"},
		{"resolve_provenance_0", "What is the provenance of ValidateToken? Reply with ONLY the provenance.", "lsp_resolved"},
		{"resolve_provenance_2", "What is the provenance of Connect? Reply with ONLY the provenance.", "structural"},
		{"resolve_score_0", "What is the score of ValidateToken? Reply with ONLY the number.", "0.95"},
		{"resolve_score_4", "What is the score of LoadYAML? Reply with ONLY the number.", "0.50"},
		{"resolve_caller", "Which symbol is ValidateToken called by? Look at the edges. Reply with ONLY the short name (after the last dot).", "HTTPHandler"},
		{"resolve_callee", "Which symbol is called by ValidateToken? Look at the edges. Reply with ONLY the short name.", "Connect"},
		{"resolve_new_symbol", "What is the kind of RecordLatency? Reply with ONLY the kind.", "function"},
	}

	correct := 0
	for _, q := range questions {
		var resp string
		var err error

		switch backendName {
		case "google":
			apiKey := os.Getenv("GOOGLE_API_KEY")
			if apiKey == "" {
				t.Skip("GOOGLE_API_KEY required")
			}
			if model == "" {
				model = "gemini-2.5-flash"
			}
			contents := []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": "You are a code intelligence assistant. Here is a tool response:\n\n" + gcfSess1}}},
				{"role": "model", "parts": []map[string]string{{"text": "I see 5 symbols and 4 edges in this blast radius result."}}},
				{"role": "user", "parts": []map[string]string{{"text": "Here is an updated tool response:\n\n" + gcfSess2}}},
				{"role": "model", "parts": []map[string]string{{"text": "I see the updated result with 6 symbols and 5 edges. Some symbols reference earlier declarations."}}},
				{"role": "user", "parts": []map[string]string{{"text": q.question}}},
			}
			resp, err = callGoogleMultiTurn(apiKey, model, contents)
		case "openai":
			apiKey := os.Getenv("OPENAI_API_KEY")
			if apiKey == "" {
				t.Skip("OPENAI_API_KEY required")
			}
			if model == "" {
				model = "gpt-4o"
			}
			msgs := []map[string]string{
				{"role": "user", "content": "You are a code intelligence assistant. Here is a tool response:\n\n" + gcfSess1},
				{"role": "assistant", "content": "I see 5 symbols and 4 edges in this blast radius result."},
				{"role": "user", "content": "Here is an updated tool response:\n\n" + gcfSess2},
				{"role": "assistant", "content": "I see the updated result with 6 symbols and 5 edges. Some symbols reference earlier declarations."},
				{"role": "user", "content": q.question},
			}
			resp, err = callOpenAIMultiTurn(apiKey, model, msgs)
		default:
			t.Fatalf("Unsupported backend: %s", backendName)
		}

		if err != nil {
			t.Logf("  %s: ERROR %v", q.name, err)
			continue
		}

		resp = strings.TrimSpace(resp)
		ok := verifySessionAnswer(q.expected, resp)
		if ok {
			correct++
			t.Logf("  %s: PASS expected=%s got=%s", q.name, q.expected, resp)
		} else {
			t.Logf("  %s: FAIL expected=%s got=%s", q.name, q.expected, resp)
		}

		time.Sleep(1 * time.Second)
	}

	t.Logf("")
	t.Logf("=== Result: %d/%d (%d%%) ===", correct, len(questions), correct*100/len(questions))
}

// TestSessionDedupDepth tests bare-ref resolution at increasing session depth.
// Symbol declared in call 1, then 4 more calls pile up. Does the model still
// resolve the original declaration at depth 5?
func TestSessionDedupDepth(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		t.Skip("Set EVAL_BACKEND=google or openai")
	}
	model := os.Getenv("EVAL_MODEL")

	// Call 1: declare the target symbol
	p1 := &gcf.Payload{Tool: "topology", TokenBudget: 5000}
	p1.Symbols = []gcf.Symbol{
		{QualifiedName: "core-router-alpha.dc.example.com", Kind: "function", Score: 0.95, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "edge-switch-001.dc.example.com", Kind: "type", Score: 0.80, Provenance: "ast_inferred", Distance: 1},
		{QualifiedName: "firewall-001.dc.example.com", Kind: "method", Score: 0.70, Provenance: "structural", Distance: 1},
	}
	p1.Edges = []gcf.Edge{
		{Source: "core-router-alpha.dc.example.com", Target: "edge-switch-001.dc.example.com", EdgeType: "calls"},
		{Source: "core-router-alpha.dc.example.com", Target: "firewall-001.dc.example.com", EdgeType: "references"},
	}

	sess := gcf.NewSession()
	call1 := gcf.EncodeWithSession(p1, sess)

	// Calls 2-5: add new symbols each time, original 3 stay as bare refs
	var depthCalls []string
	for depth := 2; depth <= 5; depth++ {
		p := &gcf.Payload{Tool: "topology", TokenBudget: 5000}
		// Re-include original symbols (will become bare refs)
		p.Symbols = append(p.Symbols, p1.Symbols...)
		// Add new symbols for this depth
		for i := 0; i < 3; i++ {
			p.Symbols = append(p.Symbols, gcf.Symbol{
				QualifiedName: fmt.Sprintf("new-device-d%d-%d.dc.example.com", depth, i),
				Kind:          "type",
				Score:         0.40,
				Provenance:    "structural",
				Distance:      2,
			})
		}
		p.Edges = append(p.Edges, p1.Edges...)
		depthCalls = append(depthCalls, gcf.EncodeWithSession(p, sess))
	}

	t.Logf("=== Bare Reference Depth Test ===")
	t.Logf("Backend: %s, Model: %s", backendName, model)
	t.Logf("Target: core-router-alpha.dc.example.com (kind=function, declared in call 1)")
	t.Logf("Testing resolution at depths 2, 3, 4, 5")
	t.Logf("")

	// The question is always the same: resolve the target from call 1
	question := "What is the kind of the symbol core-router-alpha? It may have been declared in an earlier tool response. Reply with ONLY the kind (function, type, method, or interface)."

	for depth := 2; depth <= 5; depth++ {
		// Build multi-turn conversation up to this depth
		var googleContents []map[string]any
		var openaiMsgs []map[string]string

		// Call 1
		googleContents = append(googleContents,
			map[string]any{"role": "user", "parts": []map[string]string{{"text": "Tool response (call 1):\n\n" + call1}}},
			map[string]any{"role": "model", "parts": []map[string]string{{"text": "Received call 1."}}},
		)
		openaiMsgs = append(openaiMsgs,
			map[string]string{"role": "user", "content": "Tool response (call 1):\n\n" + call1},
			map[string]string{"role": "assistant", "content": "Received call 1."},
		)

		// Calls 2 through depth
		for d := 2; d <= depth; d++ {
			callContent := depthCalls[d-2]
			googleContents = append(googleContents,
				map[string]any{"role": "user", "parts": []map[string]string{{"text": fmt.Sprintf("Tool response (call %d):\n\n%s", d, callContent)}}},
				map[string]any{"role": "model", "parts": []map[string]string{{"text": fmt.Sprintf("Received call %d.", d)}}},
			)
			openaiMsgs = append(openaiMsgs,
				map[string]string{"role": "user", "content": fmt.Sprintf("Tool response (call %d):\n\n%s", d, callContent)},
				map[string]string{"role": "assistant", "content": fmt.Sprintf("Received call %d.", d)},
			)
		}

		// Ask the question
		googleContents = append(googleContents,
			map[string]any{"role": "user", "parts": []map[string]string{{"text": question}}},
		)
		openaiMsgs = append(openaiMsgs,
			map[string]string{"role": "user", "content": question},
		)

		var resp string
		var err error

		switch backendName {
		case "google":
			apiKey := os.Getenv("GOOGLE_API_KEY")
			if apiKey == "" {
				t.Skip("GOOGLE_API_KEY required")
			}
			if model == "" {
				model = "gemini-2.5-flash"
			}
			resp, err = callGoogleMultiTurn(apiKey, model, googleContents)
		case "openai":
			apiKey := os.Getenv("OPENAI_API_KEY")
			if apiKey == "" {
				t.Skip("OPENAI_API_KEY required")
			}
			if model == "" {
				model = "gpt-4o"
			}
			resp, err = callOpenAIMultiTurn(apiKey, model, openaiMsgs)
		}

		if err != nil {
			t.Logf("  Depth %d: ERROR %v", depth, err)
			continue
		}

		resp = strings.TrimSpace(resp)
		ok := verifySessionAnswer("function", resp)
		if ok {
			t.Logf("  Depth %d (%d messages): PASS got=%s", depth, (depth*2)+1, resp)
		} else {
			t.Logf("  Depth %d (%d messages): FAIL expected=function got=%s", depth, (depth*2)+1, resp)
		}

		time.Sleep(1 * time.Second)
	}
}

// TestSessionDedupMaxDepth pushes bare-ref resolution to failure.
// Symbol declared in call 1, then up to 15 calls pile on.
// Reports the exact depth where resolution breaks.
func TestSessionDedupMaxDepth(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		t.Skip("Set EVAL_BACKEND=google or openai")
	}
	model := os.Getenv("EVAL_MODEL")

	maxDepth := 15

	// Call 1: declare 3 target symbols with distinct kinds
	p1 := &gcf.Payload{Tool: "topology", TokenBudget: 5000}
	p1.Symbols = []gcf.Symbol{
		{QualifiedName: "alpha-core.dc.example.com", Kind: "function", Score: 0.95, Provenance: "lsp_resolved", Distance: 0},
		{QualifiedName: "beta-edge.dc.example.com", Kind: "type", Score: 0.80, Provenance: "ast_inferred", Distance: 1},
		{QualifiedName: "gamma-fw.dc.example.com", Kind: "method", Score: 0.60, Provenance: "structural", Distance: 2},
	}
	p1.Edges = []gcf.Edge{
		{Source: "alpha-core.dc.example.com", Target: "beta-edge.dc.example.com", EdgeType: "calls"},
	}

	sess := gcf.NewSession()
	call1 := gcf.EncodeWithSession(p1, sess)

	// Generate calls 2 through maxDepth+1, each adding 2 new symbols
	var depthCalls []string
	for d := 2; d <= maxDepth+1; d++ {
		p := &gcf.Payload{Tool: "topology", TokenBudget: 5000}
		p.Symbols = append(p.Symbols, p1.Symbols...)
		for i := 0; i < 2; i++ {
			p.Symbols = append(p.Symbols, gcf.Symbol{
				QualifiedName: fmt.Sprintf("device-d%d-%d.dc.example.com", d, i),
				Kind:          "type",
				Score:         0.30,
				Provenance:    "structural",
				Distance:      2,
			})
		}
		p.Edges = append(p.Edges, p1.Edges...)
		depthCalls = append(depthCalls, gcf.EncodeWithSession(p, sess))
	}

	t.Logf("=== Max Depth Test (up to %d calls) ===", maxDepth)
	t.Logf("Backend: %s, Model: %s", backendName, model)
	t.Logf("Targets declared in call 1:")
	t.Logf("  alpha-core -> kind=function")
	t.Logf("  beta-edge  -> kind=type")
	t.Logf("  gamma-fw   -> kind=method")
	t.Logf("")

	questions := []struct {
		name     string
		question string
		expected string
	}{
		{"alpha_kind", "What is the kind of alpha-core? Reply with ONLY the kind.", "function"},
		{"beta_kind", "What is the kind of beta-edge? Reply with ONLY the kind.", "type"},
		{"gamma_kind", "What is the kind of gamma-fw? Reply with ONLY the kind.", "method"},
	}

	for depth := 2; depth <= maxDepth; depth++ {
		// Build conversation up to this depth
		var googleContents []map[string]any
		var openaiMsgs []map[string]string

		googleContents = append(googleContents,
			map[string]any{"role": "user", "parts": []map[string]string{{"text": "Tool response (call 1):\n\n" + call1}}},
			map[string]any{"role": "model", "parts": []map[string]string{{"text": "Received."}}},
		)
		openaiMsgs = append(openaiMsgs,
			map[string]string{"role": "user", "content": "Tool response (call 1):\n\n" + call1},
			map[string]string{"role": "assistant", "content": "Received."},
		)

		for d := 2; d <= depth; d++ {
			c := depthCalls[d-2]
			googleContents = append(googleContents,
				map[string]any{"role": "user", "parts": []map[string]string{{"text": fmt.Sprintf("Tool response (call %d):\n\n%s", d, c)}}},
				map[string]any{"role": "model", "parts": []map[string]string{{"text": "Received."}}},
			)
			openaiMsgs = append(openaiMsgs,
				map[string]string{"role": "user", "content": fmt.Sprintf("Tool response (call %d):\n\n%s", d, c)},
				map[string]string{"role": "assistant", "content": "Received."},
			)
		}

		passed := 0
		for _, q := range questions {
			gc := make([]map[string]any, len(googleContents))
			copy(gc, googleContents)
			gc = append(gc, map[string]any{"role": "user", "parts": []map[string]string{{"text": q.question}}})

			om := make([]map[string]string, len(openaiMsgs))
			copy(om, openaiMsgs)
			om = append(om, map[string]string{"role": "user", "content": q.question})

			var resp string
			var err error

			switch backendName {
			case "google":
				apiKey := os.Getenv("GOOGLE_API_KEY")
				if apiKey == "" {
					t.Skip("GOOGLE_API_KEY required")
				}
				if model == "" {
					model = "gemini-2.5-flash"
				}
				resp, err = callGoogleMultiTurn(apiKey, model, gc)
			case "openai":
				apiKey := os.Getenv("OPENAI_API_KEY")
				if apiKey == "" {
					t.Skip("OPENAI_API_KEY required")
				}
				if model == "" {
					model = "gpt-4o"
				}
				resp, err = callOpenAIMultiTurn(apiKey, model, om)
			}

			if err != nil {
				t.Logf("  Depth %2d: %s ERROR %v", depth, q.name, err)
				continue
			}

			resp = strings.TrimSpace(resp)
			if verifySessionAnswer(q.expected, resp) {
				passed++
			} else {
				t.Logf("  Depth %2d: %s FAIL expected=%s got=%s", depth, q.name, q.expected, resp)
			}

			time.Sleep(500 * time.Millisecond)
		}

		if passed == len(questions) {
			t.Logf("  Depth %2d (%2d messages): 3/3 PASS", depth, depth*2+1)
		} else {
			t.Logf("  Depth %2d (%2d messages): %d/3 — DEGRADATION DETECTED", depth, depth*2+1, passed)
			if passed == 0 {
				t.Logf("  FAILURE at depth %d. Stopping.", depth)
				break
			}
		}
	}
}

// TestSessionDedupBasic is a smaller, production-realistic test.
// 10 devices, 2 calls, multi-turn conversation, questions an agent would actually ask.
func TestSessionDedupBasic(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		t.Skip("Set EVAL_BACKEND=google or openai")
	}
	model := os.Getenv("EVAL_MODEL")

	// Build a small topology: 10 network devices
	p1 := &gcf.Payload{Tool: "network_topology", TokenBudget: 5000}
	devices := []struct {
		name, kind, prov string
		score            float64
		dist             int
	}{
		{"spine-east-001.dc.corp.net", "function", "lsp_resolved", 0.95, 0},
		{"spine-east-002.dc.corp.net", "type", "lsp_resolved", 0.90, 0},
		{"leaf-east-001.dc.corp.net", "method", "ast_inferred", 0.85, 1},
		{"leaf-east-002.dc.corp.net", "interface", "ast_inferred", 0.80, 1},
		{"leaf-east-003.dc.corp.net", "function", "lsp_resolved", 0.75, 1},
		{"border-001.dc.corp.net", "type", "structural", 0.70, 0},
		{"monitor-001.dc.corp.net", "method", "structural", 0.65, 2},
		{"firewall-001.dc.corp.net", "function", "lsp_resolved", 0.60, 2},
		{"loadbal-001.dc.corp.net", "interface", "ast_inferred", 0.55, 2},
		{"dns-001.dc.corp.net", "type", "structural", 0.50, 2},
	}
	for _, d := range devices {
		p1.Symbols = append(p1.Symbols, gcf.Symbol{
			QualifiedName: d.name, Kind: d.kind, Score: d.score,
			Provenance: d.prov, Distance: d.dist,
		})
	}
	p1.Edges = append(p1.Edges,
		gcf.Edge{Source: "spine-east-001.dc.corp.net", Target: "leaf-east-001.dc.corp.net", EdgeType: "calls"},
		gcf.Edge{Source: "spine-east-001.dc.corp.net", Target: "leaf-east-002.dc.corp.net", EdgeType: "calls"},
		gcf.Edge{Source: "spine-east-002.dc.corp.net", Target: "leaf-east-003.dc.corp.net", EdgeType: "calls"},
		gcf.Edge{Source: "border-001.dc.corp.net", Target: "spine-east-001.dc.corp.net", EdgeType: "imports"},
		gcf.Edge{Source: "border-001.dc.corp.net", Target: "spine-east-002.dc.corp.net", EdgeType: "imports"},
		gcf.Edge{Source: "firewall-001.dc.corp.net", Target: "border-001.dc.corp.net", EdgeType: "references"},
		gcf.Edge{Source: "loadbal-001.dc.corp.net", Target: "leaf-east-001.dc.corp.net", EdgeType: "implements"},
	)
	p1.TokensUsed = 350

	// Call 2: same 10 + 2 new devices. In session mode, 10 are bare refs.
	p2 := &gcf.Payload{Tool: "network_topology", TokenBudget: 5000}
	p2.Symbols = append(p2.Symbols, p1.Symbols...)
	p2.Symbols = append(p2.Symbols,
		gcf.Symbol{QualifiedName: "new-leaf-001.dc.corp.net", Kind: "method", Score: 0.45, Provenance: "lsp_resolved", Distance: 1},
		gcf.Symbol{QualifiedName: "new-spine-001.dc.corp.net", Kind: "function", Score: 0.40, Provenance: "lsp_resolved", Distance: 0},
	)
	p2.Edges = append(p2.Edges, p1.Edges...)
	p2.Edges = append(p2.Edges,
		gcf.Edge{Source: "new-spine-001.dc.corp.net", Target: "new-leaf-001.dc.corp.net", EdgeType: "calls"},
		gcf.Edge{Source: "new-spine-001.dc.corp.net", Target: "leaf-east-001.dc.corp.net", EdgeType: "calls"},
	)
	p2.TokensUsed = 400

	// Encode
	sess := gcf.NewSession()
	gcfSess1 := gcf.EncodeWithSession(p1, sess)
	gcfSess2 := gcf.EncodeWithSession(p2, sess)

	gcfFull1 := gcf.Encode(p1)
	gcfFull2 := gcf.Encode(p2)

	t.Logf("=== Session Dedup Basic (Production-Realistic) ===")
	t.Logf("Backend: %s, Model: %s", backendName, model)
	t.Logf("Call 1: 10 devices, 7 edges, session=%d tok, full=%d tok",
		len(gcfSess1)/4, len(gcfFull1)/4)
	t.Logf("Call 2: 12 devices (10 bare refs + 2 new), 9 edges, session=%d tok, full=%d tok",
		len(gcfSess2)/4, len(gcfFull2)/4)
	t.Logf("")

	// Production-realistic questions asked AFTER call 2 (multi-turn)
	type prodQuestion struct {
		name         string
		question     string
		expected     string
		needsBareRef bool
	}
	prodQuestions := []prodQuestion{
		{
			"device_lookup",
			"What is the qualified name of the device with the highest score? Reply with ONLY the device name.",
			"spine-east-001.dc.corp.net",
			true, // declared in call 1, bare ref in call 2
		},
		{
			"connection_query",
			"Which devices does spine-east-001.dc.corp.net connect to via 'calls' edges? List only the short hostnames (before .dc.corp.net), comma-separated, alphabetically. Reply with ONLY the list.",
			"leaf-east-001, leaf-east-002",
			true, // spine-east-001 is a bare ref in call 2
		},
		{
			"new_device_check",
			"Is new-spine-001.dc.corp.net present in the most recent tool response? Reply with ONLY yes or no.",
			"yes",
			false,
		},
		{
			"count_targets",
			"In the most recent tool response, how many devices are in the 'targets' group? Reply with ONLY a number.",
			"4", // spine-east-001, spine-east-002, border-001, new-spine-001
			true,
		},
		{
			"provenance_lookup",
			"What is the provenance of firewall-001.dc.corp.net? It may have been declared in an earlier response. Reply with ONLY the provenance.",
			"structural",
			true, // firewall-001 is a bare ref in call 2
		},
	}

	// Run for session and full formats
	for _, format := range []struct {
		name     string
		call1    string
		call2    string
	}{
		{"gcf_session", gcfSess1, gcfSess2},
		{"gcf_full", gcfFull1, gcfFull2},
	} {
		t.Logf("--- %s ---", format.name)

		correct := 0
		bareRefOK := 0
		bareRefTotal := 0

		for _, q := range prodQuestions {
			// Multi-turn: call 1 as first message, call 2 as second, question as third
			var resp string
			var err error

			switch backendName {
			case "google":
				apiKey := os.Getenv("GOOGLE_API_KEY")
				if apiKey == "" {
					t.Skip("GOOGLE_API_KEY required")
				}
				if model == "" {
					model = "gemini-2.5-flash"
				}
				contents := []map[string]any{
					{"role": "user", "parts": []map[string]string{{"text": "Tool response (call 1):\n\n" + format.call1}}},
					{"role": "model", "parts": []map[string]string{{"text": "Received. Ready for next tool response or questions."}}},
					{"role": "user", "parts": []map[string]string{{"text": "Tool response (call 2):\n\n" + format.call2}}},
					{"role": "model", "parts": []map[string]string{{"text": "Received. Ready for questions."}}},
					{"role": "user", "parts": []map[string]string{{"text": q.question}}},
				}
				resp, err = callGoogleMultiTurn(apiKey, model, contents)
			case "openai":
				apiKey := os.Getenv("OPENAI_API_KEY")
				if apiKey == "" {
					t.Skip("OPENAI_API_KEY required")
				}
				if model == "" {
					model = "gpt-4o"
				}
				msgs := []map[string]string{
					{"role": "user", "content": "Tool response (call 1):\n\n" + format.call1},
					{"role": "assistant", "content": "Received. Ready for next tool response or questions."},
					{"role": "user", "content": "Tool response (call 2):\n\n" + format.call2},
					{"role": "assistant", "content": "Received. Ready for questions."},
					{"role": "user", "content": q.question},
				}
				resp, err = callOpenAIMultiTurn(apiKey, model, msgs)
			default:
				t.Fatalf("Unsupported backend: %s", backendName)
			}

			if err != nil {
				t.Logf("  %s: ERROR %v", q.name, err)
				if q.needsBareRef {
					bareRefTotal++
				}
				continue
			}

			resp = strings.TrimSpace(resp)
			ok := verifySessionAnswer(q.expected, resp)

			if q.needsBareRef {
				bareRefTotal++
			}

			if ok {
				correct++
				if q.needsBareRef {
					bareRefOK++
				}
				t.Logf("  %s: PASS expected=%s got=%s", q.name, q.expected, resp)
			} else {
				t.Logf("  %s: FAIL expected=%s got=%s", q.name, q.expected, resp)
			}

			time.Sleep(1 * time.Second)
		}

		t.Logf("  Score: %d/%d, BareRef: %d/%d", correct, len(prodQuestions), bareRefOK, bareRefTotal)
		t.Logf("")
	}
}
