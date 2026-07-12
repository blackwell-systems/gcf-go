package gcf

import (
	"encoding/json"
	"fmt"
	"testing"
)

// --- scenario builders ---

func sessBase() GenericSet {
	return GenericSet{Name: "orders", Key: "id", Fields: []string{"id", "total", "status", "customer"},
		Rows: []map[string]any{
			{"id": 1001.0, "total": 59.98, "status": "shipped", "customer": "Alice"},
			{"id": 1002.0, "total": 29.99, "status": "pending", "customer": "Bob"},
			{"id": 1003.0, "total": 129.50, "status": "shipped", "customer": "Carol"},
		}}
}

// small per-turn updates (same schema) for the FixedN scenario.
func sessUpdates() []GenericSet {
	mk := func(rows ...map[string]any) GenericSet {
		return GenericSet{Name: "orders", Key: "id", Fields: []string{"id", "total", "status", "customer"}, Rows: rows}
	}
	return []GenericSet{
		mk(
			map[string]any{"id": 1001.0, "total": 59.98, "status": "shipped", "customer": "Alice"},
			map[string]any{"id": 1002.0, "total": 29.99, "status": "shipped", "customer": "Bob"}, // changed
			map[string]any{"id": 1003.0, "total": 129.50, "status": "shipped", "customer": "Carol"},
		),
		mk( // add 1004
			map[string]any{"id": 1001.0, "total": 59.98, "status": "shipped", "customer": "Alice"},
			map[string]any{"id": 1002.0, "total": 29.99, "status": "shipped", "customer": "Bob"},
			map[string]any{"id": 1003.0, "total": 129.50, "status": "shipped", "customer": "Carol"},
			map[string]any{"id": 1004.0, "total": 75.00, "status": "pending", "customer": "Dave"},
		),
		mk( // remove 1001
			map[string]any{"id": 1002.0, "total": 29.99, "status": "shipped", "customer": "Bob"},
			map[string]any{"id": 1003.0, "total": 129.50, "status": "shipped", "customer": "Carol"},
			map[string]any{"id": 1004.0, "total": 75.00, "status": "pending", "customer": "Dave"},
		),
		mk( // change 1003
			map[string]any{"id": 1002.0, "total": 29.99, "status": "shipped", "customer": "Bob"},
			map[string]any{"id": 1003.0, "total": 140.00, "status": "delivered", "customer": "Carol"},
			map[string]any{"id": 1004.0, "total": 75.00, "status": "pending", "customer": "Dave"},
		),
		mk( // add 1005
			map[string]any{"id": 1002.0, "total": 29.99, "status": "shipped", "customer": "Bob"},
			map[string]any{"id": 1003.0, "total": 140.00, "status": "delivered", "customer": "Carol"},
			map[string]any{"id": 1004.0, "total": 75.00, "status": "pending", "customer": "Dave"},
			map[string]any{"id": 1005.0, "total": 12.00, "status": "pending", "customer": "Eve"},
		),
	}
}

// larger base + one-row updates so SizeGuard's cumulative delta reaches a full.
func sizeGuardBase() GenericSet {
	s := GenericSet{Name: "rows", Key: "id", Fields: []string{"id", "total", "status", "customer"}}
	names := []string{"Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Heidi",
		"Ivan", "Judy", "Mallory", "Niaj", "Olivia", "Peggy", "Rupert", "Sybil",
		"Trent", "Uma", "Victor", "Walter"}
	for i, n := range names {
		s.Rows = append(s.Rows, map[string]any{
			"id": float64(2000 + i), "total": float64(10 + i), "status": "pending", "customer": n})
	}
	return s
}

func sizeGuardUpdates() []GenericSet {
	base := sizeGuardBase()
	clone := func() GenericSet {
		rows := make([]map[string]any, len(base.Rows))
		for i, r := range base.Rows {
			nr := map[string]any{}
			for k, v := range r {
				nr[k] = v
			}
			rows[i] = nr
		}
		return GenericSet{Name: base.Name, Key: base.Key, Fields: base.Fields, Rows: rows}
	}
	var ups []GenericSet
	for turn := 0; turn < 6; turn++ {
		g := clone()
		// change one distinct row's status each turn
		g.Rows[turn]["status"] = "shipped"
		ups = append(ups, g)
	}
	return ups
}

