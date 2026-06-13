package gcf

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// GenericStreamEncoder writes GCF tabular output incrementally as rows arrive.
// Zero buffering: each row is written immediately. A trailer summary is
// emitted on Close() with the final counts.
//
// Usage:
//
//	enc := gcf.NewGenericStreamEncoder(w)
//	enc.BeginArray("employees", []string{"id", "name", "department", "salary"})
//	enc.WriteRow([]any{1, "Alice", "Engineering", 95000})
//	enc.WriteRow([]any{2, "Bob", "Sales", 72000})
//	enc.EndArray()
//	enc.WriteKV("total", 2)
//	enc.Close()
type GenericStreamEncoder struct {
	w       io.Writer
	mu      sync.Mutex
	sections []sectionCount
	current  *activeArray
}

type sectionCount struct {
	name  string
	count int
}

type activeArray struct {
	name   string
	fields []string
	count  int
}

// NewGenericStreamEncoder creates a streaming encoder for tabular/generic data.
func NewGenericStreamEncoder(w io.Writer) *GenericStreamEncoder {
	fmt.Fprintf(w, "GCF profile=generic\n")
	return &GenericStreamEncoder{w: w}
}

// BeginArray starts a tabular array section with deferred count [?].
// Fields are declared in the header. Call WriteRow() for each record,
// then EndArray() when done.
func (enc *GenericStreamEncoder) BeginArray(name string, fields []string) {
	enc.mu.Lock()
	defer enc.mu.Unlock()

	if enc.current != nil {
		enc.endArrayLocked()
	}

	fmt.Fprintf(enc.w, "## %s [?]{%s}\n", name, strings.Join(fields, ","))
	enc.current = &activeArray{name: name, fields: fields}
}

// WriteRow emits a single pipe-separated row immediately.
// Values are formatted according to GCF value encoding rules.
func (enc *GenericStreamEncoder) WriteRow(values []any) {
	enc.mu.Lock()
	defer enc.mu.Unlock()

	if enc.current == nil {
		return
	}

	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = formatGenericStreamValue(v)
	}
	fmt.Fprintf(enc.w, "%s\n", strings.Join(parts, "|"))
	enc.current.count++
}

// EndArray closes the current array section and records its count.
func (enc *GenericStreamEncoder) EndArray() {
	enc.mu.Lock()
	defer enc.mu.Unlock()
	enc.endArrayLocked()
}

func (enc *GenericStreamEncoder) endArrayLocked() {
	if enc.current == nil {
		return
	}
	enc.sections = append(enc.sections, sectionCount{
		name:  enc.current.name,
		count: enc.current.count,
	})
	enc.current = nil
}

// WriteKV emits a key=value line immediately.
func (enc *GenericStreamEncoder) WriteKV(key string, value any) {
	enc.mu.Lock()
	defer enc.mu.Unlock()

	fmt.Fprintf(enc.w, "%s=%s\n", key, formatGenericStreamValue(value))
}

// WriteSection starts a nested object section (## key).
func (enc *GenericStreamEncoder) WriteSection(name string) {
	enc.mu.Lock()
	defer enc.mu.Unlock()

	if enc.current != nil {
		enc.endArrayLocked()
	}

	fmt.Fprintf(enc.w, "## %s\n", name)
}

// WriteInlineArray emits a primitive array inline: name[N]: val1,val2,val3
func (enc *GenericStreamEncoder) WriteInlineArray(name string, values []any) {
	enc.mu.Lock()
	defer enc.mu.Unlock()

	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = formatGenericStreamValue(v)
	}
	fmt.Fprintf(enc.w, "%s[%d]: %s\n", name, len(values), strings.Join(parts, ","))
}

// Close emits the ## _summary trailer with final counts.
// Must be called after all data has been written.
func (enc *GenericStreamEncoder) Close() error {
	enc.mu.Lock()
	defer enc.mu.Unlock()

	if enc.current != nil {
		enc.endArrayLocked()
	}

	if len(enc.sections) == 0 {
		return nil
	}

	counts := make([]string, len(enc.sections))
	for i, s := range enc.sections {
		counts[i] = fmt.Sprintf("%d", s.count)
	}
	fmt.Fprintf(enc.w, "##! summary counts=%s\n", strings.Join(counts, ","))
	return nil
}

func formatGenericStreamValue(v any) string {
	return formatScalar(v, '|')
}
