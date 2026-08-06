package gcf

import (
	"encoding/json"
	"testing"
)

func normJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestKeyedMapRoundTrip(t *testing.T) {
	cases := map[string]any{
		"flat uniform": map[string]any{
			"web-01": map[string]any{"cpu": 23, "mem": 61, "status": "ok"},
			"db-01":  map[string]any{"cpu": 41, "mem": 83, "status": "ok"},
		},
		"semi-uniform (union + ~)": map[string]any{
			"a": map[string]any{"x": 1, "y": 2},
			"b": map[string]any{"x": 3, "z": 9},
		},
		"null field": map[string]any{
			"a": map[string]any{"x": 1, "note": nil},
			"b": map[string]any{"x": 2, "note": "hi"},
		},
		"numeric-like keys": map[string]any{
			"123":  map[string]any{"v": 1},
			"4.5":  map[string]any{"v": 2},
			"true": map[string]any{"v": 3},
		},
		"key with pipe and empty": map[string]any{
			"a|b": map[string]any{"v": 1},
			"":    map[string]any{"v": 2},
		},
		"single member": map[string]any{
			"only": map[string]any{"x": 1, "y": 2},
		},
		"label collision (value field named key)": map[string]any{
			"a": map[string]any{"key": "K1", "v": 1},
			"b": map[string]any{"key": "K2", "v": 2},
		},
		"nested value object": map[string]any{
			"a": map[string]any{"id": 1, "meta": map[string]any{"region": "us", "tier": "gold"}},
			"b": map[string]any{"id": 2, "meta": map[string]any{"region": "eu", "tier": "silver"}},
		},
		"named/nested keyed map": map[string]any{
			"servers": map[string]any{
				"web-01": map[string]any{"cpu": 1, "mem": 2},
				"db-01":  map[string]any{"cpu": 3, "mem": 4},
			},
			"note": "hello",
		},
		"fallback: scalar values (not keyed)": map[string]any{
			"a": 1, "b": "two",
		},
		"fallback: empty map": map[string]any{},
		"fallback: all-empty value objects": map[string]any{
			"a": map[string]any{}, "b": map[string]any{},
		},
	}
	for name, orig := range cases {
		wire := EncodeGeneric(orig, GenericOptions{KeyedMap: true})
		got, err := DecodeGeneric(wire)
		if err != nil {
			t.Errorf("[%s] decode error: %v\nwire:\n%s", name, err, wire)
			continue
		}
		if normJSON(t, orig) != normJSON(t, got) {
			t.Errorf("[%s] MISMATCH\n orig: %s\n got:  %s\n wire:\n%s", name, normJSON(t, orig), normJSON(t, got), wire)
		}
	}
}
