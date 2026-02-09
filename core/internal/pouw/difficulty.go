// Package pouw implements difficulty adjustment for PoUW mining.
package pouw

import (
	"math/big"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// DifficultyManager handles difficulty adjustment
type DifficultyManager struct {
	mu sync.RWMutex

	// Target block time in seconds
	targetBlockTime uint64

	// Adjustment window (number of blocks)
	adjustmentWindow uint64

	// Recent block timestamps
	blockTimes []uint64

	// Current difficulty
	currentDifficulty *big.Int

	// Bounds
	minDifficulty *big.Int
	maxDifficulty *big.Int
}

// DifficultyConfig holds difficulty adjustment configuration
type DifficultyConfig struct {
	TargetBlockTime  uint64
	AdjustmentWindow uint64
	InitialDifficulty *big.Int
}

// DefaultDifficultyConfig returns default configuration
func DefaultDifficultyConfig() *DifficultyConfig {
	// Initial difficulty: 2^200
	initialDiff := new(big.Int).Exp(big.NewInt(2), big.NewInt(200), nil)

	return &DifficultyConfig{
		TargetBlockTime:   10, // 10 seconds
		AdjustmentWindow:  100,
		InitialDifficulty: initialDiff,
	}
}

// NewDifficultyManager creates a new difficulty manager
func NewDifficultyManager(cfg *DifficultyConfig) *DifficultyManager {
	if cfg == nil {
		cfg = DefaultDifficultyConfig()
	}

	// Min difficulty: 2^100
	minDiff := new(big.Int).Exp(big.NewInt(2), big.NewInt(100), nil)
	// Max difficulty: 2^256 - 1 (effectively no max)
	maxDiff := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	maxDiff.Sub(maxDiff, big.NewInt(1))

	return &DifficultyManager{
		targetBlockTime:   cfg.TargetBlockTime,
		adjustmentWindow:  cfg.AdjustmentWindow,
		currentDifficulty: new(big.Int).Set(cfg.InitialDifficulty),
		blockTimes:        make([]uint64, 0, cfg.AdjustmentWindow),
		minDifficulty:     minDiff,
		maxDifficulty:     maxDiff,
	}
}

// RecordBlock records a new block timestamp
func (dm *DifficultyManager) RecordBlock(timestamp uint64) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.blockTimes = append(dm.blockTimes, timestamp)

	// Keep only window size
	if uint64(len(dm.blockTimes)) > dm.adjustmentWindow {
		dm.blockTimes = dm.blockTimes[1:]
	}
}

// AdjustDifficulty calculates and applies difficulty adjustment
func (dm *DifficultyManager) AdjustDifficulty() *big.Int {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if len(dm.blockTimes) < 2 {
		return dm.currentDifficulty
	}

	// Calculate average block time over the window
	first := dm.blockTimes[0]
	last := dm.blockTimes[len(dm.blockTimes)-1]
	elapsed := last - first

	if elapsed == 0 {
		return dm.currentDifficulty
	}

	numBlocks := uint64(len(dm.blockTimes) - 1)
	avgBlockTime := elapsed / numBlocks

	// Calculate adjustment ratio
	// If blocks are too fast (low avg time), increase difficulty (make target smaller)
	// If blocks are too slow (high avg time), decrease difficulty (make target larger)
	ratio := float64(avgBlockTime) / float64(dm.targetBlockTime)

	// Clamp adjustment to ±4x per window
	if ratio < 0.25 {
		ratio = 0.25
	}
	if ratio > 4.0 {
		ratio = 4.0
	}

	// Apply adjustment
	// Difficulty is inversely related to target: higher difficulty = lower target = harder to find
	// If ratio > 1 (blocks too slow), we need LOWER difficulty (higher target)
	// If ratio < 1 (blocks too fast), we need HIGHER difficulty (lower target)
	adjustedDiff := new(big.Float).SetInt(dm.currentDifficulty)
	adjustedDiff.Quo(adjustedDiff, big.NewFloat(ratio))

	newDifficulty, _ := adjustedDiff.Int(nil)

	// Clamp to bounds
	if newDifficulty.Cmp(dm.minDifficulty) < 0 {
		newDifficulty = new(big.Int).Set(dm.minDifficulty)
	}
	if newDifficulty.Cmp(dm.maxDifficulty) > 0 {
		newDifficulty = new(big.Int).Set(dm.maxDifficulty)
	}

	dm.currentDifficulty = newDifficulty
	return newDifficulty
}

