# Toy Blockchain and Ledger Simulator

A minimal, single-process blockchain written from scratch in pure Go: an
append-only chain of proof-of-work-mined blocks (with automatic difficulty
retargeting and a Merkle root over each block's transactions), a signed
transaction/ledger model with account balances, full-chain validation with
tamper detection, fork resolution, and a command-line interface. Built for
the Golang Developer Assessment (Toy Blockchain and Ledger Simulator, v2.0),
including all five optional stretch goals from Section 13.

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
override with `-data <path>` on every command). The first `faucet`, `addtx`,
or `mine` run against a path creates a fresh chain seeded with the genesis
block; every command after that loads and re-saves the same file, so state
survives between runs (FR-8). `print`, `validate`, `balances`, and `resolve`
expect the chain to already exist.

| Command    | Purpose                                                          |
|------------|-------------------------------------------------------------------|
| `keygen`   | Generate a new ed25519 signing key pair and save it to a file     |
| `faucet`   | Grant funds to an account from the unlimited faucet                |
| `addtx`    | Sign and add a transaction to the pending pool                     |
| `mine`     | Mine a block from the pending pool                                  |
| `print`    | Print the chain (and any pending transactions) in readable form     |
| `validate` | Validate the whole chain; reports pass, or the first bad block      |
| `balances` | Show account balances derived by replaying the chain                |
| `resolve`  | Resolve a fork against another chain file (longest valid chain wins)|

```sh
# Everyone who wants to spend needs a key first (the faucet does not).
go run ./cmd/toychain keygen -out alice.key
go run ./cmd/toychain keygen -out bob.key

# Fund Alice from the faucet, then mine it into a block.
go run ./cmd/toychain faucet -to Alice -amount 100
go run ./cmd/toychain mine

# Alice pays Bob, signed with her key, then mine that too.
go run ./cmd/toychain addtx -key alice.key -from Alice -to Bob -amount 30
go run ./cmd/toychain mine

go run ./cmd/toychain print
go run ./cmd/toychain balances
go run ./cmd/toychain validate

# Resolve a fork: keeps whichever of the two files is longer and valid.
go run ./cmd/toychain resolve -data chain.json -other some-other-chain.json
```

Flags:

- `-data <path>` — chain JSON file (default `chain.json`), every command.
- `-difficulty <n>` — starting proof-of-work difficulty (leading hex-zero
  digits, default `4`). Only takes effect the first time a chain is created
  at `-data`; later blocks are retargeted from there (see below).
- `-blocksize <n>` — max transactions per mined block (default `5`).
  First-creation-only, like `-difficulty`.
- `-retarget-interval <n>` — blocks between automatic difficulty
  adjustments, `0` to disable (default `5`). First-creation-only.
- `-target-seconds <n>` — target seconds per block that retargeting aims
  for (default `2`). First-creation-only.
- `-workers <n>` — `mine` only: goroutines to search the nonce space with
  (default: number of CPUs).
- `keygen`: `-out <path>` — where to write the new key file (default
  `wallet.key`).
- `addtx`: `-key <path>` (required), `-from`/`-to` (accounts), `-amount`
  (integer, must be positive).
- `faucet`: `-to`/`-amount`, no key needed.
- `balances`: `-account <name>` to show a single account instead of all of them.
- `resolve`: `-other <path>` (required) — the competing chain file to compare against.

## Tests

```sh
go test ./...
# with the race detector, since mining is now concurrent:
go test -race ./...
```

Covers: deterministic hashing (same block hashes the same way twice, and a
changed field — including the Merkle root and difficulty — changes the
hash), the Merkle root itself changing with the transaction set, genesis
block invariants, mining meeting the difficulty target (both single- and
multi-worker) with a nonce that reproduces the exact hash, an honest chain
validating, tampering with an earlier block's transaction being detected
(and pinned to the right block) even when the tampered block is honestly
re-mined afterwards (signatures, not just the hash chain, catch it), a
broken previous-hash link being detected, non-positive, unsigned,
overspending, and impersonating transactions all being rejected without
changing balances, block-size limits being respected, difficulty
retargeting's pure logic (table-driven) plus an end-to-end mined-chain
version of it, a difficulty that doesn't match what retargeting prescribes
being caught by validation, fork resolution picking the longer valid chain
in both directions and rejecting mismatched or invalid candidates, and a
JSON save/load round trip. There's also an end-to-end CLI test driving
`keygen` → `faucet` → `mine` → `addtx` → `mine` → `balances` → `validate` →
`print`, plus CLI-level tests for missing keys, impersonation, and
`resolve`, against temp files.

