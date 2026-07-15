package block

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"toychain/internal/ledger"
)

// emptyMerkleRoot is the root of a block with no transactions: the SHA-256
// hash of an empty byte slice. Using a fixed constant (rather than, say, all
// zeros) keeps the "no transactions" case indistinguishable from "one
// transaction that happens to hash to zero" only in the sense that both are
// ordinary SHA-256 outputs, not a special-cased sentinel.
var emptyMerkleRoot = func() string {
	sum := sha256.Sum256(nil)
	return hex.EncodeToString(sum[:])
}()

// merkleRoot summarises txs with a binary Merkle tree instead of hashing the
// raw transaction list directly: each transaction is hashed to a leaf, pairs
// of nodes are concatenated and hashed going up the tree, and an odd node
// out at any level is paired with itself. The result is a single hash that
// changes if any transaction, or their order, changes.
func merkleRoot(txs []ledger.Transaction) string {
	if len(txs) == 0 {
		return emptyMerkleRoot
	}

	level := make([][]byte, len(txs))
	for i, tx := range txs {
		level[i] = leafHash(tx)
	}

	for len(level) > 1 {
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			next = append(next, nodeHash(left, right))
		}
		level = next
	}

	return hex.EncodeToString(level[0])
}

func leafHash(tx ledger.Transaction) []byte {
	// Transaction only contains determinate primitive fields, so this can
	// never fail in practice.
	data, err := json.Marshal(tx)
	if err != nil {
		panic("block: unexpected transaction marshal failure: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return sum[:]
}

func nodeHash(left, right []byte) []byte {
	sum := sha256.Sum256(append(append([]byte{}, left...), right...))
	return sum[:]
}
