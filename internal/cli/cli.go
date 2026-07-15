// Package cli implements the toychain command-line interface: generating
// wallets, adding transactions, mining blocks, printing the chain,
// validating it, reporting balances, and resolving a fork (FR-7).
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"time"

	"toychain/internal/chain"
	"toychain/internal/ledger"
	"toychain/internal/store"
	"toychain/internal/wallet"
)

const usageText = `toychain is a toy blockchain and ledger simulator.

Usage:
  toychain <command> [flags]

Commands:
  keygen     Generate a new signing key pair and save it to a file
  addtx      Sign and add a transaction to the pending pool
  faucet     Fund an account from the unlimited faucet (shorthand for addtx)
  mine       Mine a block from the pending pool
  print      Print the chain in a readable form
  validate   Validate the chain and report the first tampered block, if any
  balances   Show account balances derived from the chain
  resolve    Resolve a fork against another chain file (longest valid chain wins)

Every command accepts -data (default "chain.json") for the state file.
Run "toychain <command> -h" for a command's full flag list.
`

// Run executes args (typically os.Args[1:]) and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "keygen":
		err = runKeygen(rest, stdout)
	case "addtx":
		err = runAddTx(rest, stdout)
	case "faucet":
		err = runFaucet(rest, stdout)
	case "mine":
		err = runMine(rest, stdout)
	case "print":
		err = runPrint(rest, stdout)
	case "validate":
		err = runValidate(rest, stdout)
	case "balances":
		err = runBalances(rest, stdout)
	case "resolve":
		err = runResolve(rest, stdout)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "toychain: unknown command %q\n\n%s", cmd, usageText)
		return 2
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "toychain: %v\n", err)
		return 1
	}
	return 0
}

func dataFlag(fs *flag.FlagSet) *string {
	return fs.String("data", "chain.json", "path to the chain's JSON data file")
}

func newChainFlags(fs *flag.FlagSet) (difficulty, blockSize, retargetInterval *int, targetSeconds *int64) {
	difficulty = fs.Int("difficulty", chain.DefaultDifficulty, "starting proof-of-work difficulty (leading hex zeros); only used when creating a brand new chain")
	blockSize = fs.Int("blocksize", chain.DefaultMaxBlockSize, "max transactions per block; only used when creating a brand new chain")
	retargetInterval = fs.Int("retarget-interval", chain.DefaultRetargetInterval, "blocks between difficulty retargets, 0 to disable; only used when creating a brand new chain")
	targetSeconds = fs.Int64("target-seconds", chain.DefaultTargetBlockSeconds, "target seconds per block that retargeting aims for; only used when creating a brand new chain")
	return difficulty, blockSize, retargetInterval, targetSeconds
}

// loadOrCreate loads the chain at path, or creates a fresh one (using the
// given parameters) if no data file exists yet. Once a chain has been
// created, its tuning parameters are fixed: later flags are ignored so that
// validation never disagrees with the parameters a block was actually mined
// under. Start a new chain (delete the data file) to change them.
func loadOrCreate(path string, difficulty, maxBlockSize, retargetInterval int, targetSeconds int64) (*chain.Chain, error) {
	c, err := store.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return chain.NewWithRetarget(difficulty, maxBlockSize, retargetInterval, targetSeconds), nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return c, nil
}

// loadExisting loads the chain at path, refusing to silently create a new
// one — used by commands that only make sense against an already-existing
// chain (print, validate, balances, resolve).
func loadExisting(path string) (*chain.Chain, error) {
	c, err := store.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s does not exist yet; run faucet, addtx, or mine first", path)
	}
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return c, nil
}

func runKeygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "wallet.key", "path to write the new key file to")
	if err := fs.Parse(args); err != nil {
		return err
	}

	w, err := wallet.New()
	if err != nil {
		return err
	}
	if err := w.Save(*out); err != nil {
		return fmt.Errorf("saving %s: %w", *out, err)
	}

	fmt.Fprintf(stdout, "generated key: %s\naddress:       %s\n", *out, w.Address())
	return nil
}

func runAddTx(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("addtx", flag.ContinueOnError)
	data := dataFlag(fs)
	difficulty, blockSize, retargetInterval, targetSeconds := newChainFlags(fs)
	key := fs.String("key", "", "path to the sender's key file, from keygen (required)")
	from := fs.String("from", "", "sender account name")
	to := fs.String("to", "", "recipient account")
	amount := fs.Int64("amount", 0, "amount to send")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return errors.New("addtx requires -from and -to")
	}
	if *key == "" {
		return errors.New("addtx requires -key, a signing key file created with keygen")
	}

	w, err := wallet.Load(*key)
	if err != nil {
		return fmt.Errorf("loading key %s: %w", *key, err)
	}

	c, err := loadOrCreate(*data, *difficulty, *blockSize, *retargetInterval, *targetSeconds)
	if err != nil {
		return err
	}

	tx := w.Sign(ledger.Transaction{Sender: *from, Recipient: *to, Amount: *amount})
	if err := c.AddTransaction(tx); err != nil {
		return fmt.Errorf("transaction rejected: %w", err)
	}
	if err := store.Save(*data, c); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "added: %s -> %s : %d (pending: %d)\n", tx.Sender, tx.Recipient, tx.Amount, len(c.Pending))
	return nil
}

