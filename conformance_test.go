package gcf

import (
	"bytes"
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
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Operation        string          `json:"operation"`
	Input            json.RawMessage `json:"input"`
	Expected         json.RawMessage `json:"expected"`
	ExpectedError    string          `json:"expectedError"`
	InputBase64      string          `json:"inputBase64"`
	BaseSnapshot     json.RawMessage `json:"base_snapshot"`
	ExpectedSnapshot json.RawMessage `json:"expected_snapshot"`
	Options          struct {
		LabeledTrailerCounts bool `json:"labeledTrailerCounts"`
	} `json:"options"`
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

	// Floor assertion: a green conformance run MUST have exercised the full shared
	// suite. If the fixtures directory is present but yields too few files (mispathed,
	// partial, or empty checkout), fail loudly rather than pass having verified almost
	// nothing. A wholly-absent directory is handled by the Skip above; in CI the
	// separate gcf checkout step fails loudly if the repo cannot be cloned.
	const minFixtures = 150
	if len(fixtures) < minFixtures {
		t.Fatalf("discovered %d conformance fixtures at %s, expected at least %d; the shared gcf fixture set is missing or mispathed", len(fixtures), fixtureDir, minFixtures)
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
			case "graph-stream-encode":
				runGraphStreamEncodeTest(t, fix)
			case "decode":
				runDecodeTest(t, fix)
			case "error":
				runErrorTest(t, fix)
			case "encode-error":
				runEncodeErrorTest(t, fix)
			case "session":
				runSessionTest(t, data)
			case "roundtrip":
				runRoundtripTest(t, fix)
			case "roundtrip-wire":
				runRoundtripWireTest(t, fix)
			case "delta":
				runDeltaTest(t, fix)
			case "delta-verify":
				runDeltaVerifyTest(t, fix)
			case "pack-root":
				runGraphPackRootTest(t, fix)
			case "generic-pack-root":
				runGenericPackRootTest(t, fix)
			case "generic-delta":
				runGenericDeltaTest(t, fix)
			case "generic-delta-verify":
				runGenericDeltaVerifyTest(t, fix)
			case "generic-delta-decode":
				runGenericDeltaDecodeTest(t, fix)
			case "generic-delta-session":
				runGenericDeltaSessionTest(t, fix)
			default:
				t.Fatalf("unhandled operation %q; every operation must be handled or explicitly allow-listed", fix.Operation)
			}
		})
	}
}

// runDeltaTest handles the graph "delta" operation. Fixtures in graph-delta share
// this operation name but come in two shapes: an ENCODE scenario whose input is a
// DeltaPayload struct, and a VERIFY scenario whose input is a pre-encoded wire
// string (with base_snapshot/expected_snapshot). The verify-shaped fixtures require
// the graph delta wire decoder, which is not yet implemented, so they are routed to
// the same allow-listed skip as delta-verify rather than the encode path.
func runDeltaTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	// The verify-shaped fixture's input is a JSON string (the wire form), not a
	// DeltaPayload object: decode it, apply it to base_snapshot, and verify new_root.
	var asString string
	if err := json.Unmarshal(fix.Input, &asString); err == nil {
		runDeltaVerifyTest(t, fix)
		return
	}

	var input struct {
		Tool     string `json:"tool"`
		BaseRoot string `json:"baseRoot"`
		NewRoot  string `json:"newRoot"`
		Removed  []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
		} `json:"removed"`
		Added []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
		} `json:"added"`
		RemovedEdges []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			EdgeType string `json:"edgeType"`
		} `json:"removedEdges"`
		AddedEdges []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			EdgeType string `json:"edgeType"`
		} `json:"addedEdges"`
		DeltaTokens int `json:"deltaTokens"`
		FullTokens  int `json:"fullTokens"`
	}
	if err := json.Unmarshal(fix.Input, &input); err != nil {
		t.Fatalf("parsing delta input: %v", err)
	}

	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parsing expected: %v", err)
	}

	d := &DeltaPayload{
		Tool:        input.Tool,
		BaseRoot:    input.BaseRoot,
		NewRoot:     input.NewRoot,
		DeltaTokens: input.DeltaTokens,
		FullTokens:  input.FullTokens,
	}
	for _, s := range input.Removed {
		d.Removed = append(d.Removed, Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
		})
	}
	for _, s := range input.Added {
		d.Added = append(d.Added, Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
		})
	}
	for _, e := range input.RemovedEdges {
		d.RemovedEdges = append(d.RemovedEdges, Edge{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType})
	}
	for _, e := range input.AddedEdges {
		d.AddedEdges = append(d.AddedEdges, Edge{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType})
	}

	got := EncodeDelta(d)
	if got != expected {
		t.Errorf("delta encode mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(expected))
	}
}

