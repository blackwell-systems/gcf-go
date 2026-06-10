package gcf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// EncodeGeneric encodes any JSON-compatible value into GCF v2.0 generic profile.
// Input should be map[string]any, []any, string, float64, bool, or nil.
// Also accepts Go structs and typed maps via reflection.
func EncodeGeneric(data any) string {
	var b strings.Builder
	b.WriteString("GCF profile=generic\n")

	v := toAny(data)
	encodeRootValue(&b, v)
	return b.String()
}

// encodeRootValue encodes the root value after the header.
func encodeRootValue(b *strings.Builder, v any) {
	switch val := v.(type) {
	case nil:
		b.WriteString("=-\n")
	case *OrderedMap:
		encodeOrderedObject(b, val, 0)
	case map[string]any:
		encodeObject(b, val, 0)
	case []any:
		encodeRootArray(b, val)
	default:
		b.WriteByte('=')
		b.WriteString(formatScalar(v, 0))
		b.WriteByte('\n')
	}
}

// encodeOrderedObject encodes an OrderedMap preserving key insertion order.
func encodeOrderedObject(b *strings.Builder, m *OrderedMap, depth int) {
	prefix := indentStr(depth)
	for _, key := range m.Keys() {
		val, _ := m.Get(key)
		fk := formatKey(key)
		switch v := val.(type) {
		case *OrderedMap:
			b.WriteString(prefix)
			b.WriteString("## ")
			b.WriteString(fk)
			b.WriteByte('\n')
			encodeOrderedObject(b, v, depth+1)
		case map[string]any:
			b.WriteString(prefix)
			b.WriteString("## ")
			b.WriteString(fk)
			b.WriteByte('\n')
			encodeObject(b, v, depth+1)
		case []any:
			encodeNamedArray(b, fk, v, depth)
		default:
			b.WriteString(prefix)
			b.WriteString(fk)
			b.WriteByte('=')
			b.WriteString(formatScalar(val, 0))
			b.WriteByte('\n')
		}
	}
}

// encodeObject encodes a map as key=value lines and ## sections.
// Key order is lexicographic since Go maps don't preserve insertion order.
func encodeObject(b *strings.Builder, m map[string]any, depth int) {
	prefix := indentStr(depth)
	for _, key := range orderedKeys(m) {
		val := m[key]
		fk := formatKey(key)
		switch v := val.(type) {
		case *OrderedMap:
			b.WriteString(prefix)
			b.WriteString("## ")
			b.WriteString(fk)
			b.WriteByte('\n')
			encodeOrderedObject(b, v, depth+1)
		case map[string]any:
			b.WriteString(prefix)
			b.WriteString("## ")
			b.WriteString(fk)
			b.WriteByte('\n')
			encodeObject(b, v, depth+1)
		case []any:
			encodeNamedArray(b, fk, v, depth)
		default:
			b.WriteString(prefix)
			b.WriteString(fk)
			b.WriteByte('=')
			b.WriteString(formatScalar(val, 0))
			b.WriteByte('\n')
		}
	}
}

// encodeRootArray encodes a root-level array with anonymous header.
func encodeRootArray(b *strings.Builder, arr []any) {
	if len(arr) == 0 {
		b.WriteString("## [0]\n")
		return
	}
	if allPrimitives(arr) {
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = formatScalar(v, ',')
		}
		fmt.Fprintf(b, "## [%d]: %s\n", len(arr), strings.Join(parts, ","))
		return
	}
	if fields := tabularFields(arr); fields != nil {
		encodeTabular(b, "## ", arr, fields, 0)
		return
	}
	encodeExpanded(b, "## ", arr, 0)
}

// encodeNamedArray encodes a named array within an object.
func encodeNamedArray(b *strings.Builder, name string, arr []any, depth int) {
	prefix := indentStr(depth)
	if len(arr) == 0 {
		fmt.Fprintf(b, "%s## %s [0]\n", prefix, name)
		return
	}
	if allPrimitives(arr) {
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = formatScalar(v, ',')
		}
		fmt.Fprintf(b, "%s%s[%d]: %s\n", prefix, name, len(arr), strings.Join(parts, ","))
		return
	}
	if fields := tabularFields(arr); fields != nil {
		encodeTabular(b, fmt.Sprintf("%s## %s ", prefix, name), arr, fields, depth)
		return
	}
	encodeExpanded(b, fmt.Sprintf("%s## %s ", prefix, name), arr, depth)
}

