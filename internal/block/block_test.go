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
		Difficulty:   3,
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
		Difficulty:   3,
		Nonce:        42,
	}
	baseHash := base.ComputeHash()

	tamperedTx := *base
	tamperedTx.Transactions = []ledger.Transaction{{Sender: "A", Recipient: "B", Amount: 999}}
	if tamperedTx.ComputeHash() == baseHash {
		t.Fatal("changing a transaction amount did not change the hash")
	}

	tamperedDifficulty := *base
	tamperedDifficulty.Difficulty = 4
	if tamperedDifficulty.ComputeHash() == baseHash {
		t.Fatal("changing the recorded difficulty did not change the hash")
	}
}

func TestNewGenesisBlock(t *testing.T) {
	g := NewGenesisBlock(4)

	if g.Index != 0 {
		t.Fatalf("genesis index = %d, want 0", g.Index)
	}
	if g.PrevHash != GenesisPrevHash {
		t.Fatalf("genesis prev-hash = %s, want %s", g.PrevHash, GenesisPrevHash)
	}
	if g.Difficulty != 4 {
		t.Fatalf("genesis difficulty = %d, want 4", g.Difficulty)
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
	b := New(1, 1700000000, nil, GenesisPrevHash, difficulty)

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

func TestMineWithWorkersSingleAndMultipleAgree(t *testing.T) {
	const difficulty = 3

	single := New(1, 1700000000, nil, GenesisPrevHash, difficulty)
	single.MineWithWorkers(difficulty, 1)

	multi := New(1, 1700000000, nil, GenesisPrevHash, difficulty)
	multi.MineWithWorkers(difficulty, 8)

	// Different worker counts may land on different winning nonces (each
	// worker searches a different stride), but both must independently
	// satisfy the target and reproduce their own stored hash.
	if !MeetsDifficulty(single.Hash, difficulty) || single.ComputeHash() != single.Hash {
		t.Fatalf("single-worker mine did not produce a valid, self-consistent block")
	}
	if !MeetsDifficulty(multi.Hash, difficulty) || multi.ComputeHash() != multi.Hash {
		t.Fatalf("multi-worker mine did not produce a valid, self-consistent block")
	}
}

func TestMerkleRootChangesWithTransactions(t *testing.T) {
	empty := merkleRoot(nil)
	one := merkleRoot([]ledger.Transaction{{Sender: "A", Recipient: "B", Amount: 10}})
	other := merkleRoot([]ledger.Transaction{{Sender: "A", Recipient: "B", Amount: 20}})
	two := merkleRoot([]ledger.Transaction{
		{Sender: "A", Recipient: "B", Amount: 10},
		{Sender: "C", Recipient: "D", Amount: 5},
	})

	if empty == one || one == other || one == two {
		t.Fatal("merkleRoot did not change as the transaction list changed")
	}
	if merkleRoot(nil) != empty {
		t.Fatal("merkleRoot of an empty list is not deterministic")
	}
}
