package gcf

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func getIterations(defaultN int) int {
	if s := os.Getenv("GCF_ITERATIONS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return defaultN
}

// TestPropertyRoundTrip generates random JSON values and verifies
// decodeGeneric(encodeGeneric(v)) == v for each.
// This is the empirical proof of the lossless round-trip invariant.
func TestPropertyRoundTrip(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(42)) // deterministic seed for reproducibility

	for i := 0; i < iterations; i++ {
		val := genValue(rng, 0, 4)

		gcfText := EncodeGeneric(val)

		// Verify the output is valid UTF-8.
		if !utf8.ValidString(gcfText) {
			t.Fatalf("iteration %d: encoder produced invalid UTF-8", i)
		}

		// Verify the header is present.
		if !strings.HasPrefix(gcfText, "GCF profile=generic\n") {
			t.Fatalf("iteration %d: missing header\n  output: %q", i, truncate(gcfText, 200))
		}

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}

		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d random values round-tripped successfully", iterations)
}

// TestPropertyRoundTripAdversarial focuses on values most likely to break
// the scalar quoting and container selection logic.
func TestPropertyRoundTripAdversarial(t *testing.T) {
	iterations := getIterations(50_000)
	rng := rand.New(rand.NewSource(99))

	for i := 0; i < iterations; i++ {
		val := genAdversarialValue(rng, 0, 3)

		gcfText := EncodeGeneric(val)

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}

		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d adversarial values round-tripped successfully", iterations)
}

// TestPropertyRoundTripFlatten exercises the v3.2 nested-flatten path: aligned
// arrays whose shared fields are fixed-shape nested objects, with a field or an
// intermediate nested level sometimes null/absent. The scalar-only tabular
// generator above never produces this shape, so the flatten/unflatten path (and
// its null-at-depth losslessness edge) would otherwise be unexercised.
func TestPropertyRoundTripFlatten(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(7))

	for i := 0; i < iterations; i++ {
		val := genFlattenableArray(rng)
		gcfText := EncodeGeneric(val)
		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}
		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d aligned nested arrays round-tripped successfully", iterations)
}

// TestPropertyRoundTripFlattenAdversarialKeys hammers the flatten path with keys
// drawn from an alphabet that includes the empty string and every '>' arrangement
// (leading, trailing, bare, interior). These are exactly the keys the §7.4.6
// empty-key fix guards against: an empty or '>'-containing key produces an empty
// or literal path segment that the decoder refuses to invert, so a pre-fix encoder
// emitted a column it could not round-trip (silent corruption). The scalar and
// genBareKey generators never produce these, so without this test the fix is
// unverified under fuzz. Post-fix these fields fall back to the attachment form
// and MUST round-trip.
func TestPropertyRoundTripFlattenAdversarialKeys(t *testing.T) {
	iterations := getIterations(200_000)
	rng := rand.New(rand.NewSource(0x5e))

	sawEmpty, sawGT := false, false
	for i := 0; i < iterations; i++ {
		val := genFlattenableArrayWith(rng, genFlattenAdversarialKey)
		if !sawEmpty || !sawGT {
			for _, row := range val {
				for k := range row.(map[string]any) {
					if k == "" {
						sawEmpty = true
					}
					if strings.Contains(k, ">") {
						sawGT = true
					}
				}
			}
		}
		gcfText := EncodeGeneric(val)
		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}
		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	if !sawEmpty || !sawGT {
		t.Fatalf("adversarial generator failed to exercise the target keys: sawEmpty=%v sawGT=%v", sawEmpty, sawGT)
	}
	t.Logf("PASS: %d adversarial empty/'>'-key nested arrays round-tripped (sawEmpty=%v sawGT=%v)", iterations, sawEmpty, sawGT)
}

// flatShape describes a fixed nested schema: a scalar leaf or a set of named sub-shapes.
type flatShape struct {
	scalar bool
	sub    map[string]flatShape
}

func genFlatShape(rng *rand.Rand, depth, maxDepth int) flatShape {
	return genFlatShapeWith(rng, depth, maxDepth, genBareKey)
}

