package gcf

import (
	"fmt"
	"strings"
)

// EncodeGenericV3 encodes with all v3 optimizations:
// inline object schemas, no attachment indentation, no field prefix on inline
// attachments, shared array schemas. MinInlineFields = 3.
func EncodeGenericV3(data any) string {
	opts := v3Opts{
		InlineObjectSchema: true,
		DropAttachIndent:   true,
		DropFieldPrefix:    true,
		SharedArraySchema:  true,
		MinInlineFields:    3,
	}
	return encodeGenericV3(data, opts)
}

// v3Opts controls which optimizations are active.
type v3Opts struct {
	InlineObjectSchema bool
	DropAttachIndent   bool
	DropFieldPrefix    bool
	SharedArraySchema  bool
	MinInlineFields    int
}

func (o v3Opts) String() string {
	var parts []string
	if o.InlineObjectSchema {
		parts = append(parts, "inline-obj")
	}
	if o.DropAttachIndent {
		parts = append(parts, "no-indent")
	}
	if o.DropFieldPrefix {
		parts = append(parts, "no-prefix")
	}
	if o.SharedArraySchema {
		parts = append(parts, "shared-arr")
	}
	if o.MinInlineFields > 0 {
		parts = append(parts, fmt.Sprintf("min%d", o.MinInlineFields))
	}
	if len(parts) == 0 {
		return "v2-baseline"
	}
	return strings.Join(parts, "+")
}

func encodeGenericV3(data any, opts v3Opts) string {
	var b strings.Builder
	b.WriteString("GCF profile=generic\n")
	v := toAny(data)
	encodeRootValueV3(&b, v, opts)
	return b.String()
}

func encodeRootValueV3(b *strings.Builder, v any, opts v3Opts) {
	switch val := v.(type) {
	case nil:
		b.WriteString("=-\n")
	case *OrderedMap:
		encodeOrderedObjectV3(b, val, 0, opts)
	case map[string]any:
		encodeObjectV3(b, val, 0, opts)
	case []any:
		encodeRootArrayV3(b, val, opts)
	default:
		b.WriteByte('=')
		b.WriteString(formatScalar(v, 0))
		b.WriteByte('\n')
	}
}

func encodeOrderedObjectV3(b *strings.Builder, m *OrderedMap, depth int, opts v3Opts) {
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
			encodeOrderedObjectV3(b, v, depth+1, opts)
		case map[string]any:
			b.WriteString(prefix)
			b.WriteString("## ")
			b.WriteString(fk)
			b.WriteByte('\n')
			encodeObjectV3(b, v, depth+1, opts)
		case []any:
			encodeNamedArrayV3(b, fk, v, depth, opts)
		default:
			b.WriteString(prefix)
			b.WriteString(fk)
			b.WriteByte('=')
			b.WriteString(formatScalar(val, 0))
			b.WriteByte('\n')
		}
	}
}

func encodeObjectV3(b *strings.Builder, m map[string]any, depth int, opts v3Opts) {
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
			encodeOrderedObjectV3(b, v, depth+1, opts)
		case map[string]any:
			b.WriteString(prefix)
			b.WriteString("## ")
			b.WriteString(fk)
			b.WriteByte('\n')
			encodeObjectV3(b, v, depth+1, opts)
		case []any:
			encodeNamedArrayV3(b, fk, v, depth, opts)
		default:
			b.WriteString(prefix)
			b.WriteString(fk)
			b.WriteByte('=')
			b.WriteString(formatScalar(val, 0))
			b.WriteByte('\n')
		}
	}
}

func encodeRootArrayV3(b *strings.Builder, arr []any, opts v3Opts) {
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
		encodeTabularV3(b, "## ", arr, fields, 0, opts)
		return
	}
	encodeExpandedV3(b, "## ", arr, 0, opts)
}

func encodeNamedArrayV3(b *strings.Builder, name string, arr []any, depth int, opts v3Opts) {
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
		encodeTabularV3(b, fmt.Sprintf("%s## %s ", prefix, name), arr, fields, depth, opts)
		return
	}
	encodeExpandedV3(b, fmt.Sprintf("%s## %s ", prefix, name), arr, depth, opts)
}

func encodeExpandedV3(b *strings.Builder, headerPrefix string, arr []any, depth int, opts v3Opts) {
	prefix := indentStr(depth)
	fmt.Fprintf(b, "%s[%d]\n", headerPrefix, len(arr))
	for i, item := range arr {
		switch v := item.(type) {
		case *OrderedMap:
			fmt.Fprintf(b, "%s@%d {}\n", prefix, i)
			encodeOrderedObjectV3(b, v, depth+1, opts)
		case map[string]any:
			fmt.Fprintf(b, "%s@%d {}\n", prefix, i)
			encodeObjectV3(b, v, depth+1, opts)
		case []any:
			if len(v) == 0 {
				fmt.Fprintf(b, "%s@%d [0]\n", prefix, i)
			} else if allPrimitives(v) {
				parts := make([]string, len(v))
				for j, pv := range v {
					parts[j] = formatScalar(pv, ',')
				}
				fmt.Fprintf(b, "%s@%d [%d]: %s\n", prefix, i, len(v), strings.Join(parts, ","))
			} else if nf := tabularFields(v); nf != nil {
				encodeTabularV3(b, fmt.Sprintf("%s@%d ", prefix, i), v, nf, depth+1, opts)
			} else {
				encodeExpandedV3(b, fmt.Sprintf("%s@%d ", prefix, i), v, depth+1, opts)
			}
		default:
			fmt.Fprintf(b, "%s@%d =%s\n", prefix, i, formatScalar(item, 0))
		}
	}
}

