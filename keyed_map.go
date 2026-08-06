package gcf

import (
	"fmt"
	"strings"
)

// keyedMapEligible reports whether an object (map[string]any or *OrderedMap) is
// a keyed map of objects that should render as a keyed table `## [N:]{key,...}`
// (SPEC 7.2a, prototype). It returns the ordered member keys, the corresponding
// value objects, the ordered value-field union, and the key-column label.
func keyedMapEligible(m any, opts encodeOpts) (keys []string, values []any, valueFields []string, keyLabel string, ok bool) {
	switch mm := m.(type) {
	case map[string]any:
		if len(mm) == 0 {
			return nil, nil, nil, "", false
		}
		keys = orderedKeys(mm)
		for _, k := range keys {
			values = append(values, mm[k])
		}
	case *OrderedMap:
		if mm.Len() == 0 {
			return nil, nil, nil, "", false
		}
		keys = mm.Keys()
		for _, k := range keys {
			v, _ := mm.Get(k)
			values = append(values, v)
		}
	default:
		return nil, nil, nil, "", false
	}

	// Every value must be an object; build the ordered field union.
	seen := make(map[string]bool)
	for _, v := range values {
		var vk []string
		switch vo := v.(type) {
		case map[string]any:
			vk = orderedKeys(vo)
		case *OrderedMap:
			vk = vo.Keys()
		default:
			return nil, nil, nil, "", false // non-object value
		}
		for _, f := range vk {
			if !seen[f] {
				seen[f] = true
				valueFields = append(valueFields, f)
			}
		}
	}
	if len(valueFields) == 0 {
		return nil, nil, nil, "", false // all-empty value objects
	}

	// Key-column label: "key", made unique by prepending "_" on collision.
	keyLabel = "key"
	inUnion := func(s string) bool {
		for _, f := range valueFields {
			if f == s {
				return true
			}
		}
		return false
	}
	for inUnion(keyLabel) {
		keyLabel = "_" + keyLabel
	}
	return keys, values, valueFields, keyLabel, true
}

// encodeKeyedMap emits a keyed table for a map of objects. It augments each
// value object with the key column and routes through encodeTabular with the
// keyed bracket, so nested-value handling (flatten/inline/attachment/null/
// absent) is inherited unchanged. name is empty for a root/anonymous map.
func encodeKeyedMap(b *strings.Builder, name string, keys []string, values []any, valueFields []string, keyLabel string, depth int, opts encodeOpts) {
	encodeKeyedMapWithPrefix(b, keyedHeaderPrefix(name, depth), keys, values, valueFields, keyLabel, depth, opts)
}

// keyedHeaderPrefix builds the `## `/`## name ` header prefix for a keyed map.
func keyedHeaderPrefix(name string, depth int) string {
	prefix := indentStr(depth)
	if name == "" {
		return prefix + "## "
	}
	return prefix + "## " + formatKey(name) + " "
}

// encodeKeyedMapWithPrefix emits `<headerPrefix>[N:]{...}` and the keyed rows,
// reusing encodeTabular. headerPrefix is the full prefix up to the count bracket.
func encodeKeyedMapWithPrefix(b *strings.Builder, headerPrefix string, keys []string, values []any, valueFields []string, keyLabel string, depth int, opts encodeOpts) {
	fields := make([]string, 0, len(valueFields)+1)
	fields = append(fields, keyLabel)
	fields = append(fields, valueFields...)

	arr := make([]any, len(keys))
	for i, k := range keys {
		aug := make(map[string]any, len(valueFields)+1)
		switch vo := values[i].(type) {
		case map[string]any:
			for kk, vv := range vo {
				aug[kk] = vv
			}
		case *OrderedMap:
			for _, kk := range vo.Keys() {
				vv, _ := vo.Get(kk)
				aug[kk] = vv
			}
		}
		aug[keyLabel] = k
		arr[i] = aug
	}
	encodeTabular(b, headerPrefix, arr, fields, depth, opts, true)
}

// keyedRowsToMap reconstructs the map from decoded keyed-table rows: the first
// declared field is the member key; the remaining fields form the value object.
func keyedRowsToMap(rows []any, fields []string) (map[string]any, error) {
	if len(fields) < 2 {
		return nil, fmt.Errorf("keyed_map: header must declare at least two fields")
	}
	keyLabel := fields[0]
	out := make(map[string]any, len(rows))
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("keyed_map: row is not an object")
		}
		kv, present := row[keyLabel]
		if !present {
			return nil, fmt.Errorf("keyed_map: row missing key column %q", keyLabel)
		}
		ks, ok := kv.(string)
		if !ok {
			ks = fmt.Sprintf("%v", kv)
		}
		if _, dup := out[ks]; dup {
			return nil, fmt.Errorf("keyed_map: duplicate member key %q", ks)
		}
		delete(row, keyLabel)
		out[ks] = row
	}
	return out, nil
}