func runFaucet(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("faucet", flag.ContinueOnError)
	data := dataFlag(fs)
	difficulty, blockSize, retargetInterval, targetSeconds := newChainFlags(fs)
	to := fs.String("to", "", "account to fund")
	amount := fs.Int64("amount", 0, "amount to grant")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return errors.New("faucet requires -to")
	}

	c, err := loadOrCreate(*data, *difficulty, *blockSize, *retargetInterval, *targetSeconds)
	if err != nil {
		return err
	}

	tx := ledger.Transaction{Sender: ledger.FaucetAccount, Recipient: *to, Amount: *amount}
	if err := c.AddTransaction(tx); err != nil {
		return fmt.Errorf("transaction rejected: %w", err)
	}
	if err := store.Save(*data, c); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "funded: %s <- faucet : %d (pending: %d)\n", tx.Recipient, tx.Amount, len(c.Pending))
	return nil
}

func runMine(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mine", flag.ContinueOnError)
	data := dataFlag(fs)
	difficulty, blockSize, retargetInterval, targetSeconds := newChainFlags(fs)
	workers := fs.Int("workers", runtime.NumCPU(), "goroutines to search the nonce space with")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := loadOrCreate(*data, *difficulty, *blockSize, *retargetInterval, *targetSeconds)
	if err != nil {
		return err
	}

	b, result, err := c.MineBlockWithWorkers(*workers)
	if err != nil {
		return err
	}
	if err := store.Save(*data, c); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "mined block %d: hash=%s nonce=%d difficulty=%d attempts=%d duration=%s workers=%d txs=%d\n",
		b.Index, b.Hash, result.Nonce, b.Difficulty, result.Attempts, result.Duration, *workers, len(b.Transactions))
	return nil
}

func runPrint(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("print", flag.ContinueOnError)
	data := dataFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := loadExisting(*data)
	if err != nil {
		return err
	}

	for _, b := range c.Blocks {
		fmt.Fprintf(stdout, "Block %d\n", b.Index)
		fmt.Fprintf(stdout, "  Time:       %s\n", time.Unix(b.Timestamp, 0).UTC().Format(time.RFC3339))
		fmt.Fprintf(stdout, "  PrevHash:   %s\n", b.PrevHash)
		fmt.Fprintf(stdout, "  MerkleRoot: %s\n", b.MerkleRoot())
		fmt.Fprintf(stdout, "  Hash:       %s\n", b.Hash)
		fmt.Fprintf(stdout, "  Difficulty: %d\n", b.Difficulty)
		fmt.Fprintf(stdout, "  Nonce:      %d\n", b.Nonce)
		if len(b.Transactions) == 0 {
			fmt.Fprintf(stdout, "  Txs:        (none)\n")
		}
		for _, tx := range b.Transactions {
			fmt.Fprintf(stdout, "  Tx:         %s -> %s : %d\n", tx.Sender, tx.Recipient, tx.Amount)
		}
		fmt.Fprintln(stdout)
	}
	if len(c.Pending) > 0 {
		fmt.Fprintf(stdout, "Pending (%d):\n", len(c.Pending))
		for _, tx := range c.Pending {
			fmt.Fprintf(stdout, "  Tx:        %s -> %s : %d\n", tx.Sender, tx.Recipient, tx.Amount)
		}
	}
	return nil
}

func runValidate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	data := dataFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := loadExisting(*data)
	if err != nil {
		return err
	}

	result := c.Validate()
	if result.Valid {
		fmt.Fprintln(stdout, "VALID: chain integrity confirmed")
		return nil
	}
	fmt.Fprintf(stdout, "INVALID: block %d: %s\n", result.FailedIndex, result.Reason)
	return nil
}

func runBalances(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("balances", flag.ContinueOnError)
	data := dataFlag(fs)
	account := fs.String("account", "", "show only this account's balance")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := loadExisting(*data)
	if err != nil {
		return err
	}

	l := c.Ledger()
	if *account != "" {
		fmt.Fprintf(stdout, "%s: %d\n", *account, l.Balance(*account))
		return nil
	}

	balances := l.Balances()
	names := make([]string, 0, len(balances))
	for name := range balances {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(stdout, "%s: %d\n", name, balances[name])
	}
	return nil
}

func runResolve(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	data := dataFlag(fs)
	other := fs.String("other", "", "path to the competing chain's JSON data file (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *other == "" {
		return errors.New("resolve requires -other, the path to a competing chain file")
	}

	current, err := loadExisting(*data)
	if err != nil {
		return err
	}
	candidate, err := loadExisting(*other)
	if err != nil {
		return err
	}

	winner, replaced, err := chain.ResolveFork(current, candidate)
	if err != nil {
		return err
	}
	if !replaced {
		fmt.Fprintf(stdout, "kept %s: %d blocks vs %d blocks in %s\n", *data, len(current.Blocks), len(candidate.Blocks), *other)
		return nil
	}
	if err := store.Save(*data, winner); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "replaced %s: %d blocks (was %d) from %s\n", *data, len(winner.Blocks), len(current.Blocks), *other)
	return nil
}
