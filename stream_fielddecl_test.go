package gcf

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
)

// TestPropertyRoundTripStreamArrayFieldNames fuzzes the streaming tabular header
// (GenericStreamEncoder.BeginArray) with adversarial field names -- comma, pipe,
// quote, empty, leading @/#/., spaces -- which the header previously joined raw,
// producing an invalid or ambiguous field declaration. Field names now format
// via formatKey (Section 2.4), matching the buffered tabular header. A field name
// containing '>' is rejected (a flattened path is not representable in a flat
// streaming row); that path is asserted separately below.
func TestPropertyRoundTripStreamArrayFieldNames(t *testing.T) {
	iterations := getIterations(200_000)
	rng := rand.New(rand.NewSource(0x5738))
	sawSpecial := false
	for i := 0; i < iterations; i++ {
		nf := 1 + rng.Intn(5)
		var fields []string
		for len(fields) < nf {
			f := genKey(rng)
			if strings.Contains(f, ">") {
				continue // '>' is rejected, tested separately
			}
			dup := false
			for _, x := range fields {
				if x == f {
					dup = true
					break
				}
			}
			if !dup {
				fields = append(fields, f)
				if f == "" || strings.ContainsAny(f, ",|\"") {
					sawSpecial = true
				}
			}
		}
		nr := 1 + rng.Intn(6)
		var buf bytes.Buffer
		enc := NewGenericStreamEncoder(&buf)
		enc.BeginArray("rows", fields)
		expected := make([]any, 0, nr)
		for r := 0; r < nr; r++ {
			row := make([]any, len(fields))
			obj := map[string]any{}
			for j, f := range fields {
				v := genScalar(rng)
				row[j] = v
				obj[f] = v
			}
			enc.WriteRow(row)
			expected = append(expected, obj)
		}
		enc.EndArray()
		if err := enc.Close(); err != nil {
			t.Fatalf("iter %d: close: %v\n fields: %v", i, err, fields)
		}
		wire := buf.String()
		decoded, err := DecodeGeneric(wire)
		want := map[string]any{"rows": expected}
		if err != nil {
			t.Fatalf("iter %d: decode failed: %v\n fields: %v\n wire: %q", i, err, fields, truncate(wire, 500))
		}
		if !jsonDeepEqual(want, decoded) {
			t.Fatalf("iter %d: round-trip mismatch\n want: %s\n got:  %s\n wire: %q", i, jsonStr(want), jsonStr(decoded), truncate(wire, 500))
		}
	}
	if !sawSpecial {
		t.Fatalf("generator never produced a field name needing quoting (empty / , | \")")
	}
	t.Logf("PASS: %d streaming arrays with adversarial field names round-tripped", iterations)
}

// TestStreamArrayFieldNameGtRejected locks the SPEC 8.3 requirement that a
// streaming value field name containing '>' is rejected (surfaced at Close).
func TestStreamArrayFieldNameGtRejected(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)
	enc.BeginArray("rows", []string{"id", "a>b"})
	enc.WriteRow([]any{1, 2})
	enc.EndArray()
	if err := enc.Close(); err == nil {
		t.Fatalf("expected an error for a '>' field name, got nil\n wire: %q", buf.String())
	}
}
