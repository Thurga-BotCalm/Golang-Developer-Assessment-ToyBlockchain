// Package ledger tracks account balances derived from applied transactions.
package ledger

import "errors"

// FaucetAccount is a special sender exempt from balance checks, used to
// introduce initial funds into the system (see FR-4).
const FaucetAccount = "FAUCET"

var (
	ErrNonPositiveAmount   = errors.New("transaction amount must be positive")
	ErrInsufficientBalance = errors.New("sender has insufficient balance")
)

// Transaction moves Amount units from Sender to Recipient. Amount is an
// integer to avoid floating point rounding issues in balance arithmetic.
type Transaction struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    int64  `json:"amount"`
}

// Validate checks the transaction is well-formed in isolation, without
// reference to any account balance.
func (t Transaction) Validate() error {
	if t.Amount <= 0 {
		return ErrNonPositiveAmount
	}
	return nil
}

// Ledger holds account balances derived by replaying transactions in order.
type Ledger struct {
	balances map[string]int64
}

// New returns an empty ledger with no accounts.
func New() *Ledger {
	return &Ledger{balances: make(map[string]int64)}
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

// Apply validates tx and, if acceptable, updates balances in place. On
// rejection the ledger is left unchanged.
func (l *Ledger) Apply(tx Transaction) error {
	if err := tx.Validate(); err != nil {
		return err
	}
	if !l.CanAfford(tx) {
		return ErrInsufficientBalance
	}
	if tx.Sender != FaucetAccount {
		l.balances[tx.Sender] -= tx.Amount
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
