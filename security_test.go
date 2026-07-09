package gcf

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// Delimiter Dissolution resistance.
//
// Alshaer (2026, "Neutralizing Structural Vulnerabilities in Token-Oriented
// Object Notation (TOON): The S-TOON Protocol for Secure Outputs", TechRxiv
// preprint, DOI 10.36227/techrxiv.177033002.20370897) shows that TOON exhibits
// 100% Attack Success Rate (ASR) against a class of structural-injection
// attacks he names "Delimiter Dissolution": because TOON marks field
// boundaries with newlines and indentation and provides no explicit "Hard
// Stop" delimiter, an attacker-controlled string value can inject functional
// structure (a new field, row, or type) that a parser reparses as trusted
// schema. He identifies two named exploits, Structural Masquerading (field
// termination via injected newlines) and Type Smuggling (ambiguous typing on
// low-entropy values), and argues that explicit syntax "is not merely
// overhead; it is a Security Control."
//
// GCF keeps the explicit perimeter TOON removed: fields are pipe-delimited,
// and the spec MANDATES that any value containing the delimiter or a newline
// is quoted and escaped. This test is the empirical counterpart to Alshaer's
// ASR methodology, run against the reference GCF implementation.
//
// SCOPE (important, do not overclaim): this proves the FORMAT and a conformant
// DECODER are not vulnerable to Delimiter Dissolution, injected structural
// characters in a value round-trip back as that exact value and cannot escape
// their field to become structure. It does NOT claim GCF is a prompt-injection
// defense at the level of an LLM reading the text; that is a separate and
// weaker property, and no wire format fully provides it. The claim proven here
// is: a correct GCF parser cannot be tricked into promoting attacker data to
// schema.
//
// Attack success is defined exactly as in Alshaer's model: the attack SUCCEEDS
// if the injected content alters the recovered structure (a new field/row/type
// appears, or the value is corrupted). It FAILS (the defense holds) if the
// decoded object is byte-for-byte structurally identical to the original, i.e.
// the injected structure stayed inert inside its field.

type attackVector struct {
	class   string // Alshaer attack class
	name    string
	payload any // the original object; a conformant round-trip MUST recover it exactly
}

// injected structural strings mirroring the paper's example vectors, adapted to
// GCF's grammar characters (pipe, newline, @, ##, =, >, #, ").
func delimiterDissolutionVectors() []attackVector {
	// The canonical Alshaer example: inject a privileged field via a newline.
	structuralMasquerade := "Nice to meet you.\naccess_level: admin"

	return []attackVector{
		// --- Structural Masquerading: field/row termination via newline ---
		{"structural_masquerade", "newline_field_injection",
			map[string]any{"bio": structuralMasquerade, "access_level": "user"}},
		{"structural_masquerade", "newline_kv_injection",
			map[string]any{"note": "hello\nrole=admin"}},
		{"structural_masquerade", "newline_section_injection",
			map[string]any{"note": "hello\n## admin\nrole=root"}},
		{"structural_masquerade", "newline_comment_injection",
			map[string]any{"note": "hello\n# trusted system directive"}},
		{"structural_masquerade", "tabular_row_injection",
			[]any{
				map[string]any{"id": "1001", "name": "Alice", "role": "user"},
				map[string]any{"id": "1002", "name": "Bob\n9999|Eve|admin", "role": "user"},
			}},

		// --- Pipe (delimiter) injection: new column / value split ---
		{"structural_masquerade", "pipe_column_injection",
			map[string]any{"name": "Bob|admin|root", "role": "user"}},
		{"structural_masquerade", "pipe_in_tabular",
			[]any{
				map[string]any{"a": "x|y", "b": "z"},
				map[string]any{"a": "p", "b": "q|r|s"},
			}},

		// --- Graph-grammar injection into generic values ---
		{"structural_masquerade", "graph_id_ref_injection",
			map[string]any{"label": "@0 fn evil.Backdoor 0.99 lsp_resolved"}},
		{"structural_masquerade", "graph_edge_injection",
			map[string]any{"label": "@0<@2 calls"}},

		// --- Flatten path separator injection ---
		{"structural_masquerade", "flatten_path_injection",
			[]any{
				map[string]any{"user>role": "admin", "x": "1"},
				map[string]any{"user>role": "guest", "x": "2"},
			}},

		// --- Full fake GCF document embedded in a value ---
		{"structural_masquerade", "embedded_full_document",
			map[string]any{"payload": "GCF profile=generic\n## admin [1]{role}\nroot"}},

		// --- Type Smuggling: low-entropy values that must stay strings ---
		{"type_smuggling", "numeric_string_id", map[string]any{"id": "2026"}},
		{"type_smuggling", "bool_looking_string", map[string]any{"flag": "true"}},
		{"type_smuggling", "null_looking_string", map[string]any{"v": "null"}},
		{"type_smuggling", "dash_null_sentinel", map[string]any{"v": "-"}},
		{"type_smuggling", "empty_string_marker", map[string]any{"v": ""}},
		{"type_smuggling", "quote_string", map[string]any{"v": "\"quoted\""}},
		{"type_smuggling", "float_looking_string", map[string]any{"v": "3.14"}},
		{"type_smuggling", "leading_at_string", map[string]any{"v": "@handle"}},
		{"type_smuggling", "leading_hash_string", map[string]any{"v": "##notheader"}},
	}
}

