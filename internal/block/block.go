// Package block defines the Block type, its deterministic hashing scheme,
// and proof-of-work mining.
package block

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	Difficulty   int                  `json:"difficulty"`
	Nonce        int64                `json:"nonce"`
	Hash         string               `json:"hash"`
}

// hashPayload lists, in a fixed struct-field order, exactly the fields that
// feed a block's hash. The Hash field itself is deliberately excluded: it is
// the output of hashing, not an input. Transactions are not hashed directly;
// instead their Merkle root stands in for the whole list (see merkleRoot),
// so the hash changes if any transaction changes without requiring the
// entire transaction list to be rehashed as one blob.
type hashPayload struct {
	Index      int64
	Timestamp  int64
	MerkleRoot string
	PrevHash   string
	Difficulty int
	Nonce      int64
}

// ComputeHash deterministically derives this block's hash from Index,
// Timestamp, the Merkle root of Transactions, PrevHash, Difficulty, and
// Nonce, in that order. Calling it twice on an unchanged block always
// yields the same result (FR-3).
func (b *Block) ComputeHash() string {
	return b.hashWithNonce(b.Nonce)
}

// MerkleRoot returns the Merkle root of this block's transactions, exposed
// for display (see the CLI's print command) and for tests.
func (b *Block) MerkleRoot() string {
	return merkleRoot(b.Transactions)
}

// hashWithNonce computes the block's hash as if Nonce were the given value,
// without mutating the block. Kept separate from ComputeHash so that
// concurrent mining workers can probe candidate nonces without racing on a
// shared field.
func (b *Block) hashWithNonce(nonce int64) string {
	payload := hashPayload{
		Index:      b.Index,
		Timestamp:  b.Timestamp,
		MerkleRoot: merkleRoot(b.Transactions),
		PrevHash:   b.PrevHash,
		Difficulty: b.Difficulty,
		Nonce:      nonce,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// payload only contains marshalable primitives and a hex string, so
		// this can never fail in practice.
		panic("block: unexpected marshal failure: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// New builds an unmined block at the given difficulty. Call Mine on the
// result before adding it to a chain.
func New(index int64, timestamp int64, txs []ledger.Transaction, prevHash string, difficulty int) *Block {
	return &Block{
		Index:        index,
		Timestamp:    timestamp,
		Transactions: txs,
		PrevHash:     prevHash,
		Difficulty:   difficulty,
	}
}

// NewGenesisBlock returns the single, deterministic block that starts every
// chain: height 0, a fixed timestamp, no transactions, nonce 0, and
// PrevHash equal to GenesisPrevHash (FR-2). difficulty is recorded as the
// chain's starting difficulty so that later difficulty retargeting has a
// baseline to work from; the genesis block itself is not mined against it
// (see Chain.Validate). Because every input field is fixed, its hash is
// identical on every run of the program for a given difficulty.
func NewGenesisBlock(difficulty int) *Block {
	b := &Block{
		Index:        0,
		Timestamp:    0,
		Transactions: []ledger.Transaction{},
		PrevHash:     GenesisPrevHash,
		Difficulty:   difficulty,
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

// Mine searches nonces until the block's hash meets difficulty leading hex
// zero digits, then sets b.Nonce and b.Hash to the winning values (FR-5).
// The search is split across as many goroutines as the machine has CPUs; see
// MineWithWorkers to control that explicitly.
func (b *Block) Mine(difficulty int) MiningResult {
	return b.MineWithWorkers(difficulty, runtime.NumCPU())
}

// MineWithWorkers is Mine with an explicit worker count. Each worker
// searches a disjoint stride of the nonce space (worker i tries i, i+n,
// i+2n, ...); as soon as any worker finds a hash meeting difficulty, the
// others are cancelled. Every worker only reads b's fields and computes
// hashes locally via hashWithNonce, so there is no data race despite the
// concurrent search; b.Nonce and b.Hash are written once, after all workers
// have stopped.
func (b *Block) MineWithWorkers(difficulty, workers int) MiningResult {
	if workers < 1 {
		workers = 1
	}

	start := time.Now()
	var attempts int64

	type found struct {
		nonce int64
		hash  string
	}
	winner := make(chan found, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(offset int64) {
			defer wg.Done()
			for nonce := offset; ; nonce += int64(workers) {
				select {
				case <-ctx.Done():
					return
				default:
				}
				h := b.hashWithNonce(nonce)
				atomic.AddInt64(&attempts, 1)
				if MeetsDifficulty(h, difficulty) {
					select {
					case winner <- found{nonce: nonce, hash: h}:
						cancel()
					default:
					}
					return
				}
			}
		}(int64(w))
	}

	result := <-winner
	wg.Wait()

	b.Nonce = result.nonce
	b.Hash = result.hash
	return MiningResult{
		Nonce:    result.nonce,
		Hash:     result.hash,
		Attempts: atomic.LoadInt64(&attempts),
		Duration: time.Since(start),
	}
}
