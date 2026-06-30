// Controlled experiment: edge arrow direction.
//
// Tests @tgt<@src (current spec) vs @src>@tgt (natural reading order)
// across multiple models and all edge-dependent scenarios.
//
// Same payload, same task, same model. Only the arrow direction changes.
// This isolates whether edge direction causes comprehension failures.
//
// Run one model:
//   EVAL_BACKEND=openai OPENAI_API_KEY=... OPENAI_BASE_URL=https://openrouter.ai/api/v1 EVAL_MODEL=x-ai/grok-build-0.1 GOWORK=off go test -run TestArrowAB -v -timeout 30m
//
// Run all OpenRouter models:
//   EVAL_BACKEND=openai OPENAI_API_KEY=... OPENAI_BASE_URL=https://openrouter.ai/api/v1 EVAL_MODEL=all GOWORK=off go test -run TestArrowAB -v -timeout 120m
package eval

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
)

// encodeFlippedArrow re-encodes a payload with @src>@tgt instead of @tgt<@src.
func encodeFlippedArrow(p *gcf.Payload) string {
	standard := gcf.Encode(p)

	var lines []string
	for _, line := range strings.Split(standard, "\n") {
		if strings.Contains(line, "<@") {
			var tgt, src int
			var edgeType string
			n, _ := fmt.Sscanf(line, "@%d<@%d %s", &tgt, &src, &edgeType)
			if n == 3 {
				line = fmt.Sprintf("@%d>@%d %s", src, tgt, edgeType)
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestArrowAB(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		t.Skip("Set EVAL_BACKEND")
	}
	modelEnv := os.Getenv("EVAL_MODEL")

	// Edge-dependent scenarios only
	scenarios := []Scenario{
		crossPackageScenario(),
		refactorTargetScenario(),
		circularDepScenario(),
		deadCodeScenario(),
	}

	sizes := []struct {
		name       string
		numSymbols int
		numEdges   int
	}{
		{"small", 20, 10},
		{"medium", 100, 50},
		{"large", 500, 200},
	}

	variants := []struct {
		name   string
		encode func(*gcf.Payload) string
	}{
		{"@tgt<@src", func(p *gcf.Payload) string { return gcf.Encode(p) }},
		{"@src>@tgt", encodeFlippedArrow},
	}

	// Multi-model support
	models := []string{modelEnv}
	if modelEnv == "all" {
		models = []string{
			"x-ai/grok-build-0.1",
			"deepseek/deepseek-chat",
			"meta-llama/llama-3.3-70b-instruct",
			"meta-llama/llama-4-maverick",
			"moonshotai/kimi-k2.7-code",
		}
	}

	t.Logf("%s", "="+strings.Repeat("=", 99))
	t.Logf("ARROW DIRECTION CONTROLLED EXPERIMENT")
	t.Logf("%s", "="+strings.Repeat("=", 99))
	t.Logf("")
	t.Logf("Variable: edge arrow direction")
	t.Logf("  A: @tgt<@src type  (current spec, right-to-left)")
	t.Logf("  B: @src>@tgt type  (natural reading, left-to-right)")
	t.Logf("Control: same payload, same task, same model")
	t.Logf("Scenarios: %d (edge-dependent only)", len(scenarios))
	t.Logf("Sizes: %d, Models: %d", len(sizes), len(models))
	t.Logf("Total calls: %d", len(scenarios)*len(sizes)*len(variants)*len(models))
	t.Logf("")

	type result struct {
		model, scenario, size, variant string
		pass                           bool
	}
	var results []result

	for _, model := range models {
		t.Logf("--- Model: %s ---", model)
		t.Logf("")

		for _, scenario := range scenarios {
			for _, size := range sizes {
				p := scenario.BuildPayload(size.numSymbols, size.numEdges)
				expected := scenario.GroundTruth(p)

				for _, v := range variants {
					encoded := v.encode(p)

					prompt := fmt.Sprintf(
						"You are an AI coding assistant. You have access to the following tools:\n\n%s\n\n"+
							"You just received the following tool response:\n\n%s\n\n"+
							"Task: %s\n\n"+
							"Respond with a JSON object: {\"tool\": \"<name>\", \"arguments\": {<key>: <value>}}\n"+
							"If no action is warranted: {\"tool\": \"no_action\", \"arguments\": {}}\n"+
							"Respond with ONLY the JSON. No explanation.",
						scenario.ToolSchemas, encoded, scenario.Instruction,
					)

					var resp string
					var err error

					switch backendName {
					case "openai":
						apiKey := os.Getenv("OPENAI_API_KEY")
						resp, err = callOpenAI(apiKey, model, prompt)
					case "google":
						apiKey := os.Getenv("GOOGLE_API_KEY")
						resp, err = callGoogle(apiKey, model, prompt)
					default:
						t.Fatalf("Unsupported backend: %s", backendName)
					}

					if err != nil {
						t.Logf("  %s/%s/%s: ERROR %v", scenario.Name, size.name, v.name, err)
						results = append(results, result{model, scenario.Name, size.name, v.name, false})
						continue
					}

					sr := scoreResponse(expected, resp)
					pass := sr.Overall
					results = append(results, result{model, scenario.Name, size.name, v.name, pass})

					status := "PASS"
					if !pass {
						status = "FAIL"
					}
					t.Logf("  %s/%s/%s: %s", scenario.Name, size.name, v.name, status)
					if !pass {
						t.Logf("    expected: %s %v", expected.Tool, expected.Arguments)
						t.Logf("    got:      %s", strings.TrimSpace(resp))
					}

					time.Sleep(1 * time.Second)
				}
			}
		}
		t.Logf("")
	}

	// Summary per model
	t.Logf("%s", "="+strings.Repeat("=", 99))
	t.Logf("SUMMARY")
	t.Logf("%s", "="+strings.Repeat("=", 99))
	t.Logf("")

	for _, model := range models {
		stdPass, stdTotal := 0, 0
		flipPass, flipTotal := 0, 0
		for _, r := range results {
			if r.model != model {
				continue
			}
			if r.variant == "@tgt<@src" {
				stdTotal++
				if r.pass {
					stdPass++
				}
			} else {
				flipTotal++
				if r.pass {
					flipPass++
				}
			}
		}
		shortModel := model
		if idx := strings.LastIndex(model, "/"); idx >= 0 {
			shortModel = model[idx+1:]
		}
		t.Logf("%-30s  @tgt<@src: %2d/%-2d (%3.0f%%)   @src>@tgt: %2d/%-2d (%3.0f%%)   delta: %+.0f%%",
			shortModel,
			stdPass, stdTotal, pctSafe(stdPass, stdTotal),
			flipPass, flipTotal, pctSafe(flipPass, flipTotal),
			pctSafe(flipPass, flipTotal)-pctSafe(stdPass, stdTotal),
		)
	}

	// Per-scenario breakdown
	t.Logf("")
	t.Logf("Per scenario (all models combined):")
	for _, scenario := range scenarios {
		stdPass, stdTotal := 0, 0
		flipPass, flipTotal := 0, 0
		for _, r := range results {
			if r.scenario != scenario.Name {
				continue
			}
			if r.variant == "@tgt<@src" {
				stdTotal++
				if r.pass {
					stdPass++
				}
			} else {
				flipTotal++
				if r.pass {
					flipPass++
				}
			}
		}
		t.Logf("  %-24s  @tgt<@src: %2d/%-2d   @src>@tgt: %2d/%-2d   delta: %+.0f%%",
			scenario.Name,
			stdPass, stdTotal,
			flipPass, flipTotal,
			pctSafe(flipPass, flipTotal)-pctSafe(stdPass, stdTotal),
		)
	}
}

func pctSafe(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}
