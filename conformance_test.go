package gcf

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type conformanceFixture struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Operation     string `json:"operation"`
	Input         json.RawMessage `json:"input"`
	Expected      json.RawMessage `json:"expected"`
	ExpectedError string          `json:"expectedError"`
	InputBase64   string          `json:"inputBase64"`
}

func TestConformance(t *testing.T) {
	fixtureDir := filepath.Join("..", "gcf", "tests", "conformance")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skipf("conformance fixtures not found at %s", fixtureDir)
	}

	var fixtures []string
	err := filepath.Walk(fixtureDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			fixtures = append(fixtures, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Fatal("no fixtures found")
	}

	t.Logf("Found %d fixtures", len(fixtures))

	for _, path := range fixtures {
		relPath, _ := filepath.Rel(fixtureDir, path)
		t.Run(relPath, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}

			var fix conformanceFixture
			if err := json.Unmarshal(data, &fix); err != nil {
				t.Fatalf("parsing fixture: %v", err)
			}

			switch fix.Operation {
			case "encode":
				runEncodeTest(t, fix)
			case "decode":
				runDecodeTest(t, fix)
			case "error":
				runErrorTest(t, fix)
			case "session":
				runSessionTest(t, data)
			case "delta":
				t.Skipf("delta operation not yet implemented")
			default:
				t.Skipf("unsupported operation: %s", fix.Operation)
			}
		})
	}
}

func runEncodeTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	// Parse expected as string.
	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parsing expected: %v", err)
	}

	// Detect graph profile encode tests.
	if strings.HasPrefix(expected, "GCF profile=graph") {
		runGraphEncodeTest(t, fix, expected)
		return
	}

	// Parse input preserving key insertion order.
	input, err := ParseJSONOrdered(fix.Input)
	if err != nil {
		t.Fatalf("parsing input: %v", err)
	}

	got := EncodeGeneric(input)
	if got != expected {
		t.Errorf("encode mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(expected))
	}

	// Round-trip: decode(encode(input)) == input
	decoded, err := DecodeGeneric(got)
	if err != nil {
		t.Errorf("round-trip decode failed: %v", err)
		return
	}
	if !jsonEqual(input, decoded) {
		t.Errorf("round-trip mismatch:\n  input:   %v\n  decoded: %v", input, decoded)
	}
}