// --- unit tests ---

func TestSessionFixedNPattern(t *testing.T) {
	s := NewGenericDeltaSession(sessBase(), "orders_query", FixedN(3))
	wantFull := []bool{false, false, true, false, false} // re-anchor on turn 3
	for i, up := range sessUpdates() {
		_, isFull, err := s.Next(up)
		if err != nil {
			t.Fatalf("turn %d: %v", i+1, err)
		}
		if isFull != wantFull[i] {
			t.Errorf("turn %d: isFull=%v, want %v", i+1, isFull, wantFull[i])
		}
	}
}

func TestSessionSizeGuardTriggers(t *testing.T) {
	s := NewGenericDeltaSession(sizeGuardBase(), "", SizeGuard())
	anchors := 0
	for i, up := range sizeGuardUpdates() {
		_, isFull, err := s.Next(up)
		if err != nil {
			t.Fatalf("turn %d: %v", i+1, err)
		}
		if isFull {
			anchors++
		}
	}
	if anchors == 0 {
		t.Fatal("SizeGuard never re-anchored across 6 turns; scenario should trigger at least one")
	}
}

func TestSessionSchemaChangeReanchors(t *testing.T) {
	s := NewGenericDeltaSession(sessBase(), "orders_query", FixedN(15))
	changed := sessBase()
	changed.Fields = []string{"id", "total", "status"} // drop a column
	changed.Rows = []map[string]any{{"id": 1001.0, "total": 59.98, "status": "shipped"}}
	_, isFull, err := s.Next(changed)
	if err != nil {
		t.Fatalf("schema-change turn: %v", err)
	}
	if !isFull {
		t.Error("schema change must force a full re-anchor")
	}
}

// TestSessionFixedN15Over30Turns: with N=15 over 30 update turns, exactly two
// emissions are full re-anchors (turns 15 and 30); the other 28 are deltas.
// (Plus the one bootstrap full from CurrentFull that establishes the base.)
func TestSessionFixedN15Over30Turns(t *testing.T) {
	s := NewGenericDeltaSession(sessBase(), "orders_query", FixedN(15))
	_ = s.CurrentFull() // bootstrap full (turn 0), not counted below

	fulls, deltas := 0, 0
	var fullTurns []int
	prev := sessBase()
	for turn := 1; turn <= 30; turn++ {
		// mutate one row's total each turn so every turn is a real, same-schema delta
		next := GenericSet{Name: prev.Name, Key: prev.Key, Fields: prev.Fields}
		for j, r := range prev.Rows {
			nr := map[string]any{}
			for k, v := range r {
				nr[k] = v
			}
			if j == turn%len(prev.Rows) {
				nr["total"] = float64(turn) + 0.5
			}
			next.Rows = append(next.Rows, nr)
		}
		_, isFull, err := s.Next(next)
		if err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
		if isFull {
			fulls++
			fullTurns = append(fullTurns, turn)
		} else {
			deltas++
		}
		prev = next
	}
	if fulls != 2 || deltas != 28 {
		t.Errorf("over 30 turns: got %d fulls / %d deltas, want 2 / 28", fulls, deltas)
	}
	if len(fullTurns) != 2 || fullTurns[0] != 15 || fullTurns[1] != 30 {
		t.Errorf("full re-anchors at turns %v, want [15 30]", fullTurns)
	}
}

