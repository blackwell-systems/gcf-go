// Operational eval: after reading data in format X, does the model make the
// correct tool call? Tests extraction + decision-making + argument construction.
//
// Ground truth is deterministic, computed from the payload. No LLM judge.
// Same data encoded in GCF, JSON, and TOON. Same task instruction.
//
// 12 scenarios x 3 sizes (20, 100, 500) x 3 formats = 108 LLM calls per run.
//
// Run:
//
//	cd gcf-go/eval
//	EVAL_BACKEND=google GOOGLE_API_KEY=... EVAL_MODEL=gemini-2.5-flash GOWORK=off go test -run TestOperational -v -timeout 60m
//	EVAL_BACKEND=openai OPENAI_API_KEY=... EVAL_MODEL=gpt-5.5 GOWORK=off go test -run TestOperational -v -timeout 60m
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	gcf "github.com/blackwell-systems/gcf-go"
)

// ToolCall is the expected model output.
type ToolCall struct {
	Tool      string            `json:"tool"`
	Arguments map[string]string `json:"arguments"`
}

// Scenario defines one operational eval scenario.
type Scenario struct {
	Name        string
	Category    string
	Instruction string
	ToolSchemas string // JSON schema of available tools
	BuildPayload func(numSymbols, numEdges int) *gcf.Payload
	GroundTruth  func(p *gcf.Payload) ToolCall
}

// ScoreResult captures the 5-dimension scoring.
type ScoreResult struct {
	ToolCorrect     bool
	ArgsComplete    bool
	ArgsCorrect     bool
	NoHallucination bool
	Overall         bool
}

func scoreResponse(expected ToolCall, response string) ScoreResult {
	response = strings.TrimSpace(response)
	// Strip markdown code fences
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var got ToolCall
	if err := json.Unmarshal([]byte(response), &got); err != nil {
		// Try to find JSON in response
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start >= 0 && end > start {
			json.Unmarshal([]byte(response[start:end+1]), &got)
		}
	}

	r := ScoreResult{}

	// 1. Tool selection
	r.ToolCorrect = strings.EqualFold(got.Tool, expected.Tool)

	// 2. Argument completeness (all required keys present)
	r.ArgsComplete = true
	for k := range expected.Arguments {
		if _, ok := got.Arguments[k]; !ok {
			r.ArgsComplete = false
			break
		}
	}

	// 3. Argument correctness (every value matches)
	r.ArgsCorrect = r.ArgsComplete
	if r.ArgsComplete {
		for k, v := range expected.Arguments {
			gotV := got.Arguments[k]
			if !valuesMatch(v, gotV) {
				r.ArgsCorrect = false
				break
			}
		}
	}

	// 4. No hallucination (no extra arguments beyond schema)
	r.NoHallucination = true
	for k := range got.Arguments {
		if _, ok := expected.Arguments[k]; !ok {
			r.NoHallucination = false
			break
		}
	}

	// 5. Overall
	r.Overall = r.ToolCorrect && r.ArgsComplete && r.ArgsCorrect && r.NoHallucination

	return r
}

func valuesMatch(expected, got string) bool {
	expected = strings.TrimSpace(strings.ToLower(expected))
	got = strings.TrimSpace(strings.ToLower(got))

	if expected == got {
		return true
	}

	// Handle comma-separated lists (order-insensitive)
	if strings.Contains(expected, ",") {
		eParts := sortedParts(expected)
		gParts := sortedParts(got)
		return eParts == gParts
	}

	return false
}

func sortedParts(s string) string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// ═══════════════════════════════════════════════════════════════════════════
// Scenario builders
// ═══════════════════════════════════════════════════════════════════════════

func buildScenarios() []Scenario {
	return []Scenario{
		// A. Tool selection (data tells you which tool)
		deadCodeScenario(),
		circularDepScenario(),
		missingImplScenario(),

		// B. Argument construction from extracted fields
		refactorTargetScenario(),
		testCoverageScenario(),
		crossPackageScenario(),

		// C. Conditional logic (data says "don't act")
		noDeadCodeScenario(),
		ambiguousTargetScenario(),

		// D. Multi-step
		filterThenActScenario(),

		// E. Precision under scale
		needleScenario(),
		aggregateScenario(),
		highestScoreScenario(),
	}
}

