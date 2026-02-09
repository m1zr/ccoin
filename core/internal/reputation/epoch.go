// Package reputation implements the full reputation system with epochs.
package reputation

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Epoch tracking errors
var (
	ErrEpochNotFound    = errors.New("epoch not found")
	ErrEpochNotFinalized = errors.New("epoch not finalized")
	ErrMinerBanned      = errors.New("miner is banned")
)

// Constants for reputation system
const (
	// Epoch length in blocks
	EpochLength = 1000

	// EWMA decay factor
	EWMAAlpha = 0.1

	// Reputation bounds
	MinReputation = 0.1
	MaxReputation = 3.0
	InitialReputation = 1.0

	// Penalty factors
	InvalidBlockPenalty = 0.2
	DoubleVotePenalty   = 0.5
	OfflinePenalty      = 0.05

	// Slashing parameters
	SlashThreshold    = 0.5  // Reputation below this triggers slashing review
	BanThreshold      = 0.3  // Reputation below this triggers automatic ban
	BanDurationBlocks = 10000
)

// EpochManager manages epoch-based reputation tracking
type EpochManager struct {
	mu sync.RWMutex

	// Current epoch
	currentEpoch uint64

	// Epoch data
	epochs map[uint64]*Epoch

	// Miner data
	miners map[types.Address]*MinerReputation

	// Storage backend
	store ReputationStore

	// Current block height
	currentHeight uint64
}

// Epoch represents a single epoch's statistics
type Epoch struct {
	EpochNumber uint64
	StartBlock  uint64
	EndBlock    uint64
	Finalized   bool

	// Aggregate statistics
	TotalBlocks      uint64
	TotalQuality     float64
	ParticipantCount int

	// Per-miner statistics
	MinerStats map[types.Address]*EpochMinerStats
}

// EpochMinerStats tracks a miner's performance in an epoch
type EpochMinerStats struct {
	BlocksProduced   uint64
	TotalQuality     float64
	AverageQuality   float64
	InvalidAttempts  uint64
	SlashingEvents   uint64
}

// MinerReputation holds the full reputation state for a miner
type MinerReputation struct {
	Address types.Address

	// Current reputation score (EWMA)
	Score float64

	// Historical data
	TotalBlocks       uint64
	TotalQuality      float64
	EpochsActive      uint64
	LastActiveEpoch   uint64
	LastActiveBlock   uint64

	// Slashing state
	SlashingCount     uint64
	TotalSlashed      uint64
	IsBanned          bool
	BanExpiresBlock   uint64

	// Staking data
	StakedAmount      uint64
	LockedUntilBlock  uint64
}

// ReputationStore defines persistence interface
type ReputationStore interface {
	SaveMiner(ctx context.Context, miner *MinerReputation) error
	GetMiner(ctx context.Context, addr types.Address) (*MinerReputation, error)
	ListMiners(ctx context.Context, limit int) ([]*MinerReputation, error)
	SaveEpoch(ctx context.Context, epoch *Epoch) error
	GetEpoch(ctx context.Context, epochNum uint64) (*Epoch, error)
}

// NewEpochManager creates a new epoch manager
func NewEpochManager(store ReputationStore) *EpochManager {
	return &EpochManager{
		epochs: make(map[uint64]*Epoch),
		miners: make(map[types.Address]*MinerReputation),
		store:  store,
	}
}

// ProcessBlock processes a new block for reputation updates
func (em *EpochManager) ProcessBlock(ctx context.Context, block *types.Block) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	header := block.Header
	em.currentHeight = header.Height

	// Check if we need to start a new epoch
	newEpoch := header.Height / EpochLength
	if newEpoch > em.currentEpoch {
		if err := em.finalizeEpoch(ctx, em.currentEpoch); err != nil {
			return err
		}
		em.currentEpoch = newEpoch
		em.startEpoch(newEpoch, header.Height)
	}

	// Get or create miner
	miner := em.getOrCreateMiner(header.MinerAddress)

	// Check if miner is banned
	if miner.IsBanned && header.Height < miner.BanExpiresBlock {
		return ErrMinerBanned
	} else if miner.IsBanned && header.Height >= miner.BanExpiresBlock {
		miner.IsBanned = false
	}

	// Update epoch stats
	epoch := em.epochs[em.currentEpoch]
	if epoch.MinerStats[header.MinerAddress] == nil {
		epoch.MinerStats[header.MinerAddress] = &EpochMinerStats{}
	}
	stats := epoch.MinerStats[header.MinerAddress]
	stats.BlocksProduced++
	stats.TotalQuality += header.QualityScore
	stats.AverageQuality = stats.TotalQuality / float64(stats.BlocksProduced)

	epoch.TotalBlocks++
	epoch.TotalQuality += header.QualityScore

	// Update miner statistics
	miner.TotalBlocks++
	miner.TotalQuality += header.QualityScore
	miner.LastActiveEpoch = em.currentEpoch
	miner.LastActiveBlock = header.Height

	// Update EWMA reputation
	miner.Score = em.calculateEWMA(miner.Score, header.QualityScore)

	// Clamp to bounds
	miner.Score = clamp(miner.Score, MinReputation, MaxReputation)

	return em.store.SaveMiner(ctx, miner)
}