// genFlatShapeWith is the key-parametrized generator. keyFn chooses each nested
// key, so an adversarial alphabet (empty string, '>' arrangements) can be threaded
// through the whole flatten path.
func genFlatShapeWith(rng *rand.Rand, depth, maxDepth int, keyFn func(*rand.Rand) string) flatShape {
	if depth >= maxDepth || rng.Float64() < 0.45 {
		return flatShape{scalar: true}
	}
	sub := make(map[string]flatShape)
	n := 1 + rng.Intn(3)
	for i := 0; i < n; i++ {
		sub[keyFn(rng)] = genFlatShapeWith(rng, depth+1, maxDepth, keyFn)
	}
	if len(sub) == 0 {
		return flatShape{scalar: true}
	}
	return flatShape{sub: sub}
}

func materializeFlatShape(rng *rand.Rand, sh flatShape) any {
	if sh.scalar {
		return genScalar(rng)
	}
	obj := map[string]any{}
	for k, s := range sh.sub {
		// A nested sub-object is sometimes null (intermediate null — the case the
		// pre-fix encoder dropped) instead of a full object.
		if !s.scalar && rng.Float64() < 0.3 {
			obj[k] = nil
		} else {
			obj[k] = materializeFlatShape(rng, s)
		}
	}
	return obj
}

func genFlattenableArray(rng *rand.Rand) []any {
	return genFlattenableArrayWith(rng, genBareKey)
}

// adversarialFlattenKeys is the alphabet for the empty-key / '>' flatten fuzz. It
// deliberately includes the empty string and every leading/trailing/bare/interior
// '>' arrangement, so the flatten-eligibility guard (§7.4.6.1.3) is stressed on
// exactly the keys that would produce empty or literal path segments — the class
// that silently corrupted round-trips before the fix. Plain keys are mixed in so
// flatten still triggers (mixed eligible/ineligible fields).
var adversarialFlattenKeys = []string{"", ">", ">>", "a>b", "a>", ">b", ">a>", "a>>b", "a", "b", "c", "id", "m", "n"}

func genFlattenAdversarialKey(rng *rand.Rand) string {
	return adversarialFlattenKeys[rng.Intn(len(adversarialFlattenKeys))]
}

func genFlattenableArrayWith(rng *rand.Rand, keyFn func(*rand.Rand) string) []any {
	rows := 2 + rng.Intn(6)
	schema := map[string]flatShape{"id": {scalar: true}}
	order := []string{"id"}
	hasNested := false
	n := 1 + rng.Intn(3)
	for i := 0; i < n; i++ {
		k := keyFn(rng)
		if _, exists := schema[k]; exists {
			continue
		}
		s := genFlatShapeWith(rng, 1, 3, keyFn)
		schema[k] = s
		order = append(order, k)
		if !s.scalar {
			hasNested = true
		}
	}
	if !hasNested {
		k := keyFn(rng)
		if _, exists := schema[k]; !exists {
			schema[k] = flatShape{sub: map[string]flatShape{keyFn(rng): {sub: map[string]flatShape{keyFn(rng): {scalar: true}}}}}
			order = append(order, k)
		}
	}
	arr := make([]any, 0, rows)
	for i := 0; i < rows; i++ {
		row := map[string]any{}
		for _, f := range order {
			r := rng.Float64()
			if r < 0.12 {
				continue // field absent this row
			} else if r < 0.24 {
				row[f] = nil // field present-null (top-level null)
			} else {
				row[f] = materializeFlatShape(rng, schema[f])
			}
		}
		arr = append(arr, row)
	}
	return arr
}

// --- Value generators ---

func genValue(rng *rand.Rand, depth, maxDepth int) any {
	if depth >= maxDepth {
		return genScalar(rng)
	}
	switch rng.Intn(10) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	case 3, 4:
		return genString(rng)
	case 5, 6:
		return genObject(rng, depth, maxDepth)
	case 7, 8:
		return genArray(rng, depth, maxDepth)
	default:
		return genScalar(rng)
	}
}

func genScalar(rng *rand.Rand) any {
	switch rng.Intn(5) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	default:
		return genString(rng)
	}
}

func genNumber(rng *rand.Rand) float64 {
	switch rng.Intn(8) {
	case 0:
		return 0
	case 1:
		return float64(rng.Intn(1000))
	case 2:
		return -float64(rng.Intn(1000))
	case 3:
		return float64(rng.Intn(1000000)) + rng.Float64()
	case 4:
		return math.Copysign(0, -1) // negative zero
	case 5:
		// Large number requiring exponent.
		return float64(rng.Intn(999)+1) * 1e18
	case 6:
		// Small number requiring exponent.
		return float64(rng.Intn(999)+1) * 1e-10
	default:
		return rng.Float64()*2000 - 1000
	}
}

