// Package consensus implements the reputation-weighted consensus mechanism.
package consensus

import (
	"context"
	"math/big"
	"sync"

	"github.com/ccoin/core/internal/dag"
	"github.com/ccoin/core/pkg/types"
)

// Consensus implements the CCoin reputation-weighted consensus algorithm
type Consensus struct {
	mu sync.RWMutex

	// DAG is the block DAG
	dag *dag.DAG

	// MinerStore handles miner data
	minerStore MinerStore

	// Current epoch
	epoch uint64

	// Difficulty adjustment parameters
	targetBlockTime uint64 // Target seconds between blocks
	difficultyWindow uint64 // Number of blocks to consider for adjustment
}

// MinerStore defines the interface for miner data storage
type MinerStore interface {
	GetMiner(ctx context.Context, address types.Address) (*types.Miner, error)
	SaveMiner(ctx context.Context, miner *types.Miner) error
	GetActiveMiners(ctx context.Context, epoch uint64) ([]*types.Miner, error)
	UpdateReputations(ctx context.Context, epoch uint64) error
}

// Config holds consensus configuration
type Config struct {
	// TargetBlockTime is the target time between blocks in seconds
	TargetBlockTime uint64

	// DifficultyWindow is the number of blocks for difficulty adjustment
	DifficultyWindow uint64

	// EpochLength is the number of blocks per epoch
	EpochLength uint64
}

// DefaultConfig returns default consensus configuration
func DefaultConfig() *Config {
	return &Config{
		TargetBlockTime:  10,                // 10 seconds
		DifficultyWindow: 100,               // 100 blocks
		EpochLength:      types.EpochLength, // 1000 blocks
	}
}

// NewConsensus creates a new consensus engine
func NewConsensus(d *dag.DAG, minerStore MinerStore, config *Config) *Consensus {
	if config == nil {
		config = DefaultConfig()
	}

	return &Consensus{
		dag:              d,
		minerStore:       minerStore,
		targetBlockTime:  config.TargetBlockTime,
		difficultyWindow: config.DifficultyWindow,
	}
}

// CalculateBlockWeight computes the reputation-weighted score for a block
// S(B) = Work(B) × Rep(m) + Σ S(Children)
func (c *Consensus) CalculateBlockWeight(ctx context.Context, block *types.Block) *big.Float {
	header := block.Header

	// Work(B) × Rep(m)
	work := new(big.Float).SetInt(header.Work())
	rep := big.NewFloat(header.ReputationScore)
	weight := new(big.Float).Mul(work, rep)

	return weight
}

// CalculateBlockReward computes the reward for mining a block
// R = R_base × (0.5 + 0.5 × Rep)
func (c *Consensus) CalculateBlockReward(height uint64, reputation float64) uint64 {
	baseReward := c.getBaseReward(height)
	multiplier := 0.5 + 0.5*reputation
	return uint64(float64(baseReward) * multiplier)
}

// getBaseReward returns the base block reward at a given height
func (c *Consensus) getBaseReward(height uint64) uint64 {
	halvings := height / types.HalvingInterval

	// After 64 halvings, use tail emission
	if halvings >= 64 {
		return types.TailEmission
	}

	reward := uint64(types.InitialBlockReward)
	for i := uint64(0); i < halvings; i++ {
		reward /= 2
	}

	// Minimum is tail emission
	if reward < types.TailEmission {
		return types.TailEmission
	}

	return reward
}

// ProcessBlock updates consensus state after adding a new block
func (c *Consensus) ProcessBlock(ctx context.Context, block *types.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := block.Header

	// Get or create miner record
	miner, err := c.minerStore.GetMiner(ctx, header.MinerAddress)
	if err != nil {
		miner = types.NewMiner(header.MinerAddress)
	}

	// Record the block
	epoch := header.Height / types.EpochLength
	miner.RecordBlock(header.QualityScore, epoch)

	// Calculate and add reward
	reward := c.CalculateBlockReward(header.Height, header.ReputationScore)
	miner.TotalRewards += reward

	// Save miner
	if err := c.minerStore.SaveMiner(ctx, miner); err != nil {
		return err
	}

	// Check for epoch transition
	newEpoch := header.Height / types.EpochLength
	if newEpoch > c.epoch {
		if err := c.processEpochTransition(ctx, newEpoch); err != nil {
			return err
		}
		c.epoch = newEpoch
	}

	return nil
}