// calculateEWMA computes exponentially weighted moving average
func (em *EpochManager) calculateEWMA(current, newValue float64) float64 {
	return EWMAAlpha*newValue + (1-EWMAAlpha)*current
}

// startEpoch initializes a new epoch
func (em *EpochManager) startEpoch(epochNum, startBlock uint64) {
	em.epochs[epochNum] = &Epoch{
		EpochNumber: epochNum,
		StartBlock:  startBlock,
		EndBlock:    startBlock + EpochLength - 1,
		MinerStats:  make(map[types.Address]*EpochMinerStats),
	}
}

// finalizeEpoch completes an epoch and distributes rewards
func (em *EpochManager) finalizeEpoch(ctx context.Context, epochNum uint64) error {
	epoch, exists := em.epochs[epochNum]
	if !exists {
		return nil // No epoch to finalize
	}

	if epoch.Finalized {
		return nil
	}

	epoch.Finalized = true
	epoch.ParticipantCount = len(epoch.MinerStats)

	// Apply decay to inactive miners
	for addr, miner := range em.miners {
		if miner.LastActiveEpoch < epochNum {
			// Apply inactivity penalty
			inactiveEpochs := epochNum - miner.LastActiveEpoch
			decayFactor := math.Pow(1-OfflinePenalty, float64(inactiveEpochs))
			miner.Score *= decayFactor
			miner.Score = clamp(miner.Score, MinReputation, MaxReputation)

			if err := em.store.SaveMiner(ctx, miner); err != nil {
				return err
			}
		}

		// Check for automatic banning
		if miner.Score < BanThreshold && !miner.IsBanned {
			miner.IsBanned = true
			miner.BanExpiresBlock = em.currentHeight + BanDurationBlocks
			if err := em.store.SaveMiner(ctx, miner); err != nil {
				return err
			}
		}

		_ = addr // unused
	}

	return em.store.SaveEpoch(ctx, epoch)
}

// getOrCreateMiner gets or creates a miner entry
func (em *EpochManager) getOrCreateMiner(addr types.Address) *MinerReputation {
	if miner, exists := em.miners[addr]; exists {
		return miner
	}

	miner := &MinerReputation{
		Address: addr,
		Score:   InitialReputation,
	}
	em.miners[addr] = miner
	return miner
}

// ApplyPenalty applies a penalty to a miner's reputation
func (em *EpochManager) ApplyPenalty(ctx context.Context, addr types.Address, penaltyType PenaltyType) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	miner := em.getOrCreateMiner(addr)

	var penalty float64
	switch penaltyType {
	case PenaltyInvalidBlock:
		penalty = InvalidBlockPenalty
	case PenaltyDoubleVote:
		penalty = DoubleVotePenalty
	case PenaltyOffline:
		penalty = OfflinePenalty
	default:
		penalty = 0.1
	}

	miner.Score *= (1 - penalty)
	miner.Score = clamp(miner.Score, MinReputation, MaxReputation)
	miner.SlashingCount++

	// Update epoch stats
	if epoch, exists := em.epochs[em.currentEpoch]; exists {
		if stats, exists := epoch.MinerStats[addr]; exists {
			stats.SlashingEvents++
		}
	}

	return em.store.SaveMiner(ctx, miner)
}

// PenaltyType defines types of reputation penalties
type PenaltyType uint8

const (
	PenaltyInvalidBlock PenaltyType = iota
	PenaltyDoubleVote
	PenaltyOffline
	PenaltyLowQuality
)

// GetMinerReputation returns a miner's current reputation
func (em *EpochManager) GetMinerReputation(addr types.Address) float64 {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if miner, exists := em.miners[addr]; exists {
		return miner.Score
	}
	return InitialReputation
}

// GetTopMiners returns the top miners by reputation
func (em *EpochManager) GetTopMiners(limit int) []*MinerReputation {
	em.mu.RLock()
	defer em.mu.RUnlock()

	miners := make([]*MinerReputation, 0, len(em.miners))
	for _, m := range em.miners {
		if !m.IsBanned {
			miners = append(miners, m)
		}
	}

	// Sort by score (simple bubble sort for now)
	for i := 0; i < len(miners)-1; i++ {
		for j := i + 1; j < len(miners); j++ {
			if miners[j].Score > miners[i].Score {
				miners[i], miners[j] = miners[j], miners[i]
			}
		}
	}

	if len(miners) > limit {
		miners = miners[:limit]
	}

	return miners
}

// GetEpochStats returns statistics for an epoch
func (em *EpochManager) GetEpochStats(epochNum uint64) (*Epoch, error) {
	em.mu.RLock()
	defer em.mu.RUnlock()

	epoch, exists := em.epochs[epochNum]
	if !exists {
		return nil, ErrEpochNotFound
	}
	return epoch, nil
}

// clamp clamps a value between min and max
func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// CalculateBlockWeight calculates the weight of a block based on miner reputation
// Weight(B) = Work(B) × Rep(miner)
func (em *EpochManager) CalculateBlockWeight(work float64, minerAddr types.Address) float64 {
	return work * em.GetMinerReputation(minerAddr)
}