func genString(rng *rand.Rand) string {
	n := rng.Intn(20)
	var b strings.Builder
	for i := 0; i < n; i++ {
		switch rng.Intn(16) {
		case 0:
			b.WriteByte(' ')
		case 1:
			b.WriteRune(rune('a' + rng.Intn(26)))
		case 2:
			b.WriteRune(rune('A' + rng.Intn(26)))
		case 3:
			b.WriteRune(rune('0' + rng.Intn(10)))
		case 4:
			b.WriteByte('|')
		case 5:
			b.WriteByte(',')
		case 6:
			b.WriteByte('=')
		case 7:
			b.WriteByte('"')
		case 8:
			b.WriteByte('\\')
		case 9:
			b.WriteByte('\n')
		case 10:
			b.WriteByte('\t')
		case 11:
			// Unicode.
			b.WriteRune(rune(0x100 + rng.Intn(0x1000)))
		case 12:
			b.WriteByte('#')
		case 13:
			b.WriteByte('@')
		case 14:
			b.WriteByte('>')
		default:
			b.WriteRune(rune('a' + rng.Intn(26)))
		}
	}
	return b.String()
}

var bareKeyChars = "abcdefghijklmnopqrstuvwxyz_"

func genBareKey(rng *rand.Rand) string {
	n := 1 + rng.Intn(8)
	b := make([]byte, n)
	for i := range b {
		b[i] = bareKeyChars[rng.Intn(len(bareKeyChars))]
	}
	return string(b)
}

func genKey(rng *rand.Rand) string {
	if rng.Intn(4) == 0 {
		// Adversarial key that requires quoting.
		return genAdversarialString(rng)
	}
	return genBareKey(rng)
}

func genObject(rng *rand.Rand, depth, maxDepth int) map[string]any {
	n := rng.Intn(6)
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		key := genBareKey(rng)
		// Avoid duplicate keys.
		for j := 0; j < 3; j++ {
			if _, exists := m[key]; !exists {
				break
			}
			key = genBareKey(rng)
		}
		m[key] = genValue(rng, depth+1, maxDepth)
	}
	return m
}

func genArray(rng *rand.Rand, depth, maxDepth int) []any {
	n := rng.Intn(6)
	arr := make([]any, n)

	// Decide array type.
	switch rng.Intn(4) {
	case 0:
		// All primitives.
		for i := range arr {
			arr[i] = genScalar(rng)
		}
	case 1:
		// All objects (uniform, tabular).
		fields := make([]string, 1+rng.Intn(4))
		for j := range fields {
			fields[j] = genBareKey(rng)
		}
		for i := range arr {
			obj := make(map[string]any, len(fields))
			for _, f := range fields {
				if rng.Intn(5) == 0 {
					continue // missing field
				}
				obj[f] = genScalar(rng)
			}
			arr[i] = obj
		}
	case 2:
		// All objects with some nested values.
		for i := range arr {
			obj := make(map[string]any)
			obj[genBareKey(rng)] = genScalar(rng)
			if rng.Intn(3) == 0 && depth+1 < maxDepth {
				obj[genBareKey(rng)] = genValue(rng, depth+2, maxDepth)
			}
			arr[i] = obj
		}
	default:
		// Mixed.
		for i := range arr {
			arr[i] = genValue(rng, depth+1, maxDepth)
		}
	}
	return arr
}

// --- Adversarial generators ---

// Strings most likely to break scalar quoting.
var collisionStrings = []string{
	"true", "false", "-", "~", "^",
	"0", "1", "42", "-1", "3.14", "1e10", "-0",
	"", " ", "  ", " x", "x ",
	"#", "# comment", "@0", "@handle",
	"+1", ".5", "+.3", "01", "00",
	"null", "NULL", "True", "False",
	"|", ",", "=", "\"", "\\",
	"\n", "\r", "\t", "\b",
	"a|b", "a,b", "a=b",
	"hello world",
}

func genAdversarialString(rng *rand.Rand) string {
	if rng.Intn(3) == 0 {
		return collisionStrings[rng.Intn(len(collisionStrings))]
	}
	return genString(rng)
}

