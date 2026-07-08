package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"toychain/internal/chain"
	"toychain/internal/ledger"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain.json")

	c := chain.New(2, chain.DefaultMaxBlockSize)
	if err := c.AddTransaction(ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: "Alice", Amount: 100}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.MineBlock(); err != nil {
		t.Fatal(err)
	}

	if err := Save(path, c); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Blocks) != len(c.Blocks) {
		t.Fatalf("loaded %d blocks, want %d", len(loaded.Blocks), len(c.Blocks))
	}
	if loaded.Blocks[1].Hash != c.Blocks[1].Hash {
		t.Fatalf("loaded block hash %s != saved hash %s", loaded.Blocks[1].Hash, c.Blocks[1].Hash)
	}
	if result := loaded.Validate(); !result.Valid {
		t.Fatalf("loaded chain failed validation: %s", result.Reason)
	}
	if got := loaded.Ledger().Balance("Alice"); got != 100 {
		t.Fatalf("loaded Alice balance = %d, want 100", got)
	}
}

func TestLoadMissingFileReturnsNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load(missing) = %v, want an os.ErrNotExist error", err)
	}
}
