package gcf

import (
	"math"
	"strings"
	"testing"
)

// The encoder MUST reject a native integer outside the int64 numeric domain (SPEC
// 2.3.2). This is the encode-side enforcement that a shared JSON conformance fixture
// cannot express, because JSON text has no way to carry a native uint64 above the
// int64 maximum. EncodeGenericChecked returns the error; the infallible EncodeGeneric
// panics with the same message (loud rejection, no wire emitted, no substitution).
func TestEncodeGenericNativeInt64Domain(t *testing.T) {
	// In-domain values: no error from the checked form, no panic from the plain form.
	inDomain := []any{
		map[string]any{"n": int64(math.MaxInt64)},
		map[string]any{"n": int64(math.MinInt64)},
		map[string]any{"n": uint64(math.MaxInt64)}, // fits int64 exactly
		map[string]any{"n": uint(0)},
	}
	for _, v := range inDomain {
		if _, err := EncodeGenericChecked(v); err != nil {
			t.Errorf("in-domain value %#v: unexpected error: %v", v, err)
		}
		_ = EncodeGeneric(v) // must not panic
	}

	// A uint64 above int64 max is out of domain wherever it sits in the value.
	type withU64 struct{ N uint64 }
	outOfDomain := []any{
		map[string]any{"n": uint64(math.MaxInt64) + 1},
		map[string]any{"outer": map[string]any{"n": uint64(math.MaxUint64)}},
		[]any{int64(1), uint64(math.MaxInt64) + 1},
		withU64{N: math.MaxUint64},
	}
	for _, v := range outOfDomain {
		// Checked form returns an out_of_range error, no wire.
		got, err := EncodeGenericChecked(v)
		if err == nil {
			t.Errorf("out-of-domain value %#v: expected error, got wire %q", v, got)
		} else if !strings.Contains(err.Error(), "out_of_range") {
			t.Errorf("out-of-domain value %#v: error missing out_of_range: %v", v, err)
		}
		// Plain form panics with the same out_of_range error.
		assertOutOfRangePanic(t, v)
	}
}

func assertOutOfRangePanic(t *testing.T, v any) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("EncodeGeneric(%#v): expected panic, got none", v)
			return
		}
		err, ok := r.(error)
		if !ok || !strings.Contains(err.Error(), "out_of_range") {
			t.Errorf("EncodeGeneric(%#v): panic value is not an out_of_range error: %v", v, r)
		}
	}()
	EncodeGeneric(v)
}
