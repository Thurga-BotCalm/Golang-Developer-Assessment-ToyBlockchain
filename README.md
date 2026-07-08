# Toy Blockchain and Ledger Simulator

A minimal, single-process blockchain written from scratch in pure Go: an
append-only chain of proof-of-work-mined blocks, a transaction/ledger model
with account balances, full-chain validation with tamper detection, and a
command-line interface. Built for the Golang Developer Assessment
(Toy Blockchain and Ledger Simulator, v2.0).

## Requirements

- Go 1.22 or newer (developed and tested with the toolchain reported by `go version`).
- No external dependencies — only the standard library.

## Build and run

```sh
go build -o toychain ./cmd/toychain
./toychain <command> [flags]
```

Or run directly without a separate build step:

```sh
go run ./cmd/toychain <command> [flags]
```

## Commands

State is a single JSON file (default `chain.json` in the working directory,
override with `-data <path>` on every command). The very first command run
against a path creates a fresh chain seeded with the genesis block; every
command after that loads and re-saves the same file, so state survives
between runs (FR-8).

| Command    | Purpose                                                      |
|------------|---------------------------------------------------------------|
| `faucet`   | Grant funds to an account from the unlimited faucet           |
| `addtx`    | Add a transaction to the pending pool                         |
| `mine`     | Mine a block from the pending pool                             |
| `print`    | Print the chain (and any pending transactions) in readable form |
| `validate` | Validate the whole chain; reports pass, or the first bad block |
| `balances` | Show account balances derived by replaying the chain           |

```sh
# Fund Alice from the faucet, then mine it into a block.
go run ./cmd/toychain faucet -to Alice -amount 100
go run ./cmd/toychain mine

# Alice pays Bob, then mine that too.
go run ./cmd/toychain addtx -from Alice -to Bob -amount 30
go run ./cmd/toychain mine

go run ./cmd/toychain print
go run ./cmd/toychain balances
go run ./cmd/toychain validate
```

Flags:

- `-data <path>` — chain JSON file (default `chain.json`), every command.
- `-difficulty <n>` — required leading hex-zero digits (default `4`). Only
  takes effect the first time a chain is created at `-data`; ignored on an
  existing file (see Design decisions).
- `-blocksize <n>` — max transactions per mined block (default `5`). Same
  first-creation-only rule as `-difficulty`.
- `addtx`/`faucet`: `-from`/`-to` (accounts), `-amount` (integer, must be positive).
- `balances`: `-account <name>` to show a single account instead of all of them.

## Tests

```sh
go test ./...
```

Covers: deterministic hashing (same block hashes the same way twice, and a
changed field changes the hash), genesis block invariants, mining meeting the
difficulty target with a nonce that reproduces the exact hash, an honest
chain validating, tampering with an earlier block's transaction being
detected (and pinned to the right block), a broken previous-hash link being
detected, non-positive and overspending transactions being rejected without
changing balances, block-size limits being respected, and a JSON save/load
round trip. There's also an end-to-end CLI test driving `faucet` → `mine` →
`addtx` → `mine` → `balances` → `validate` → `print` against a temp file.

## Design decisions

- **Package layout**: `internal/block` (block type, hashing, mining),
  `internal/ledger` (transactions, account balances), `internal/chain`
  (chain assembly, pending pool, validation), `internal/store` (JSON
  persistence), `internal/cli` (command dispatch), `cmd/toychain` (thin
  `main` that calls `cli.Run`). Each package owns one responsibility, per
  the spec's guidance to avoid a single large `main.go`.
- **Hashing**: SHA-256 over a JSON serialisation of `{Index, Timestamp,
  Transactions, PrevHash, Nonce}`, in that field order, with the block's own
  `Hash` field excluded (it's the output, not an input). See
  `internal/block/block.go`'s `hashPayload` type and `ComputeHash`. Because
  `encoding/json` marshals struct fields in declaration order and every field
  is a determinate type (ints, strings, a slice of a determinate struct),
  the same field values always produce the same bytes and thus the same hash.
- **Genesis block**: fixed at height 0, timestamp 0, no transactions, nonce 0,
  and `PrevHash` equal to 64 hex zeros (`block.GenesisPrevHash`). It is not
  mined — it's a well-known constant, not attacker-reachable data — so
  `Validate` checks it against `NewGenesisBlock`'s invariants directly rather
  than a proof-of-work target.
- **Difficulty and block size are fixed per chain**: they're recorded on the
  chain the moment it's created (from flags or defaults) and every later
  `mine` call reuses those stored values; flags passed to later commands are
  ignored once a data file exists. The alternative — letting difficulty
  change on a live chain — would require validation to know which difficulty
  applied to each historical block, which is exactly what real chains solve
  with per-block difficulty fields and retargeting rules (see the research
  report). That's out of scope for this toy, so the simplification is to
  delete the data file and start over if you want different parameters.
- **Faucet**: `ledger.FaucetAccount` ("FAUCET") bypasses balance checks so
  the CLI can seed funds without a genesis pre-allocation. Encouraged
  explicitly by FR-4.
- **Pending pool projection**: `Chain.AddTransaction` validates a new
  transaction against confirmed balances *plus* whatever's already pending
  (`projectedLedger`), so two pending transactions that would jointly
  overdraw an account are still caught before mining, not after.
- **Persistence**: the entire chain state — mined blocks and the pending
  pool — round-trips through one indented JSON file via `internal/store`.
  Simple, inspectable, and sufficient for a single-process toy (FR-8).

## Known limitations

- **Single fixed difficulty for the chain's lifetime.** No retargeting; see
  above and the research report's discussion of alternatives.
- **No signatures.** Anyone can construct a transaction claiming to be any
  sender; there's no cryptographic proof of authorization. Out of scope per
  the assessment brief, called out there as a stretch goal.
- **No Merkle tree.** A block's transactions are hashed as a flat list
  inside the block hash, not summarised by a Merkle root. Fine at this
  transaction-per-block scale; would matter for large blocks or light
  clients.
- **Single process, no peers.** There's no network, gossip, or fork
  resolution — by design, per the assessment's scope.
- **Sequential mining.** `Mine` searches nonces on a single goroutine. Fine
  at the default difficulty (well under a second); concurrent mining is
  listed as a stretch goal.

## Research report

See [`RESEARCH.md`](RESEARCH.md) for the tamper-evidence experiment,
difficulty-vs-effort measurements, the hashing/validation design write-up,
and the discussion questions.