func genAdversarialValue(rng *rand.Rand, depth, maxDepth int) any {
	if depth >= maxDepth {
		return genAdversarialScalar(rng)
	}
	switch rng.Intn(8) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	case 3:
		return genAdversarialString(rng)
	case 4:
		return genAdversarialObject(rng, depth, maxDepth)
	case 5:
		return genAdversarialArray(rng, depth, maxDepth)
	case 6:
		// Empty containers.
		if rng.Intn(2) == 0 {
			return map[string]any{}
		}
		return []any{}
	default:
		return genAdversarialScalar(rng)
	}
}

func genAdversarialScalar(rng *rand.Rand) any {
	switch rng.Intn(6) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	default:
		return genAdversarialString(rng)
	}
}

func genAdversarialObject(rng *rand.Rand, depth, maxDepth int) map[string]any {
	n := rng.Intn(5)
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		key := genKey(rng)
		for j := 0; j < 3; j++ {
			if _, exists := m[key]; !exists {
				break
			}
			key = genKey(rng)
		}
		m[key] = genAdversarialValue(rng, depth+1, maxDepth)
	}
	return m
}

func genAdversarialArray(rng *rand.Rand, depth, maxDepth int) []any {
	n := rng.Intn(5)
	arr := make([]any, n)

	switch rng.Intn(5) {
	case 0:
		// Primitive array with collision strings.
		for i := range arr {
			arr[i] = genAdversarialScalar(rng)
		}
	case 1:
		// Uniform objects with missing/null mix.
		fields := []string{genBareKey(rng), genBareKey(rng), genBareKey(rng)}
		for i := range arr {
			obj := make(map[string]any)
			for _, f := range fields {
				switch rng.Intn(4) {
				case 0:
					// missing
				case 1:
					obj[f] = nil // null
				default:
					obj[f] = genAdversarialScalar(rng)
				}
			}
			arr[i] = obj
		}
	case 2:
		// Objects with nested values (tests ^ attachments).
		for i := range arr {
			obj := make(map[string]any)
			obj[genBareKey(rng)] = genAdversarialScalar(rng)
			if rng.Intn(2) == 0 && depth+1 < maxDepth {
				nested := make(map[string]any)
				nested[genBareKey(rng)] = genAdversarialScalar(rng)
				obj[genBareKey(rng)] = nested
			}
			if rng.Intn(3) == 0 {
				obj[genBareKey(rng)] = []any{genAdversarialScalar(rng)}
			}
			arr[i] = obj
		}
	case 3:
		// Nested arrays.
		for i := range arr {
			inner := make([]any, rng.Intn(3))
			for j := range inner {
				inner[j] = genAdversarialScalar(rng)
			}
			arr[i] = inner
		}
	default:
		// Mixed everything.
		for i := range arr {
			arr[i] = genAdversarialValue(rng, depth+1, maxDepth)
		}
	}
	return arr
}

// --- Comparison ---

// jsonDeepEqual normalizes both values through JSON marshaling to handle
// int64 vs float64 and map key ordering differences.
func jsonDeepEqual(a, b any) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var aNorm, bNorm any
	json.Unmarshal(aJSON, &aNorm)
	json.Unmarshal(bJSON, &bNorm)
	return reflect.DeepEqual(aNorm, bNorm)
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (%d bytes total)", len(s))
}

