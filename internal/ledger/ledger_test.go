package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return priv
}

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

	key := newKey(t)
	tx := Transaction{Sender: "A", Recipient: "B", Amount: 150}
	tx.Sign(key)

	err := l.Apply(tx)
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

	key := newKey(t)
	tx := Transaction{Sender: "A", Recipient: "B", Amount: 40}
	tx.Sign(key)
	mustApply(t, l, tx)

	if got := l.Balance("A"); got != 60 {
		t.Fatalf("sender balance = %d, want 60", got)
	}
	if got := l.Balance("B"); got != 40 {
		t.Fatalf("recipient balance = %d, want 40", got)
	}
}

func TestApplyRejectsUnsignedNonFaucetTransaction(t *testing.T) {
	l := New()
	mustApply(t, l, Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 100})

	err := l.Apply(Transaction{Sender: "A", Recipient: "B", Amount: 10})
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Apply(unsigned) = %v, want ErrInvalidSignature", err)
	}
}

func TestApplyRejectsTamperedAmountEvenIfResigned(t *testing.T) {
	l := New()
	mustApply(t, l, Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 100})

	key := newKey(t)
	tx := Transaction{Sender: "A", Recipient: "B", Amount: 10}
	tx.Sign(key)

	// Simulate an attacker who intercepts the signed transaction and bumps
	// the amount without the private key: the signature no longer matches.
	tx.Amount = 90

	if err := l.Apply(tx); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Apply(tampered) = %v, want ErrInvalidSignature", err)
	}
}

func TestApplyRejectsSenderKeyMismatch(t *testing.T) {
	l := New()
	mustApply(t, l, Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 100})

	firstKey := newKey(t)
	first := Transaction{Sender: "A", Recipient: "B", Amount: 10}
	first.Sign(firstKey)
	mustApply(t, l, first)

	// A different key claiming to be the same sender name: this is exactly
	// the impersonation a bare, unsigned toy ledger cannot stop.
	otherKey := newKey(t)
	second := Transaction{Sender: "A", Recipient: "C", Amount: 5}
	second.Sign(otherKey)

	err := l.Apply(second)
	if !errors.Is(err, ErrSenderKeyMismatch) {
		t.Fatalf("Apply(different key, same sender name) = %v, want ErrSenderKeyMismatch", err)
	}
}

func TestApplyAllowsSameKeyRepeatedly(t *testing.T) {
	l := New()
	mustApply(t, l, Transaction{Sender: FaucetAccount, Recipient: "A", Amount: 100})

	key := newKey(t)
	first := Transaction{Sender: "A", Recipient: "B", Amount: 10}
	first.Sign(key)
	mustApply(t, l, first)

	second := Transaction{Sender: "A", Recipient: "C", Amount: 5}
	second.Sign(key)
	mustApply(t, l, second)

	if got := l.Balance("A"); got != 85 {
		t.Fatalf("sender balance = %d, want 85", got)
	}
}

func mustApply(t *testing.T, l *Ledger, tx Transaction) {
	t.Helper()
	if err := l.Apply(tx); err != nil {
		t.Fatalf("Apply(%+v) failed: %v", tx, err)
	}
}
