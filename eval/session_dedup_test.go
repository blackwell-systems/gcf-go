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

	return []*sessionCall{
		{
			CallNum: 1, GCFSession: gcfSess1, GCFFull: gcfFull1, JSON: string(json1),
			SymbolCount: 50, EdgeCount: 80, NewSymbols: 50, BareRefs: 0,
			Questions: []sessionQuestion{
				{"symbol_count", "How many symbols are in this context? Reply with ONLY a number.", "50", false},
				{"edge_count", "How many edges are in this context? Reply with ONLY a number.", "80", false},
				{"target_kind", fmt.Sprintf("What is the kind of the symbol %s? Reply with ONLY the kind (function, type, method, or interface).", targetShort), targetKind, false},
				{"function_count", "How many symbols have kind 'function' (or 'fn')? Reply with ONLY a number.", fmt.Sprintf("%d", countKind(p1, "function")), false},
			},
		},
		{
			CallNum: 2, GCFSession: gcfSess2, GCFFull: gcfFull2, JSON: string(json2),
			SymbolCount: 60, EdgeCount: 100, NewSymbols: 10, BareRefs: 50,
			Questions: []sessionQuestion{
				{"symbol_count", "How many symbols are in the current context (including previously transmitted ones)? Reply with ONLY a number.", "60", true},
				{"edge_count", "How many edges are in the current context? Reply with ONLY a number.", "100", false},
				{"target_kind", fmt.Sprintf("What is the kind of the symbol %s? It may have been declared in a previous tool response. Reply with ONLY the kind (function, type, method, or interface).", targetShort), targetKind, true},
				{"function_count", "How many symbols have kind 'function' (or 'fn'), including previously transmitted ones? Reply with ONLY a number.", fmt.Sprintf("%d", countKind(p2, "function")), true},
			},
		},
		{
			CallNum: 3, GCFSession: gcfSess3, GCFFull: gcfFull3, JSON: string(json3),
			SymbolCount: 65, EdgeCount: 110, NewSymbols: 5, BareRefs: 60,
			Questions: []sessionQuestion{
				{"symbol_count", "How many symbols are in the current context (including all previously transmitted ones)? Reply with ONLY a number.", "65", true},
				{"edge_count", "How many edges are in the current context? Reply with ONLY a number.", "110", false},
				{"target_kind", fmt.Sprintf("What is the kind of the symbol %s? It was declared in an earlier tool response. Reply with ONLY the kind (function, type, method, or interface).", targetShort), targetKind, true},
				{"function_count", "How many symbols have kind 'function' (or 'fn'), across all tool responses? Reply with ONLY a number.", fmt.Sprintf("%d", countKind(p3, "function")), true},
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
