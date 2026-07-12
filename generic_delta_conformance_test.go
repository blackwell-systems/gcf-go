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
