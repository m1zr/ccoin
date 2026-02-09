// Package reputation implements the miner reputation system.
package reputation

import (
	"context"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Manager handles miner reputation tracking and updates
type Manager struct {
	mu sync.RWMutex

	// store is the persistent storage backend
	store Store

	// cache maintains in-memory miner data
	cache map[types.Address]*types.Miner

	// currentEpoch tracks the current epoch
	currentEpoch uint64
}

// Store defines the interface for reputation storage
type Store interface {
	GetMiner(ctx context.Context, address types.Address) (*types.Miner, error)
	SaveMiner(ctx context.Context, miner *types.Miner) error
	GetActiveMiners(ctx context.Context, epoch uint64) ([]*types.Miner, error)
	GetAllMiners(ctx context.Context) ([]*types.Miner, error)
}

// NewManager creates a new reputation manager
func NewManager(store Store) *Manager {
	return &Manager{
		store: store,
		cache: make(map[types.Address]*types.Miner),
	}
}

// GetMiner retrieves a miner, returning a new one if not found
func (m *Manager) GetMiner(ctx context.Context, address types.Address) (*types.Miner, error) {
	m.mu.RLock()
	if miner, exists := m.cache[address]; exists {
		m.mu.RUnlock()
		return miner, nil
	}
	m.mu.RUnlock()

	// Try to load from storage
	miner, err := m.store.GetMiner(ctx, address)
	if err != nil {
		// Create new miner with initial reputation
		miner = types.NewMiner(address)
	}

	m.mu.Lock()
	m.cache[address] = miner
	m.mu.Unlock()

	return miner, nil
}

// SaveMiner persists a miner's data
func (m *Manager) SaveMiner(ctx context.Context, miner *types.Miner) error {
	m.mu.Lock()
	m.cache[miner.Address] = miner
	m.mu.Unlock()

	return m.store.SaveMiner(ctx, miner)
}

// RecordBlock records a new block for a miner
func (m *Manager) RecordBlock(ctx context.Context, address types.Address, qualityScore float64, epoch uint64) error {
	miner, err := m.GetMiner(ctx, address)
	if err != nil {
		return err
	}

	miner.RecordBlock(qualityScore, epoch)

	return m.SaveMiner(ctx, miner)
}

// UpdateReputations updates all miner reputations at epoch transition
// Rep_t = λ * Rep_{t-1} + (1 - λ) * Q̄_epoch
func (m *Manager) UpdateReputations(ctx context.Context, epoch uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update all cached miners
	for _, miner := range m.cache {
		if miner.LastActiveEpoch == epoch-1 {
			// Active in previous epoch - update reputation
			miner.UpdateReputation(miner.EpochQualityAverage())
		} else if epoch-miner.LastActiveEpoch > 10 {
			// Inactive for too long - decay toward initial
			m.decayReputation(miner)
		}

		// Reset epoch counters
		miner.EpochBlocks = 0
		miner.EpochQualitySum = 0

		// Persist
		if err := m.store.SaveMiner(ctx, miner); err != nil {
			return err
		}
	}

	// Also update miners not in cache
	miners, err := m.store.GetActiveMiners(ctx, epoch-1)
	if err != nil {
		return err
	}

	for _, miner := range miners {
		if _, exists := m.cache[miner.Address]; exists {
			continue // Already updated
		}

		miner.UpdateReputation(miner.EpochQualityAverage())
		miner.EpochBlocks = 0
		miner.EpochQualitySum = 0

		if err := m.store.SaveMiner(ctx, miner); err != nil {
			return err
		}
	}

	m.currentEpoch = epoch
	return nil
}

// decayReputation gradually decays an inactive miner's reputation toward initial
func (m *Manager) decayReputation(miner *types.Miner) {
	// Decay 5% toward initial per epoch of inactivity
	diff := miner.ReputationScore - types.InitialReputation
	miner.ReputationScore = miner.ReputationScore - 0.05*diff

	// Clamp to bounds
	if miner.ReputationScore < types.MinReputation {
		miner.ReputationScore = types.MinReputation
	}
	if miner.ReputationScore > types.MaxReputation {
		miner.ReputationScore = types.MaxReputation
	}
}

// ApplyPenalty applies a reputation penalty to a miner
func (m *Manager) ApplyPenalty(ctx context.Context, address types.Address, severity types.PenaltySeverity) error {
	miner, err := m.GetMiner(ctx, address)
	if err != nil {
		return err
	}

	miner.ApplyPenalty(severity)

	return m.SaveMiner(ctx, miner)
}

// BanMiner temporarily bans a miner
func (m *Manager) BanMiner(ctx context.Context, address types.Address, currentBlock, duration uint64) error {
	miner, err := m.GetMiner(ctx, address)
	if err != nil {
		return err
	}

	miner.Ban(currentBlock, duration)

	return m.SaveMiner(ctx, miner)
}

// IsBanned checks if a miner is currently banned
func (m *Manager) IsBanned(ctx context.Context, address types.Address, currentBlock uint64) (bool, error) {
	miner, err := m.GetMiner(ctx, address)
	if err != nil {
		return false, err
	}

	return miner.CheckBan(currentBlock), nil
}

// GetTopMiners returns miners sorted by reputation
func (m *Manager) GetTopMiners(ctx context.Context, limit int) ([]*types.Miner, error) {
	miners, err := m.store.GetAllMiners(ctx)
	if err != nil {
		return nil, err
	}

	// Sort by reputation descending
	for i := 0; i < len(miners)-1; i++ {
		for j := i + 1; j < len(miners); j++ {
			if miners[j].ReputationScore > miners[i].ReputationScore {
				miners[i], miners[j] = miners[j], miners[i]
			}
		}
	}

	if limit > 0 && len(miners) > limit {
		miners = miners[:limit]
	}

	return miners, nil
}

// CalculateInfluence calculates a miner's influence in consensus
// Higher reputation = higher influence in DAG ordering
func (m *Manager) CalculateInfluence(reputation float64) float64 {
	// Reputation directly affects block weight in DAG
	return reputation
}

// CalculateRewardMultiplier calculates the block reward multiplier
// R = R_base × (0.5 + 0.5 × Rep)
func (m *Manager) CalculateRewardMultiplier(reputation float64) float64 {
	return 0.5 + 0.5*reputation
}

// CalculateTaskPriority calculates priority for task queue access
// Higher reputation = access to higher-value tasks
func (m *Manager) CalculateTaskPriority(reputation float64) float64 {
	return reputation
}

// Stats returns aggregate statistics about the miner network
type Stats struct {
	TotalMiners     int
	ActiveMiners    int
	AverageRep      float64
	HighestRep      float64
	TotalBlocks     uint64
	TotalQuality    float64
}

// GetStats returns network-wide miner statistics
func (m *Manager) GetStats(ctx context.Context, epoch uint64) (*Stats, error) {
	miners, err := m.store.GetAllMiners(ctx)
	if err != nil {
		return nil, err
	}

	stats := &Stats{}
	stats.TotalMiners = len(miners)

	var totalRep float64
	for _, miner := range miners {
		totalRep += miner.ReputationScore
		stats.TotalBlocks += miner.TotalBlocks
		stats.TotalQuality += miner.TotalQualityScore

		if miner.ReputationScore > stats.HighestRep {
			stats.HighestRep = miner.ReputationScore
		}

		if miner.LastActiveEpoch >= epoch-1 {
			stats.ActiveMiners++
		}
	}

	if stats.TotalMiners > 0 {
		stats.AverageRep = totalRep / float64(stats.TotalMiners)
	}

	return stats, nil
}