// GetDifficulty returns the current difficulty
func (dm *DifficultyManager) GetDifficulty() *big.Int {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return new(big.Int).Set(dm.currentDifficulty)
}

// SetDifficulty sets the difficulty (for loading from storage)
func (dm *DifficultyManager) SetDifficulty(difficulty *big.Int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.currentDifficulty = new(big.Int).Set(difficulty)
}

// CalculateNextDifficulty calculates what the next difficulty should be
// This is used for block validation
func (dm *DifficultyManager) CalculateNextDifficulty(parentTimestamps []uint64) *big.Int {
	if len(parentTimestamps) < 2 {
		return dm.GetDifficulty()
	}

	// Calculate average block time from parent timestamps
	windowSize := dm.adjustmentWindow
	if uint64(len(parentTimestamps)) < windowSize {
		windowSize = uint64(len(parentTimestamps))
	}

	recentTimes := parentTimestamps[len(parentTimestamps)-int(windowSize):]
	if len(recentTimes) < 2 {
		return dm.GetDifficulty()
	}

	elapsed := recentTimes[len(recentTimes)-1] - recentTimes[0]
	numBlocks := uint64(len(recentTimes) - 1)
	avgBlockTime := elapsed / numBlocks

	ratio := float64(avgBlockTime) / float64(dm.targetBlockTime)
	if ratio < 0.25 {
		ratio = 0.25
	}
	if ratio > 4.0 {
		ratio = 4.0
	}

	currentDiff := dm.GetDifficulty()
	adjustedDiff := new(big.Float).SetInt(currentDiff)
	adjustedDiff.Quo(adjustedDiff, big.NewFloat(ratio))

	newDifficulty, _ := adjustedDiff.Int(nil)

	if newDifficulty.Cmp(dm.minDifficulty) < 0 {
		return new(big.Int).Set(dm.minDifficulty)
	}
	if newDifficulty.Cmp(dm.maxDifficulty) > 0 {
		return new(big.Int).Set(dm.maxDifficulty)
	}

	return newDifficulty
}

// ValidateBlockDifficulty checks if a block meets the difficulty target
func ValidateBlockDifficulty(header *types.BlockHeader) bool {
	// Hash must be less than difficulty target
	hashInt := new(big.Int).SetBytes(header.Hash[:])
	return hashInt.Cmp(header.Difficulty) < 0
}

// EstimateHashrate estimates the network hashrate from difficulty and block time
func EstimateHashrate(difficulty *big.Int, avgBlockTime float64) *big.Int {
	if avgBlockTime <= 0 {
		return big.NewInt(0)
	}

	// Hashrate ≈ Difficulty / BlockTime
	hashrate := new(big.Float).SetInt(difficulty)
	hashrate.Quo(hashrate, big.NewFloat(avgBlockTime))

	result, _ := hashrate.Int(nil)
	return result
}

// QualityAdjustedDifficulty adjusts difficulty based on quality scores
// Higher quality = slightly lower effective difficulty (reward good work)
type QualityAdjustedDifficulty struct {
	baseDifficulty *big.Int
	qualityFactor  float64 // 0.0 to 1.0, how much quality affects difficulty
}

// NewQualityAdjustedDifficulty creates a new quality-adjusted difficulty calculator
func NewQualityAdjustedDifficulty(base *big.Int, qualityFactor float64) *QualityAdjustedDifficulty {
	return &QualityAdjustedDifficulty{
		baseDifficulty: base,
		qualityFactor:  qualityFactor,
	}
}

// EffectiveDifficulty calculates the effective difficulty given a quality score
func (qad *QualityAdjustedDifficulty) EffectiveDifficulty(qualityScore float64) *big.Int {
	// Effective difficulty = base * (1 - qualityFactor * (qualityScore - 0.5))
	// This gives a slight advantage to high-quality work
	adjustment := 1.0 - qad.qualityFactor*(qualityScore-0.5)
	if adjustment < 0.5 {
		adjustment = 0.5
	}
	if adjustment > 1.5 {
		adjustment = 1.5
	}

	adjusted := new(big.Float).SetInt(qad.baseDifficulty)
	adjusted.Mul(adjusted, big.NewFloat(adjustment))

	result, _ := adjusted.Int(nil)
	return result
}