// runDeltaVerifyTest decodes a graph delta wire payload, applies it to the
// fixture's base_snapshot, and verifies new_root. For a root_mismatch fixture it
// asserts VerifyDelta returns the expected error; otherwise it asserts success and
// that the applied snapshot's pack_root matches the expected_snapshot.
func runDeltaVerifyTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	var wire string
	if err := json.Unmarshal(fix.Input, &wire); err != nil {
		t.Fatalf("delta-verify input must be a wire string: %v", err)
	}
	dd, err := DecodeDelta(wire)
	if err != nil {
		t.Fatalf("DecodeDelta: %v", err)
	}
	baseSyms, baseEdges := parseSnapshot(t, fix.BaseSnapshot)

	result, resultEdges, verr := VerifyDelta(baseSyms, baseEdges,
		dd.Removed, dd.Added, dd.RemovedEdges, dd.AddedEdges, dd.NewRoot)

	if fix.ExpectedError != "" {
		if verr == nil {
			t.Errorf("expected error %q, got success", fix.ExpectedError)
		} else if !strings.Contains(verr.Error(), fix.ExpectedError) {
			t.Errorf("expected error %q, got: %v", fix.ExpectedError, verr)
		}
		return
	}
	if verr != nil {
		t.Errorf("delta verify failed: %v", verr)
		return
	}
	// The applied snapshot must match the expected snapshot; compare by pack_root
	// (content identity, order-independent).
	expSyms, expEdges := parseSnapshot(t, fix.ExpectedSnapshot)
	if got, want := PackRoot(result, resultEdges), PackRoot(expSyms, expEdges); got != want {
		t.Errorf("applied snapshot mismatch:\n  got pack_root:      %s\n  expected pack_root: %s", got, want)
	}
}

// parseSnapshot parses a {symbols, edges} snapshot object into typed slices.
func parseSnapshot(t *testing.T, raw json.RawMessage) ([]Symbol, []Edge) {
	t.Helper()
	var snap struct {
		Symbols []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
			Distance      int     `json:"distance"`
		} `json:"symbols"`
		Edges []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			EdgeType string `json:"edgeType"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("parsing snapshot: %v", err)
	}
	var syms []Symbol
	for _, s := range snap.Symbols {
		syms = append(syms, Symbol{QualifiedName: s.QualifiedName, Kind: s.Kind, Score: s.Score, Provenance: s.Provenance, Distance: s.Distance})
	}
	var edges []Edge
	for _, e := range snap.Edges {
		edges = append(edges, Edge{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType})
	}
	return syms, edges
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
	// Re-encode idempotence: encode(decode(got)) == got. Order-sensitive, so it
	// catches a decoder that drops object field order, which jsonEqual (normalizing
	// through unordered maps) cannot. Object key ordering is a preserved round-trip
	// property (SPEC 52, 931).
	if reEncoded := EncodeGeneric(decoded); reEncoded != got {
		t.Errorf("re-encode not idempotent (field order or value loss):\n  got:  %s\n  renc: %s", quote(got), quote(reEncoded))
	}
}

func runGraphEncodeTest(t *testing.T, fix conformanceFixture, expected string) {
	t.Helper()

	var input struct {
		Tool        string `json:"tool"`
		TokenBudget int    `json:"tokenBudget"`
		TokensUsed  int    `json:"tokensUsed"`
		PackRoot    string `json:"packRoot"`
		Symbols     []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
			Distance      int     `json:"distance"`
		} `json:"symbols"`
		Edges []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			EdgeType string `json:"edgeType"`
			Status   string `json:"status"`
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
	// Re-encode idempotence: encode(decode(got)) == got. Confirms the graph decoder
	// reconstructs the payload without dropping or reordering fields (SPEC 52, 931).
	decoded, err := Decode(got)
	if err != nil {
		t.Errorf("round-trip decode failed: %v", err)
		return
	}
	if reEncoded := Encode(decoded); reEncoded != got {
		t.Errorf("graph re-encode not idempotent:\n  got:  %s\n  renc: %s", quote(got), quote(reEncoded))
	}
}

// runGraphPackRootTest builds the symbol/edge graph from the fixture input and
// asserts that PackRoot produces the expected content-addressed sha256 hash. This
// exercises the graph pack_root operation the graph-encode path does not verify.
func runGraphPackRootTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parsing expected: %v", err)
	}

	var input struct {
		Symbols []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
			Distance      int     `json:"distance"`
		} `json:"symbols"`
		Edges []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			EdgeType string `json:"edgeType"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(fix.Input, &input); err != nil {
		t.Fatalf("parsing pack-root input: %v", err)
	}

	var symbols []Symbol
	for _, s := range input.Symbols {
		symbols = append(symbols, Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
			Distance:      s.Distance,
		})
	}
	var edges []Edge
	for _, e := range input.Edges {
		edges = append(edges, Edge{
			Source:   e.Source,
			Target:   e.Target,
			EdgeType: e.EdgeType,
		})
	}

	got := PackRoot(symbols, edges)
	if got != expected {
		t.Errorf("pack-root mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(expected))
	}
}

