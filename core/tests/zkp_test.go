// Package tests provides tests for the ZKP components.
package tests

import (
	"context"
	"testing"

	"github.com/ccoin/core/internal/zkp"
	"github.com/ccoin/core/pkg/types"
)

// Test Pedersen commitment creation
func TestPedersenCommitment(t *testing.T) {
	pc, err := zkp.NewPedersenCommitter()
	if err != nil {
		t.Fatalf("Failed to create Pedersen committer: %v", err)
	}

	// Create commitment
	value := uint64(1000)
	commitment, blinder, err := pc.Commit(value)
	if err != nil {
		t.Fatalf("Failed to create commitment: %v", err)
	}

	// Verify commitment
	valid := pc.Verify(commitment, value, blinder)
	if !valid {
		t.Error("Commitment verification should succeed")
	}

	// Verify with wrong value should fail
	valid = pc.Verify(commitment, value+1, blinder)
	if valid {
		t.Error("Commitment verification with wrong value should fail")
	}
}

// Test homomorphic property
func TestPedersenHomomorphic(t *testing.T) {
	pc, err := zkp.NewPedersenCommitter()
	if err != nil {
		t.Fatalf("Failed to create Pedersen committer: %v", err)
	}

	// Create two commitments
	v1, v2 := uint64(100), uint64(200)
	c1, b1, _ := pc.Commit(v1)
	c2, b2, _ := pc.Commit(v2)

	// Add commitments
	cSum, bSum := pc.Add(c1, b1, c2, b2)

	// Verify sum
	valid := pc.Verify(cSum, v1+v2, bSum)
	if !valid {
		t.Error("Homomorphic addition verification should succeed")
	}
}

// Test nullifier derivation
func TestNullifierDerivation(t *testing.T) {
	ctx := context.Background()
	store := zkp.NewInMemoryNullifierStore()
	ns := zkp.NewNullifierSet(store)

	spendingKey := make([]byte, 32)
	spendingKey[0] = 1
	commitment := types.Hash{1, 2, 3}
	position := uint64(42)

	// Derive nullifier
	nullifier := zkp.DeriveNullifier(spendingKey, commitment, position)

	// Should be deterministic
	nullifier2 := zkp.DeriveNullifier(spendingKey, commitment, position)
	if nullifier != nullifier2 {
		t.Error("Nullifier derivation should be deterministic")
	}

	// Different position should give different nullifier
	nullifier3 := zkp.DeriveNullifier(spendingKey, commitment, position+1)
	if nullifier == nullifier3 {
		t.Error("Different position should give different nullifier")
	}

	// Check not spent
	spent, err := ns.IsSpent(ctx, nullifier)
	if err != nil {
		t.Fatalf("Failed to check spent status: %v", err)
	}
	if spent {
		t.Error("New nullifier should not be spent")
	}

	// Mark as spent
	txHash := types.Hash{10, 20, 30}
	err = ns.MarkSpent(ctx, nullifier, txHash, 100)
	if err != nil {
		t.Fatalf("Failed to mark spent: %v", err)
	}

	// Should now be spent
	spent, err = ns.IsSpent(ctx, nullifier)
	if err != nil {
		t.Fatalf("Failed to check spent status: %v", err)
	}
	if !spent {
		t.Error("Nullifier should now be spent")
	}
}

// Test Merkle tree
func TestMerkleTree(t *testing.T) {
	ctx := context.Background()
	store := zkp.NewInMemoryTreeStore()
	tree := zkp.NewCommitmentTree(store, 32)

	// Add commitments
	commitments := []types.Hash{
		{1, 1, 1},
		{2, 2, 2},
		{3, 3, 3},
		{4, 4, 4},
	}

	for _, c := range commitments {
		_, err := tree.AddCommitment(ctx, c)
		if err != nil {
			t.Fatalf("Failed to add commitment: %v", err)
		}
	}

	// Get root
	root := tree.GetRoot()
	if root == (types.Hash{}) {
		t.Error("Root should not be empty")
	}

	// Get path for first commitment
	path, err := tree.GetPath(ctx, 0)
	if err != nil {
		t.Fatalf("Failed to get path: %v", err)
	}

	// Verify path
	valid := tree.VerifyPath(ctx, commitments[0], path, 0)
	if !valid {
		t.Error("Path verification should succeed")
	}

	// Verify with wrong commitment should fail
	valid = tree.VerifyPath(ctx, types.Hash{99, 99, 99}, path, 0)
	if valid {
		t.Error("Path verification with wrong commitment should fail")
	}
}

// Test transaction builder
func TestTransactionBuilder(t *testing.T) {
	ctx := context.Background()
	cm := zkp.NewCircuitManager()

	// Note: Circuit compilation would be done separately in setup
	// This test uses simulated proofs

	// Create test note
	note := &zkp.Note{
		Value:      1000,
		Position:   0,
		Commitment: types.Hash{1, 2, 3},
		Blinder:    make([]byte, 32),
		Spent:      false,
	}

	// Create output
	recipient := types.Address{10, 20, 30}
	
	// Build would create transaction
	// (simplified test without full proof generation)
	_ = zkp.NewTransactionBuilder(cm)
	_ = note
	_ = recipient
}
