package ledger

import (
	"errors"
	"testing"
)

func TestApplyRejectsNonPositiveAmount(t *testing.T) {
	l := New()
	err := l.Apply(Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 0})
	if !errors.Is(err, ErrNonPositiveAmount) {
		t.Fatalf("Apply(amount=0) = %v, want ErrNonPositiveAmount", err)
	}

	err = l.Apply(Transaction{Sender: FaucetAccount, Recipient: "A", Amount: -5})
	if !errors.Is(err, ErrNonPositiveAmount) {
		t.Fatalf("Apply(amount=-5) = %v, want ErrNonPositiveAmount", err)
	}
}

func TestApplyRejectsOverspend(t *testing.T) {
	l := New()
	if err := l.Apply(Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 100}); err != nil {
		t.Fatalf("funding account failed: %v", err)
	}

	err := l.Apply(Transaction{Sender: "A", Recipient: "B", Amount: 150})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("overspend Apply() = %v, want ErrInsufficientBalance", err)
	}

	if got := l.Balance("A"); got != 100 {
		t.Fatalf("balance after rejected overspend = %d, want unchanged 100", got)
	}
}

func TestFaucetHasUnlimitedFunds(t *testing.T) {
	l := New()
	if err := l.Apply(Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 1_000_000}); err != nil {
		t.Fatalf("faucet grant failed: %v", err)
	}
	if got := l.Balance("A"); got != 1_000_000 {
		t.Fatalf("balance = %d, want 1000000", got)
	}
}

func TestApplyUpdatesBothBalances(t *testing.T) {
	l := New()
	mustApply(t, l, Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 100})
	mustApply(t, l, Transaction{Sender: "A", Recipient: "B", Amount: 40})

	if got := l.Balance("A"); got != 60 {
		t.Fatalf("sender balance = %d, want 60", got)
	}
	if got := l.Balance("B"); got != 40 {
		t.Fatalf("recipient balance = %d, want 40", got)
	}
}

func mustApply(t *testing.T, l *Ledger, tx Transaction) {
	t.Helper()
	if err := l.Apply(tx); err != nil {
		t.Fatalf("Apply(%+v) failed: %v", tx, err)
	}
}