// TestFlattenOptOut verifies that flatten=false produces attachment syntax
// instead of path columns, and still round-trips correctly.
func TestFlattenOptOut(t *testing.T) {
	data := map[string]any{
		"orders": []any{
			map[string]any{
				"id":       "ORD-1",
				"customer": map[string]any{"name": "Alice", "email": "alice@co.com"},
				"total":    99.99,
			},
			map[string]any{
				"id":       "ORD-2",
				"customer": map[string]any{"name": "Bob", "email": "bob@co.com"},
				"total":    49.99,
			},
		},
	}

	// Default (flatten on): should have path columns.
	withFlatten := EncodeGeneric(data)
	t.Logf("=== FLATTEN ON (default) ===\n%s", withFlatten)
	if !strings.Contains(withFlatten, "customer>") {
		t.Fatalf("expected path columns with default flatten=true, got:\n%s", withFlatten)
	}

	// Flatten off: should have attachment syntax, no path columns.
	noFlatten := EncodeGeneric(data, GenericOptions{NoFlatten: true})
	t.Logf("=== FLATTEN OFF ===\n%s", noFlatten)
	if strings.Contains(noFlatten, "customer>") {
		t.Fatalf("expected no path columns with flatten=false, got:\n%s", noFlatten)
	}
	if !strings.Contains(noFlatten, ".customer") {
		t.Fatalf("expected attachment syntax with flatten=false, got:\n%s", noFlatten)
	}

	// Both must round-trip.
	for _, tc := range []struct {
		name string
		gcf  string
	}{
		{"flatten-on", withFlatten},
		{"flatten-off", noFlatten},
	} {
		decoded, err := DecodeGeneric(tc.gcf)
		if err != nil {
			t.Fatalf("%s: decode failed: %v\n  gcf: %q", tc.name, err, tc.gcf)
		}
		if !jsonDeepEqual(data, decoded) {
			t.Fatalf("%s: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				tc.name, jsonStr(data), tc.gcf, jsonStr(decoded))
		}
	}
}

// TestFlattenOptOutRoundTrip runs the property round-trip with flatten disabled.
func TestFlattenOptOutRoundTrip(t *testing.T) {
	iterations := getIterations(10_000)
	rng := rand.New(rand.NewSource(77))

	opts := GenericOptions{NoFlatten: true}
	for i := 0; i < iterations; i++ {
		val := genValue(rng, 0, 4)

		gcfText := EncodeGeneric(val, opts)

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}

		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d random values round-tripped with flatten=false", iterations)
}

// TestGtFieldEdgeCases covers all edge cases for field names containing ">".
func TestGtFieldEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		data any
	}{
		{
			name: "literal > key",
			data: []any{map[string]any{">": 1}, map[string]any{">": 2}},
		},
		{
			name: "> at start",
			data: []any{map[string]any{">foo": "a", "id": 1}, map[string]any{">foo": "b", "id": 2}},
		},
		{
			name: "> at end",
			data: []any{map[string]any{"foo>": "a", "id": 1}, map[string]any{"foo>": "b", "id": 2}},
		},
		{
			name: "double >>",
			data: []any{map[string]any{"a>>b": "x"}, map[string]any{"a>>b": "y"}},
		},
		{
			name: "multiple > in key",
			data: []any{map[string]any{"a>b>c": "x"}, map[string]any{"a>b>c": "y"}},
		},
		{
			name: "> field with null value",
			data: []any{map[string]any{"a>b": nil, "id": 1}, map[string]any{"a>b": "hello", "id": 2}},
		},
		{
			name: "> field with object value",
			data: []any{
				map[string]any{"a>b": map[string]any{"x": 1}, "id": 1},
				map[string]any{"a>b": map[string]any{"x": 2}, "id": 2},
			},
		},
		{
			name: "> field with array value",
			data: []any{
				map[string]any{"a>b": []any{1, 2}, "id": 1},
				map[string]any{"a>b": []any{3}, "id": 2},
			},
		},
		{
			name: "all fields have >",
			data: []any{map[string]any{">": 1, "a>b": 2}, map[string]any{">": 3, "a>b": 4}},
		},
		{
			name: "mix of > literal and flattened",
			data: []any{
				map[string]any{"id": 1, "x>y": "lit", "nested": map[string]any{"a": "v1", "b": "v2"}},
				map[string]any{"id": 2, "x>y": "lit2", "nested": map[string]any{"a": "v3", "b": "v4"}},
			},
		},
		{
			name: "> field absent in some rows",
			data: []any{
				map[string]any{"id": 1, "a>b": "present"},
				map[string]any{"id": 2},
			},
		},
		{
			name: "key looks like flattened path but is literal",
			data: []any{
				map[string]any{"id": 1, "customer>name": "Alice"},
				map[string]any{"id": 2, "customer>name": "Bob"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Test both flatten modes.
			for _, noFlatten := range []bool{false, true} {
				encoded := EncodeGeneric(tc.data, GenericOptions{NoFlatten: noFlatten})
				decoded, err := DecodeGeneric(encoded)
				if err != nil {
					t.Fatalf("noFlatten=%v: decode failed: %v\n  gcf: %q", noFlatten, err, encoded)
				}
				if !jsonDeepEqual(tc.data, decoded) {
					t.Fatalf("noFlatten=%v: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
						noFlatten, jsonStr(tc.data), encoded, jsonStr(decoded))
				}
			}
		})
	}
}
