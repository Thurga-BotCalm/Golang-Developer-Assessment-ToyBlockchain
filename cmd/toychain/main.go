// Command toychain is a minimal, from-scratch blockchain and ledger
// simulator driven entirely from the command line.
package main

import (
	"os"

	"toychain/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
