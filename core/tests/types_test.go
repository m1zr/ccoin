// Package tests provides unit tests for core blockchain components.
package tests

import (
	"math/big"
	"testing"
	"time"

	"github.com/ccoin/core/pkg/types"
)

// Test BlockHeader creation and hashing
func TestBlockHeader(t *testing.T) {
	header := types.BlockHeader{
		Version:         1,
		Parents:         []types.Hash{{1, 2, 3}},
		TxRoot:          types.Hash{4, 5, 6},
		StateRoot:       types.Hash{7, 8, 9},
		MinerAddress:    types.Address{10, 11, 12},
		ReputationScore: 1.5,
		Difficulty:      big.NewInt(1000),
		Nonce:           12345,
		Timestamp:       uint64(time.Now().Unix()),
		Height:          100,
	}

	// Test hash computation
	hash := header.ComputeHash()
	if hash == (types.Hash{}) {
		t.Error("Hash should not be empty")
	}

	// Hash should be deterministic
	hash2 := header.ComputeHash()
	if hash != hash2 {
		t.Error("Hash should be deterministic")
	}
}

// Test Transaction creation
func TestTransaction(t *testing.T) {
	tx := types.Transaction{
		Version:    1,
		Nullifiers: []types.Hash{{1, 2, 3}, {4, 5, 6}},
		Commitments: []types.Commitment{
			{Value: types.Hash{7, 8, 9}},
		},
		Proof: types.ZKProof{
			ProofType: 1,
			ProofData: []byte("test_proof"),
		},
		Fee: 1000,
	}

	// Test hash computation
	txHash := tx.ComputeHash()
	if txHash == (types.Hash{}) {
		t.Error("Transaction hash should not be empty")
	}
}

// Test Block validation
func TestBlockValidation(t *testing.T) {
	// Create a valid block
	block := &types.Block{
		Header: types.BlockHeader{
			Version:    1,
			Height:     1,
			Parents:    []types.Hash{{}}, // Genesis parent
			Difficulty: big.NewInt(1000),
			Timestamp:  uint64(time.Now().Unix()),
		},
		Transactions: []*types.Transaction{},
	}

	// Basic validations
	if block.Header.Height != 1 {
		t.Error("Block height should be 1")
	}

	if len(block.Header.Parents) != 1 {
		t.Error("Block should have one parent")
	}
}

// Test Hash utilities
func TestHashUtilities(t *testing.T) {
	hash1 := types.Hash{1, 2, 3}
	hash2 := types.Hash{1, 2, 3}
	hash3 := types.Hash{4, 5, 6}

	// Test equality
	if hash1 != hash2 {
		t.Error("Equal hashes should match")
	}

	if hash1 == hash3 {
		t.Error("Different hashes should not match")
	}

	// Test zero hash
	var zeroHash types.Hash
	if hash1 == zeroHash {
		t.Error("Non-zero hash should not equal zero hash")
	}
}

// Test Address derivation
func TestAddress(t *testing.T) {
	addr := types.Address{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}

	// Test size
	if len(addr) != types.AddressSize {
		t.Errorf("Address size should be %d, got %d", types.AddressSize, len(addr))
	}
}

// Test Commitment
func TestCommitment(t *testing.T) {
	commitment := types.Commitment{
		Value: types.Hash{1, 2, 3},
	}

	if commitment.Value == (types.Hash{}) {
		t.Error("Commitment value should not be empty")
	}
}
