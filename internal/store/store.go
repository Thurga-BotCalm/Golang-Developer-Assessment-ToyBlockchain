// Package store persists a chain to disk as JSON so that state survives
// between runs of the command-line program (FR-8).
package store

import (
	"encoding/json"
	"os"

	"toychain/internal/chain"
)

// Save writes c to path as indented JSON, creating the file if needed or
// truncating it if it already exists.
func Save(path string, c *chain.Chain) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads and decodes a chain previously written by Save. If path does
// not exist, it returns an error satisfying errors.Is(err, os.ErrNotExist) so
// callers can distinguish "no saved state yet" from a genuine decoding
// failure.
func Load(path string) (*chain.Chain, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c chain.Chain
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
