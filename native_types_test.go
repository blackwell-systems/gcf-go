package gcf

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// TestEncodeGenericNativeContainers verifies that EncodeGeneric normalizes
// native Go container types (e.g. []map[string]any, map[string]int, []struct)
// passed directly by the caller, producing the same output as the equivalent
// value routed through ParseJSONOrdered.
//
// Regression for https://github.com/blackwell-systems/gcf-go/issues/2: a nested
// []map[string]any is a distinct Go type from []any, so before normalization
// recursed into container fast paths it fell through to Go's fmt map printing
// instead of a tabular section.
func TestEncodeGenericNativeContainers(t *testing.T) {
	// The exact payload from the issue's Getting Started example.
	data := map[string]any{
		"employees": []map[string]any{
			{"id": 1, "name": "Alice", "department": "Engineering", "salary": 95000},
			{"id": 2, "name": "Bob", "department": "Sales", "salary": 72000},
		},
	}

	want := "GCF profile=generic\n" +
		"## employees [2]{department,id,name,salary}\n" +
		"Engineering|1|Alice|95000\n" +
		"Sales|2|Bob|72000\n"

	got := EncodeGeneric(data)
	if got != want {
		t.Errorf("native map[string]any input:\n got:\n%s\nwant:\n%s", got, want)
	}

	// Encoding the native value directly must match routing it through
	// ParseJSONOrdered, which fully normalizes to *OrderedMap/[]any.
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	val, err := ParseJSONOrdered(b)
	if err != nil {
		t.Fatalf("ParseJSONOrdered: %v", err)
	}
	if viaJSON := EncodeGeneric(val); got != viaJSON {
		t.Errorf("direct native input != via ParseJSONOrdered:\n direct:\n%s\n via:\n%s", got, viaJSON)
	}
}

