package gcf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// TestKeyedMapSelectionR3 locks the R3 selection rule (single-member-wrapper
// deferral) at the wire level. Round-trip alone cannot prove selection because
// both the greedy and the R3 form round-trip; these assert the chosen form.
func TestKeyedMapSelectionR3(t *testing.T) {
	ord := func(s string) any { v, _ := ParseJSONOrdered([]byte(s)); return v }
	enc := func(s string) string { return EncodeGeneric(ord(s), GenericOptions{KeyedMap: true}) }

	// (1) Single-member wrapper of a map defers to a NAMED keyed map, not a
	// greedy one-row table that flattens the inner map's keys into columns.
	if w := enc(`{"users":{"u1":{"a":1,"b":2},"u2":{"a":3,"b":4}}}`); !strings.Contains(w, "## users [2:]{key,a,b}") {
		t.Errorf("wrapper should defer to `## users [2:]{key,a,b}`, got:\n%s", w)
	}

	// (2) Non-regression: a record with a uniform nested-object field still
	// flattens per Section 7.4.6 (objects with >=2 members tabulate as before).
	if w := enc(`{"row1":{"tags":{"a":1,"b":2}},"row2":{"tags":{"a":3,"b":4}}}`); !strings.Contains(w, `"tags>a"`) {
		t.Errorf("nested record field should flatten to tags>a, got:\n%s", w)
	}

	// (3) An empty-string wrapper key defers to a quoted empty-name section
	// (`## "" `), distinct from the anonymous root form, and round-trips.
	inEmpty := `{"":{"g":{"kpdy":{"z":1},"v":"s"}}}`
	if w := enc(inEmpty); !strings.Contains(w, `## "" `) {
		t.Errorf("empty-key wrapper should defer to `## \"\" ` (named empty), got:\n%s", w)
	} else {
		got, err := DecodeGeneric(w)
		if err != nil {
			t.Fatalf("empty-name decode: %v\n%s", err, w)
		}
		if normJSON(t, ord(inEmpty)) != normJSON(t, got) {
			t.Errorf("empty-name wrapper round-trip broken\n want: %s\n got:  %s", normJSON(t, ord(inEmpty)), normJSON(t, got))
		}
	}

	// (4) A single flat record still keys as [1:] (mirrors a one-row array),
	// since its value is not itself keyed-eligible.
	if w := enc(`{"only":{"a":1,"b":2}}`); !strings.Contains(w, "## [1:]{key,a,b}") {
		t.Errorf("single flat record should key as `## [1:]{key,a,b}`, got:\n%s", w)
	}
}

func TestKeyedMapNestedPositions(t *testing.T) {
	kmap := func() map[string]any {
		return map[string]any{
			"web-01": map[string]any{"cpu": 1, "mem": 2},
			"db-01":  map[string]any{"cpu": 3, "mem": 4},
		}
	}
	cases := map[string]any{
		// keyed map as an expanded-array item (mixed array)
		"expanded item": []any{kmap(), 42, "tail"},
		// keyed map as a tabular-row attachment (nested field is a map of objects)
		"tabular attachment": []any{
			map[string]any{"id": 1, "nodes": kmap()},
			map[string]any{"id": 2, "nodes": map[string]any{"x": map[string]any{"cpu": 9, "mem": 8}}},
		},
		// keyed map whose value objects themselves contain a keyed map (deep)
		"deep nested keyed map": map[string]any{
			"a": map[string]any{"id": 1, "sub": map[string]any{"s1": map[string]any{"v": 1}, "s2": map[string]any{"v": 2}}},
			"b": map[string]any{"id": 2, "sub": map[string]any{"s3": map[string]any{"v": 3}}},
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
		} else {
			t.Logf("[%s] OK\n%s", name, wire)
		}
	}
}

func TestKeyedMapStreaming(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)
	enc.BeginKeyedMap("servers", "key", []string{"cpu", "mem"})
	enc.WriteRow([]any{"web-01", 23, 61})
	enc.WriteRow([]any{"db-01", 41, 83})
	enc.WriteRow([]any{"cache-1", 7, 12})
	enc.EndArray()
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	wire := buf.String()
	got, err := DecodeGeneric(wire)
	if err != nil {
		t.Fatalf("decode: %v\nwire:\n%s", err, wire)
	}
	want := map[string]any{
		"servers": map[string]any{
			"web-01":  map[string]any{"cpu": 23, "mem": 61},
			"db-01":   map[string]any{"cpu": 41, "mem": 83},
			"cache-1": map[string]any{"cpu": 7, "mem": 12},
		},
	}
	if normJSON(t, want) != normJSON(t, got) {
		t.Errorf("MISMATCH\n want: %s\n got:  %s\n wire:\n%s", normJSON(t, want), normJSON(t, got), wire)
	} else {
		t.Logf("streaming keyed map OK\n%s", wire)
	}
}

// keyedMapToSet / setToKeyedMap: a keyed map IS a GenericSet whose identity is
// the map key (SPEC 7.2a delta reuses 10a unchanged).
func keyedMapToSet(m map[string]any, keyLabel string) GenericSet {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	var vf []string
	for _, k := range keys {
		vo := m[k].(map[string]any)
		vk := make([]string, 0, len(vo))
		for f := range vo {
			vk = append(vk, f)
		}
		sort.Strings(vk)
		for _, f := range vk {
			if !seen[f] {
				seen[f] = true
				vf = append(vf, f)
			}
		}
	}
	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		row := map[string]any{keyLabel: k}
		for f, v := range m[k].(map[string]any) {
			row[f] = v
		}
		rows = append(rows, row)
	}
	return GenericSet{Name: "servers", Key: keyLabel, Fields: append([]string{keyLabel}, vf...), Rows: rows}
}

func setToKeyedMap(s GenericSet) map[string]any {
	out := make(map[string]any, len(s.Rows))
	for _, row := range s.Rows {
		k := fmt.Sprintf("%v", row[s.Key])
		vo := map[string]any{}
		for f, v := range row {
			if f != s.Key {
				vo[f] = v
			}
		}
		out[k] = vo
	}
	return out
}

func TestKeyedMapDelta(t *testing.T) {
	base := map[string]any{
		"web-01": map[string]any{"cpu": 20, "status": "ok"},
		"db-01":  map[string]any{"cpu": 40, "status": "ok"},
		"old":    map[string]any{"cpu": 5, "status": "warn"},
	}
	next := map[string]any{
		"web-01": map[string]any{"cpu": 88, "status": "warn"}, // changed
		"db-01":  map[string]any{"cpu": 40, "status": "ok"},   // unchanged
		"new":    map[string]any{"cpu": 1, "status": "ok"},    // added
		// "old" removed
	}
	bset := keyedMapToSet(base, "key")
	nset := keyedMapToSet(next, "key")

	d, err := DiffGenericSets(bset, nset)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	wire := EncodeGenericDelta(d)
	recon, err := VerifyGenericDelta(bset, d, GenericPackRoot(nset))
	if err != nil {
		t.Fatalf("verify: %v\ndelta:\n%s", err, wire)
	}
	got := setToKeyedMap(recon)
	if normJSON(t, next) != normJSON(t, got) {
		t.Errorf("MISMATCH\n next: %s\n got:  %s\ndelta:\n%s", normJSON(t, next), normJSON(t, got), wire)
	} else {
		t.Logf("keyed-map delta OK (map key = identity)\n%s", wire)
	}
}
