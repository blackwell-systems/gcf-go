package gcf

import (
	"encoding/json"
	"math/rand"
	"testing"
)

func TestBracketColonAdversarial(t *testing.T) {
	adversarial := []string{
		"ERR[404]: Not Found",
		"[Speaker 1]: Hello",
		"[0]: looks like array",
		"[100]: big number",
		"[abc]: non-numeric",
		"[-1]: negative",
		"value[0] ok",
		"array[10]",
		"[just brackets]",
		"key: value",
		"has:colon",
		"http://example.com",
		"ERR[404]: Not Found and [500]: Server Error",
		"[a]: first [b]: second",
		"[]: empty",
		"[ 0 ]: spaced",
		"[[0]]: nested",
		"[0]: at start",
		"middle [0]: here",
		"at end [0]:",
		"ERROR[ENOENT]: File not found",
		"LOG[2026-06-18T10:30:00Z]: Server started",
		"ICD-10[J06.9]: Acute upper respiratory infection",
		"port[443]: HTTPS",
		"config[database.host]: localhost",
		"slot[0]: empty",
		"field[name]: John",
	}

	for _, s := range adversarial {
		obj := map[string]any{"value": s}
		encoded := EncodeGeneric(obj)
		decoded, err := DecodeGeneric(encoded)
		if err != nil {
			t.Errorf("ERR on %q: %v", s, err)
			continue
		}
		orig, _ := json.Marshal(obj)
		got, _ := json.Marshal(decoded)
		if string(orig) != string(got) {
			t.Errorf("Mismatch on %q:\n  encoded: %q\n  decoded: %s", s, encoded, got)
		}
	}
}

func TestBracketColonFuzz10M(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 10M fuzz in short mode")
	}

	rng := rand.New(rand.NewSource(42))
	chars := []byte("abcdefghijklmnopqrstuvwxyz0123456789 _-.[]{}():=|,'\"\\/@ #")

	pass := 0
	fail := 0

	for i := 0; i < 10_000_000; i++ {
		length := 1 + rng.Intn(40)
		buf := make([]byte, length)
		for j := range buf {
			buf[j] = chars[rng.Intn(len(chars))]
		}
		s := string(buf)

		obj := map[string]any{"v": s}
		encoded := EncodeGeneric(obj)
		decoded, err := DecodeGeneric(encoded)
		if err != nil {
			fail++
			if fail <= 5 {
				t.Errorf("Error on %q: %v", s, err)
			}
			continue
		}
		orig, _ := json.Marshal(obj)
		got, _ := json.Marshal(decoded)
		if string(orig) != string(got) {
			fail++
			if fail <= 5 {
				t.Errorf("Mismatch on %q", s)
			}
			continue
		}
		pass++
	}

	if fail > 0 {
		t.Fatalf("%d failures out of 10,000,000", fail)
	}
	t.Logf("10,000,000 passed, 0 failed")
}