// The load-bearing test: a consumer that applies each emission (full -> decode,
// delta -> decode+verify) stays byte-for-byte in sync with the producer's state
// at every turn, under both policies.
func TestSessionConsumerStaysInSync(t *testing.T) {
	for _, tc := range []struct {
		name   string
		base   GenericSet
		ups    []GenericSet
		tool   string
		policy ReanchorPolicy
	}{
		{"fixedN3", sessBase(), sessUpdates(), "orders_query", FixedN(3)},
		{"sizeGuard", sizeGuardBase(), sizeGuardUpdates(), "", SizeGuard()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewGenericDeltaSession(tc.base, tc.tool, tc.policy)
			held, _, err := DecodeGenericFull(s.CurrentFull())
			if err != nil {
				t.Fatalf("initial full: %v", err)
			}
			for i, up := range tc.ups {
				wire, isFull, err := s.Next(up)
				if err != nil {
					t.Fatalf("turn %d produce: %v", i+1, err)
				}
				if isFull {
					held, _, err = DecodeGenericFull(wire)
					if err != nil {
						t.Fatalf("turn %d decode full: %v", i+1, err)
					}
				} else {
					d, err := DecodeGenericDelta(wire)
					if err != nil {
						t.Fatalf("turn %d decode delta: %v", i+1, err)
					}
					held, err = VerifyGenericDelta(held, d, d.NewRoot)
					if err != nil {
						t.Fatalf("turn %d apply delta: %v", i+1, err)
					}
				}
				if got, want := GenericPackRoot(held), GenericPackRoot(up); got != want {
					t.Fatalf("turn %d: consumer root %s != producer root %s (isFull=%v)", i+1, got, want, isFull)
				}
			}
		})
	}
}

// --- fixture generator ---

type sessionFixture struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Operation   string            `json:"operation"`
	Input       sessionFixtureIn  `json:"input"`
	Expected    sessionFixtureOut `json:"expected"`
}

type sessionFixtureIn struct {
	Base    map[string]any `json:"base"`
	Tool    string         `json:"tool"`
	Policy  map[string]any `json:"policy"`
	Updates []map[string]any `json:"updates"`
}

type sessionFixtureOut struct {
	InitialFull string             `json:"initialFull"`
	Emissions   []sessionEmission  `json:"emissions"`
}

type sessionEmission struct {
	IsFull bool   `json:"isFull"`
	Wire   string `json:"wire"`
}

func setJSON(s GenericSet) map[string]any {
	return map[string]any{"name": s.Name, "key": s.Key, "fields": s.Fields, "rows": s.Rows}
}

// TestDumpSessionFixtures prints complete conformance-fixture JSON for the
// session scenarios. Run with -run TestDumpSessionFixtures -v.
func TestDumpSessionFixtures(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("fixture generator; run with -v")
	}
	emit := func(name, desc string, base GenericSet, tool string, policy map[string]any, p ReanchorPolicy, ups []GenericSet) {
		s := NewGenericDeltaSession(base, tool, p)
		out := sessionFixtureOut{InitialFull: s.CurrentFull()}
		for _, up := range ups {
			wire, isFull, err := s.Next(up)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out.Emissions = append(out.Emissions, sessionEmission{IsFull: isFull, Wire: wire})
		}
		fx := sessionFixture{
			Name: name, Description: desc, Operation: "generic-delta-session",
			Input: sessionFixtureIn{Base: setJSON(base), Tool: tool, Policy: policy,
				Updates: func() []map[string]any {
					out := make([]map[string]any, len(ups))
					for i, u := range ups {
						out[i] = setJSON(u)
					}
					return out
				}()},
			Expected: out,
		}
		b, _ := json.MarshalIndent(fx, "", "  ")
		fmt.Printf("=== %s ===\n%s\n", name, string(b))
	}

	emit("session_fixed_n", "FixedN(3): re-anchor every 3 turns; delta,delta,FULL,delta,delta.",
		sessBase(), "orders_query", map[string]any{"mode": "fixedN", "n": 3}, FixedN(3), sessUpdates())

	emit("session_size_guard", "SizeGuard: re-anchor once cumulative delta reaches full-payload size.",
		sizeGuardBase(), "", map[string]any{"mode": "sizeGuard"}, SizeGuard(), sizeGuardUpdates())

	// schema change mid-session -> forced full
	sc := sessBase()
	sc.Fields = []string{"id", "total", "status"}
	sc.Rows = []map[string]any{{"id": 1001.0, "total": 59.98, "status": "shipped"}}
	emit("session_schema_change", "A schema change cannot be a delta; the session forces a full (10a.7).",
		sessBase(), "orders_query", map[string]any{"mode": "fixedN", "n": 15}, FixedN(15), []GenericSet{sc})
}