## Design decisions

- **Package layout**: `internal/block` (block type, Merkle root, hashing,
  concurrent mining), `internal/ledger` (transactions, signatures, account
  balances, key binding), `internal/wallet` (key generation and file
  storage), `internal/chain` (chain assembly, pending pool, difficulty
  retargeting, validation, fork resolution), `internal/store` (JSON
  persistence), `internal/cli` (command dispatch), `cmd/toychain` (thin
  `main` that calls `cli.Run`). Each package owns one responsibility, per
  the spec's guidance to avoid a single large `main.go`.
- **Hashing**: SHA-256 over a JSON serialisation of `{Index, Timestamp,
  MerkleRoot, PrevHash, Difficulty, Nonce}`, in that field order, with the
  block's own `Hash` field excluded (it's the output, not an input). See
  `internal/block/block.go`'s `hashPayload` type and `ComputeHash`/
  `hashWithNonce`. `MerkleRoot` is computed fresh from `Transactions` every
  time (see below), never read from a stored field, so the hash is always
  tied to the live transaction list. Because `encoding/json` marshals struct
  fields in declaration order and every field is a determinate type, the
  same field values always produce the same bytes and thus the same hash.
- **Merkle root** (stretch goal): `internal/block/merkle.go` hashes each
  transaction to a leaf (SHA-256 of its JSON encoding), then pairs and
  hashes nodes going up the tree (an odd node out is paired with itself),
  down to one root. This summarises the whole transaction list as one hash
  cheaply, and `Block.MerkleRoot()` exposes it for the `print` command.
  It is *not* stored as a persisted field — always recomputed from
  `Transactions` — so there is nothing to keep in sync or trust separately;
  the block hash simply cannot be right unless the Merkle root of the
  current transaction list is.
- **Genesis block**: fixed at height 0, timestamp 0, no transactions, nonce
  0, `PrevHash` equal to 64 hex zeros (`block.GenesisPrevHash`), and
  `Difficulty` equal to the chain's configured starting difficulty. It is
  not mined — it's a well-known constant, not attacker-reachable data — so
  `Validate` checks it against `NewGenesisBlock`'s invariants directly
  rather than a proof-of-work target. Its `Timestamp` is a fixed sentinel
  (`0`), not a real wall-clock value; retargeting is written to never treat
  that sentinel as a real elapsed time (see below).
- **Digital signatures** (stretch goal): `internal/ledger/ledger.go` adds
  `PubKey`/`Sig` fields to `Transaction` and ed25519 `Sign`/
  `VerifySignature` methods (via the standard library's `crypto/ed25519`,
  no third-party crypto). `internal/wallet` generates and persists key
  pairs to a small JSON file for the CLI to load. `Transaction.Validate`
  now requires a verifying signature for any non-`FAUCET` sender.
  Account names stay human-readable (`Alice`, not a raw hex address): the
  `Ledger` binds a sender name to whichever key first successfully spends
  from it (trust-on-first-use) and rejects any later transaction from that
  name signed by a different key (`ErrSenderKeyMismatch`). This closes the
  gap called out in the original "known limitations" — that anyone could
  construct a transaction merely claiming to be a given sender — without
  giving up friendly account names. `Chain.Validate` independently
  re-verifies every mined transaction's signature, which matters on its own:
  at this toy's deliberately low difficulty, an attacker can cheaply re-mine
  a tampered block (and, on a real chain, everything after it), so hash-chain
  integrity alone is only as strong as the proof-of-work difficulty. A forged
  signature is what they still can't produce without the sender's private
  key (see `TestTamperingSurvivesRemineIsStillCaughtBySignature` and the
  research report). The faucet stays a keyless, trusted system account, as
  before.
- **Concurrent mining** (stretch goal): `Block.MineWithWorkers` splits the
  nonce search across a configurable number of goroutines, each trying a
  disjoint stride (`worker i` tries `i, i+n, i+2n, ...`). Every worker only
  *reads* the block's fields and hashes candidate nonces locally via
  `hashWithNonce` (which takes the nonce as a parameter rather than mutating
  `b.Nonce`), so there's no shared mutable state to race on; `b.Nonce` and
  `b.Hash` are written once, after every worker has stopped, by the single
  caller goroutine. The first worker to find a satisfying hash cancels the
  rest via `context.Context`. `Block.Mine` defaults to `runtime.NumCPU()`
  workers; the CLI's `mine -workers <n>` overrides it. Verified race-free
  with `go test -race`.
