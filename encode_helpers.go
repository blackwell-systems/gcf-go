package gcf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func tabularFields(arr []any) []string {
	if len(arr) == 0 {
		return nil
	}
	var fieldOrder []string
	seen := make(map[string]struct{})
	for _, item := range arr {
		if !isObjectItem(item) {
			return nil // not an object
		}
		// An empty object is a valid object that contributes no fields to the
		// union; it must not disqualify the array from tabular form (Section 7.3).
		// objectItemKeys returns nil for an empty object, so a nil-keys check here
		// would wrongly conflate "empty object" with "not an object".
		keys := objectItemKeys(item)
		for _, k := range keys {
			if _, exists := seen[k]; !exists {
				fieldOrder = append(fieldOrder, k)
				seen[k] = struct{}{}
			}
		}
	}
	if len(fieldOrder) == 0 {
		return nil // all empty objects: use expanded form
	}
	return fieldOrder
}

// isObjectItem reports whether item is a JSON object (OrderedMap or
// map[string]any), distinct from objectItemKeys which cannot tell an empty
// object apart from a non-object (both yield nil keys).
func isObjectItem(item any) bool {
	switch item.(type) {
	case *OrderedMap, map[string]any:
		return true
	default:
		return false
	}
}

// objectItemKeys returns the keys of an object item (OrderedMap or map[string]any).
// Returns nil if the item is not an object.
func objectItemKeys(item any) []string {
	switch m := item.(type) {
	case *OrderedMap:
		return m.Keys()
	case map[string]any:
		return orderedKeys(m)
	default:
		return nil
	}
}

// objectItemGet retrieves a value from an object item by key.
func objectItemGet(item any, key string) (any, bool) {
	switch m := item.(type) {
	case *OrderedMap:
		return m.Get(key)
	case map[string]any:
		v, ok := m[key]
		return v, ok
	default:
		return nil, false
	}
}

// encodeTabular encodes an array of objects in tabular form.
// Handles both *OrderedMap and map[string]any elements.
func allPrimitives(arr []any) bool {
	for _, v := range arr {
		switch v.(type) {
		case *OrderedMap, map[string]any, []any:
			return false
		}
	}
	return true
}

// orderedKeys returns map keys in lexicographic order.
// Go maps are unordered, so per spec Section 7.2 we use lexicographic ordering.
func orderedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toAny converts arbitrary Go values to JSON-compatible any types, recursively.
//
// The container fast paths recurse into their values so that native Go types the
// caller passes directly (e.g. []map[string]any, map[string]int, []MyStruct) are
// normalized at every depth. The encoder's type switches recognize only
// *OrderedMap, map[string]any, and []any; without full recursion a nested value
// such as []map[string]any (a distinct type from []any) would fall through to the
// default scalar path and emit Go's fmt map printing instead of a tabular section.
func toAny(data any) any {
	if data == nil {
		return nil
	}
	switch v := data.(type) {
	case *OrderedMap:
		out := NewOrderedMap()
		for _, k := range v.Keys() {
			val, _ := v.Get(k)
			out.Set(k, toAny(val))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = toAny(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = toAny(val)
		}
		return out
	case string:
		return v
	case bool:
		return v
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	v := reflect.ValueOf(data)
	return reflectToAny(v)
}

func reflectToAny(v reflect.Value) any {
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Map:
		m := make(map[string]any, v.Len())
		for _, k := range v.MapKeys() {
			m[fmt.Sprintf("%v", k.Interface())] = reflectToAny(v.MapIndex(k))
		}
		return m
	case reflect.Slice, reflect.Array:
		if v.IsNil() {
			return nil
		}
		arr := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			arr[i] = reflectToAny(v.Index(i))
		}
		return arr
	case reflect.Struct:
		m := make(map[string]any)
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			m[f.Name] = reflectToAny(v.Field(i))
		}
		return m
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.String:
		return v.String()
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// indentStr returns 2*depth spaces for indentation.
func indentStr(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
}
