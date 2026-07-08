// Package cli implements the toychain command-line interface: adding
// transactions, mining blocks, printing the chain, validating it, and
// reporting balances (FR-7).
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"toychain/internal/chain"
	"toychain/internal/ledger"
	"toychain/internal/store"
)

const usageText = `toychain is a toy blockchain and ledger simulator.

Usage:
  toychain <command> [flags]

Commands:
  addtx      Add a transaction to the pending pool
  faucet     Fund an account from the unlimited faucet (shorthand for addtx)
  mine       Mine a block from the pending pool
  print      Print the chain in a readable form
  validate   Validate the chain and report the first tampered block, if any
  balances   Show account balances derived from the chain

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

func newChainFlags(fs *flag.FlagSet) (difficulty, blockSize *int) {
	difficulty = fs.Int("difficulty", chain.DefaultDifficulty, "proof-of-work difficulty (leading hex zeros); only used when creating a brand new chain")
	blockSize = fs.Int("blocksize", chain.DefaultMaxBlockSize, "max transactions per block; only used when creating a brand new chain")
	return difficulty, blockSize
}

// loadOrCreate loads the chain at path, or creates a fresh one (using
// difficulty and maxBlockSize) if no data file exists yet. Once a chain has
// been created, its difficulty and block size are fixed: later flags are
// ignored so that validation never disagrees with the parameters a block was
// actually mined under. Start a new chain (delete the data file) to change
// them.
func loadOrCreate(path string, difficulty, maxBlockSize int) (*chain.Chain, error) {
	c, err := store.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return chain.New(difficulty, maxBlockSize), nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return c, nil
}

func runAddTx(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("addtx", flag.ContinueOnError)
	data := dataFlag(fs)
	difficulty, blockSize := newChainFlags(fs)
	from := fs.String("from", "", "sender account")
	to := fs.String("to", "", "recipient account")
	amount := fs.Int64("amount", 0, "amount to send")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return errors.New("addtx requires -from and -to")
	}

	c, err := loadOrCreate(*data, *difficulty, *blockSize)
	if err != nil {
		return err
	}

	tx := ledger.Transaction{Sender: *from, Recipient: *to, Amount: *amount}
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
	difficulty, blockSize := newChainFlags(fs)
	to := fs.String("to", "", "account to fund")
	amount := fs.Int64("amount", 0, "amount to grant")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return errors.New("faucet requires -to")
	}

	c, err := loadOrCreate(*data, *difficulty, *blockSize)
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
	difficulty, blockSize := newChainFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := loadOrCreate(*data, *difficulty, *blockSize)
	if err != nil {
		return err
	}

	b, result, err := c.MineBlock()
	if err != nil {
		return err
	}
	if err := store.Save(*data, c); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "mined block %d: hash=%s nonce=%d attempts=%d duration=%s txs=%d\n",
		b.Index, b.Hash, result.Nonce, result.Attempts, result.Duration, len(b.Transactions))
	return nil
}

func runPrint(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("print", flag.ContinueOnError)
	data := dataFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := loadOrCreate(*data, chain.DefaultDifficulty, chain.DefaultMaxBlockSize)
	if err != nil {
		return err
	}

	for _, b := range c.Blocks {
		fmt.Fprintf(stdout, "Block %d\n", b.Index)
		fmt.Fprintf(stdout, "  Time:      %s\n", time.Unix(b.Timestamp, 0).UTC().Format(time.RFC3339))
		fmt.Fprintf(stdout, "  PrevHash:  %s\n", b.PrevHash)
		fmt.Fprintf(stdout, "  Hash:      %s\n", b.Hash)
		fmt.Fprintf(stdout, "  Nonce:     %d\n", b.Nonce)
		if len(b.Transactions) == 0 {
			fmt.Fprintf(stdout, "  Txs:       (none)\n")
		}
		for _, tx := range b.Transactions {
			fmt.Fprintf(stdout, "  Tx:        %s -> %s : %d\n", tx.Sender, tx.Recipient, tx.Amount)
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

	c, err := loadOrCreate(*data, chain.DefaultDifficulty, chain.DefaultMaxBlockSize)
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

	c, err := loadOrCreate(*data, chain.DefaultDifficulty, chain.DefaultMaxBlockSize)
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