// v3FieldAttachment extends fieldAttachment with inline schema info.
type v3FieldAttachment struct {
	name         string
	value        any
	inline       bool
	inlineFields []string
}

// inlineSchemaFields checks if a given field across all rows in a tabular array
// is eligible for inline schema encoding: all values are objects with the same
// keys and all values are primitives (no nested objects or arrays).
// The first row must have the field so the decoder sees ^{fields} on row 0.
func inlineSchemaFields(arr []any, fieldName string) []string {
	if len(arr) > 0 {
		v, exists := objectItemGet(arr[0], fieldName)
		if !exists || v == nil {
			return nil
		}
		if objectItemKeys(v) == nil {
			return nil
		}
	}
	var canonicalKeys []string
	for _, item := range arr {
		v, exists := objectItemGet(item, fieldName)
		if !exists || v == nil {
			continue
		}
		keys := objectItemKeys(v)
		if keys == nil {
			return nil
		}
		for _, k := range keys {
			val, _ := objectItemGet(v, k)
			switch val.(type) {
			case *OrderedMap, map[string]any, []any:
				return nil
			}
		}
		if canonicalKeys == nil {
			canonicalKeys = keys
		} else {
			if len(keys) != len(canonicalKeys) {
				return nil
			}
			for i, k := range keys {
				if k != canonicalKeys[i] {
					return nil
				}
			}
		}
	}
	return canonicalKeys
}

func inlineSchemaFieldsV3(arr []any, fieldName string, minFields int) []string {
	fields := inlineSchemaFields(arr, fieldName)
	if fields != nil && len(fields) >= minFields {
		return fields
	}
	return nil
}

func sharedArraySchema(arr []any, fieldName string) []string {
	// The first row must have this field so the decoder sees {fields} on row 0.
	if len(arr) > 0 {
		v, exists := objectItemGet(arr[0], fieldName)
		if !exists || v == nil {
			return nil
		}
		if _, ok := v.([]any); !ok {
			return nil
		}
	}
	var canonicalFields []string
	for _, item := range arr {
		v, exists := objectItemGet(item, fieldName)
		if !exists || v == nil {
			continue
		}
		arrVal, ok := v.([]any)
		if !ok {
			return nil
		}
		fields := tabularFields(arrVal)
		if fields == nil {
			return nil
		}
		// All values in the array items must be scalars for shared schema to work.
		for _, arrItem := range arrVal {
			keys := objectItemKeys(arrItem)
			if keys == nil {
				return nil
			}
			for _, k := range keys {
				val, _ := objectItemGet(arrItem, k)
				switch val.(type) {
				case *OrderedMap, map[string]any, []any:
					return nil
				}
			}
		}
		if canonicalFields == nil {
			canonicalFields = fields
		} else {
			if len(fields) != len(canonicalFields) {
				return nil
			}
			for i, f := range fields {
				if f != canonicalFields[i] {
					return nil
				}
			}
		}
	}
	return canonicalFields
}

