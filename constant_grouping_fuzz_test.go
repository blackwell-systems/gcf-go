package gcf

// Targeted fuzz/property coverage for spec v3.6.0 (constant-column factoring §7.4.7 and
// value-grouping §7.4.8). The existing default-path round-trips (TestPropertyRoundTrip and
// friends) already exercise constant-factoring because it rides the default encoder, but
// value-grouping is opt-in (EncodeGenericGrouped) and is touched by no other property test,
// and neither new grammar had a decoder-robustness (mutation) fuzz. These harnesses close
// both gaps and are the ones to mirror into the other SDKs.

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// recField reads a field from a record that may be a map (input) or an *OrderedMap (decoded).
func recField(rec any, name string) any {
	switch m := rec.(type) {
	case map[string]any:
		return m[name]
	case *OrderedMap:
		v, _ := m.Get(name)
		return v
	}
	return nil
}

// --- constant-column factoring: constant-biased generator -------------------------------

// genConstBiasedArray builds a tabular array (>=2 records, scalar leaves only) in which a
// random subset of fields is held constant across every record, drawing constant values from
// the adversarial scalar pool so the factored value hits the quoting path (commas, braces,
// quotes, leading/trailing space, "-", "~"-like, numeric-like). Sometimes every field is
// constant, exercising the all-constant / last-column-retained edge of §7.4.7.1.
func genConstBiasedArray(rng *rand.Rand) []any {
	n := 2 + rng.Intn(6) // 2..7 records
	k := 1 + rng.Intn(5) // 1..5 fields
	fields := make([]string, 0, k)
	used := map[string]bool{}
	for len(fields) < k {
		f := genBareKey(rng)
		if used[f] {
			continue
		}
		used[f] = true
		fields = append(fields, f)
	}
	constVal := map[string]any{}
	forceAll := rng.Intn(8) == 0 // ~12% all-constant
	for _, f := range fields {
		if forceAll || rng.Intn(2) == 0 {
			constVal[f] = genAdversarialScalar(rng)
		}
	}
	arr := make([]any, n)
	for i := 0; i < n; i++ {
		rec := map[string]any{}
		for _, f := range fields {
			if v, ok := constVal[f]; ok {
				rec[f] = v
			} else {
				rec[f] = genScalar(rng)
			}
		}
		arr[i] = rec
	}
	return arr
}

func TestPropertyRoundTripConstantBiased(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(0xC0))
	factored := 0
	for i := 0; i < iterations; i++ {
		val := genConstBiasedArray(rng)
		gcfText := EncodeGeneric(val)
		if !utf8.ValidString(gcfText) {
			t.Fatalf("iteration %d: encoder produced invalid UTF-8", i)
		}
		if headerHasFactoredColumn(gcfText) {
			factored++
		}
		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input: %s\n  gcf:   %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}
		if !jsonDeepEqual(any(val), decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	if factored == 0 {
		t.Fatalf("coverage gap: no factored headers produced in %d iterations", iterations)
	}
	t.Logf("PASS: %d constant-biased arrays round-tripped (%d factored headers)", iterations, factored)
}

// headerHasFactoredColumn reports whether the first tabular header contains a name=value entry.
func headerHasFactoredColumn(gcf string) bool {
	for _, line := range strings.Split(gcf, "\n") {
		if strings.HasPrefix(line, "## ") {
			open := strings.IndexByte(line, '{')
			closeB := strings.LastIndexByte(line, '}')
			if open >= 0 && closeB > open && strings.IndexByte(line[open:closeB], '=') >= 0 {
				return true
			}
		}
	}
	return false
}

// --- value-grouping: keyed-set generator ------------------------------------------------

// genGroupedSet builds a keyed set suitable for EncodeGenericGrouped: a unique key field, a
// low-cardinality group field (values drawn adversarially, including null, brackets, commas,
// "="), and 0..3 extra scalar fields some of which may be constant. Key uniqueness is by
// construction, so the encoder never errors on this input.
func genGroupedSet(rng *rand.Rand) (arr []any, keyField, groupField string) {
	keyField, groupField = "k", "g"
	n := 2 + rng.Intn(8) // 2..9 records
	// a small pool of distinct group values
	poolSize := 1 + rng.Intn(4)
	pool := make([]any, poolSize)
	for i := range pool {
		pool[i] = genAdversarialScalar(rng)
	}
	// extra fields, some constant
	extraN := rng.Intn(4)
	extras := make([]string, 0, extraN)
	used := map[string]bool{"k": true, "g": true}
	for len(extras) < extraN {
		f := genBareKey(rng)
		if used[f] {
			continue
		}
		used[f] = true
		extras = append(extras, f)
	}
	extraConst := map[string]any{}
	for _, f := range extras {
		if rng.Intn(2) == 0 {
			extraConst[f] = genAdversarialScalar(rng)
		}
	}
	arr = make([]any, n)
	for i := 0; i < n; i++ {
		rec := map[string]any{
			keyField:   fmt.Sprintf("k%04d", i),
			groupField: pool[rng.Intn(poolSize)],
		}
		for _, f := range extras {
			if v, ok := extraConst[f]; ok {
				rec[f] = v
			} else {
				rec[f] = genScalar(rng)
			}
		}
		arr[i] = rec
	}
	return arr, keyField, groupField
}

