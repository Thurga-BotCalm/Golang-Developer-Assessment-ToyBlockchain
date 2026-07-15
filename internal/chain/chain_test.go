package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"

	"toychain/internal/block"
	"toychain/internal/ledger"
)

const testDifficulty = 2

// cloneChain returns a deep copy of c, independent of c's own slices, via a
// JSON round trip — the same representation the chain is persisted with.
// Chain.Blocks holds pointers backed by a slice that a plain struct copy
// (*dst = *src) would alias, so two "forks" built that way could silently
// clobber each other's blocks on append.
func cloneChain(t *testing.T, c *Chain) *Chain {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshaling chain: %v", err)
	}
	var clone Chain
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshaling chain: %v", err)
	}
	return &clone
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return priv
}

func signedTx(priv ed25519.PrivateKey, sender, recipient string, amount int64) ledger.Transaction {
	tx := ledger.Transaction{Sender: sender, Recipient: recipient, Amount: amount}
	tx.Sign(priv)
	return tx
}

func fundAndMine(t *testing.T, c *Chain, account string, amount int64) {
	t.Helper()
	if err := c.AddTransaction(ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: account, Amount: amount}); err != nil {
		t.Fatalf("faucet AddTransaction failed: %v", err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}
}

func TestNewChainStartsAtGenesis(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)

	if len(c.Blocks) != 1 {
		t.Fatalf("chain has %d blocks, want 1", len(c.Blocks))
	}
	if c.Blocks[0].Index != 0 {
		t.Fatalf("genesis index = %d, want 0", c.Blocks[0].Index)
	}
}

func TestHonestChainValidates(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)

	alice := newKey(t)
	if err := c.AddTransaction(signedTx(alice, "Alice", "Bob", 30)); err != nil {
		t.Fatalf("AddTransaction failed: %v", err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	result := c.Validate()
	if !result.Valid {
		t.Fatalf("expected valid chain, got invalid at block %d: %s", result.FailedIndex, result.Reason)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)

	alice := newKey(t)
	if err := c.AddTransaction(signedTx(alice, "Alice", "Bob", 30)); err != nil {
		t.Fatalf("AddTransaction failed: %v", err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	if before := c.Validate(); !before.Valid {
		t.Fatalf("chain should be valid before tampering, got: %s", before.Reason)
	}

	// Tamper with a transaction amount inside the first mined (non-genesis)
	// block, without re-signing or recomputing its hash — exactly what an
	// attacker editing stored data would do.
	c.Blocks[1].Transactions[0].Amount = 999999

	result := c.Validate()
	if result.Valid {
		t.Fatal("expected tampered chain to be invalid")
	}
	if result.FailedIndex != c.Blocks[1].Index {
		t.Fatalf("failed index = %d, want %d", result.FailedIndex, c.Blocks[1].Index)
	}
}

func TestTamperingSurvivesRemineIsStillCaughtBySignature(t *testing.T) {
	// At this toy's low difficulty, an attacker can cheaply re-mine a
	// tampered block (and, in a real chain, everything after it) so the
	// hash chain alone would look consistent again. A valid transaction
	// signature is the thing they still can't forge without Alice's key.
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)

	alice := newKey(t)
	if err := c.AddTransaction(signedTx(alice, "Alice", "Bob", 30)); err != nil {
		t.Fatalf("AddTransaction failed: %v", err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	// Tamper the last block specifically: since nothing comes after it,
	// re-mining it cannot break any prev-hash link downstream, isolating
	// signature verification as the only remaining line of defence.
	tampered := c.Latest()
	tampered.Transactions[0].Amount = 999999
	tampered.Nonce = 0
	tampered.Hash = ""
	tampered.Mine(tampered.Difficulty) // re-mine so the hash chain looks consistent again

	result := c.Validate()
	if result.Valid {
		t.Fatal("expected re-mined but unsigned-for-the-new-amount block to still be invalid")
	}
	if result.FailedIndex != tampered.Index {
		t.Fatalf("failed index = %d, want %d", result.FailedIndex, tampered.Index)
	}
}

func TestBrokenPrevHashLinkIsDetected(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)
	fundAndMine(t, c, "Bob", 50)

	c.Blocks[2].PrevHash = "deadbeef"

	result := c.Validate()
	if result.Valid {
		t.Fatal("expected chain with broken prev-hash link to be invalid")
	}
	if result.FailedIndex != c.Blocks[2].Index {
		t.Fatalf("failed index = %d, want %d", result.FailedIndex, c.Blocks[2].Index)
	}
}

func TestOverspendingTransactionIsRejected(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)

	alice := newKey(t)
	err := c.AddTransaction(signedTx(alice, "Alice", "Bob", 150))
	if !errors.Is(err, ledger.ErrInsufficientBalance) {
		t.Fatalf("AddTransaction(overspend) = %v, want ErrInsufficientBalance", err)
	}

	if got := c.Ledger().Balance("Alice"); got != 100 {
		t.Fatalf("Alice balance = %d, want unchanged 100", got)
	}
}

func TestUnsignedTransactionIsRejected(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)

	err := c.AddTransaction(ledger.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 10})
	if !errors.Is(err, ledger.ErrInvalidSignature) {
		t.Fatalf("AddTransaction(unsigned) = %v, want ErrInvalidSignature", err)
	}
}