// runGraphStreamEncodeTest drives the streaming graph encoder and compares its
// exact bytes to the fixture. This is the only conformance path that exercises
// the streaming encoder's header and trailer; buffered graph encode uses
// runGraphEncodeTest. The streaming header MUST carry profile=graph (SPEC 3.1).
func runGraphStreamEncodeTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parsing expected: %v", err)
	}

	var input struct {
		Tool        string `json:"tool"`
		TokenBudget int    `json:"tokenBudget"`
		TokensUsed  int    `json:"tokensUsed"`
		PackRoot    string `json:"packRoot"`
		Symbols     []struct {
			QualifiedName string  `json:"qualifiedName"`
			Kind          string  `json:"kind"`
			Score         float64 `json:"score"`
			Provenance    string  `json:"provenance"`
			Distance      int     `json:"distance"`
		} `json:"symbols"`
		Edges []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			EdgeType string `json:"edgeType"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(fix.Input, &input); err != nil {
		t.Fatalf("parsing graph stream input: %v", err)
	}

	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf, input.Tool, StreamOptions{
		TokenBudget:          input.TokenBudget,
		TokensUsed:           input.TokensUsed,
		PackRoot:             input.PackRoot,
		LabeledTrailerCounts: fix.Options.LabeledTrailerCounts,
	})
	for _, s := range input.Symbols {
		enc.WriteSymbol(Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
			Distance:      s.Distance,
		})
	}
	for _, e := range input.Edges {
		enc.WriteEdge(Edge{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType})
	}
	enc.Close()

	got := buf.String()
	if got != expected {
		t.Errorf("stream encode mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(expected))
	}
	if !strings.HasPrefix(got, "GCF profile=graph ") {
		t.Errorf("streaming header missing profile=graph: %s", quote(got))
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

func runRoundtripTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	input, err := ParseJSONOrdered(fix.Input)
	if err != nil {
		t.Fatalf("parsing input: %v", err)
	}

	encoded := EncodeGeneric(input)

	// If expected is a string, verify encoded output matches.
	var expectedStr string
	if err := json.Unmarshal(fix.Expected, &expectedStr); err == nil {
		if encoded != expectedStr {
			t.Errorf("encode mismatch:\n  got:      %s\n  expected: %s", quote(encoded), quote(expectedStr))
		}
	}

	// Verify round-trip.
	decoded, err := DecodeGeneric(encoded)
	if err != nil {
		t.Fatalf("decode error: %v\n  encoded: %s", err, quote(encoded))
	}
	if !jsonEqual(input, decoded) {
		t.Errorf("round-trip mismatch:\n  input:   %v\n  decoded: %v", input, decoded)
	}
}

// runRoundtripWireTest handles the "roundtrip-wire" operation: input and expected
// are both wire strings. It decodes the input wire, re-encodes, and requires the
// result to equal the expected wire. The value never becomes a host JSON number, so
// values that a JSON parser would float (integers beyond 2^53) can be pinned here.
func runRoundtripWireTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	var inputWire string
	if err := json.Unmarshal(fix.Input, &inputWire); err != nil {
		t.Fatalf("parsing input wire: %v", err)
	}
	var expectedWire string
	if err := json.Unmarshal(fix.Expected, &expectedWire); err != nil {
		t.Fatalf("parsing expected wire: %v", err)
	}

	decoded, err := DecodeGeneric(inputWire)
	if err != nil {
		t.Fatalf("decode failed: %v\n  input: %s", err, quote(inputWire))
	}
	reencoded := EncodeGeneric(decoded)
	if reencoded != expectedWire {
		t.Errorf("wire idempotence mismatch:\n  got:      %s\n  expected: %s", quote(reencoded), quote(expectedWire))
	}
}

// runEncodeErrorTest handles the "encode-error" operation: the input is a JSON
// value (encode-side, not a wire string) that is out of the numeric domain. Ingest
// through ParseJSONOrdered (the JSON->value bridge) must raise a domain error, or,
// for a value the bridge preserves, the encoder must. The value never becomes an
// approximate host number.
func runEncodeErrorTest(t *testing.T, fix conformanceFixture) {
	t.Helper()

	input, err := ParseJSONOrdered(fix.Input)
	if err != nil {
		if !strings.Contains(err.Error(), fix.ExpectedError) {
			t.Errorf("wrong error category:\n  got:      %s\n  expected: %s", err.Error(), fix.ExpectedError)
		}
		return
	}
	// Ingest preserved the value (e.g. a bignum an int64-backed bridge cannot hold
	// is not reachable in Go, but other SDKs may preserve it); the encoder is then
	// the domain-enforcement site. EncodeGeneric is currently infallible, so a
	// successful ingest here means the value was in-domain, which is a failure.
	t.Fatalf("expected error %q, got successful ingest: %#v", fix.ExpectedError, input)
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
		Name  string `json:"name"`
		Calls []struct {
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
				Target   string `json:"target"`
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
