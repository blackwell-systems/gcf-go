package gcf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// EncodeGeneric encodes any value into GCF tabular format.
// Unlike Encode (which handles the graph Payload type), EncodeGeneric
// works on arbitrary maps, slices, and primitives using GCF's tabular
// encoding grammar.
func EncodeGeneric(data any) string {
	if data == nil {
		return ""
	}
	v := reflect.ValueOf(data)
	if !v.IsValid() {
		return ""
	}
	// Dereference pointers.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		lines := encodeGenericObject(v, 0)
		return strings.Join(lines, "\n")
	case reflect.Slice:
		if v.IsNil() || v.Len() == 0 {
			return ""
		}
		lines := encodeGenericArray(v, "", 0)
		return strings.Join(lines, "\n")
	case reflect.Struct:
		lines := encodeGenericStruct(v, 0)
		return strings.Join(lines, "\n")
	default:
		return formatGenericValue(v)
	}
}

// encodeGenericObject encodes a map value (must be of kind Map).
func encodeGenericObject(v reflect.Value, depth int) []string {
	var lines []string
	keys := sortedMapKeys(v)

	for _, keyStr := range keys {
		val := v.MapIndex(reflect.ValueOf(keyStr))
		val = derefValue(val)

		if !val.IsValid() || isNilValue(val) {
			continue
		}

		switch {
		case isSliceOrArray(val):
			lines = append(lines, encodeGenericArray(val, keyStr, depth)...)
		case isObject(val):
			lines = append(lines, indentStr(depth)+"## "+keyStr)
			lines = append(lines, encodeGenericObjectAny(val, depth+1)...)
		default:
			lines = append(lines, indentStr(depth)+keyStr+"="+formatGenericValue(val))
		}
	}
	return lines
}

// encodeGenericStruct encodes a struct value.
func encodeGenericStruct(v reflect.Value, depth int) []string {
	var lines []string
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		fv := derefValue(v.Field(i))
		if !fv.IsValid() || isNilValue(fv) {
			continue
		}

		name := field.Name

		switch {
		case isSliceOrArray(fv):
			lines = append(lines, encodeGenericArray(fv, name, depth)...)
		case isObject(fv):
			lines = append(lines, indentStr(depth)+"## "+name)
			lines = append(lines, encodeGenericObjectAny(fv, depth+1)...)
		default:
			lines = append(lines, indentStr(depth)+name+"="+formatGenericValue(fv))
		}
	}
	return lines
}

// encodeGenericObjectAny dispatches encoding for a map or struct value.
func encodeGenericObjectAny(v reflect.Value, depth int) []string {
	v = derefValue(v)
	switch v.Kind() {
	case reflect.Map:
		return encodeGenericObject(v, depth)
	case reflect.Struct:
		return encodeGenericStruct(v, depth)
	default:
		return nil
	}
}

// encodeGenericArray encodes a slice/array value.
func encodeGenericArray(v reflect.Value, name string, depth int) []string {
	if v.Len() == 0 {
		if name != "" {
			return []string{indentStr(depth) + "## " + name + " [0]"}
		}
		return nil
	}

	// Check for uniform object array (tabular encoding).
	if isUniformGenericObjectArray(v) {
		return encodeGenericTabular(v, name, depth)
	}

	// Non-uniform: per-item encoding.
	var lines []string
	if name != "" {
		lines = append(lines, fmt.Sprintf("%s## %s [%d]", indentStr(depth), name, v.Len()))
	}
	for i := 0; i < v.Len(); i++ {
		item := derefValue(v.Index(i))
		if !item.IsValid() {
			continue
		}
		switch {
		case isObject(item):
			lines = append(lines, fmt.Sprintf("%s@%d", indentStr(depth), i))
			lines = append(lines, encodeGenericObjectAny(item, depth+1)...)
		case isSliceOrArray(item):
			lines = append(lines, encodeGenericArray(item, fmt.Sprintf("%d", i), depth+1)...)
		default:
			lines = append(lines, fmt.Sprintf("%s@%d %s", indentStr(depth), i, formatGenericValue(item)))
		}
	}
	return lines
}

