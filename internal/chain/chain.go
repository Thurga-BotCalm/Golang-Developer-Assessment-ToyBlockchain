// Package chain assembles blocks into an append-only, tamper-evident
// blockchain: it owns the pending transaction pool, drives proof-of-work
// mining with automatic difficulty retargeting, validates the chain end to
// end, and can resolve a fork against a competing chain.
package chain

import (
	"fmt"
	"runtime"
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

	// DefaultRetargetInterval is how many blocks pass between difficulty
	// adjustments, and DefaultTargetBlockSeconds is the block time the
	// retargeting rule tries to hold to. See retargetDifficulty.
	DefaultRetargetInterval   = 5
	DefaultTargetBlockSeconds = 2

	// minDifficulty and maxDifficulty bound where retargeting can push the
	// difficulty: never to 0 (which would make proof-of-work meaningless)
	// and never so high that mining stops finishing in seconds on a laptop
	// (NFR "tunable performance").
	minDifficulty = 1
	maxDifficulty = 8
)

// Chain is an ordered, append-only sequence of blocks starting from a fixed
// genesis block, together with the pool of transactions waiting to be mined.
// Difficulty is the chain's starting difficulty, recorded on the genesis
// block; every block after that carries its own Difficulty, adjusted by
// retargetDifficulty every RetargetInterval blocks to hold block times near
// TargetBlockSeconds. MaxBlockSize, RetargetInterval, and TargetBlockSeconds
// are fixed when the chain is first created (see README limitations).
type Chain struct {
	Blocks             []*block.Block       `json:"blocks"`
	Pending            []ledger.Transaction `json:"pending"`
	Difficulty         int                  `json:"difficulty"`
	MaxBlockSize       int                  `json:"max_block_size"`
	RetargetInterval   int                  `json:"retarget_interval"`
	TargetBlockSeconds int64                `json:"target_block_seconds"`
}

// New creates a chain containing only the genesis block, using the default
// retargeting parameters. See NewWithRetarget to override them.
func New(difficulty, maxBlockSize int) *Chain {
	return NewWithRetarget(difficulty, maxBlockSize, DefaultRetargetInterval, DefaultTargetBlockSeconds)
}

// NewWithRetarget creates a chain containing only the genesis block, with
// explicit difficulty-retargeting parameters. A non-positive retargetInterval
// disables retargeting entirely: every block keeps the starting difficulty.
func NewWithRetarget(difficulty, maxBlockSize, retargetInterval int, targetBlockSeconds int64) *Chain {
	return &Chain{
		Blocks:             []*block.Block{block.NewGenesisBlock(difficulty)},
		Pending:            []ledger.Transaction{},
		Difficulty:         difficulty,
		MaxBlockSize:       maxBlockSize,
		RetargetInterval:   retargetInterval,
		TargetBlockSeconds: targetBlockSeconds,
	}
}

// Latest returns the most recently added block.
func (c *Chain) Latest() *block.Block {
	return c.Blocks[len(c.Blocks)-1]
}

// Ledger replays every transaction in every mined block, in order, and
// returns the resulting account balances and key bindings. The pending pool
// is not included: only mined transactions affect balances (FR-4).
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

// AddTransaction validates tx — amount, signature, key binding, and balance,
// all against confirmed balances plus any transactions already pending —
// and, if acceptable, appends it to the pending pool (FR-4, FR-7). On
// rejection the pool is left unchanged.
func (c *Chain) AddTransaction(tx ledger.Transaction) error {
	l := c.projectedLedger()
	if err := l.Apply(tx); err != nil {
		return err
	}
	c.Pending = append(c.Pending, tx)
	return nil
}

// retargetDifficulty computes the difficulty the next block (the one that
// would become blocks[len(blocks)]) should be mined at, given the blocks
// accepted so far. Every interval mined (non-genesis) blocks, it compares
// how long those interval blocks actually took against
// interval*targetSeconds: much faster than target raises the difficulty by
// one, much slower lowers it by one, otherwise it holds steady. This is
// deliberately simple compared to real retargeting algorithms (e.g.
// Bitcoin's, which scales proportionally rather than by a fixed step) — see
// the research report for the tradeoff.
//
// The very first interval-sized window is skipped rather than retargeted:
// it would otherwise have to measure elapsed time against the genesis
// block's Timestamp, which is a fixed sentinel (0), not a real wall-clock
// value (FR-2), and would read as an enormous, meaningless elapsed time.
// Retargeting only ever compares two real, mined blocks' timestamps.
//
// It is a pure function of blocks so that both MineBlock (deciding the next
// block's difficulty) and Validate (checking a stored block's difficulty was
// the one retargeting actually prescribed) call the exact same logic and can
// never disagree.
func retargetDifficulty(blocks []*block.Block, interval int, targetSeconds int64) int {
	n := len(blocks)
	prev := blocks[n-1].Difficulty

	if interval <= 0 {
		return prev
	}

	mined := n - 1 // blocks mined so far, excluding genesis
	if mined == 0 || mined%interval != 0 {
		return prev
	}

	oldestIndex := n - 1 - interval
	if oldestIndex < 1 {
		return prev
	}

	newest := blocks[n-1]
	oldest := blocks[oldestIndex]
	actual := newest.Timestamp - oldest.Timestamp
	expected := targetSeconds * int64(interval)

	switch {
	case actual < expected/2 && prev < maxDifficulty:
		return prev + 1
	case actual > expected*2 && prev > minDifficulty:
		return prev - 1
	default:
		return prev
	}
}

