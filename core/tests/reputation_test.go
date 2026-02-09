// Package tests provides integration tests for reputation system.
package tests

import (
	"context"
	"testing"

	"github.com/ccoin/core/internal/reputation"
	"github.com/ccoin/core/pkg/types"
)

// Mock reputation store
type mockReputationStore struct {
	miners  map[types.Address]*reputation.MinerReputation
	epochs  map[uint64]*reputation.EpochData
}

func newMockReputationStore() *mockReputationStore {
	return &mockReputationStore{
		miners: make(map[types.Address]*reputation.MinerReputation),
		epochs: make(map[uint64]*reputation.EpochData),
	}
}

func (s *mockReputationStore) SaveMiner(ctx context.Context, m *reputation.MinerReputation) error {
	s.miners[m.Address] = m
	return nil
}

func (s *mockReputationStore) GetMiner(ctx context.Context, addr types.Address) (*reputation.MinerReputation, error) {
	return s.miners[addr], nil
}

func (s *mockReputationStore) SaveEpoch(ctx context.Context, e *reputation.EpochData) error {
	s.epochs[e.EpochNumber] = e
	return nil
}

func (s *mockReputationStore) GetEpoch(ctx context.Context, num uint64) (*reputation.EpochData, error) {
	return s.epochs[num], nil
}

// Test EWMA reputation updates
func TestEWMAReputation(t *testing.T) {
	ctx := context.Background()
	store := newMockReputationStore()
	em := reputation.NewEpochManager(store)

	minerAddr := types.Address{1, 2, 3}

	// Process blocks with varying quality
	qualities := []float64{1.0, 0.8, 1.2, 0.9, 1.1}
	
	for i, quality := range qualities {
		block := &types.Block{
			Header: types.BlockHeader{
				Height:       uint64(i + 1),
				MinerAddress: minerAddr,
				QualityScore: quality,
			},
		}

		err := em.ProcessBlock(ctx, block)
		if err != nil {
			t.Fatalf("Failed to process block: %v", err)
		}
	}

	// Get miner reputation
	miner := em.GetMiner(minerAddr)
	if miner == nil {
		t.Fatal("Miner should exist")
	}

	// EWMA should be between min and max
	if miner.Score < reputation.MinReputation || miner.Score > reputation.MaxReputation {
		t.Errorf("Reputation %f out of bounds", miner.Score)
	}

	// Should have processed all blocks
	if miner.TotalBlocks != uint64(len(qualities)) {
		t.Errorf("Expected %d blocks, got %d", len(qualities), miner.TotalBlocks)
	}
}

// Test miner banning
func TestMinerBanning(t *testing.T) {
	ctx := context.Background()
	store := newMockReputationStore()
	em := reputation.NewEpochManager(store)

	minerAddr := types.Address{1, 2, 3}

	// Process low quality blocks to trigger ban
	for i := 0; i < 50; i++ {
		block := &types.Block{
			Header: types.BlockHeader{
				Height:       uint64(i + 1),
				MinerAddress: minerAddr,
				QualityScore: 0.1, // Very low quality
			},
		}

		_ = em.ProcessBlock(ctx, block)
	}

	miner := em.GetMiner(minerAddr)
	// Low quality should decrease reputation
	if miner.Score >= 1.0 {
		t.Error("Score should decrease with low quality blocks")
	}
}

// Test slashing
func TestSlashing(t *testing.T) {
	ctx := context.Background()
	store := newMockSlashingStore()
	sm := reputation.NewSlashingManager(store)

	minerAddr := types.Address{5, 6, 7}
	initialStake := uint64(100000)

	// Register stake
	err := sm.RegisterStake(ctx, minerAddr, initialStake)
	if err != nil {
		t.Fatalf("Failed to register stake: %v", err)
	}

	// Submit evidence
	evidence := &reputation.SlashingEvidence{
		OffenseType:  reputation.OffenseInvalidBlock,
		MinerAddress: minerAddr,
		BlockHeight:  100,
		EvidenceHash: types.Hash{1, 2, 3},
		ProofData:    []byte("proof"),
	}

	err = sm.SubmitEvidence(ctx, evidence)
	if err != nil {
		t.Fatalf("Failed to submit evidence: %v", err)
	}

	// Process slashing
	slashed, err := sm.ProcessEvidence(ctx, evidence.EvidenceHash, minerAddr, 101)
	if err != nil {
		t.Fatalf("Failed to process slashing: %v", err)
	}

	if slashed == 0 {
		t.Error("Should have slashed some amount")
	}

	// Check remaining stake
	remaining := sm.GetStake(minerAddr)
	if remaining != initialStake-slashed {
		t.Errorf("Remaining stake should be %d, got %d", initialStake-slashed, remaining)
	}
}

// Mock slashing store
type mockSlashingStore struct {
	evidence map[types.Hash]*reputation.SlashingEvidence
	stakes   map[types.Address]uint64
}

func newMockSlashingStore() *mockSlashingStore {
	return &mockSlashingStore{
		evidence: make(map[types.Hash]*reputation.SlashingEvidence),
		stakes:   make(map[types.Address]uint64),
	}
}

func (s *mockSlashingStore) SaveEvidence(ctx context.Context, e *reputation.SlashingEvidence) error {
	s.evidence[e.EvidenceHash] = e
	return nil
}

func (s *mockSlashingStore) GetEvidence(ctx context.Context, h types.Hash) (*reputation.SlashingEvidence, error) {
	return s.evidence[h], nil
}

func (s *mockSlashingStore) SaveStake(ctx context.Context, addr types.Address, stake uint64) error {
	s.stakes[addr] = stake
	return nil
}

func (s *mockSlashingStore) GetStake(ctx context.Context, addr types.Address) (uint64, error) {
	return s.stakes[addr], nil
}
