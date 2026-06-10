package gcf

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func getIterations(defaultN int) int {
	if s := os.Getenv("GCF_ITERATIONS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return defaultN
}

// TestPropertyRoundTrip generates random JSON values and verifies
// decodeGeneric(encodeGeneric(v)) == v for each.
// This is the empirical proof of the lossless round-trip invariant.
func TestPropertyRoundTrip(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(42)) // deterministic seed for reproducibility

	for i := 0; i < iterations; i++ {
		val := genValue(rng, 0, 4)

		gcfText := EncodeGeneric(val)

		// Verify the output is valid UTF-8.
		if !utf8.ValidString(gcfText) {
			t.Fatalf("iteration %d: encoder produced invalid UTF-8", i)
		}

		// Verify the header is present.
		if !strings.HasPrefix(gcfText, "GCF profile=generic\n") {
			t.Fatalf("iteration %d: missing header\n  output: %q", i, truncate(gcfText, 200))
		}

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}

		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d random values round-tripped successfully", iterations)
}

// TestPropertyRoundTripAdversarial focuses on values most likely to break
// the scalar quoting and container selection logic.
func TestPropertyRoundTripAdversarial(t *testing.T) {
	iterations := getIterations(50_000)
	rng := rand.New(rand.NewSource(99))

	for i := 0; i < iterations; i++ {
		val := genAdversarialValue(rng, 0, 3)

		gcfText := EncodeGeneric(val)

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed: %v\n  input:  %s\n  gcf:    %q",
				i, err, jsonStr(val), truncate(gcfText, 500))
		}

		if !jsonDeepEqual(val, decoded) {
			t.Fatalf("iteration %d: round-trip mismatch\n  input:   %s\n  gcf:     %q\n  decoded: %s",
				i, jsonStr(val), truncate(gcfText, 500), jsonStr(decoded))
		}
	}
	t.Logf("PASS: %d adversarial values round-tripped successfully", iterations)
}

// --- Value generators ---

func genValue(rng *rand.Rand, depth, maxDepth int) any {
	if depth >= maxDepth {
		return genScalar(rng)
	}
	switch rng.Intn(10) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	case 3, 4:
		return genString(rng)
	case 5, 6:
		return genObject(rng, depth, maxDepth)
	case 7, 8:
		return genArray(rng, depth, maxDepth)
	default:
		return genScalar(rng)
	}
}

func genScalar(rng *rand.Rand) any {
	switch rng.Intn(5) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	default:
		return genString(rng)
	}
}

func genNumber(rng *rand.Rand) float64 {
	switch rng.Intn(8) {
	case 0:
		return 0
	case 1:
		return float64(rng.Intn(1000))
	case 2:
		return -float64(rng.Intn(1000))
	case 3:
		return float64(rng.Intn(1000000)) + rng.Float64()
	case 4:
		return math.Copysign(0, -1) // negative zero
	case 5:
		// Large number requiring exponent.
		return float64(rng.Intn(999)+1) * 1e18
	case 6:
		// Small number requiring exponent.
		return float64(rng.Intn(999)+1) * 1e-10
	default:
		return rng.Float64()*2000 - 1000
	}
}

func genString(rng *rand.Rand) string {
	n := rng.Intn(20)
	var b strings.Builder
	for i := 0; i < n; i++ {
		switch rng.Intn(15) {
		case 0:
			b.WriteByte(' ')
		case 1:
			b.WriteRune(rune('a' + rng.Intn(26)))
		case 2:
			b.WriteRune(rune('A' + rng.Intn(26)))
		case 3:
			b.WriteRune(rune('0' + rng.Intn(10)))
		case 4:
			b.WriteByte('|')
		case 5:
			b.WriteByte(',')
		case 6:
			b.WriteByte('=')
		case 7:
			b.WriteByte('"')
		case 8:
			b.WriteByte('\\')
		case 9:
			b.WriteByte('\n')
		case 10:
			b.WriteByte('\t')
		case 11:
			// Unicode.
			b.WriteRune(rune(0x100 + rng.Intn(0x1000)))
		case 12:
			b.WriteByte('#')
		case 13:
			b.WriteByte('@')
		default:
			b.WriteRune(rune('a' + rng.Intn(26)))
		}
	}
	return b.String()
}

var bareKeyChars = "abcdefghijklmnopqrstuvwxyz_"

func genBareKey(rng *rand.Rand) string {
	n := 1 + rng.Intn(8)
	b := make([]byte, n)
	for i := range b {
		b[i] = bareKeyChars[rng.Intn(len(bareKeyChars))]
	}
	return string(b)
}

func genKey(rng *rand.Rand) string {
	if rng.Intn(4) == 0 {
		// Adversarial key that requires quoting.
		return genAdversarialString(rng)
	}
	return genBareKey(rng)
}

func genObject(rng *rand.Rand, depth, maxDepth int) map[string]any {
	n := rng.Intn(6)
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		key := genBareKey(rng)
		// Avoid duplicate keys.
		for j := 0; j < 3; j++ {
			if _, exists := m[key]; !exists {
				break
			}
			key = genBareKey(rng)
		}
		m[key] = genValue(rng, depth+1, maxDepth)
	}
	return m
}

