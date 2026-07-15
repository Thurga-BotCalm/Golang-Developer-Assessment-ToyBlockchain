// Package ledger tracks account balances derived from applied transactions.
package ledger

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
)

// FaucetAccount is a special sender exempt from balance checks, used to
// introduce initial funds into the system (see FR-4). It is also exempt
// from signature checks: it is a trusted system account, not a real wallet.
const FaucetAccount = "FAUCET"

var (
	ErrNonPositiveAmount   = errors.New("transaction amount must be positive")
	ErrInsufficientBalance = errors.New("sender has insufficient balance")
	ErrInvalidSignature    = errors.New("transaction signature is missing or does not verify")
	ErrSenderKeyMismatch   = errors.New("transaction is signed by a different key than this account has used before")
)

// Transaction moves Amount units from Sender to Recipient. Amount is an
// integer to avoid floating point rounding issues in balance arithmetic.
// PubKey and Sig authenticate the transaction: PubKey is the hex-encoded
// ed25519 public key that signed it, and Sig is the hex-encoded signature
// over SigningMessage(). Both are required for any sender except
// FaucetAccount (see VerifySignature).
type Transaction struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    int64  `json:"amount"`
	PubKey    string `json:"pubkey,omitempty"`
	Sig       string `json:"sig,omitempty"`
}

// SigningMessage returns the canonical bytes a signature is computed over:
// every field that determines what the transaction does, in a fixed order.
// PubKey and Sig are themselves excluded, since they are the signature's
// output, not its input.
func (t Transaction) SigningMessage() []byte {
	return []byte(fmt.Sprintf("%s|%s|%d", t.Sender, t.Recipient, t.Amount))
}

// Sign signs the transaction with priv, setting PubKey and Sig in place.
func (t *Transaction) Sign(priv ed25519.PrivateKey) {
	pub := priv.Public().(ed25519.PublicKey)
	t.PubKey = hex.EncodeToString(pub)
	t.Sig = hex.EncodeToString(ed25519.Sign(priv, t.SigningMessage()))
}

// VerifySignature reports whether the transaction carries a valid ed25519
// signature over SigningMessage() from the key in PubKey. FaucetAccount
// transactions are always considered valid: the faucet is a trusted system
// account with no real key, by design (see FR-4).
func (t Transaction) VerifySignature() bool {
	if t.Sender == FaucetAccount {
		return true
	}
	pub, err := hex.DecodeString(t.PubKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(t.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), t.SigningMessage(), sig)
}

// Validate checks the transaction is well-formed in isolation, without
// reference to any account balance or prior key usage: the amount must be
// positive and, for a non-faucet sender, the signature must verify.
func (t Transaction) Validate() error {
	if t.Amount <= 0 {
		return ErrNonPositiveAmount
	}
	if !t.VerifySignature() {
		return ErrInvalidSignature
	}
	return nil
}

// Ledger holds account balances, and the signing key each account has bound
// itself to, derived by replaying transactions in order.
type Ledger struct {
	balances map[string]int64
	keys     map[string]string // account name -> hex pubkey, bound on first signed spend
}

// New returns an empty ledger with no accounts.
func New() *Ledger {
	return &Ledger{balances: make(map[string]int64), keys: make(map[string]string)}
}

// Balance returns the current balance of account, defaulting to 0.
func (l *Ledger) Balance(account string) int64 {
	return l.balances[account]
}

// CanAfford reports whether tx.Sender has enough balance to cover tx, taking
// the faucet's unlimited funds into account.
func (l *Ledger) CanAfford(tx Transaction) bool {
	if tx.Sender == FaucetAccount {
		return true
	}
	return l.balances[tx.Sender] >= tx.Amount
}

// Apply validates tx — well-formedness, signature, and (for a returning
// sender) that the signature matches the key this account bound itself to
// the first time it spent — and, if acceptable, updates balances in place.
// The first time a non-faucet account is ever a sender, its signing key is
// bound for future transactions from that account name (trust-on-first-use);
// this is what stops a second party from later constructing transactions
// that merely claim to be from that account name, closing the gap a bare,
// unsigned toy ledger would otherwise have. On rejection the ledger is left
// unchanged.
func (l *Ledger) Apply(tx Transaction) error {
	if err := tx.Validate(); err != nil {
		return err
	}
	if tx.Sender != FaucetAccount {
		if bound, ok := l.keys[tx.Sender]; ok && bound != tx.PubKey {
			return ErrSenderKeyMismatch
		}
	}
	if !l.CanAfford(tx) {
		return ErrInsufficientBalance
	}
	if tx.Sender != FaucetAccount {
		l.balances[tx.Sender] -= tx.Amount
		if _, ok := l.keys[tx.Sender]; !ok {
			l.keys[tx.Sender] = tx.PubKey
		}
	}
	l.balances[tx.Recipient] += tx.Amount
	return nil
}

// Balances returns a copy of every known account balance.
func (l *Ledger) Balances() map[string]int64 {
	out := make(map[string]int64, len(l.balances))
	for k, v := range l.balances {
		out[k] = v
	}
	return out
}