- **Difficulty retargeting** (stretch goal): every `RetargetInterval` mined
  blocks, `chain.retargetDifficulty` compares how long that interval
  actually took against `RetargetInterval * TargetBlockSeconds`: much faster
  raises the difficulty by one, much slower lowers it by one (bounded to
  `[1, 8]` to keep mining both meaningful and laptop-fast), otherwise it
  holds steady. This is deliberately a simple fixed-step rule rather than
  Bitcoin's proportional-scaling one — see the research report for the
  tradeoff. It's a pure function of the accepted blocks so far, so both
  `MineBlock` (deciding the *next* block's difficulty) and `Validate`
  (checking a stored block's difficulty against what retargeting would have
  prescribed at that height) call the exact same logic and can never
  disagree — a validator recomputes the whole difficulty history from
  scratch rather than trusting whatever difficulty a block claims for
  itself. The very first interval-sized window is skipped rather than
  retargeted, because it would otherwise measure elapsed time against the
  genesis block's fixed `Timestamp: 0` sentinel and read as a nonsensical,
  enormous "elapsed time" (see `retargetDifficulty`'s comment).
  `RetargetInterval`/`TargetBlockSeconds` are chain-level parameters, fixed
  at creation like difficulty and block size.
- **Fork resolution** (stretch goal): `chain.ResolveFork(current, other)`
  implements the longest-valid-chain rule — `other` must validate
  successfully and have strictly more blocks than `current` to replace it;
  ties keep the incumbent. The CLI's `resolve -other <path>` command loads
  a second chain file, resolves against it, and saves the winner back over
  `-data` if it changed. There's no networking (out of scope, Section 4.2),
  so "the competing chain" is just another local JSON file — the natural
  fit for a single-process toy with no peers to gossip with.
- **Difficulty and block size are fixed per chain**: recorded on the chain
  the moment it's created (from flags or defaults); retargeting adjusts
  every *block's own* difficulty from there, but the chain-level
  `RetargetInterval`/`TargetBlockSeconds` themselves never change mid-chain.
  Delete the data file and start over to change them.
- **Faucet**: `ledger.FaucetAccount` ("FAUCET") bypasses both balance and
  signature checks so the CLI can seed funds without a genesis
  pre-allocation or a system-level key pair. Encouraged explicitly by FR-4.
- **Pending pool projection**: `Chain.AddTransaction` validates a new
  transaction against confirmed balances *plus* whatever's already pending
  (`projectedLedger`), so two pending transactions that would jointly
  overdraw an account are still caught before mining, not after.
- **Persistence**: the entire chain state — mined blocks, the pending pool,
  and the tuning parameters — round-trips through one indented JSON file via
  `internal/store`. Simple, inspectable, and sufficient for a single-process
  toy (FR-8). Key files (`internal/wallet`) are separate, small JSON files
  containing a hex-encoded key pair; there is no passphrase protection (see
  limitations).

## Known limitations

- **No networking.** There's no peer discovery or gossip; fork resolution
  (`resolve -other <path>`) compares two local chain files rather than
  chains obtained from actual peers — by design, per the assessment's scope
  (Section 4.2).
- **Fork resolution is length-only.** It implements the classic
  longest-valid-chain rule, not a cumulative-work rule; since difficulty can
  vary block to block under retargeting, a chain with fewer, harder blocks
  could in principle represent more total work than a longer, easier one.
  Real chains such as Bitcoin resolve on cumulative work for exactly this
  reason.
- **Key binding is trust-on-first-use, with no revocation.** Whoever signs
  the first transaction from a given account name owns that name for the
  life of the chain; there's no mechanism to rotate or revoke a
  compromised key. Key files also have no passphrase protection — anyone
  with the file can sign as that account.
- **Retargeting is a simple fixed step, not proportional.** It moves
  difficulty by exactly one level per adjustment regardless of how far off
  target the actual block time was, unlike, e.g., Bitcoin's proportional
  retargeting. Simpler to reason about and to validate deterministically;
  slower to correct a large timing error.
- **No Merkle proofs.** The Merkle root summarises a block's transactions
  for hashing, but nothing exposes an inclusion proof for a single
  transaction without the full list — fine at this toy's scale; would
  matter for light clients.
- **Single process.** Everything — mining, validation, fork resolution —
  runs as one local program acting on JSON files; nothing here is a
  network service.

## Research report

See [`Report.pdf`](Report.pdf) for the tamper-evidence experiment,
difficulty-vs-effort measurements, the hashing/validation design write-up,
and the discussion questions. Note that it predates the stretch-goal work
described above; it covers the required Section 7 investigations against
the core implementation.