// TestEncodeGenericNativeShapes covers additional native container shapes that
// share the same normalization path.
func TestEncodeGenericNativeShapes(t *testing.T) {
	type emp struct {
		ID   int
		Name string
		Dept string
	}
	cases := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "slice of ints",
			in:   map[string]any{"nums": []int{1, 2, 3}},
			want: "GCF profile=generic\nnums[3]: 1,2,3\n",
		},
		{
			name: "map of ints",
			in:   map[string]int{"a": 1, "b": 2},
			want: "GCF profile=generic\na=1\nb=2\n",
		},
		{
			name: "slice of structs",
			in:   map[string]any{"team": []emp{{1, "Alice", "Eng"}, {2, "Bob", "Sales"}}},
			want: "GCF profile=generic\n## team [2]{Dept,ID,Name}\nEng|1|Alice\nSales|2|Bob\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EncodeGeneric(tc.in); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// TestEncodeGenericNativeRoundTrip is an end-to-end losslessness check for the
// native-input path: DecodeGeneric(EncodeGeneric(v)) must equal v (normalized
// through JSON) for a spread of native Go container and scalar types, including
// nested []map[string]any, native scalar widths, structs, and arrays of arrays.
func TestEncodeGenericNativeRoundTrip(t *testing.T) {
	type addr struct {
		City string
		Zip  int
	}
	type person struct {
		ID   int
		Name string
		Addr addr
		Tags []string
	}
	cases := []struct {
		name string
		in   any
	}{
		{"issue example", map[string]any{"employees": []map[string]any{
			{"id": 1, "name": "Alice", "department": "Engineering", "salary": 95000},
			{"id": 2, "name": "Bob", "department": "Sales", "salary": 72000},
		}}},
		{"slice of ints", map[string]any{"nums": []int{1, 2, 3}}},
		{"map of ints", map[string]int{"a": 1, "b": 2, "c": 3}},
		{"nested native maps", map[string]any{
			"outer": map[string]int{"x": 1, "y": 2},
			"list":  []map[string]any{{"k": "v", "n": 3, "b": true}, {"k": "w", "n": 4, "b": false}},
		}},
		{"slice of structs", map[string]any{"people": []person{
			{1, "Alice", addr{"NYC", 10001}, []string{"a", "b"}},
			{2, "Bob", addr{"LA", 90001}, []string{"c"}},
		}}},
		{"array of arrays", map[string]any{"grid": [][]int{{1, 2}, {3, 4}, {5, 6}}}},
		{"mixed scalar widths", map[string]any{"f": float32(1.5), "i8": int8(7), "u": uint(9), "i64": int64(1 << 40), "s": "hi", "ok": true}},
		{"root native slice", []map[string]any{{"a": 1, "b": 2, "c": 3}, {"a": 4, "b": 5, "c": 6}}},
		{"root struct", person{7, "Carol", addr{"SF", 94107}, []string{"x"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := EncodeGeneric(tc.in)
			if !strings.HasPrefix(wire, "GCF profile=generic\n") {
				t.Fatalf("missing header: %q", wire)
			}
			decoded, err := DecodeGeneric(wire)
			if err != nil {
				t.Fatalf("decode failed: %v\nwire:\n%s", err, wire)
			}
			if !jsonDeepEqual(tc.in, decoded) {
				t.Fatalf("round-trip mismatch\n in:      %s\n wire:\n%s decoded: %s",
					jsonStr(tc.in), wire, jsonStr(decoded))
			}
		})
	}
}

// genNativeValue produces random values built from concrete native Go container
// types ([]int, []map[string]any, map[string]int, map[string]any) rather than the
// canonical []any/map[string]any the other property tests use. This specifically
// exercises the recursive normalization that reaches the encoder's type switches.
func genNativeValue(rng *rand.Rand, depth, maxDepth int) any {
	if depth >= maxDepth || rng.Intn(3) == 0 {
		switch rng.Intn(5) {
		case 0:
			return rng.Intn(1000)
		case 1:
			return int64(rng.Intn(1000))
		case 2:
			return rng.Float64() * 100
		case 3:
			return rng.Intn(2) == 0
		default:
			return fmt.Sprintf("s%d", rng.Intn(100))
		}
	}
	switch rng.Intn(4) {
	case 0:
		n := rng.Intn(4)
		a := make([]int, n)
		for i := range a {
			a[i] = rng.Intn(100)
		}
		return a
	case 1:
		// Uniform []map[string]any (drives the tabular path).
		n := rng.Intn(3) + 1
		fields := []string{"a", "b", "c"}
		arr := make([]map[string]any, n)
		for i := range arr {
			m := map[string]any{}
			for _, f := range fields {
				m[f] = genNativeValue(rng, depth+1, maxDepth)
			}
			arr[i] = m
		}
		return arr
	case 2:
		m := map[string]any{}
		k := rng.Intn(3) + 1
		for i := 0; i < k; i++ {
			m[fmt.Sprintf("k%d", i)] = genNativeValue(rng, depth+1, maxDepth)
		}
		return m
	default:
		m := map[string]int{}
		k := rng.Intn(3) + 1
		for i := 0; i < k; i++ {
			m[fmt.Sprintf("n%d", i)] = rng.Intn(100)
		}
		return m
	}
}

// TestEncodeGenericNativeRoundTripFuzz round-trips randomly generated native-typed
// containers to prove the recursive normalizer is lossless across depth and across
// concrete Go container/scalar types, not just for the hand-written cases above.
func TestEncodeGenericNativeRoundTripFuzz(t *testing.T) {
	iterations := getIterations(20_000)
	rng := rand.New(rand.NewSource(2026))
	for i := 0; i < iterations; i++ {
		val := map[string]any{"root": genNativeValue(rng, 0, 4)}
		wire := EncodeGeneric(val)
		decoded, err := DecodeGeneric(wire)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n in:   %s\n wire:\n%s", i, err, jsonStr(val), wire)
		}
		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n in:      %s\n wire:\n%s decoded: %s",
				i, jsonStr(val), wire, jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d native-typed values round-tripped losslessly", iterations)
}
