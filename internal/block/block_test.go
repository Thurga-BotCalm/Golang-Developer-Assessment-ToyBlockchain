package block

import (
	"testing"

	"toychain/internal/ledger"
)

func TestComputeHashIsDeterministic(t *testing.T) {
	b := &Block{
		Index:        1,
		Timestamp:    1700000000,
		Transactions: []ledger.Transaction{{Sender: "A", Recipient: "B", Amount: 10}},
		PrevHash:     GenesisPrevHash,
		Nonce:        42,
	}

	h1 := b.ComputeHash()
	h2 := b.ComputeHash()

	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s != %s", h1, h2)
	}
}

func TestComputeHashChangesWithFields(t *testing.T) {
	base := &Block{
		Index:        1,
		Timestamp:    1700000000,
		Transactions: []ledger.Transaction{{Sender: "A", Recipient: "B", Amount: 10}},
		PrevHash:     GenesisPrevHash,
		Nonce:        42,
	}
	baseHash := base.ComputeHash()

	tampered := *base
	tampered.Transactions = []ledger.Transaction{{Sender: "A", Recipient: "B", Amount: 999}}
	if tampered.ComputeHash() == baseHash {
		t.Fatal("changing a transaction amount did not change the hash")
	}
}

func TestNewGenesisBlock(t *testing.T) {
	g := NewGenesisBlock()

	if g.Index != 0 {
		t.Fatalf("genesis index = %d, want 0", g.Index)
	}
	if g.PrevHash != GenesisPrevHash {
		t.Fatalf("genesis prev-hash = %s, want %s", g.PrevHash, GenesisPrevHash)
	}
	if g.Hash != g.ComputeHash() {
		t.Fatal("genesis hash does not match its own recomputation")
	}
	if len(g.Transactions) != 0 {
		t.Fatalf("genesis has %d transactions, want 0", len(g.Transactions))
	}
}

func TestMeetsDifficulty(t *testing.T) {
	cases := []struct {
		hash       string
		difficulty int
		want       bool
	}{
		{"0000abcd", 4, true},
		{"0000abcd", 5, false},
		{"1000abcd", 1, false},
		{"anything", 0, true},
		{"ab", 5, false},
	}
	for _, c := range cases {
		if got := MeetsDifficulty(c.hash, c.difficulty); got != c.want {
			t.Errorf("MeetsDifficulty(%q, %d) = %v, want %v", c.hash, c.difficulty, got, c.want)
		}
	}
}

func TestMineSatisfiesDifficultyTarget(t *testing.T) {
	const difficulty = 3
	b := New(1, 1700000000, nil, GenesisPrevHash)

	result := b.Mine(difficulty)

	if !MeetsDifficulty(b.Hash, difficulty) {
		t.Fatalf("mined hash %s does not meet difficulty %d", b.Hash, difficulty)
	}
	if result.Nonce != b.Nonce {
		t.Fatalf("result nonce %d != block nonce %d", result.Nonce, b.Nonce)
	}
	// The found nonce must reproduce the exact same hash.
	if b.ComputeHash() != b.Hash {
		t.Fatal("recomputing with the winning nonce did not reproduce the stored hash")
	}
}