// encodeGenericTabular encodes a uniform array of objects as a table.
func encodeGenericTabular(v reflect.Value, name string, depth int) []string {
	// Extract field names from first element.
	first := derefValue(v.Index(0))
	allFields := objectKeys(first)

	// Separate primitive from nested fields (sampled from first element).
	var primitiveFields, nestedFields []string
	for _, f := range allFields {
		sample := derefValue(objectFieldValue(first, f))
		if sample.IsValid() && (isObject(sample) || isSliceOrArray(sample)) {
			nestedFields = append(nestedFields, f)
		} else {
			primitiveFields = append(primitiveFields, f)
		}
	}

	// Header.
	var lines []string
	var header string
	if name != "" {
		header = fmt.Sprintf("## %s [%d]{%s}", name, v.Len(), strings.Join(primitiveFields, ","))
	} else {
		header = fmt.Sprintf("## [%d]{%s}", v.Len(), strings.Join(primitiveFields, ","))
	}
	lines = append(lines, indentStr(depth)+header)

	hasNested := len(nestedFields) > 0

	for i := 0; i < v.Len(); i++ {
		row := derefValue(v.Index(i))
		vals := make([]string, len(primitiveFields))
		for j, f := range primitiveFields {
			fv := objectFieldValue(row, f)
			fv = derefValue(fv)
			if !fv.IsValid() || isNilValue(fv) {
				vals[j] = "-"
			} else {
				vals[j] = formatGenericValue(fv)
			}
		}

		rowStr := strings.Join(vals, "|")
		if hasNested {
			lines = append(lines, fmt.Sprintf("%s@%d %s", indentStr(depth), i, rowStr))
		} else {
			lines = append(lines, indentStr(depth)+rowStr)
		}

		// Inline nested fields after the row.
		if hasNested {
			for _, nf := range nestedFields {
				nv := derefValue(objectFieldValue(row, nf))
				if !nv.IsValid() || isNilValue(nv) {
					continue
				}
				if isSliceOrArray(nv) {
					lines = append(lines, encodeGenericArray(nv, nf, depth+1)...)
				} else if isObject(nv) {
					lines = append(lines, indentStr(depth+1)+"."+nf)
					lines = append(lines, encodeGenericObjectAny(nv, depth+2)...)
				}
			}
		}
	}
	return lines
}

// isUniformGenericObjectArray checks whether a slice contains uniform objects
// (same keys). Samples up to 5 items; requires 70% key overlap.
func isUniformGenericObjectArray(v reflect.Value) bool {
	if v.Len() == 0 {
		return false
	}
	first := derefValue(v.Index(0))
	if !isObject(first) {
		return false
	}
	firstKeys := objectKeys(first)
	if len(firstKeys) == 0 {
		return false
	}
	firstSet := make(map[string]struct{}, len(firstKeys))
	for _, k := range firstKeys {
		firstSet[k] = struct{}{}
	}

	checkCount := v.Len()
	if checkCount > 5 {
		checkCount = 5
	}
	for i := 1; i < checkCount; i++ {
		item := derefValue(v.Index(i))
		if !isObject(item) {
			return false
		}
		itemKeys := objectKeys(item)
		overlap := 0
		for _, k := range itemKeys {
			if _, ok := firstSet[k]; ok {
				overlap++
			}
		}
		if float64(overlap) < float64(len(firstKeys))*0.7 {
			return false
		}
	}
	return true
}

// formatGenericValue converts a reflect.Value to its GCF string representation.
func formatGenericValue(v reflect.Value) string {
	v = derefValue(v)
	if !v.IsValid() || isNilValue(v) {
		return "-"
	}

	// Unwrap interface values.
	if v.Kind() == reflect.Interface {
		v = v.Elem()
		if !v.IsValid() {
			return "-"
		}
	}

	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		s := fmt.Sprintf("%g", v.Float())
		return s
	case reflect.String:
		s := v.String()
		if s == "" {
			return `""`
		}
		if strings.ContainsAny(s, "|\n") {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
		return s
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// objectKeys returns the field/key names for a map or struct value.
func objectKeys(v reflect.Value) []string {
	v = derefValue(v)
	switch v.Kind() {
	case reflect.Map:
		return sortedMapKeys(v)
	case reflect.Struct:
		t := v.Type()
		var keys []string
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() {
				keys = append(keys, t.Field(i).Name)
			}
		}
		return keys
	default:
		return nil
	}
}

// objectFieldValue returns the value for a given field name in a map or struct.
func objectFieldValue(v reflect.Value, key string) reflect.Value {
	v = derefValue(v)
	switch v.Kind() {
	case reflect.Map:
		return v.MapIndex(reflect.ValueOf(key))
	case reflect.Struct:
		return v.FieldByName(key)
	default:
		return reflect.Value{}
	}
}

// sortedMapKeys returns map keys as strings, sorted alphabetically.
func sortedMapKeys(v reflect.Value) []string {
	keys := v.MapKeys()
	strs := make([]string, len(keys))
	for i, k := range keys {
		strs[i] = fmt.Sprintf("%v", k.Interface())
	}
	sort.Strings(strs)
	return strs
}

// derefValue dereferences pointers and interfaces to get the underlying value.
func derefValue(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		v = v.Elem()
	}
	return v
}

// isNilValue reports whether v is a nil pointer, interface, slice, or map.
func isNilValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:
		return v.IsNil()
	default:
		return false
	}
}

// isObject reports whether v is a map or struct (not a slice/array).
func isObject(v reflect.Value) bool {
	v = derefValue(v)
	return v.Kind() == reflect.Map || v.Kind() == reflect.Struct
}

// isSliceOrArray reports whether v is a slice or array.
func isSliceOrArray(v reflect.Value) bool {
	v = derefValue(v)
	return v.Kind() == reflect.Slice || v.Kind() == reflect.Array
}

// indentStr returns 2*depth spaces for indentation.
func indentStr(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
}