// --- A1: Dead code detection ---

func deadCodeScenario() Scenario {
	return Scenario{
		Name:     "dead_code",
		Category: "tool_selection",
		Instruction: "Analyze the code graph. If any function has zero incoming calls (no other symbol calls it), " +
			"it is dead code and should be deleted. If there is dead code, respond with the appropriate tool call. " +
			"If all functions have callers, respond with no_action.",
		ToolSchemas: `[
			{"name": "delete_symbol", "description": "Delete a dead code symbol", "parameters": {"symbol": "qualified name of the symbol to delete"}},
			{"name": "no_action", "description": "No action needed", "parameters": {}},
			{"name": "refactor_symbol", "description": "Refactor a symbol", "parameters": {"symbol": "qualified name", "reason": "why"}},
			{"name": "add_test", "description": "Add test coverage", "parameters": {"symbol": "qualified name"}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Ensure every symbol except symbol 0 has at least one incoming edge
			deadQN := p.Symbols[0].QualifiedName
			incoming := map[string]int{}
			for _, e := range p.Edges {
				incoming[e.Target]++
			}
			// Remove any edges targeting the dead symbol
			var filtered []gcf.Edge
			for _, e := range p.Edges {
				if e.Target != deadQN {
					filtered = append(filtered, e)
				}
			}
			// Add edges to any symbol that has zero incoming (except the dead one)
			for i, s := range p.Symbols {
				if s.QualifiedName == deadQN {
					continue
				}
				if incoming[s.QualifiedName] == 0 {
					src := p.Symbols[(i+3)%len(p.Symbols)]
					if src.QualifiedName == deadQN {
						src = p.Symbols[(i+5)%len(p.Symbols)]
					}
					filtered = append(filtered, gcf.Edge{
						Source:   src.QualifiedName,
						Target:   s.QualifiedName,
						EdgeType: "calls",
					})
				}
			}
			p.Edges = filtered
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			// Find the symbol with zero incoming edges (should be exactly one)
			incomingCount := map[string]int{}
			for _, s := range p.Symbols {
				incomingCount[s.QualifiedName] = 0
			}
			for _, e := range p.Edges {
				incomingCount[e.Target]++
			}
			for _, s := range p.Symbols {
				if incomingCount[s.QualifiedName] == 0 {
					return ToolCall{
						Tool:      "delete_symbol",
						Arguments: map[string]string{"symbol": s.QualifiedName},
					}
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- A2: Circular dependency ---

func circularDepScenario() Scenario {
	return Scenario{
		Name:     "circular_dep",
		Category: "tool_selection",
		Instruction: "Analyze the code graph for circular dependencies. A cycle exists when symbol A calls B and B calls A " +
			"(directly or through a chain). If you find a cycle, report it. List the symbols involved sorted alphabetically by their short name (after the last dot).",
		ToolSchemas: `[
			{"name": "report_cycle", "description": "Report a dependency cycle", "parameters": {"symbols": "comma-separated list of short symbol names in the cycle, sorted alphabetically"}},
			{"name": "no_action", "description": "No cycles found", "parameters": {}},
			{"name": "delete_symbol", "description": "Delete a symbol", "parameters": {"symbol": "qualified name"}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Inject a cycle between symbols 0 and 1
			s0 := p.Symbols[0].QualifiedName
			s1 := p.Symbols[1].QualifiedName
			// Ensure both directions exist
			hasForward, hasBack := false, false
			for _, e := range p.Edges {
				if e.Source == s0 && e.Target == s1 {
					hasForward = true
				}
				if e.Source == s1 && e.Target == s0 {
					hasBack = true
				}
			}
			if !hasForward {
				p.Edges = append(p.Edges, gcf.Edge{Source: s0, Target: s1, EdgeType: "calls"})
			}
			if !hasBack {
				p.Edges = append(p.Edges, gcf.Edge{Source: s1, Target: s0, EdgeType: "calls"})
			}
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			// Find the cycle (symbols 0 and 1)
			s0 := shortName(p.Symbols[0].QualifiedName)
			s1 := shortName(p.Symbols[1].QualifiedName)
			names := []string{s0, s1}
			sort.Strings(names)
			return ToolCall{
				Tool:      "report_cycle",
				Arguments: map[string]string{"symbols": strings.Join(names, ", ")},
			}
		},
	}
}

// --- A3: Missing implementation ---

func missingImplScenario() Scenario {
	return Scenario{
		Name:     "missing_impl",
		Category: "tool_selection",
		Instruction: "Analyze the code graph. Find any interface that has zero 'implements' edges pointing to it " +
			"(no type implements it). If found, use find_implementations to search for implementations. " +
			"If all interfaces have implementations, respond with no_action.",
		ToolSchemas: `[
			{"name": "find_implementations", "description": "Search for implementations of an interface", "parameters": {"interface": "qualified name of the interface"}},
			{"name": "no_action", "description": "All interfaces have implementations", "parameters": {}},
			{"name": "delete_symbol", "description": "Delete a symbol", "parameters": {"symbol": "qualified name"}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Find first interface: this will be the unimplemented one
			var targetIface string
			for _, s := range p.Symbols {
				if s.Kind == "interface" {
					targetIface = s.QualifiedName
					break
				}
			}
			if targetIface == "" {
				p.Symbols[3].Kind = "interface"
				targetIface = p.Symbols[3].QualifiedName
			}
			// Remove all implements edges targeting the target interface
			var filtered []gcf.Edge
			for _, e := range p.Edges {
				if !(e.Target == targetIface && e.EdgeType == "implements") {
					filtered = append(filtered, e)
				}
			}
			// Ensure every OTHER interface has at least one implements edge
			implTargets := map[string]bool{}
			for _, e := range filtered {
				if e.EdgeType == "implements" {
					implTargets[e.Target] = true
				}
			}
			for i, s := range p.Symbols {
				if s.Kind == "interface" && s.QualifiedName != targetIface && !implTargets[s.QualifiedName] {
					// Add an implements edge from a non-interface symbol
					src := p.Symbols[(i+2)%len(p.Symbols)]
					if src.Kind == "interface" {
						src = p.Symbols[(i+5)%len(p.Symbols)]
					}
					filtered = append(filtered, gcf.Edge{
						Source:   src.QualifiedName,
						Target:   s.QualifiedName,
						EdgeType: "implements",
					})
				}
			}
			p.Edges = filtered
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			// Should be exactly one interface with zero implements edges
			implTargets := map[string]bool{}
			for _, e := range p.Edges {
				if e.EdgeType == "implements" {
					implTargets[e.Target] = true
				}
			}
			for _, s := range p.Symbols {
				if s.Kind == "interface" && !implTargets[s.QualifiedName] {
					return ToolCall{
						Tool:      "find_implementations",
						Arguments: map[string]string{"interface": s.QualifiedName},
					}
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- B1: Refactor target ---

func refactorTargetScenario() Scenario {
	return Scenario{
		Name:     "refactor_target",
		Category: "argument_construction",
		Instruction: "Find the function with the most incoming 'calls' edges (highest blast radius). " +
			"Extract it into a new function. List all its callers (short names, sorted alphabetically).",
		ToolSchemas: `[
			{"name": "extract_function", "description": "Extract a function for refactoring", "parameters": {"symbol": "qualified name of function to extract", "callers": "comma-separated short names of callers, sorted alphabetically"}},
			{"name": "no_action", "description": "No refactoring needed", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Make symbol 2 the hot target: add extra calls edges to it
			hotQN := p.Symbols[2].QualifiedName
			for i := 4; i < min(numSymbols, 10); i++ {
				p.Edges = append(p.Edges, gcf.Edge{
					Source:   p.Symbols[i].QualifiedName,
					Target:   hotQN,
					EdgeType: "calls",
				})
			}
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			// Count incoming calls edges per symbol
			callCount := map[string]int{}
			callers := map[string][]string{}
			for _, e := range p.Edges {
				if e.EdgeType == "calls" {
					callCount[e.Target]++
					callers[e.Target] = append(callers[e.Target], shortName(e.Source))
				}
			}
			// Find max
			maxQN := ""
			maxCount := 0
			for _, s := range p.Symbols {
				if callCount[s.QualifiedName] > maxCount {
					maxCount = callCount[s.QualifiedName]
					maxQN = s.QualifiedName
				}
			}
			callerList := callers[maxQN]
			sort.Strings(callerList)
			// Deduplicate
			seen := map[string]bool{}
			var deduped []string
			for _, c := range callerList {
				if !seen[c] {
					seen[c] = true
					deduped = append(deduped, c)
				}
			}
			return ToolCall{
				Tool: "extract_function",
				Arguments: map[string]string{
					"symbol":  maxQN,
					"callers": strings.Join(deduped, ", "),
				},
			}
		},
	}
}

// --- B2: Test coverage gap ---

func testCoverageScenario() Scenario {
	return Scenario{
		Name:     "test_coverage",
		Category: "argument_construction",
		Instruction: "Find any symbol with provenance 'ast_inferred' and score above 0.80. " +
			"These are high-confidence symbols lacking direct LSP resolution, meaning they likely lack test coverage. " +
			"Use get_tests_for_file with the symbol's package path (everything before the last dot). " +
			"If no such symbol exists, respond with no_action.",
		ToolSchemas: `[
			{"name": "get_tests_for_file", "description": "Find tests for a file/package", "parameters": {"package": "package path", "symbol": "qualified name of the symbol"}},
			{"name": "no_action", "description": "No coverage gaps found", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Set target symbol to ast_inferred + high score
			p.Symbols[1].Provenance = "ast_inferred"
			p.Symbols[1].Score = 0.88
			// Ensure no OTHER symbol has ast_inferred + score > 0.80
			for i := range p.Symbols {
				if i == 1 {
					continue
				}
				if p.Symbols[i].Provenance == "ast_inferred" && p.Symbols[i].Score > 0.80 {
					p.Symbols[i].Provenance = "lsp_resolved"
				}
			}
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			for _, s := range p.Symbols {
				if s.Provenance == "ast_inferred" && s.Score > 0.80 {
					pkg := s.QualifiedName
					if dot := strings.LastIndex(pkg, "."); dot >= 0 {
						pkg = pkg[:dot]
					}
					return ToolCall{
						Tool: "get_tests_for_file",
						Arguments: map[string]string{
							"package": pkg,
							"symbol":  s.QualifiedName,
						},
					}
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- B3: Cross-package dependency ---

func crossPackageScenario() Scenario {
	return Scenario{
		Name:     "cross_package",
		Category: "argument_construction",
		Instruction: "Find the first 'imports' edge where source and target are in different packages. " +
			"A package is everything up to the last dot in the qualified name. " +
			"Report it using check_dependency with the source and target package paths.",
		ToolSchemas: `[
			{"name": "check_dependency", "description": "Check a cross-package dependency", "parameters": {"source_package": "source package path", "target_package": "target package path"}},
			{"name": "no_action", "description": "No cross-package dependencies", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			return buildFixture(numSymbols, numEdges)
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			for _, e := range p.Edges {
				if e.EdgeType == "imports" {
					srcPkg := packageOf(e.Source)
					tgtPkg := packageOf(e.Target)
					if srcPkg != tgtPkg {
						return ToolCall{
							Tool: "check_dependency",
							Arguments: map[string]string{
								"source_package": srcPkg,
								"target_package": tgtPkg,
							},
						}
					}
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- C1: No dead code ---

func noDeadCodeScenario() Scenario {
	return Scenario{
		Name:     "no_dead_code",
		Category: "conditional",
		Instruction: "Analyze the code graph. If any function has zero incoming calls (no other symbol calls it), " +
			"it is dead code and should be deleted. If all functions have callers, respond with no_action.",
		ToolSchemas: `[
			{"name": "delete_symbol", "description": "Delete dead code", "parameters": {"symbol": "qualified name"}},
			{"name": "no_action", "description": "No dead code found", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Ensure every symbol has at least one incoming edge
			incomingCount := map[string]int{}
			for _, e := range p.Edges {
				incomingCount[e.Target]++
			}
			for _, s := range p.Symbols {
				if incomingCount[s.QualifiedName] == 0 {
					// Add a calls edge to it from a random other symbol
					src := p.Symbols[(indexOf(p, s.QualifiedName)+3)%len(p.Symbols)]
					p.Edges = append(p.Edges, gcf.Edge{
						Source:   src.QualifiedName,
						Target:   s.QualifiedName,
						EdgeType: "calls",
					})
				}
			}
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- C2: Ambiguous target ---

func ambiguousTargetScenario() Scenario {
	return Scenario{
		Name:     "ambiguous_target",
		Category: "conditional",
		Instruction: "Find the function with the most incoming 'calls' edges (highest blast radius). " +
			"If there is a single winner, extract it. If two or more functions tie for the highest count, " +
			"respond with report_ambiguity listing the tied symbols.",
		ToolSchemas: `[
			{"name": "extract_function", "description": "Extract a function", "parameters": {"symbol": "qualified name"}},
			{"name": "report_ambiguity", "description": "Report ambiguous target", "parameters": {"symbols": "comma-separated short names of tied symbols, sorted alphabetically"}},
			{"name": "no_action", "description": "No action needed", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Make symbols 0 and 1 tie for highest calls count
			s0 := p.Symbols[0].QualifiedName
			s1 := p.Symbols[1].QualifiedName
			// Clear existing calls to these two
			var filtered []gcf.Edge
			for _, e := range p.Edges {
				if e.EdgeType == "calls" && (e.Target == s0 || e.Target == s1) {
					continue
				}
				filtered = append(filtered, e)
			}
			p.Edges = filtered
			// Add exactly 5 callers to each
			for i := 2; i < min(7, numSymbols); i++ {
				p.Edges = append(p.Edges, gcf.Edge{Source: p.Symbols[i].QualifiedName, Target: s0, EdgeType: "calls"})
				p.Edges = append(p.Edges, gcf.Edge{Source: p.Symbols[i].QualifiedName, Target: s1, EdgeType: "calls"})
			}
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			callCount := map[string]int{}
			for _, e := range p.Edges {
				if e.EdgeType == "calls" {
					callCount[e.Target]++
				}
			}
			maxCount := 0
			for _, c := range callCount {
				if c > maxCount {
					maxCount = c
				}
			}
			var tied []string
			for _, s := range p.Symbols {
				if callCount[s.QualifiedName] == maxCount {
					tied = append(tied, shortName(s.QualifiedName))
				}
			}
			sort.Strings(tied)
			return ToolCall{
				Tool:      "report_ambiguity",
				Arguments: map[string]string{"symbols": strings.Join(tied, ", ")},
			}
		},
	}
}

// --- D1: Filter then act ---

func filterThenActScenario() Scenario {
	return Scenario{
		Name:     "filter_then_act",
		Category: "multi_step",
		Instruction: "Find all symbols with score below 0.30. These are low-confidence symbols that should be deprecated. " +
			"If 3 or more such symbols exist, use bulk_deprecate with the list of their short names sorted alphabetically. " +
			"If fewer than 3, respond with no_action.",
		ToolSchemas: `[
			{"name": "bulk_deprecate", "description": "Deprecate multiple low-confidence symbols", "parameters": {"symbols": "comma-separated short names, sorted alphabetically", "count": "number of symbols"}},
			{"name": "no_action", "description": "Not enough low-confidence symbols", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			return buildFixture(numSymbols, numEdges)
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			seen := map[string]bool{}
			var lowConf []string
			for _, s := range p.Symbols {
				if s.Score < 0.30 {
					sn := shortName(s.QualifiedName)
					if !seen[sn] {
						seen[sn] = true
						lowConf = append(lowConf, sn)
					}
				}
			}
			sort.Strings(lowConf)
			if len(lowConf) >= 3 {
				return ToolCall{
					Tool: "bulk_deprecate",
					Arguments: map[string]string{
						"symbols": strings.Join(lowConf, ", "),
						"count":   fmt.Sprintf("%d", len(lowConf)),
					},
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- E1: Needle in haystack ---

func needleScenario() Scenario {
	return Scenario{
		Name:     "needle_interface",
		Category: "precision",
		Instruction: "Find the one interface in the 'extended' group (distance 2). " +
			"Use promote_symbol to move it to the targets group. " +
			"If there is no interface in the extended group, respond with no_action.",
		ToolSchemas: `[
			{"name": "promote_symbol", "description": "Move a symbol to a higher priority group", "parameters": {"symbol": "qualified name of the interface"}},
			{"name": "no_action", "description": "No interface found in extended group", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			p := buildFixture(numSymbols, numEdges)
			// Ensure exactly one interface in extended group
			// First, make all extended interfaces into functions
			for i := range p.Symbols {
				if p.Symbols[i].Distance == 2 && p.Symbols[i].Kind == "interface" {
					p.Symbols[i].Kind = "function"
				}
			}
			// Then make one specific extended symbol an interface
			for i := range p.Symbols {
				if p.Symbols[i].Distance == 2 {
					p.Symbols[i].Kind = "interface"
					break
				}
			}
			return p
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			for _, s := range p.Symbols {
				if s.Distance == 2 && s.Kind == "interface" {
					return ToolCall{
						Tool:      "promote_symbol",
						Arguments: map[string]string{"symbol": s.QualifiedName},
					}
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- E2: Aggregate then decide ---

func aggregateScenario() Scenario {
	return Scenario{
		Name:     "aggregate_functions",
		Category: "precision",
		Instruction: "Count the number of symbols with kind 'function' (or 'fn'). " +
			"If functions make up more than 20%% of all symbols, use report_ratio. " +
			"Otherwise, respond with no_action.",
		ToolSchemas: `[
			{"name": "report_ratio", "description": "Report function ratio", "parameters": {"function_count": "number of functions", "total_count": "total number of symbols", "percentage": "percentage as integer"}},
			{"name": "no_action", "description": "Function ratio is acceptable", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			return buildFixture(numSymbols, numEdges)
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			fnCount := 0
			for _, s := range p.Symbols {
				if s.Kind == "function" {
					fnCount++
				}
			}
			pct := fnCount * 100 / len(p.Symbols)
			if pct > 20 {
				return ToolCall{
					Tool: "report_ratio",
					Arguments: map[string]string{
						"function_count": fmt.Sprintf("%d", fnCount),
						"total_count":    fmt.Sprintf("%d", len(p.Symbols)),
						"percentage":     fmt.Sprintf("%d", pct),
					},
				}
			}
			return ToolCall{Tool: "no_action", Arguments: map[string]string{}}
		},
	}
}

// --- E3: Highest score extraction ---

func highestScoreScenario() Scenario {
	return Scenario{
		Name:     "highest_score",
		Category: "precision",
		Instruction: "Find the symbol with the highest score. Report its qualified name, kind, and score using inspect_symbol.",
		ToolSchemas: `[
			{"name": "inspect_symbol", "description": "Inspect a specific symbol", "parameters": {"symbol": "qualified name", "kind": "symbol kind", "score": "score as decimal string"}},
			{"name": "no_action", "description": "No action needed", "parameters": {}}
		]`,
		BuildPayload: func(numSymbols, numEdges int) *gcf.Payload {
			return buildFixture(numSymbols, numEdges)
		},
		GroundTruth: func(p *gcf.Payload) ToolCall {
			best := p.Symbols[0]
			for _, s := range p.Symbols[1:] {
				if s.Score > best.Score {
					best = s
				}
			}
			return ToolCall{
				Tool: "inspect_symbol",
				Arguments: map[string]string{
					"symbol": best.QualifiedName,
					"kind":   best.Kind,
					"score":  fmt.Sprintf("%.2f", best.Score),
				},
			}
		},
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

func shortName(qn string) string {
	if dot := strings.LastIndex(qn, "."); dot >= 0 {
		return qn[dot+1:]
	}
	return qn
}

func packageOf(qn string) string {
	if dot := strings.LastIndex(qn, "."); dot >= 0 {
		return qn[:dot]
	}
	return qn
}

func indexOf(p *gcf.Payload, qn string) int {
	for i, s := range p.Symbols {
		if s.QualifiedName == qn {
			return i
		}
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ═══════════════════════════════════════════════════════════════════════════
// Test harness
// ═══════════════════════════════════════════════════════════════════════════

func TestOperational(t *testing.T) {
	backendName := os.Getenv("EVAL_BACKEND")
	if backendName == "" {
		backendName = "cli"
	}
	model := os.Getenv("EVAL_MODEL")

	scenarios := buildScenarios()
	sizes := []struct {
		name       string
		numSymbols int
		numEdges   int
	}{
		{"small", 20, 10},
		{"medium", 100, 50},
		{"large", 500, 200},
	}

	type formatDef struct {
		name   string
		encode func(p *gcf.Payload) string
	}
	formats := []formatDef{
		{"gcf", func(p *gcf.Payload) string { return gcf.Encode(p) }},
		{"json", func(p *gcf.Payload) string { b, _ := json.MarshalIndent(p, "", "  "); return string(b) }},
		{"toon", func(p *gcf.Payload) string { s, _ := encodeTOON(p); return s }},
	}

	t.Logf("=== Operational Eval ===")
	t.Logf("Backend: %s, Model: %s", backendName, model)
	t.Logf("Scenarios: %d, Sizes: %d, Formats: %d", len(scenarios), len(sizes), len(formats))
	t.Logf("")

	// Per-format aggregate scores
	type formatScore struct {
		toolOK, argsComplete, argsCorrect, noHalluc, overall, total int
	}
	formatScores := map[string]*formatScore{}
	for _, f := range formats {
		formatScores[f.name] = &formatScore{}
	}

	// Per-scenario results for summary table
	type scenarioFormatResult struct {
		overall int
		total   int
	}
	scenarioResults := map[string]map[string]map[string]*scenarioFormatResult{} // scenario -> size -> format -> result

	for _, scenario := range scenarios {
		t.Logf("--- %s (%s) ---", scenario.Name, scenario.Category)

		for _, size := range sizes {
			p := scenario.BuildPayload(size.numSymbols, size.numEdges)
			expected := scenario.GroundTruth(p)

			if _, ok := scenarioResults[scenario.Name]; !ok {
				scenarioResults[scenario.Name] = map[string]map[string]*scenarioFormatResult{}
			}
			if _, ok := scenarioResults[scenario.Name][size.name]; !ok {
				scenarioResults[scenario.Name][size.name] = map[string]*scenarioFormatResult{}
			}

			for _, f := range formats {
				encoded := f.encode(p)

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
				case "cli":
					resp, err = callCLI(prompt)
				case "api":
					apiKey := os.Getenv("ANTHROPIC_API_KEY")
					if apiKey == "" {
						t.Skip("ANTHROPIC_API_KEY required")
					}
					resp, err = callAPI(apiKey, model, prompt)
				case "openai":
					apiKey := os.Getenv("OPENAI_API_KEY")
					if apiKey == "" {
						t.Skip("OPENAI_API_KEY required")
					}
					if model == "" {
						model = "gpt-4o"
					}
					resp, err = callOpenAI(apiKey, model, prompt)
				case "google":
					apiKey := os.Getenv("GOOGLE_API_KEY")
					if apiKey == "" {
						t.Skip("GOOGLE_API_KEY required")
					}
					if model == "" {
						model = "gemini-2.5-flash"
					}
					resp, err = callGoogle(apiKey, model, prompt)
				default:
					t.Fatalf("Unknown backend: %s", backendName)
				}

				if err != nil {
					t.Logf("  %s/%s/%s: ERROR %v", scenario.Name, size.name, f.name, err)
					formatScores[f.name].total++
					scenarioResults[scenario.Name][size.name][f.name] = &scenarioFormatResult{0, 1}
					continue
				}

				result := scoreResponse(expected, resp)
				fs := formatScores[f.name]
				fs.total++
				if result.ToolCorrect {
					fs.toolOK++
				}
				if result.ArgsComplete {
					fs.argsComplete++
				}
				if result.ArgsCorrect {
					fs.argsCorrect++
				}
				if result.NoHallucination {
					fs.noHalluc++
				}
				if result.Overall {
					fs.overall++
				}

				sr := &scenarioFormatResult{total: 1}
				if result.Overall {
					sr.overall = 1
				}
				scenarioResults[scenario.Name][size.name][f.name] = sr

				status := "PASS"
				if !result.Overall {
					status = "FAIL"
				}
				detail := fmt.Sprintf("tool=%v args_complete=%v args_correct=%v no_halluc=%v",
					result.ToolCorrect, result.ArgsComplete, result.ArgsCorrect, result.NoHallucination)

				t.Logf("  %s/%s/%s: %s (%s)", scenario.Name, size.name, f.name, status, detail)
				if !result.Overall {
					t.Logf("    expected: %s %v", expected.Tool, expected.Arguments)
					t.Logf("    got:      %s", strings.TrimSpace(resp))
				}

				time.Sleep(1 * time.Second)
			}
		}
		t.Logf("")
	}

	// Summary table
	t.Logf("%s", "="+strings.Repeat("=", 99))
	t.Logf("SUMMARY")
	t.Logf("%s", "="+strings.Repeat("=", 99))
	t.Logf("")

	// Per-scenario breakdown
	t.Logf("%-24s %6s  %-14s %-14s %-14s",
		"Scenario", "Size", "GCF", "TOON", "JSON")
	t.Logf("%s", strings.Repeat("-", 80))
	for _, scenario := range scenarios {
		for _, size := range sizes {
			gcfR := scenarioResults[scenario.Name][size.name]["gcf"]
			toonR := scenarioResults[scenario.Name][size.name]["toon"]
			jsonR := scenarioResults[scenario.Name][size.name]["json"]

			fmtResult := func(r *scenarioFormatResult) string {
				if r == nil {
					return "N/A"
				}
				return fmt.Sprintf("%d/%d", r.overall, r.total)
			}
			t.Logf("%-24s %6s  %-14s %-14s %-14s",
				scenario.Name, size.name, fmtResult(gcfR), fmtResult(toonR), fmtResult(jsonR))
		}
	}

	// Per-format totals
	t.Logf("")
	t.Logf("%-8s %8s %8s %10s %10s %10s",
		"Format", "Overall", "ToolSel", "ArgCompl", "ArgCorr", "NoHalluc")
	t.Logf("%s", strings.Repeat("-", 60))
	for _, f := range formats {
		fs := formatScores[f.name]
		pct := func(n, d int) string {
			if d == 0 {
				return "N/A"
			}
			return fmt.Sprintf("%.1f%%", float64(n)/float64(d)*100)
		}
		t.Logf("%-8s %8s %8s %10s %10s %10s",
			f.name,
			pct(fs.overall, fs.total),
			pct(fs.toolOK, fs.total),
			pct(fs.argsComplete, fs.total),
			pct(fs.argsCorrect, fs.total),
			pct(fs.noHalluc, fs.total),
		)
	}
}

// TestOperationalGroundTruth verifies ground truth functions without LLM calls.
func TestOperationalGroundTruth(t *testing.T) {
	scenarios := buildScenarios()
	sizes := []struct {
		numSymbols int
		numEdges   int
	}{
		{20, 10},
		{100, 50},
		{500, 200},
	}

	for _, scenario := range scenarios {
		for _, size := range sizes {
			p := scenario.BuildPayload(size.numSymbols, size.numEdges)
			gt := scenario.GroundTruth(p)

			if gt.Tool == "" {
				t.Errorf("%s @ %d: ground truth has empty tool", scenario.Name, size.numSymbols)
			}

			// Verify the ground truth makes sense
			switch scenario.Name {
			case "dead_code":
				if gt.Tool != "delete_symbol" {
					t.Errorf("%s @ %d: expected delete_symbol, got %s", scenario.Name, size.numSymbols, gt.Tool)
				}
				if gt.Arguments["symbol"] == "" {
					t.Errorf("%s @ %d: empty symbol argument", scenario.Name, size.numSymbols)
				}
			case "no_dead_code":
				if gt.Tool != "no_action" {
					t.Errorf("%s @ %d: expected no_action, got %s", scenario.Name, size.numSymbols, gt.Tool)
				}
			case "circular_dep":
				if gt.Tool != "report_cycle" {
					t.Errorf("%s @ %d: expected report_cycle, got %s", scenario.Name, size.numSymbols, gt.Tool)
				}
			case "ambiguous_target":
				if gt.Tool != "report_ambiguity" {
					t.Errorf("%s @ %d: expected report_ambiguity, got %s", scenario.Name, size.numSymbols, gt.Tool)
				}
			}

			t.Logf("%s @ %d sym: tool=%s args=%v", scenario.Name, size.numSymbols, gt.Tool, gt.Arguments)
		}
	}
}

func callCLI(prompt string) (string, error) {
	cmd := exec.Command("claude", "-p", prompt)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