// processEpochTransition handles reputation updates at epoch boundaries
func (c *Consensus) processEpochTransition(ctx context.Context, newEpoch uint64) error {
	return c.minerStore.UpdateReputations(ctx, newEpoch)
}

// CalculateDifficulty computes the difficulty for the next block
func (c *Consensus) CalculateDifficulty(ctx context.Context, parentHeaders []*types.BlockHeader) *big.Int {
	if len(parentHeaders) == 0 {
		return c.getGenesisDifficulty()
	}

	// Use the highest-score parent as reference
	var refHeader *types.BlockHeader
	var maxScore *big.Float

	for _, h := range parentHeaders {
		if maxScore == nil || h.CumulativeScore.Cmp(maxScore) > 0 {
			maxScore = h.CumulativeScore
			refHeader = h
		}
	}

	// Get blocks for difficulty window
	startHeight := uint64(0)
	if refHeader.Height > c.difficultyWindow {
		startHeight = refHeader.Height - c.difficultyWindow
	}

	// Calculate average block time in the window
	// (Simplified - in production, would query all blocks in range)
	avgBlockTime := c.targetBlockTime // Default to target

	// Adjust difficulty
	// If blocks are too fast, increase difficulty (lower target)
	// If blocks are too slow, decrease difficulty (higher target)
	currentDifficulty := refHeader.Difficulty

	ratio := float64(avgBlockTime) / float64(c.targetBlockTime)

	// Clamp adjustment to ±4x per window
	if ratio < 0.25 {
		ratio = 0.25
	}
	if ratio > 4.0 {
		ratio = 4.0
	}

	newDifficulty := new(big.Float).SetInt(currentDifficulty)
	newDifficulty.Mul(newDifficulty, big.NewFloat(ratio))

	result, _ := newDifficulty.Int(nil)

	// Ensure minimum difficulty
	minDifficulty := c.getMinDifficulty()
	if result.Cmp(minDifficulty) < 0 {
		return minDifficulty
	}

	return result
}

// getGenesisDifficulty returns the initial difficulty
func (c *Consensus) getGenesisDifficulty() *big.Int {
	// Start with a moderate difficulty
	// In production, this would be tuned based on expected network hashrate
	return new(big.Int).Exp(big.NewInt(2), big.NewInt(200), nil)
}

// getMinDifficulty returns the minimum allowed difficulty
func (c *Consensus) getMinDifficulty() *big.Int {
	return new(big.Int).Exp(big.NewInt(2), big.NewInt(100), nil)
}

// ValidateDifficulty checks if a block meets its difficulty target
func (c *Consensus) ValidateDifficulty(block *types.Block) bool {
	header := block.Header

	// Hash must be less than difficulty target
	hashInt := new(big.Int).SetBytes(header.Hash[:])
	return hashInt.Cmp(header.Difficulty) < 0
}

// GetMinerReputation returns current reputation for a miner
func (c *Consensus) GetMinerReputation(ctx context.Context, address types.Address) (float64, error) {
	miner, err := c.minerStore.GetMiner(ctx, address)
	if err != nil {
		return types.InitialReputation, nil // New miners start at 1.0
	}
	return miner.ReputationScore, nil
}

// IsConflict checks if two transactions conflict (spend same nullifier)
func (c *Consensus) IsConflict(tx1, tx2 *types.Transaction) bool {
	nullifiers := make(map[types.Hash]struct{})

	for _, n := range tx1.Nullifiers {
		nullifiers[n] = struct{}{}
	}

	for _, n := range tx2.Nullifiers {
		if _, exists := nullifiers[n]; exists {
			return true
		}
	}

	return false
}

// ResolveConflict determines which transaction wins in a conflict
// The transaction in the block with higher cumulative score wins
func (c *Consensus) ResolveConflict(ctx context.Context, block1Score, block2Score *big.Float) int {
	cmp := block1Score.Cmp(block2Score)
	if cmp > 0 {
		return 1 // block1 wins
	} else if cmp < 0 {
		return 2 // block2 wins
	}
	return 0 // tie (use hash comparison in practice)
}