// tabularFields returns the ordered field union if arr is eligible for tabular
// encoding (all objects, non-empty union). Returns nil if not eligible.
// Handles both *OrderedMap and map[string]any elements.
func tabularFields(arr []any) []string {
	if len(arr) == 0 {
		return nil
	}
	var fieldOrder []string
	seen := make(map[string]struct{})
	for _, item := range arr {
		keys := objectItemKeys(item)
		if keys == nil {
			return nil // not an object
		}
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
func encodeTabular(b *strings.Builder, headerPrefix string, arr []any, fields []string, depth int) {
	prefix := indentStr(depth)

	// Format field declaration.
	fmtFields := make([]string, len(fields))
	for i, f := range fields {
		fmtFields[i] = formatKey(f)
	}

	// Header.
	fmt.Fprintf(b, "%s[%d]{%s}\n", headerPrefix, len(arr), strings.Join(fmtFields, ","))

	for i, item := range arr {
		// Build cell values.
		cells := make([]string, len(fields))
		var attachments []fieldAttachment
		rowHasAttachment := false
		for j, f := range fields {
			v, exists := objectItemGet(item, f)
			if !exists {
				cells[j] = "~"
				continue
			}
			if v == nil {
				cells[j] = "-"
				continue
			}
			switch nested := v.(type) {
			case *OrderedMap:
				cells[j] = "^"
				attachments = append(attachments, fieldAttachment{name: f, value: nested})
				rowHasAttachment = true
			case map[string]any:
				cells[j] = "^"
				attachments = append(attachments, fieldAttachment{name: f, value: nested})
				rowHasAttachment = true
			case []any:
				cells[j] = "^"
				attachments = append(attachments, fieldAttachment{name: f, value: nested})
				rowHasAttachment = true
			default:
				cells[j] = formatScalar(v, '|')
			}
		}

		row := strings.Join(cells, "|")
		if rowHasAttachment {
			fmt.Fprintf(b, "%s@%d %s\n", prefix, i, row)
		} else {
			b.WriteString(prefix)
			b.WriteString(row)
			b.WriteByte('\n')
		}

		// Emit attachments.
		for _, att := range attachments {
			attPrefix := prefix + "  "
			fk := formatKey(att.name)
			switch av := att.value.(type) {
			case *OrderedMap:
				fmt.Fprintf(b, "%s.%s {}\n", attPrefix, fk)
				encodeOrderedObject(b, av, depth+2)
			case map[string]any:
				fmt.Fprintf(b, "%s.%s {}\n", attPrefix, fk)
				encodeObject(b, av, depth+2)
			case []any:
				encodeAttachmentArray(b, attPrefix, fk, av, depth+2)
			}
		}
	}
}

// encodeAttachmentArray encodes an array as a .field attachment.
func encodeAttachmentArray(b *strings.Builder, attPrefix, fk string, arr []any, depth int) {
	if len(arr) == 0 {
		fmt.Fprintf(b, "%s.%s [0]\n", attPrefix, fk)
	} else if allPrimitives(arr) {
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = formatScalar(v, ',')
		}
		fmt.Fprintf(b, "%s.%s [%d]: %s\n", attPrefix, fk, len(arr), strings.Join(parts, ","))
	} else if nestedFields := tabularFields(arr); nestedFields != nil {
		encodeTabular(b, fmt.Sprintf("%s.%s ", attPrefix, fk), arr, nestedFields, depth)
	} else {
		encodeExpanded(b, fmt.Sprintf("%s.%s ", attPrefix, fk), arr, depth)
	}
}

type fieldAttachment struct {
	name  string
	value any
}

// encodeExpanded encodes a mixed/non-uniform array in expanded per-item form.
func encodeExpanded(b *strings.Builder, headerPrefix string, arr []any, depth int) {
	prefix := indentStr(depth)
	fmt.Fprintf(b, "%s[%d]\n", headerPrefix, len(arr))

	for i, item := range arr {
		switch v := item.(type) {
		case *OrderedMap:
			fmt.Fprintf(b, "%s@%d {}\n", prefix, i)
			encodeOrderedObject(b, v, depth+1)
		case map[string]any:
			fmt.Fprintf(b, "%s@%d {}\n", prefix, i)
			encodeObject(b, v, depth+1)
		case []any:
			encodeExpandedArrayItem(b, prefix, i, v, depth)
		default:
			fmt.Fprintf(b, "%s@%d =%s\n", prefix, i, formatScalar(item, 0))
		}
	}
}

// encodeExpandedArrayItem encodes a nested array as an expanded item.
func encodeExpandedArrayItem(b *strings.Builder, prefix string, idx int, arr []any, depth int) {
	if len(arr) == 0 {
		fmt.Fprintf(b, "%s@%d [0]\n", prefix, idx)
	} else if allPrimitives(arr) {
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = formatScalar(v, ',')
		}
		fmt.Fprintf(b, "%s@%d [%d]: %s\n", prefix, idx, len(arr), strings.Join(parts, ","))
	} else if nestedFields := tabularFields(arr); nestedFields != nil {
		encodeTabular(b, fmt.Sprintf("%s@%d ", prefix, idx), arr, nestedFields, depth+1)
	} else {
		encodeExpanded(b, fmt.Sprintf("%s@%d ", prefix, idx), arr, depth+1)
	}
}

// allPrimitives returns true if every element is a primitive (not object or array).
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

// toAny converts arbitrary Go values to JSON-compatible any types.
func toAny(data any) any {
	if data == nil {
		return nil
	}
	switch v := data.(type) {
	case *OrderedMap:
		return v
	case map[string]any:
		return v
	case []any:
		return v
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
	case nil:
		return nil
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
