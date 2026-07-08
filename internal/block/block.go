// Package block defines the Block type, its deterministic hashing scheme,
// and proof-of-work mining.
package block

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"toychain/internal/ledger"
)

// GenesisPrevHash is the fixed, well-known previous-hash value that marks a
// block as the start of the chain. It is 64 hex zeros, matching the width of
// a SHA-256 digest.
var GenesisPrevHash = strings.Repeat("0", 64)

// Block is one entry in the chain. Hash is derived from every other field
// (see ComputeHash) and is only trustworthy once it has been set by Mine or
// verified by RecomputeHash.
type Block struct {
	Index        int64                `json:"index"`
	Timestamp    int64                `json:"timestamp"`
	Transactions []ledger.Transaction `json:"transactions"`
	PrevHash     string               `json:"prev_hash"`
	Nonce        int64                `json:"nonce"`
	Hash         string               `json:"hash"`
}

// hashPayload lists, in a fixed struct-field order, exactly the fields that
// feed a block's hash. The Hash field itself is deliberately excluded: it is
// the output of hashing, not an input. encoding/json marshals struct fields
// in declaration order, so this produces a stable byte serialisation for a
// given set of field values.
type hashPayload struct {
	Index        int64
	Timestamp    int64
	Transactions []ledger.Transaction
	PrevHash     string
	Nonce        int64
}

// ComputeHash deterministically derives this block's hash from Index,
// Timestamp, Transactions, PrevHash, and Nonce, in that order. Calling it
// twice on an unchanged block always yields the same result (FR-3).
func (b *Block) ComputeHash() string {
	payload := hashPayload{
		Index:        b.Index,
		Timestamp:    b.Timestamp,
		Transactions: b.Transactions,
		PrevHash:     b.PrevHash,
		Nonce:        b.Nonce,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// payload only contains marshalable primitives and slices of
		// ledger.Transaction, so this can never fail in practice.
		panic("block: unexpected marshal failure: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// New builds an unmined block. Call Mine on the result before adding it to a
// chain.
func New(index int64, timestamp int64, txs []ledger.Transaction, prevHash string) *Block {
	return &Block{
		Index:        index,
		Timestamp:    timestamp,
		Transactions: txs,
		PrevHash:     prevHash,
	}
}

// NewGenesisBlock returns the single, deterministic block that starts every
// chain: height 0, a fixed timestamp, no transactions, nonce 0, and
// PrevHash equal to GenesisPrevHash (FR-2). Because every input field is
// fixed, its hash is identical on every run of the program.
func NewGenesisBlock() *Block {
	b := &Block{
		Index:        0,
		Timestamp:    0,
		Transactions: []ledger.Transaction{},
		PrevHash:     GenesisPrevHash,
		Nonce:        0,
	}
	b.Hash = b.ComputeHash()
	return b
}

// MeetsDifficulty reports whether hash has at least difficulty leading hex
// zero digits.
func MeetsDifficulty(hash string, difficulty int) bool {
	if difficulty <= 0 {
		return true
	}
	if difficulty > len(hash) {
		return false
	}
	return strings.Count(hash[:difficulty], "0") == difficulty
}

// MiningResult reports how much work Mine performed.
type MiningResult struct {
	Nonce    int64
	Hash     string
	Attempts int64
	Duration time.Duration
}

// Mine searches nonces starting from 0 until the block's hash meets
// difficulty leading hex zero digits, then sets b.Nonce and b.Hash to the
// winning values (FR-5). It reports how many hashes were tried and how long
// the search took.
func (b *Block) Mine(difficulty int) MiningResult {
	start := time.Now()
	var attempts int64
	for nonce := int64(0); ; nonce++ {
		b.Nonce = nonce
		h := b.ComputeHash()
		attempts++
		if MeetsDifficulty(h, difficulty) {
			b.Hash = h
			return MiningResult{
				Nonce:    nonce,
				Hash:     h,
				Attempts: attempts,
				Duration: time.Since(start),
			}
		}
	}
}
