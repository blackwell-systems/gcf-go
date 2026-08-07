package gcf

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func runGenericPackRootTest(t *testing.T, fix conformanceFixture) {
	t.Helper()
	var in struct {
		Key    string           `json:"key"`
		Fields []string         `json:"fields"`
		Rows   []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(fix.Input, &in); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parse expected: %v", err)
	}
	got := GenericPackRoot(GenericSet{Key: in.Key, Fields: in.Fields, Rows: in.Rows})
	if got != expected {
		t.Errorf("pack-root mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

type deltaInput struct {
	Tool        string           `json:"tool"`
	Key         string           `json:"key"`
	Fields      []string         `json:"fields"`
	BaseRoot    string           `json:"baseRoot"`
	NewRoot     string           `json:"newRoot"`
	Added       []map[string]any `json:"added"`
	Changed     []map[string]any `json:"changed"`
	Removed     []any            `json:"removed"`
	DeltaTokens int              `json:"deltaTokens"`
	FullTokens  int              `json:"fullTokens"`
}

func (in deltaInput) payload() *GenericDeltaPayload {
	return &GenericDeltaPayload{
		Tool: in.Tool, Key: in.Key, Fields: in.Fields,
		BaseRoot: in.BaseRoot, NewRoot: in.NewRoot,
		Added: in.Added, Changed: in.Changed, Removed: in.Removed,
		DeltaTokens: in.DeltaTokens, FullTokens: in.FullTokens,
	}
}

func runGenericDeltaTest(t *testing.T, fix conformanceFixture) {
	t.Helper()
	var in deltaInput
	if err := json.Unmarshal(fix.Input, &in); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parse expected: %v", err)
	}
	got := EncodeGenericDelta(in.payload())
	if got != expected {
		t.Errorf("delta encode mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(expected))
	}
	// Re-encode idempotence: encode(decode(got)) == got, ignoring the derived
	// savings= header stat (computed from the original set sizes at encode time and
	// not carried in the wire, so a decode/re-encode legitimately cannot reconstruct
	// it). Confirms the delta decoder preserves fields and their order (SPEC 52, 931).
	decoded, err := DecodeGenericDelta(got)
	if err != nil {
		t.Errorf("delta round-trip decode failed: %v", err)
		return
	}
	if reEncoded := EncodeGenericDelta(decoded); stripDeltaSavings(reEncoded) != stripDeltaSavings(got) {
		t.Errorf("delta re-encode not idempotent:\n  got:  %s\n  renc: %s", quote(got), quote(reEncoded))
	}
}

// stripDeltaSavings removes the derived ` savings=...` header stat so re-encode
// idempotence can be checked on the parts of the wire the payload actually carries.
func stripDeltaSavings(s string) string {
	idx := strings.Index(s, " savings=")
	if idx < 0 {
		return s
	}
	end := idx + len(" savings=")
	for end < len(s) && s[end] != ' ' && s[end] != '\n' {
		end++
	}
	return s[:idx] + s[end:]
}

func runGenericDeltaVerifyTest(t *testing.T, fix conformanceFixture) {
	t.Helper()
	var in struct {
		Base struct {
			Key    string           `json:"key"`
			Fields []string         `json:"fields"`
			Rows   []map[string]any `json:"rows"`
		} `json:"base"`
		Delta           deltaInput `json:"delta"`
		ExpectedNewRoot string     `json:"expectedNewRoot"`
	}
	if err := json.Unmarshal(fix.Input, &in); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	base := GenericSet{Key: in.Base.Key, Fields: in.Base.Fields, Rows: in.Base.Rows}
	result, err := VerifyGenericDelta(base, in.Delta.payload(), in.ExpectedNewRoot)
	if fix.ExpectedError != "" {
		if err == nil || !strings.Contains(err.Error(), fix.ExpectedError) {
			t.Errorf("wrong error: got %v, expected %q", err, fix.ExpectedError)
		}
		return
	}
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parse expected: %v", err)
	}
	if got := GenericPackRoot(result); got != expected {
		t.Errorf("verify result root mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

// runGenericDeltaSessionTest drives a GenericDeltaSession through the fixture's
// updates and checks the initial full plus every (isFull, wire) emission — the
// producer-side re-anchor cadence contract, byte-identical across SDKs.
func runGenericDeltaSessionTest(t *testing.T, fix conformanceFixture) {
	t.Helper()
	var in struct {
		Base    setInput          `json:"base"`
		Tool    string            `json:"tool"`
		Policy  struct {
			Mode string `json:"mode"`
			N    int    `json:"n"`
		} `json:"policy"`
		Updates []setInput `json:"updates"`
	}
	if err := json.Unmarshal(fix.Input, &in); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	var exp struct {
		InitialFull string `json:"initialFull"`
		Emissions   []struct {
			IsFull bool   `json:"isFull"`
			Wire   string `json:"wire"`
		} `json:"emissions"`
	}
	if err := json.Unmarshal(fix.Expected, &exp); err != nil {
		t.Fatalf("parse expected: %v", err)
	}

	var policy ReanchorPolicy
	switch in.Policy.Mode {
	case "sizeGuard":
		policy = SizeGuard()
	default: // fixedN
		policy = FixedN(in.Policy.N)
	}

	s := NewGenericDeltaSession(in.Base.set(), in.Tool, policy)
	if got := s.CurrentFull(); got != exp.InitialFull {
		t.Errorf("initial full mismatch:\n  got:      %s\n  expected: %s", quote(got), quote(exp.InitialFull))
	}
	for i, up := range in.Updates {
		wire, isFull, err := s.Next(up.set())
		if err != nil {
			t.Fatalf("turn %d: %v", i+1, err)
		}
		if i >= len(exp.Emissions) {
			t.Fatalf("turn %d: no expected emission", i+1)
		}
		if isFull != exp.Emissions[i].IsFull {
			t.Errorf("turn %d: isFull=%v, want %v", i+1, isFull, exp.Emissions[i].IsFull)
		}
		if wire != exp.Emissions[i].Wire {
			t.Errorf("turn %d wire mismatch:\n  got:      %s\n  expected: %s", i+1, quote(wire), quote(exp.Emissions[i].Wire))
		}
	}
}

type setInput struct {
	Name   string           `json:"name"`
	Key    string           `json:"key"`
	Fields []string         `json:"fields"`
	Rows   []map[string]any `json:"rows"`
}

func (s setInput) set() GenericSet {
	return GenericSet{Name: s.Name, Key: s.Key, Fields: s.Fields, Rows: s.Rows}
}

// TestDumpGenericDeltaFixtureValues prints the computed roots and wire text used
// to author the conformance fixtures. Run with -run TestDump -v.
func TestDumpGenericDeltaFixtureValues(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("fixture generator; run with -v")
	}
	base, next := ordersBase(), ordersNext()
	d, _ := DiffGenericSets(base, next)
	d.Tool = "orders_query"
	d.DeltaTokens = 30
	d.FullTokens = 160
	fmt.Println("BASE_ROOT=" + GenericPackRoot(base))

	fmt.Println("NEXT_ROOT=" + GenericPackRoot(next))
	fmt.Println("---DELTA_WIRE---")
	fmt.Print(EncodeGenericDelta(d))
	fmt.Println("---FULL_WIRE---")
	fmt.Print(EncodeGenericFull(base, "orders_query"))
	fmt.Println("---END---")
}

// runGenericDeltaDecodeTest parses a delta wire payload, applies it to the given
// base, and checks the recomputed root — the cross-SDK consumer-parse contract.
func runGenericDeltaDecodeTest(t *testing.T, fix conformanceFixture) {
	t.Helper()
	var in struct {
		Wire string `json:"wire"`
		Base struct {
			Key    string           `json:"key"`
			Fields []string         `json:"fields"`
			Rows   []map[string]any `json:"rows"`
		} `json:"base"`
		ExpectedNewRoot string `json:"expectedNewRoot"`
	}
	if err := json.Unmarshal(fix.Input, &in); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	d, err := DecodeGenericDelta(in.Wire)
	if err != nil {
		if fix.ExpectedError != "" && strings.Contains(err.Error(), fix.ExpectedError) {
			return
		}
		t.Fatalf("decode delta: %v", err)
	}
	base := GenericSet{Key: in.Base.Key, Fields: in.Base.Fields, Rows: in.Base.Rows}
	result, err := VerifyGenericDelta(base, d, in.ExpectedNewRoot)
	if fix.ExpectedError != "" {
		if err == nil || !strings.Contains(err.Error(), fix.ExpectedError) {
			t.Errorf("wrong error: got %v, expected %q", err, fix.ExpectedError)
		}
		return
	}
	if err != nil {
		t.Fatalf("apply decoded delta: %v", err)
	}
	var expected string
	if err := json.Unmarshal(fix.Expected, &expected); err != nil {
		t.Fatalf("parse expected: %v", err)
	}
	if got := GenericPackRoot(result); got != expected {
		t.Errorf("decoded-apply root mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

func TestDumpHardeningValues(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("fixture generator; run with -v")
	}
	// A. nulls
	nulls := GenericSet{Name: "items", Key: "id", Fields: []string{"id", "total", "status", "customer"},
		Rows: []map[string]any{
			{"id": 2001.0, "total": 10.0, "status": nil, "customer": "Amy"},
			{"id": 2002.0, "total": nil, "status": "open", "customer": nil},
		}}
	fmt.Println("NULLS_ROOT=" + GenericPackRoot(nulls))
	fmt.Println("NULLS_FULL:\n" + EncodeGenericFull(nulls, ""))

	// B. string keys (quoting path): sku "1001" spells a number -> quoted
	sku := GenericSet{Name: "parts", Key: "sku", Fields: []string{"sku", "name", "qty"},
		Rows: []map[string]any{
			{"sku": "1001", "name": "Widget", "qty": 5.0},
			{"sku": "A-200", "name": "Gadget", "qty": 3.0},
		}}
	fmt.Println("SKU_ROOT=" + GenericPackRoot(sku))
	fmt.Println("SKU_FULL:\n" + EncodeGenericFull(sku, ""))

	// C. larger set (12 rows)
	large := GenericSet{Name: "rows", Key: "id", Fields: []string{"id", "v"}}
	for i := 0; i < 12; i++ {
		large.Rows = append(large.Rows, map[string]any{"id": float64(3000 + i), "v": float64(i * i)})
	}
	fmt.Println("LARGE_ROOT=" + GenericPackRoot(large))

	// D. empty delta (base == next): no sections, base_root == new_root
	base := ordersBase()
	empty, _ := DiffGenericSets(base, base)
	fmt.Printf("EMPTY_COUNTS added=%d changed=%d removed=%d baseEqNew=%v\n",
		len(empty.Added), len(empty.Changed), len(empty.Removed), empty.BaseRoot == empty.NewRoot)
	fmt.Println("EMPTY_WIRE:\n" + EncodeGenericDelta(empty))
	fmt.Println("BASE_ROOT=" + GenericPackRoot(base))
}