func sortByKey(arr []any, keyField string) {
	sort.SliceStable(arr, func(i, j int) bool {
		return fmt.Sprint(recField(arr[i], keyField)) < fmt.Sprint(recField(arr[j], keyField))
	})
}

func TestPropertyRoundTripGrouped(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(0x6C))
	ok := 0
	for i := 0; i < iterations; i++ {
		val, kf, gf := genGroupedSet(rng)
		gcfText, err := EncodeGenericGrouped(val, kf, gf)
		if err != nil {
			t.Fatalf("iteration %d: EncodeGenericGrouped failed on valid keyed set: %v\n  input: %s",
				i, err, jsonStr(val))
		}
		if !utf8.ValidString(gcfText) {
			t.Fatalf("iteration %d: grouped encoder produced invalid UTF-8", i)
		}
		decodedAny, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input: %s\n  gcf:   %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}
		decoded, isArr := decodedAny.([]any)
		if !isArr {
			t.Fatalf("iteration %d: grouped decode did not yield an array: %T", i, decodedAny)
		}
		if len(decoded) != len(val) {
			t.Fatalf("iteration %d: record count %d != %d\n  gcf: %q", i, len(decoded), len(val), truncate(gcfText, 500))
		}
		// compare as a set keyed by kf: sort both by key, then order-insensitive deep-equal.
		inCopy := append([]any(nil), val...)
		sortByKey(inCopy, kf)
		sortByKey(decoded, kf)
		if !jsonDeepEqual(any(inCopy), any(decoded)) {
			t.Fatalf("iteration %d: grouped round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(inCopy), truncate(gcfText, 500), jsonStr(decoded))
		}
		ok++
	}
	t.Logf("PASS: %d grouped keyed sets round-tripped", ok)
}

// --- decoder robustness (mutation) ------------------------------------------------------

func mutate(rng *rand.Rand, b []byte) []byte {
	if len(b) == 0 {
		return []byte{byte(rng.Intn(128))}
	}
	switch rng.Intn(6) {
	case 0: // flip a byte
		i := rng.Intn(len(b))
		b[i] ^= byte(1 << uint(rng.Intn(8)))
	case 1: // delete a byte
		i := rng.Intn(len(b))
		b = append(b[:i], b[i+1:]...)
	case 2: // insert a structural byte
		i := rng.Intn(len(b) + 1)
		ch := []byte("|{}[]=@#.-~\"\n ")[rng.Intn(13)]
		b = append(b[:i], append([]byte{ch}, b[i:]...)...)
	case 3: // truncate
		b = b[:rng.Intn(len(b))]
	case 4: // duplicate a line fragment
		i := rng.Intn(len(b))
		b = append(b[:i], append(append([]byte{}, b[i:]...), b[i:]...)...)
	default: // random byte
		i := rng.Intn(len(b))
		b[i] = byte(rng.Intn(128))
	}
	return b
}

// TestConstantGroupedDecodeRobustness mutates valid factored/grouped wire and requires the
// decoder to error cleanly, never panic, on the result. A panic here is a parser bug.
func TestConstantGroupedDecodeRobustness(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(0xF0))
	for i := 0; i < iterations; i++ {
		var wire string
		if rng.Intn(2) == 0 {
			wire = EncodeGeneric(genConstBiasedArray(rng))
		} else {
			arr, kf, gf := genGroupedSet(rng)
			w, err := EncodeGenericGrouped(arr, kf, gf)
			if err != nil {
				continue
			}
			wire = w
		}
		b := []byte(wire)
		for m := 1 + rng.Intn(4); m > 0; m-- {
			b = mutate(rng, b)
		}
		in := string(b)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("iteration %d: panic on mutated input: %v\n  input: %q", i, r, truncate(in, 500))
				}
			}()
			_, _ = DecodeGeneric(in) // an error is fine; a panic is not
		}()
	}
	t.Logf("PASS: %d mutated inputs decoded without panic", iterations)
}

// FuzzConstantGroupedDecode is the native-fuzz entrypoint (go test -fuzz). Under a plain
// `go test` it runs the seed corpus. The invariant is panic-freedom on arbitrary input.
func FuzzConstantGroupedDecode(f *testing.F) {
	f.Add("GCF profile=generic\n## [2]{id,region=us-east,level}\nu1|3\nu2|1\n")
	f.Add("GCF profile=generic\n## [2]{a=1,b}\n2\n2\n")
	f.Add("GCF profile=generic\n## [2]{@id,dept,x} group=dept\ndept=Sales [2]\nu1|1\nu2|2\n")
	f.Add("GCF profile=generic\n## [1]{@id,g,x} group=g\ng=- [1]\nu1|1\n")
	f.Fuzz(func(t *testing.T, s string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic: %v\n  input: %q", r, truncate(s, 500))
			}
		}()
		_, _ = DecodeGeneric(s)
	})
}