func runGraphEncodeTest(t *testing.T, fix conformanceFixture, expected string) {
	t.Helper()

	var input struct {
		Tool        string  `json:"tool"`
		TokenBudget int     `json:"tokenBudget"`
		TokensUsed  int     `json:"tokensUsed"`
		PackRoot    string  `json:"packRoot"`
		Symbols     []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
			Distance      int     `json:"distance"`
		} `json:"symbols"`
		Edges []struct {
			Source   string `json:"source"`
			Target  string `json:"target"`
			EdgeType string `json:"edgeType"`
			Status  string `json:"status"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(fix.Input, &input); err != nil {
		t.Fatalf("parsing graph input: %v", err)
	}

	p := &Payload{
		Tool:        input.Tool,
		TokenBudget: input.TokenBudget,
		TokensUsed:  input.TokensUsed,
		PackRoot:    input.PackRoot,
	}
	for _, s := range input.Symbols {
		p.Symbols = append(p.Symbols, Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
			Distance:      s.Distance,
		})
	}
	for _, e := range input.Edges {
		p.Edges = append(p.Edges, Edge{
			Source:   e.Source,
			Target:   e.Target,
			EdgeType: e.EdgeType,
			Status:   e.Status,
		})
	}

	got := Encode(p)
	if got != expected {
		t.Errorf("encode mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(expected))
	}
}

func runDecodeTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	var inputStr string
	if err := json.Unmarshal(fix.Input, &inputStr); err != nil {
		t.Fatalf("parsing input: %v", err)
	}

	var expected any
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parsing expected: %v", err)
	}

	got, err := DecodeGeneric(inputStr)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !jsonSubset(expected, got) {
		t.Errorf("decode mismatch:\n  got:      %v\n  expected: %v", got, expected)
	}
}

func runErrorTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	var inputStr string
	if fix.InputBase64 != "" {
		// Base64-encoded raw bytes.
		raw, err := base64.StdEncoding.DecodeString(fix.InputBase64)
		if err != nil {
			t.Fatalf("decoding base64: %v", err)
		}
		inputStr = string(raw)
	} else {
		if err := json.Unmarshal(fix.Input, &inputStr); err != nil {
			t.Fatalf("parsing input: %v", err)
		}
	}

	_, err := DecodeGeneric(inputStr)
	if err == nil {
		t.Fatalf("expected error %q, got success", fix.ExpectedError)
	}
	if !strings.Contains(err.Error(), fix.ExpectedError) {
		t.Errorf("wrong error category:\n  got:      %s\n  expected: %s", err.Error(), fix.ExpectedError)
	}
}

// jsonEqual compares two values using JSON normalization.
// Handles *OrderedMap by converting to plain maps first.
func jsonEqual(a, b any) bool {
	a = normalizeOrdered(a)
	b = normalizeOrdered(b)
	// Normalize through JSON to handle int64 vs float64.
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)

	var aNorm, bNorm any
	json.Unmarshal(aJSON, &aNorm)
	json.Unmarshal(bJSON, &bNorm)

	return reflect.DeepEqual(aNorm, bNorm)
}

func normalizeOrdered(v any) any {
	switch val := v.(type) {
	case *OrderedMap:
		return val.ToMap()
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeOrdered(item)
		}
		return out
	default:
		return v
	}
}

// jsonSubset checks that all keys in expected exist in got with matching values.
// Extra keys in got are tolerated (for graph decoder which always emits all fields).
func jsonSubset(expected, got any) bool {
	expected = normalizeOrdered(expected)
	got = normalizeOrdered(got)

	eJSON, _ := json.Marshal(expected)
	gJSON, _ := json.Marshal(got)

	var eNorm, gNorm any
	json.Unmarshal(eJSON, &eNorm)
	json.Unmarshal(gJSON, &gNorm)

	return subsetMatch(eNorm, gNorm)
}

func subsetMatch(expected, got any) bool {
	switch e := expected.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, ev := range e {
			gv, exists := g[k]
			if !exists {
				return false
			}
			if !subsetMatch(ev, gv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok {
			return false
		}
		if len(e) != len(g) {
			return false
		}
		for i := range e {
			if !subsetMatch(e[i], g[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(expected, got)
	}
}

func runSessionTest(t *testing.T, data []byte) {
	t.Helper()

	var fix struct {
		Name      string `json:"name"`
		Calls     []struct {
			Input    json.RawMessage `json:"input"`
			Expected string          `json:"expected"`
		} `json:"calls"`
	}
	if err := json.Unmarshal(data, &fix); err != nil {
		t.Fatalf("parsing session fixture: %v", err)
	}

	sess := NewSession()
	for i, call := range fix.Calls {
		var input struct {
			Tool    string `json:"tool"`
			Symbols []struct {
				QualifiedName string  `json:"qualifiedName"`
				Kind          string  `json:"kind"`
				Score         float64 `json:"score"`
				Provenance    string  `json:"provenance"`
				Distance      int     `json:"distance"`
			} `json:"symbols"`
			Edges []struct {
				Source   string `json:"source"`
				Target  string `json:"target"`
				EdgeType string `json:"edgeType"`
			} `json:"edges"`
		}
		if err := json.Unmarshal(call.Input, &input); err != nil {
			t.Fatalf("call %d: parsing input: %v", i, err)
		}

		p := &Payload{Tool: input.Tool}
		for _, s := range input.Symbols {
			p.Symbols = append(p.Symbols, Symbol{
				QualifiedName: s.QualifiedName,
				Kind:          s.Kind,
				Score:         s.Score,
				Provenance:    s.Provenance,
				Distance:      s.Distance,
			})
		}
		for _, e := range input.Edges {
			p.Edges = append(p.Edges, Edge{
				Source:   e.Source,
				Target:   e.Target,
				EdgeType: e.EdgeType,
			})
		}

		got := EncodeWithSession(p, sess)
		if got != call.Expected {
			t.Errorf("call %d: encode mismatch:\n  got:      %s\n  expected: %s", i, quote(got), quote(call.Expected))
		}
	}
}

func quote(s string) string {
	return fmt.Sprintf("%q", s)
}
