package gcf

import (
	"strings"
	"testing"
)

func row(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func ordersBase() GenericSet {
	return GenericSet{
		Name: "orders", Key: "id", Fields: []string{"id", "total", "status", "customer"},
		Rows: []map[string]any{
			row("id", 1001.0, "total", 59.98, "status", "shipped", "customer", "Alice"),
			row("id", 1002.0, "total", 29.99, "status", "pending", "customer", "Bob"),
			row("id", 1003.0, "total", 129.50, "status", "shipped", "customer", "Carol"),
		},
	}
}

func ordersNext() GenericSet {
	return GenericSet{
		Name: "orders", Key: "id", Fields: []string{"id", "total", "status", "customer"},
		Rows: []map[string]any{
			// 1001 removed; 1002 changed (pending -> shipped); 1003 unchanged; 1004 added
			row("id", 1002.0, "total", 29.99, "status", "shipped", "customer", "Bob"),
			row("id", 1003.0, "total", 129.50, "status", "shipped", "customer", "Carol"),
			row("id", 1004.0, "total", 75.00, "status", "pending", "customer", "Dave"),
		},
	}
}

// The self-proving unit: Diff -> encode -> apply -> the recomputed root matches.
func TestGenericDeltaRoundTripByRoot(t *testing.T) {
	base, next := ordersBase(), ordersNext()
	d, err := DiffGenericSets(base, next)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(d.Added) != 1 || len(d.Changed) != 1 || len(d.Removed) != 1 {
		t.Fatalf("expected 1 added / 1 changed / 1 removed, got %d/%d/%d", len(d.Added), len(d.Changed), len(d.Removed))
	}
	if d.NewRoot != GenericPackRoot(next) {
		t.Fatalf("diff NewRoot != PackRoot(next)")
	}

	result, err := VerifyGenericDelta(base, d, GenericPackRoot(next))
	if err != nil {
		t.Fatalf("verify/apply: %v", err)
	}
	if GenericPackRoot(result) != GenericPackRoot(next) {
		t.Fatalf("applied result root != next root")
	}
}

// PackRoot is order-agnostic (set semantics, Section 10a.3/10a.6).
func TestGenericPackRootRowOrderInvariant(t *testing.T) {
	a := ordersBase()
	b := ordersBase()
	b.Rows = []map[string]any{b.Rows[2], b.Rows[0], b.Rows[1]} // reorder
	if GenericPackRoot(a) != GenericPackRoot(b) {
		t.Fatalf("row order changed the pack root")
	}
}

// canonicalCell must not let a string collide with a typed literal.
func TestCanonicalCellNoTypeCollision(t *testing.T) {
	cases := [][2]any{
		{nil, "-"},                 // null
		{true, "true"},             // bool
		{"true", `"true"`},         // string that spells a bool -> quoted
		{"-", `"-"`},               // string that spells null -> quoted
		{59.98, "59.98"},           // number
		{"59.98", `"59.98"`},       // string that spells a number -> quoted
		{"a\tb", `"a\tb"`},         // tab in a string -> escaped, record-safe
	}
	for _, c := range cases {
		if got := canonicalCell(c[0]); got != c[1] {
			t.Errorf("canonicalCell(%#v) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestGenericDeltaInvariants(t *testing.T) {
	base := ordersBase()
	baseRoot := GenericPackRoot(base)

	// duplicate identity
	dup := ordersBase()
	dup.Rows = append(dup.Rows, row("id", 1001.0, "total", 1.0, "status", "x", "customer", "y"))
	if _, err := DiffGenericSets(dup, ordersNext()); err == nil || !strings.Contains(err.Error(), "duplicate identity") {
		t.Errorf("expected duplicate-identity error, got %v", err)
	}

	// schema change
	sc := ordersNext()
	sc.Fields = []string{"id", "total", "status"} // dropped a column
	if _, err := DiffGenericSets(base, sc); err == nil || !strings.Contains(err.Error(), "schema change") {
		t.Errorf("expected schema-change error, got %v", err)
	}

	// add-existing
	addExisting := &GenericDeltaPayload{Key: "id", Fields: base.Fields, BaseRoot: baseRoot, NewRoot: "sha256:x",
		Added: []map[string]any{row("id", 1001.0, "total", 1.0, "status", "s", "customer", "c")}}
	if _, err := VerifyGenericDelta(base, addExisting, "sha256:x"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected add-existing error, got %v", err)
	}

	// change-missing
	changeMissing := &GenericDeltaPayload{Key: "id", Fields: base.Fields, BaseRoot: baseRoot, NewRoot: "sha256:x",
		Changed: []map[string]any{row("id", 9999.0, "total", 1.0, "status", "s", "customer", "c")}}
	if _, err := VerifyGenericDelta(base, changeMissing, "sha256:x"); err == nil || !strings.Contains(err.Error(), "not in base") {
		t.Errorf("expected change-missing error, got %v", err)
	}

	// remove-missing
	removeMissing := &GenericDeltaPayload{Key: "id", Fields: base.Fields, BaseRoot: baseRoot, NewRoot: "sha256:x",
		Removed: []any{"9999"}}
	if _, err := VerifyGenericDelta(base, removeMissing, "sha256:x"); err == nil || !strings.Contains(err.Error(), "not in base") {
		t.Errorf("expected remove-missing error, got %v", err)
	}

	// base_mismatch
	wrongBase := &GenericDeltaPayload{Key: "id", Fields: base.Fields, BaseRoot: "sha256:wrong", NewRoot: baseRoot}
	if _, err := VerifyGenericDelta(base, wrongBase, baseRoot); err == nil || !strings.Contains(err.Error(), "base_mismatch") {
		t.Errorf("expected base_mismatch error, got %v", err)
	}

	// root_mismatch (well-formed ops, wrong expected root)
	d, _ := DiffGenericSets(base, ordersNext())
	if _, err := VerifyGenericDelta(base, d, "sha256:deadbeef"); err == nil || !strings.Contains(err.Error(), "root_mismatch") {
		t.Errorf("expected root_mismatch error, got %v", err)
	}
}

// Full payload round-trips: decode(encode(set)) preserves the pack root, and the
// header pack_root matches the recomputed one.
func TestGenericFullWireRoundTrip(t *testing.T) {
	base := ordersBase()
	wire := EncodeGenericFull(base, "orders_query")
	got, root, err := DecodeGenericFull(wire)
	if err != nil {
		t.Fatalf("decode full: %v", err)
	}
	if GenericPackRoot(got) != GenericPackRoot(base) {
		t.Fatalf("full round-trip changed the pack root")
	}
	if root != GenericPackRoot(base) {
		t.Fatalf("header pack_root %s != recomputed %s", root, GenericPackRoot(base))
	}
	if got.Key != base.Key || !sameStrings(got.Fields, base.Fields) || got.Name != base.Name {
		t.Fatalf("decoded schema mismatch: %+v", got)
	}
}

// The complete server -> wire -> consumer loop, all through text:
// encode a full base, decode it (consumer's held set), diff to next, encode the
// delta, decode it, apply it, and confirm the recomputed root matches.
func TestGenericDeltaEndToEnd(t *testing.T) {
	base, next := ordersBase(), ordersNext()

	// Server call 1: full base. Consumer decodes and holds it.
	held, _, err := DecodeGenericFull(EncodeGenericFull(base, "orders_query"))
	if err != nil {
		t.Fatalf("decode full: %v", err)
	}

	// Server call 2: diff and encode a delta over the wire.
	d, err := DiffGenericSets(base, next)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	deltaWire := EncodeGenericDelta(d)

	// Consumer: parse the delta and apply it to the held set.
	parsed, err := DecodeGenericDelta(deltaWire)
	if err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	result, err := VerifyGenericDelta(held, parsed, GenericPackRoot(next))
	if err != nil {
		t.Fatalf("apply decoded delta: %v", err)
	}
	if GenericPackRoot(result) != GenericPackRoot(next) {
		t.Fatalf("end-to-end result root != next root")
	}
	// Re-encoding the decoded delta reproduces the wire (stable round-trip).
	parsed.Tool, parsed.DeltaTokens, parsed.FullTokens = d.Tool, d.DeltaTokens, d.FullTokens
	if EncodeGenericDelta(parsed) != deltaWire {
		t.Fatalf("delta wire not stable through decode/re-encode")
	}
}