func TestImpersonationIsRejected(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	fundAndMine(t, c, "Alice", 100)

	alice := newKey(t)
	if err := c.AddTransaction(signedTx(alice, "Alice", "Bob", 10)); err != nil {
		t.Fatalf("AddTransaction failed: %v", err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	// A different key claiming to spend as "Alice", now that Alice's key is
	// bound on-chain from her first mined spend.
	impostor := newKey(t)
	err := c.AddTransaction(signedTx(impostor, "Alice", "Eve", 5))
	if !errors.Is(err, ledger.ErrSenderKeyMismatch) {
		t.Fatalf("AddTransaction(impostor) = %v, want ErrSenderKeyMismatch", err)
	}
}

func TestMineRespectsMaxBlockSize(t *testing.T) {
	c := New(testDifficulty, 2)
	if err := c.AddTransaction(ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: "A", Amount: 10}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddTransaction(ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: "B", Amount: 10}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddTransaction(ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: "C", Amount: 10}); err != nil {
		t.Fatal(err)
	}

	b, _, err := c.MineBlock()
	if err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	if len(b.Transactions) != 2 {
		t.Fatalf("mined block has %d transactions, want 2", len(b.Transactions))
	}
	if len(c.Pending) != 1 {
		t.Fatalf("pending pool has %d transactions left, want 1", len(c.Pending))
	}
}

func TestMineWithNoPendingTransactionsFails(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	if _, _, err := c.MineBlock(); err == nil {
		t.Fatal("expected MineBlock to fail with an empty pending pool")
	}
}

func TestMineWithWorkersProducesAValidChain(t *testing.T) {
	c := New(testDifficulty, DefaultMaxBlockSize)
	if err := c.AddTransaction(ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: "A", Amount: 10}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.MineBlockWithWorkers(4); err != nil {
		t.Fatalf("MineBlockWithWorkers failed: %v", err)
	}
	if result := c.Validate(); !result.Valid {
		t.Fatalf("chain mined with 4 workers is invalid: %s", result.Reason)
	}
}

func TestRetargetDifficultyPureFunction(t *testing.T) {
	mkBlocks := func(difficulties []int, timestamps []int64) []*block.Block {
		blocks := make([]*block.Block, len(difficulties))
		for i := range difficulties {
			blocks[i] = &block.Block{Index: int64(i), Timestamp: timestamps[i], Difficulty: difficulties[i]}
		}
		return blocks
	}

	t.Run("holds steady through the first window (genesis timestamp is not a real clock)", func(t *testing.T) {
		// interval=2: after 2 mined blocks the window's "oldest" reference
		// would be the genesis block itself (Timestamp 0), which is
		// deliberately skipped rather than retargeted against.
		blocks := mkBlocks([]int{2, 2, 2}, []int64{0, 1, 2})
		if got := retargetDifficulty(blocks, 2, 10); got != 2 {
			t.Fatalf("difficulty = %d, want 2 (steady)", got)
		}
	})

	t.Run("raises difficulty when blocks come in much faster than target", func(t *testing.T) {
		// interval=2, target=100s/block -> expected 200s for the window.
		// blocks[1..3] took 2s, far under expected/2.
		blocks := mkBlocks([]int{2, 2, 2, 2, 2}, []int64{0, 1, 2, 3, 4})
		got := retargetDifficulty(blocks, 2, 100)
		if got != 3 {
			t.Fatalf("difficulty = %d, want 3 (raised)", got)
		}
	})

	t.Run("lowers difficulty when blocks come in much slower than target", func(t *testing.T) {
		// interval=2, target=1s/block -> expected 2s for the window.
		// blocks[1..3] took 400s, far over expected*2.
		blocks := mkBlocks([]int{3, 3, 3, 3, 3}, []int64{0, 1, 2, 401, 402})
		got := retargetDifficulty(blocks, 2, 1)
		if got != 2 {
			t.Fatalf("difficulty = %d, want 2 (lowered)", got)
		}
	})

	t.Run("never drops below minDifficulty", func(t *testing.T) {
		blocks := mkBlocks([]int{minDifficulty, minDifficulty, minDifficulty}, []int64{0, 1, 1000})
		got := retargetDifficulty(blocks, 1, 1)
		if got != minDifficulty {
			t.Fatalf("difficulty = %d, want floor of %d", got, minDifficulty)
		}
	})

	t.Run("never exceeds maxDifficulty", func(t *testing.T) {
		blocks := mkBlocks([]int{maxDifficulty, maxDifficulty, maxDifficulty}, []int64{0, 100, 101})
		got := retargetDifficulty(blocks, 1, 100)
		if got != maxDifficulty {
			t.Fatalf("difficulty = %d, want ceiling of %d", got, maxDifficulty)
		}
	})

	t.Run("disabled when interval is non-positive", func(t *testing.T) {
		blocks := mkBlocks([]int{5, 5, 5}, []int64{0, 1, 2})
		if got := retargetDifficulty(blocks, 0, 10); got != 5 {
			t.Fatalf("difficulty = %d, want 5 (unchanged, retargeting disabled)", got)
		}
	})
}

func TestMiningRetargetsDifficultyUpward(t *testing.T) {
	// A huge target-seconds value guarantees real (sub-second) mining always
	// looks "too fast", so difficulty should climb once enough real blocks
	// exist to retarget against (see retargetDifficulty's genesis-skip rule).
	c := NewWithRetarget(1, DefaultMaxBlockSize, 1, 1_000_000)
	for i := 0; i < 4; i++ {
		fundAndMine(t, c, "A", 1)
	}

	if result := c.Validate(); !result.Valid {
		t.Fatalf("retargeted chain is invalid: block %d: %s", result.FailedIndex, result.Reason)
	}
	if got := c.Latest().Difficulty; got <= 1 {
		t.Fatalf("difficulty after retargeting = %d, want it to have climbed above the starting 1", got)
	}
}

func TestValidateDetectsDifficultyThatDoesNotMatchRetargeting(t *testing.T) {
	c := NewWithRetarget(1, DefaultMaxBlockSize, 1, 1_000_000)
	for i := 0; i < 4; i++ {
		fundAndMine(t, c, "A", 1)
	}
	if result := c.Validate(); !result.Valid {
		t.Fatalf("expected honest chain valid, got: %s", result.Reason)
	}

	last := c.Blocks[len(c.Blocks)-1]
	wrongDifficulty := last.Difficulty - 1
	if wrongDifficulty < minDifficulty {
		t.Skip("difficulty already at floor, cannot construct a lower-but-valid difficulty for this run")
	}

	// Re-mine the last block, honestly, but at a difficulty retargeting
	// would not have prescribed at this height.
	remined := block.New(last.Index, last.Timestamp, last.Transactions, last.PrevHash, wrongDifficulty)
	remined.Mine(wrongDifficulty)
	c.Blocks[len(c.Blocks)-1] = remined

	result := c.Validate()
	if result.Valid {
		t.Fatal("expected validation to catch a difficulty that retargeting would not have prescribed")
	}
	if result.FailedIndex != remined.Index {
		t.Fatalf("failed index = %d, want %d", result.FailedIndex, remined.Index)
	}
}

func TestResolveForkPrefersTheLongerValidChain(t *testing.T) {
	base := New(1, DefaultMaxBlockSize)
	fundAndMine(t, base, "A", 10)

	// Diverge from the same shared history: short gets one more block, long
	// gets two more.
	short := cloneChain(t, base)
	fundAndMine(t, short, "B", 5)

	long := cloneChain(t, base)
	fundAndMine(t, long, "C", 5)
	fundAndMine(t, long, "D", 5)

	winner, replaced, err := ResolveFork(short, long)
	if err != nil {
		t.Fatalf("ResolveFork failed: %v", err)
	}
	if !replaced {
		t.Fatal("expected the longer chain to replace the shorter one")
	}
	if len(winner.Blocks) != len(long.Blocks) {
		t.Fatalf("winner has %d blocks, want %d", len(winner.Blocks), len(long.Blocks))
	}

	// And the reverse: resolving the long chain against the shorter one
	// keeps the long chain, unreplaced.
	winner, replaced, err = ResolveFork(long, short)
	if err != nil {
		t.Fatalf("ResolveFork failed: %v", err)
	}
	if replaced {
		t.Fatal("expected the shorter candidate not to replace the longer current chain")
	}
	if winner != long {
		t.Fatal("expected the current (longer) chain back unchanged")
	}
}

func TestResolveForkRejectsDifferentGenesis(t *testing.T) {
	a := New(1, DefaultMaxBlockSize)
	b := New(2, DefaultMaxBlockSize) // different starting difficulty -> different genesis hash

	if _, _, err := ResolveFork(a, b); err == nil {
		t.Fatal("expected ResolveFork to reject chains with different genesis blocks")
	}
}

func TestResolveForkRejectsInvalidCandidate(t *testing.T) {
	base := New(1, DefaultMaxBlockSize)
	fundAndMine(t, base, "A", 10)

	current := cloneChain(t, base)
	candidate := cloneChain(t, base)
	fundAndMine(t, candidate, "B", 5)
	fundAndMine(t, candidate, "C", 5)
	candidate.Blocks[1].Transactions = nil // tamper, without re-mining

	winner, replaced, err := ResolveFork(current, candidate)
	if err == nil {
		t.Fatal("expected ResolveFork to reject an invalid candidate chain")
	}
	if replaced || winner != current {
		t.Fatal("expected the current chain to be kept when the candidate is invalid")
	}
}