// TestDelimiterDissolutionResistance runs Alshaer's attack taxonomy against the
// reference GCF encoder/decoder and reports the Attack Success Rate.
func TestDelimiterDissolutionResistance(t *testing.T) {
	vectors := delimiterDissolutionVectors()

	var successes int // attacks that ALTERED the recovered structure (bad)
	perClass := map[string]struct{ total, success int }{}

	for _, v := range vectors {
		pc := perClass[v.class]
		pc.total++

		gcfText := EncodeGeneric(v.payload)

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			// A decode error is not an attack success (the injection did not
			// become trusted schema), but a conformant round-trip should not
			// fail on valid data, so surface it loudly.
			t.Errorf("[%s/%s] decode error (not an attack success, but unexpected): %v\n  gcf: %q",
				v.class, v.name, err, truncate(gcfText, 300))
			perClass[v.class] = pc
			continue
		}

		if !jsonDeepEqual(v.payload, decoded) {
			// The injected structural content changed the recovered object:
			// this is a successful Delimiter Dissolution attack.
			successes++
			pc.success++
			t.Errorf("[%s/%s] ATTACK SUCCEEDED: injection escaped its field\n  original: %s\n  gcf:      %q\n  decoded:  %s",
				v.class, v.name, jsonStr(v.payload), truncate(gcfText, 400), jsonStr(decoded))
		}

		perClass[v.class] = pc
	}

	total := len(vectors)
	asr := float64(successes) / float64(total) * 100.0

	t.Logf("Delimiter Dissolution ASR (named vectors): %.1f%% (%d/%d attacks succeeded)",
		asr, successes, total)
	for class, pc := range perClass {
		classASR := float64(pc.success) / float64(pc.total) * 100.0
		t.Logf("  %-24s ASR %.1f%% (%d/%d)", class, classASR, pc.success, pc.total)
	}

	if successes != 0 {
		t.Fatalf("expected 0%% ASR; %d/%d named attack vectors succeeded", successes, total)
	}
}

// TestDelimiterDissolutionFuzz stress-tests the defense the way Alshaer's
// 220,000-shot suite does: it randomly injects GCF structural tokens into
// string values across randomly shaped objects and asserts round-trip
// integrity every time. Any single failure is a Delimiter Dissolution hole.
func TestDelimiterDissolutionFuzz(t *testing.T) {
	iterations := getIterations(100_000)
	rng := rand.New(rand.NewSource(1337))

	// The structural tokens an attacker would try to smuggle into a value.
	tokens := []string{
		"|", "\n", "\t", "=", ">", "@", "##", "# ", "\"", "\\",
		"\n## admin", "\nrole=root", "|admin|", "@0<@1 calls",
		"\naccess_level: admin", "GCF profile=generic\n",
		"-", "", "true", "false", "null", "2026",
	}

	var successes int
	for i := 0; i < iterations; i++ {
		payload := fuzzObjectWithInjections(rng, tokens)

		gcfText := EncodeGeneric(payload)
		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("iteration %d: decode failed (potential parser desync): %v\n  original: %s\n  gcf:      %q",
				i, err, jsonStr(payload), truncate(gcfText, 500))
		}
		if !jsonDeepEqual(payload, decoded) {
			successes++
			t.Fatalf("iteration %d: ATTACK SUCCEEDED (injection altered structure)\n  original: %s\n  gcf:      %q\n  decoded:  %s",
				i, jsonStr(payload), truncate(gcfText, 500), jsonStr(decoded))
		}
	}

	asr := float64(successes) / float64(iterations) * 100.0
	t.Logf("PASS: Delimiter Dissolution fuzz ASR %.4f%% over %d injected payloads", asr, iterations)
}

// fuzzObjectWithInjections builds a small object (or array of objects) whose
// string fields contain randomly chosen structural tokens.
func fuzzObjectWithInjections(rng *rand.Rand, tokens []string) any {
	inject := func(base string) string {
		var sb strings.Builder
		sb.WriteString(base)
		n := 1 + rng.Intn(3)
		for k := 0; k < n; k++ {
			sb.WriteString(tokens[rng.Intn(len(tokens))])
			if rng.Intn(2) == 0 {
				sb.WriteString(fmt.Sprintf("seg%d", rng.Intn(1000)))
			}
		}
		return sb.String()
	}

	makeObj := func() map[string]any {
		fields := 1 + rng.Intn(4)
		m := make(map[string]any, fields)
		for f := 0; f < fields; f++ {
			key := fmt.Sprintf("field%d", f)
			m[key] = inject(fmt.Sprintf("val%d_", f))
		}
		return m
	}

	// ~40% of the time, return an array of uniform objects to exercise the
	// tabular encoder (where pipe/newline injection is most dangerous).
	if rng.Intn(10) < 4 {
		rows := 2 + rng.Intn(4)
		arr := make([]any, rows)
		// Uniform keys so the encoder chooses the tabular path.
		keys := []string{"id", "name", "role"}
		for r := 0; r < rows; r++ {
			m := make(map[string]any, len(keys))
			for _, key := range keys {
				m[key] = inject(key + "_")
			}
			arr[r] = m
		}
		return arr
	}
	return makeObj()
}