func genArray(rng *rand.Rand, depth, maxDepth int) []any {
	n := rng.Intn(6)
	arr := make([]any, n)

	// Decide array type.
	switch rng.Intn(4) {
	case 0:
		// All primitives.
		for i := range arr {
			arr[i] = genScalar(rng)
		}
	case 1:
		// All objects (uniform, tabular).
		fields := make([]string, 1+rng.Intn(4))
		for j := range fields {
			fields[j] = genBareKey(rng)
		}
		for i := range arr {
			obj := make(map[string]any, len(fields))
			for _, f := range fields {
				if rng.Intn(5) == 0 {
					continue // missing field
				}
				obj[f] = genScalar(rng)
			}
			arr[i] = obj
		}
	case 2:
		// All objects with some nested values.
		for i := range arr {
			obj := make(map[string]any)
			obj[genBareKey(rng)] = genScalar(rng)
			if rng.Intn(3) == 0 && depth+1 < maxDepth {
				obj[genBareKey(rng)] = genValue(rng, depth+2, maxDepth)
			}
			arr[i] = obj
		}
	default:
		// Mixed.
		for i := range arr {
			arr[i] = genValue(rng, depth+1, maxDepth)
		}
	}
	return arr
}

// --- Adversarial generators ---

// Strings most likely to break scalar quoting.
var collisionStrings = []string{
	"true", "false", "-", "~", "^",
	"0", "1", "42", "-1", "3.14", "1e10", "-0",
	"", " ", "  ", " x", "x ",
	"#", "# comment", "@0", "@handle",
	"+1", ".5", "+.3", "01", "00",
	"null", "NULL", "True", "False",
	"|", ",", "=", "\"", "\\",
	"\n", "\r", "\t", "\b",
	"a|b", "a,b", "a=b",
	"hello world",
}

func genAdversarialString(rng *rand.Rand) string {
	if rng.Intn(3) == 0 {
		return collisionStrings[rng.Intn(len(collisionStrings))]
	}
	return genString(rng)
}

func genAdversarialValue(rng *rand.Rand, depth, maxDepth int) any {
	if depth >= maxDepth {
		return genAdversarialScalar(rng)
	}
	switch rng.Intn(8) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	case 3:
		return genAdversarialString(rng)
	case 4:
		return genAdversarialObject(rng, depth, maxDepth)
	case 5:
		return genAdversarialArray(rng, depth, maxDepth)
	case 6:
		// Empty containers.
		if rng.Intn(2) == 0 {
			return map[string]any{}
		}
		return []any{}
	default:
		return genAdversarialScalar(rng)
	}
}

func genAdversarialScalar(rng *rand.Rand) any {
	switch rng.Intn(6) {
	case 0:
		return nil
	case 1:
		return rng.Float64() < 0.5
	case 2:
		return genNumber(rng)
	default:
		return genAdversarialString(rng)
	}
}

func genAdversarialObject(rng *rand.Rand, depth, maxDepth int) map[string]any {
	n := rng.Intn(5)
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		key := genKey(rng)
		for j := 0; j < 3; j++ {
			if _, exists := m[key]; !exists {
				break
			}
			key = genKey(rng)
		}
		m[key] = genAdversarialValue(rng, depth+1, maxDepth)
	}
	return m
}

func genAdversarialArray(rng *rand.Rand, depth, maxDepth int) []any {
	n := rng.Intn(5)
	arr := make([]any, n)

	switch rng.Intn(5) {
	case 0:
		// Primitive array with collision strings.
		for i := range arr {
			arr[i] = genAdversarialScalar(rng)
		}
	case 1:
		// Uniform objects with missing/null mix.
		fields := []string{genBareKey(rng), genBareKey(rng), genBareKey(rng)}
		for i := range arr {
			obj := make(map[string]any)
			for _, f := range fields {
				switch rng.Intn(4) {
				case 0:
					// missing
				case 1:
					obj[f] = nil // null
				default:
					obj[f] = genAdversarialScalar(rng)
				}
			}
			arr[i] = obj
		}
	case 2:
		// Objects with nested values (tests ^ attachments).
		for i := range arr {
			obj := make(map[string]any)
			obj[genBareKey(rng)] = genAdversarialScalar(rng)
			if rng.Intn(2) == 0 && depth+1 < maxDepth {
				nested := make(map[string]any)
				nested[genBareKey(rng)] = genAdversarialScalar(rng)
				obj[genBareKey(rng)] = nested
			}
			if rng.Intn(3) == 0 {
				obj[genBareKey(rng)] = []any{genAdversarialScalar(rng)}
			}
			arr[i] = obj
		}
	case 3:
		// Nested arrays.
		for i := range arr {
			inner := make([]any, rng.Intn(3))
			for j := range inner {
				inner[j] = genAdversarialScalar(rng)
			}
			arr[i] = inner
		}
	default:
		// Mixed everything.
		for i := range arr {
			arr[i] = genAdversarialValue(rng, depth+1, maxDepth)
		}
	}
	return arr
}

// --- Comparison ---

// jsonDeepEqual normalizes both values through JSON marshaling to handle
// int64 vs float64 and map key ordering differences.
func jsonDeepEqual(a, b any) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var aNorm, bNorm any
	json.Unmarshal(aJSON, &aNorm)
	json.Unmarshal(bJSON, &bNorm)
	return reflect.DeepEqual(aNorm, bNorm)
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (%d bytes total)", len(s))
}
