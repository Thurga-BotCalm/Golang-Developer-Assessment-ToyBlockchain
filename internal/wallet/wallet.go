// Package wallet generates and persists ed25519 key pairs used to sign
// transactions, and stores them as small JSON key files on disk. It has no
// notion of a "blockchain identity" beyond a key pair: a Wallet just signs
// whatever transaction it is asked to.
package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"toychain/internal/ledger"
)

// Wallet is an ed25519 key pair used to sign transactions.
type Wallet struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// New generates a fresh key pair.
func New() (*Wallet, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("wallet: generating key pair: %w", err)
	}
	return &Wallet{PublicKey: pub, PrivateKey: priv}, nil
}

// Address returns the hex-encoded public key. It is not required to match
// any particular account name: a ledger.Transaction's Sender is a free-form
// label, and it is the ledger that binds a label to a key on first use (see
// ledger.Ledger.Apply), not the wallet.
func (w *Wallet) Address() string {
	return hex.EncodeToString(w.PublicKey)
}

// Sign returns a copy of tx signed with this wallet's private key.
func (w *Wallet) Sign(tx ledger.Transaction) ledger.Transaction {
	tx.Sign(w.PrivateKey)
	return tx
}

// keyFile is the on-disk JSON representation of a Wallet.
type keyFile struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// Save writes w to path as JSON. The file contains the raw private key and
// must be kept secret; this is a toy format with no passphrase protection.
func (w *Wallet) Save(path string) error {
	data, err := json.MarshalIndent(keyFile{
		PublicKey:  hex.EncodeToString(w.PublicKey),
		PrivateKey: hex.EncodeToString(w.PrivateKey),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Load reads a Wallet previously written by Save.
func Load(path string) (*Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kf keyFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("wallet: decoding %s: %w", path, err)
	}
	priv, err := hex.DecodeString(kf.PrivateKey)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("wallet: %s does not contain a valid private key", path)
	}
	pub, err := hex.DecodeString(kf.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("wallet: %s does not contain a valid public key", path)
	}
	return &Wallet{PublicKey: pub, PrivateKey: priv}, nil
}
