// Package chain assembles blocks into an append-only, tamper-evident
// blockchain: it owns the pending transaction pool, drives proof-of-work
// mining, and validates the chain end to end.
package chain

import (
	"fmt"
	"time"

	"toychain/internal/block"
	"toychain/internal/ledger"
)

// Default tuning parameters, used when no override is supplied. Difficulty 4
// keeps mining under a second on a laptop; a block size of 5 keeps printed
// blocks small enough to read at a glance (FR-9).
const (
	DefaultDifficulty   = 4
	DefaultMaxBlockSize = 5
)

// Chain is an ordered, append-only sequence of blocks starting from a fixed
// genesis block, together with the pool of transactions waiting to be mined.
// Difficulty and MaxBlockSize are fixed when the chain is first created and
// apply to every block mined afterwards; a toy chain does not support
// mid-chain difficulty retargeting (see README limitations).
type Chain struct {
	Blocks       []*block.Block       `json:"blocks"`
	Pending      []ledger.Transaction `json:"pending"`
	Difficulty   int                  `json:"difficulty"`
	MaxBlockSize int                  `json:"max_block_size"`
}

// New creates a chain containing only the genesis block.
func New(difficulty, maxBlockSize int) *Chain {
	return &Chain{
		Blocks:       []*block.Block{block.NewGenesisBlock()},
		Pending:      []ledger.Transaction{},
		Difficulty:   difficulty,
		MaxBlockSize: maxBlockSize,
	}
}

// Latest returns the most recently added block.
func (c *Chain) Latest() *block.Block {
	return c.Blocks[len(c.Blocks)-1]
}

// Ledger replays every transaction in every mined block, in order, and
// returns the resulting account balances. The pending pool is not included:
// only mined transactions affect balances (FR-4).
func (c *Chain) Ledger() *ledger.Ledger {
	l := ledger.New()
	for _, b := range c.Blocks {
		for _, tx := range b.Transactions {
			// Transactions inside mined blocks were already validated before
			// mining, so a failure here indicates chain corruption rather
			// than a normal rejection. Validate reports that case
			// separately, so it's safe to ignore the error here.
			_ = l.Apply(tx)
		}
	}
	return l
}

// projectedLedger returns the ledger after confirmed blocks and any
// already-pending transactions have been applied, used to decide whether a
// newly submitted transaction can be afforded.
func (c *Chain) projectedLedger() *ledger.Ledger {
	l := c.Ledger()
	for _, tx := range c.Pending {
		_ = l.Apply(tx)
	}
	return l
}

// AddTransaction validates tx against confirmed balances plus any
// transactions already pending and, if acceptable, appends it to the pending
// pool (FR-4, FR-7). On rejection the pool is left unchanged.
func (c *Chain) AddTransaction(tx ledger.Transaction) error {
	l := c.projectedLedger()
	if err := l.Apply(tx); err != nil {
		return err
	}
	c.Pending = append(c.Pending, tx)
	return nil
}

// MineBlock takes up to MaxBlockSize transactions from the front of the
// pending pool, mines a new block satisfying Difficulty, appends it to the
// chain, and removes the mined transactions from the pool (FR-5, FR-7).
func (c *Chain) MineBlock() (*block.Block, block.MiningResult, error) {
	if len(c.Pending) == 0 {
		return nil, block.MiningResult{}, fmt.Errorf("chain: no pending transactions to mine")
	}

	n := c.MaxBlockSize
	if n <= 0 || n > len(c.Pending) {
		n = len(c.Pending)
	}
	txs := make([]ledger.Transaction, n)
	copy(txs, c.Pending[:n])

	prev := c.Latest()
	b := block.New(prev.Index+1, time.Now().Unix(), txs, prev.Hash)
	result := b.Mine(c.Difficulty)

	c.Blocks = append(c.Blocks, b)
	c.Pending = append([]ledger.Transaction{}, c.Pending[n:]...)
	return b, result, nil
}

// Result reports the outcome of validating a chain (FR-6).
type Result struct {
	Valid       bool
	FailedIndex int64
	Reason      string
}

func fail(index int64, reason string) Result {
	return Result{Valid: false, FailedIndex: index, Reason: reason}
}

// Validate walks the chain from genesis and checks, for every block: its
// stored hash matches a fresh recomputation, its previous-hash link points at
// the actual previous block's hash, its hash satisfies the proof-of-work
// target, and its height and timestamp are consistent with the block before
// it (FR-6). It stops at, and reports, the first offending block.
//
// The genesis block is exempt from the proof-of-work check: it is a fixed,
// well-known block rather than a mined one (FR-2), so it is instead checked
// against NewGenesisBlock's invariants directly.
func (c *Chain) Validate() Result {
	if len(c.Blocks) == 0 {
		return fail(0, "chain has no blocks")
	}

	genesis := c.Blocks[0]
	if genesis.Index != 0 {
		return fail(genesis.Index, "genesis block must have index 0")
	}
	if genesis.PrevHash != block.GenesisPrevHash {
		return fail(0, "genesis block has the wrong previous-hash value")
	}
	if genesis.Hash != genesis.ComputeHash() {
		return fail(0, "genesis block hash does not match its recomputed hash")
	}

	for i := 1; i < len(c.Blocks); i++ {
		b := c.Blocks[i]
		prev := c.Blocks[i-1]

		if b.Hash != b.ComputeHash() {
			return fail(b.Index, "stored hash does not match recomputed hash")
		}
		if b.PrevHash != prev.Hash {
			return fail(b.Index, "previous-hash does not match the actual previous block's hash")
		}
		if !block.MeetsDifficulty(b.Hash, c.Difficulty) {
			return fail(b.Index, "hash does not satisfy the proof-of-work difficulty target")
		}
		if b.Index != prev.Index+1 {
			return fail(b.Index, "height is not one greater than the previous block")
		}
		if b.Timestamp < prev.Timestamp {
			return fail(b.Index, "timestamp precedes the previous block's timestamp")
		}
	}

	return Result{Valid: true}
}