// MineBlock takes up to MaxBlockSize transactions from the front of the
// pending pool, mines a new block at the retargeted difficulty, appends it
// to the chain, and removes the mined transactions from the pool (FR-5,
// FR-7). The nonce search is spread across as many goroutines as the machine
// has CPUs; see MineBlockWithWorkers to control that explicitly.
func (c *Chain) MineBlock() (*block.Block, block.MiningResult, error) {
	return c.MineBlockWithWorkers(runtime.NumCPU())
}

// MineBlockWithWorkers is MineBlock with an explicit mining worker count.
func (c *Chain) MineBlockWithWorkers(workers int) (*block.Block, block.MiningResult, error) {
	if len(c.Pending) == 0 {
		return nil, block.MiningResult{}, fmt.Errorf("chain: no pending transactions to mine")
	}

	n := c.MaxBlockSize
	if n <= 0 || n > len(c.Pending) {
		n = len(c.Pending)
	}
	txs := make([]ledger.Transaction, n)
	copy(txs, c.Pending[:n])

	difficulty := retargetDifficulty(c.Blocks, c.RetargetInterval, c.TargetBlockSeconds)
	prev := c.Latest()
	b := block.New(prev.Index+1, time.Now().Unix(), txs, prev.Hash, difficulty)
	result := b.MineWithWorkers(difficulty, workers)

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
// stored hash matches a recomputation (which, via the Merkle root, catches
// any change to any transaction), every previous-hash link is intact, every
// block satisfies its own recorded proof-of-work target, every block's
// recorded difficulty matches what retargeting actually prescribes at that
// point, heights and timestamps are consistent, and every transaction's
// signature verifies (FR-6).
//
// The signature check matters independently of the hash check: at this toy's
// deliberately low difficulty, an attacker can cheaply re-mine a tampered
// block and every block after it, defeating hash-chain tamper-evidence on
// its own. They cannot, however, forge a valid signature without the
// sender's private key, so a tampered transaction is still caught even after
// a full re-mine (see the research report).
//
// It stops at, and reports, the first offending block.
//
// The genesis block is exempt from the proof-of-work and retargeting
// checks: it is a fixed, well-known block rather than a mined one (FR-2), so
// it is instead checked against NewGenesisBlock's invariants directly.
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
	if genesis.Difficulty != c.Difficulty {
		return fail(0, "genesis block difficulty does not match the chain's starting difficulty")
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
		if !block.MeetsDifficulty(b.Hash, b.Difficulty) {
			return fail(b.Index, "hash does not satisfy the block's own proof-of-work difficulty target")
		}
		if expected := retargetDifficulty(c.Blocks[:i], c.RetargetInterval, c.TargetBlockSeconds); b.Difficulty != expected {
			return fail(b.Index, fmt.Sprintf("difficulty %d does not match the %d retargeting prescribes at this height", b.Difficulty, expected))
		}
		if b.Index != prev.Index+1 {
			return fail(b.Index, "height is not one greater than the previous block")
		}
		if b.Timestamp < prev.Timestamp {
			return fail(b.Index, "timestamp precedes the previous block's timestamp")
		}
		for _, tx := range b.Transactions {
			if !tx.VerifySignature() {
				return fail(b.Index, "contains a transaction with an invalid or missing signature")
			}
		}
	}

	return Result{Valid: true}
}

// ResolveFork compares the current chain against a competing one and applies
// the longest-valid-chain rule: whichever chain has more blocks and
// validates successfully wins. Both chains are expected to share the same
// genesis block — in this toy, genesis is always identical by construction
// (FR-2), so this check is really just a guard against comparing two chains
// that were never the same chain at all (e.g. built with different data
// files from scratch by mistake).
//
// It returns the winning chain and whether other replaced current. On a tie
// in block count, or if other is invalid, current is kept. It is an error to
// resolve against an empty chain or one with a different genesis hash.
func ResolveFork(current, other *Chain) (winner *Chain, replaced bool, err error) {
	if len(current.Blocks) == 0 || len(other.Blocks) == 0 {
		return current, false, fmt.Errorf("chain: cannot resolve an empty chain")
	}
	if current.Blocks[0].Hash != other.Blocks[0].Hash {
		return current, false, fmt.Errorf("chain: not a fork of the same chain (genesis blocks differ)")
	}
	if result := other.Validate(); !result.Valid {
		return current, false, fmt.Errorf("chain: candidate chain is invalid at block %d: %s", result.FailedIndex, result.Reason)
	}
	if len(other.Blocks) > len(current.Blocks) {
		return other, true, nil
	}
	return current, false, nil
}
