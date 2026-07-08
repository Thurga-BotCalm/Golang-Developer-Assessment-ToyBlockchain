package chain

import (
	"errors"
	"testing"

	"toychain/internal/ledger"
)

const testDifficulty = 2

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

	if err := c.AddTransaction(ledger.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 30}); err != nil {
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

	if err := c.AddTransaction(ledger.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 30}); err != nil {
		t.Fatalf("AddTransaction failed: %v", err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	if before := c.Validate(); !before.Valid {
		t.Fatalf("chain should be valid before tampering, got: %s", before.Reason)
	}

	// Tamper with a transaction amount inside the first mined (non-genesis)
	// block, without recomputing its hash — exactly what an attacker editing
	// stored data would do.
	c.Blocks[1].Transactions[0].Amount = 999999

	result := c.Validate()
	if result.Valid {
		t.Fatal("expected tampered chain to be invalid")
	}
	if result.FailedIndex != c.Blocks[1].Index {
		t.Fatalf("failed index = %d, want %d", result.FailedIndex, c.Blocks[1].Index)
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

	err := c.AddTransaction(ledger.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 150})
	if !errors.Is(err, ledger.ErrInsufficientBalance) {
		t.Fatalf("AddTransaction(overspend) = %v, want ErrInsufficientBalance", err)
	}

	if got := c.Ledger().Balance("Alice"); got != 100 {
		t.Fatalf("Alice balance = %d, want unchanged 100", got)
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