func encodeTabularV3(b *strings.Builder, headerPrefix string, arr []any, fields []string, depth int, opts v3Opts) {
	prefix := indentStr(depth)

	inlineSchemas := make(map[string][]string)
	if opts.InlineObjectSchema {
		minF := opts.MinInlineFields
		if minF <= 0 {
			minF = 1
		}
		for _, f := range fields {
			if ifs := inlineSchemaFieldsV3(arr, f, minF); ifs != nil {
				inlineSchemas[f] = ifs
			}
		}
	}

	sharedArrSchemas := make(map[string][]string)
	if opts.SharedArraySchema {
		for _, f := range fields {
			if sas := sharedArraySchema(arr, f); sas != nil {
				sharedArrSchemas[f] = sas
			}
		}
	}

	fmtFields := make([]string, len(fields))
	for i, f := range fields {
		fmtFields[i] = formatKey(f)
	}

	fmt.Fprintf(b, "%s[%d]{%s}\n", headerPrefix, len(arr), strings.Join(fmtFields, ","))

	for i, item := range arr {
		cells := make([]string, len(fields))
		var attachments []v3FieldAttachment
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
				if ifs, ok := inlineSchemas[f]; ok {
					if i == 0 {
						fmtIF := make([]string, len(ifs))
						for k, inf := range ifs {
							fmtIF[k] = formatKey(inf)
						}
						cells[j] = "^{" + strings.Join(fmtIF, ",") + "}"
					} else {
						cells[j] = "^"
					}
					attachments = append(attachments, v3FieldAttachment{name: f, value: nested, inline: true, inlineFields: ifs})
				} else {
					cells[j] = "^"
					attachments = append(attachments, v3FieldAttachment{name: f, value: nested})
				}
				rowHasAttachment = true
			case map[string]any:
				if ifs, ok := inlineSchemas[f]; ok {
					if i == 0 {
						fmtIF := make([]string, len(ifs))
						for k, inf := range ifs {
							fmtIF[k] = formatKey(inf)
						}
						cells[j] = "^{" + strings.Join(fmtIF, ",") + "}"
					} else {
						cells[j] = "^"
					}
					attachments = append(attachments, v3FieldAttachment{name: f, value: nested, inline: true, inlineFields: ifs})
				} else {
					cells[j] = "^"
					attachments = append(attachments, v3FieldAttachment{name: f, value: nested})
				}
				rowHasAttachment = true
			case []any:
				cells[j] = "^"
				attachments = append(attachments, v3FieldAttachment{name: f, value: nested})
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

		for _, att := range attachments {
			attIndent := prefix + "  "
			if opts.DropAttachIndent {
				attIndent = prefix
			}
			fk := formatKey(att.name)

			if att.inline {
				vals := make([]string, len(att.inlineFields))
				for k, inf := range att.inlineFields {
					v, exists := objectItemGet(att.value, inf)
					if !exists {
						vals[k] = "~"
					} else {
						vals[k] = formatScalar(v, '|')
					}
				}
				if opts.DropFieldPrefix {
					fmt.Fprintf(b, "%s%s\n", attIndent, strings.Join(vals, "|"))
				} else {
					fmt.Fprintf(b, "%s.%s %s\n", attIndent, fk, strings.Join(vals, "|"))
				}
			} else {
				switch av := att.value.(type) {
				case *OrderedMap:
					fmt.Fprintf(b, "%s.%s {}\n", attIndent, fk)
					encodeOrderedObjectV3(b, av, depth+2, opts)
				case map[string]any:
					fmt.Fprintf(b, "%s.%s {}\n", attIndent, fk)
					encodeObjectV3(b, av, depth+2, opts)
				case []any:
					if sas, ok := sharedArrSchemas[att.name]; ok && i > 0 {
						encodeAttachmentArrayV3Shared(b, attIndent, fk, av, depth+2, opts, sas)
					} else {
						encodeAttachmentArrayV3(b, attIndent, fk, av, depth+2, opts)
					}
				}
			}
		}
	}
}

func encodeAttachmentArrayV3(b *strings.Builder, attPrefix, fk string, arr []any, depth int, opts v3Opts) {
	if len(arr) == 0 {
		fmt.Fprintf(b, "%s.%s [0]\n", attPrefix, fk)
	} else if allPrimitives(arr) {
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = formatScalar(v, ',')
		}
		fmt.Fprintf(b, "%s.%s [%d]: %s\n", attPrefix, fk, len(arr), strings.Join(parts, ","))
	} else if nestedFields := tabularFields(arr); nestedFields != nil {
		encodeTabularV3(b, fmt.Sprintf("%s.%s ", attPrefix, fk), arr, nestedFields, depth, opts)
	} else {
		encodeExpandedV3(b, fmt.Sprintf("%s.%s ", attPrefix, fk), arr, depth, opts)
	}
}

func encodeAttachmentArrayV3Shared(b *strings.Builder, attPrefix, fk string, arr []any, depth int, opts v3Opts, sharedFields []string) {
	if len(arr) == 0 {
		fmt.Fprintf(b, "%s.%s [0]\n", attPrefix, fk)
	} else if allPrimitives(arr) {
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = formatScalar(v, ',')
		}
		fmt.Fprintf(b, "%s.%s [%d]: %s\n", attPrefix, fk, len(arr), strings.Join(parts, ","))
	} else if nf := tabularFields(arr); nf != nil && fieldsMatch(nf, sharedFields) {
		prefix := indentStr(depth)
		fmt.Fprintf(b, "%s.%s [%d]\n", attPrefix, fk, len(arr))
		for _, item := range arr {
			cells := make([]string, len(sharedFields))
			for j, f := range sharedFields {
				v, exists := objectItemGet(item, f)
				if !exists {
					cells[j] = "~"
				} else if v == nil {
					cells[j] = "-"
				} else {
					cells[j] = formatScalar(v, '|')
				}
			}
			b.WriteString(prefix)
			b.WriteString(strings.Join(cells, "|"))
			b.WriteByte('\n')
		}
	} else {
		// Fields don't match shared schema: fall back to full encoder with {fields}.
		encodeAttachmentArrayV3(b, attPrefix, fk, arr, depth, opts)
	}
}

func fieldsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
