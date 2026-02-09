// Package reputation implements slashing and staking for the reputation system.
package reputation

import (
	"context"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Slashing errors
var (
	ErrInsufficientStake    = errors.New("insufficient stake")
	ErrStakeLocked          = errors.New("stake is locked")
	ErrSlashingLimitReached = errors.New("slashing limit reached")
	ErrNotSlashable         = errors.New("violation not slashable")
)

// SlashingType defines types of slashable offenses
type SlashingType uint8

const (
	SlashTypeInvalidBlock SlashingType = iota
	SlashTypeDoubleSign
	SlashTypeEquivocation
	SlashTypeCensorship
	SlashTypeFraudulentPoUW
)

// SlashingConfig holds slashing parameters
type SlashingConfig struct {
	// Percentage of stake to slash for each offense type
	SlashRates map[SlashingType]float64

	// Minimum stake required to mine
	MinimumStake uint64

	// Lock period for stake (in blocks)
	StakeLockPeriod uint64

	// Maximum cumulative slashing before forced exit
	MaxCumulativeSlash float64
}

// DefaultSlashingConfig returns default slashing configuration
func DefaultSlashingConfig() *SlashingConfig {
	return &SlashingConfig{
		SlashRates: map[SlashingType]float64{
			SlashTypeInvalidBlock:   0.05, // 5%
			SlashTypeDoubleSign:     0.20, // 20%
			SlashTypeEquivocation:   0.10, // 10%
			SlashTypeCensorship:     0.05, // 5%
			SlashTypeFraudulentPoUW: 0.50, // 50%
		},
		MinimumStake:       1000000, // 1M CCoin minimum
		StakeLockPeriod:    10000,   // ~1 day at 10s blocks
		MaxCumulativeSlash: 0.75,    // Force exit at 75% total slashed
	}
}

// SlashingManager handles stake slashing
type SlashingManager struct {
	mu sync.RWMutex

	config *SlashingConfig

	// Staking state per miner
	stakes map[types.Address]*StakeInfo

	// Slashing evidence
	evidence map[types.Hash]*SlashingEvidence

	// Storage
	store SlashingStore
}

// StakeInfo holds a miner's stake information
type StakeInfo struct {
	Address          types.Address
	TotalStaked      uint64
	AvailableStake   uint64
	LockedStake      uint64
	LockedUntilBlock uint64
	TotalSlashed     uint64
	SlashingRatio    float64 // Cumulative slashing percentage
	BondedAt         uint64
}

// SlashingEvidence represents evidence of a slashable offense
type SlashingEvidence struct {
	EvidenceHash types.Hash
	Type         SlashingType
	MinerAddress types.Address
	BlockHeight  uint64
	Description  string
	ProofData    []byte
	Processed    bool
	SlashAmount  uint64
}

// SlashingStore defines persistence for slashing
type SlashingStore interface {
	SaveStake(ctx context.Context, stake *StakeInfo) error
	GetStake(ctx context.Context, addr types.Address) (*StakeInfo, error)
	SaveEvidence(ctx context.Context, evidence *SlashingEvidence) error
	GetPendingEvidence(ctx context.Context) ([]*SlashingEvidence, error)
}

// NewSlashingManager creates a new slashing manager
func NewSlashingManager(store SlashingStore, config *SlashingConfig) *SlashingManager {
	if config == nil {
		config = DefaultSlashingConfig()
	}

	return &SlashingManager{
		config:   config,
		stakes:   make(map[types.Address]*StakeInfo),
		evidence: make(map[types.Hash]*SlashingEvidence),
		store:    store,
	}
}

// Stake adds stake for a miner
func (sm *SlashingManager) Stake(ctx context.Context, addr types.Address, amount uint64, currentBlock uint64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stake := sm.getOrCreateStake(addr)
	stake.TotalStaked += amount
	stake.AvailableStake += amount
	stake.LockedUntilBlock = currentBlock + sm.config.StakeLockPeriod

	if stake.BondedAt == 0 {
		stake.BondedAt = currentBlock
	}

	return sm.store.SaveStake(ctx, stake)
}

// Unstake removes stake for a miner (after lock period)
func (sm *SlashingManager) Unstake(ctx context.Context, addr types.Address, amount uint64, currentBlock uint64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stake, exists := sm.stakes[addr]
	if !exists {
		return ErrInsufficientStake
	}

	if currentBlock < stake.LockedUntilBlock {
		return ErrStakeLocked
	}

	if amount > stake.AvailableStake {
		return ErrInsufficientStake
	}

	stake.TotalStaked -= amount
	stake.AvailableStake -= amount

	return sm.store.SaveStake(ctx, stake)
}

// SubmitEvidence submits slashing evidence
func (sm *SlashingManager) SubmitEvidence(ctx context.Context, evidence *SlashingEvidence) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if evidence already exists
	if _, exists := sm.evidence[evidence.EvidenceHash]; exists {
		return nil // Already submitted
	}

	sm.evidence[evidence.EvidenceHash] = evidence
	return sm.store.SaveEvidence(ctx, evidence)
}

// ProcessSlashing processes a slashing event
func (sm *SlashingManager) ProcessSlashing(ctx context.Context, evidenceHash types.Hash) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	evidence, exists := sm.evidence[evidenceHash]
	if !exists {
		return errors.New("evidence not found")
	}

	if evidence.Processed {
		return nil // Already processed
	}

	stake := sm.getOrCreateStake(evidence.MinerAddress)

	// Calculate slash amount
	slashRate, exists := sm.config.SlashRates[evidence.Type]
	if !exists {
		return ErrNotSlashable
	}

	slashAmount := uint64(float64(stake.TotalStaked) * slashRate)
	if slashAmount > stake.AvailableStake {
		slashAmount = stake.AvailableStake
	}

	// Apply slashing
	stake.AvailableStake -= slashAmount
	stake.TotalSlashed += slashAmount
	stake.SlashingRatio = float64(stake.TotalSlashed) / float64(stake.TotalStaked+stake.TotalSlashed)

	evidence.SlashAmount = slashAmount
	evidence.Processed = true

	// Check if forced exit
	if stake.SlashingRatio >= sm.config.MaxCumulativeSlash {
		// Force exit - miner loses all remaining stake
		stake.TotalSlashed += stake.AvailableStake
		stake.AvailableStake = 0
	}

	if err := sm.store.SaveStake(ctx, stake); err != nil {
		return err
	}

	return sm.store.SaveEvidence(ctx, evidence)
}

// getOrCreateStake gets or creates stake info
func (sm *SlashingManager) getOrCreateStake(addr types.Address) *StakeInfo {
	if stake, exists := sm.stakes[addr]; exists {
		return stake
	}

	stake := &StakeInfo{Address: addr}
	sm.stakes[addr] = stake
	return stake
}

// IsEligibleToMine checks if a miner has sufficient stake to mine
func (sm *SlashingManager) IsEligibleToMine(addr types.Address) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stake, exists := sm.stakes[addr]
	if !exists {
		return false
	}

	return stake.AvailableStake >= sm.config.MinimumStake
}

// GetStakeInfo returns stake information for a miner
func (sm *SlashingManager) GetStakeInfo(addr types.Address) *StakeInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if stake, exists := sm.stakes[addr]; exists {
		return stake
	}
	return nil
}

// GetTotalStaked returns the total staked amount across all miners
func (sm *SlashingManager) GetTotalStaked() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var total uint64
	for _, stake := range sm.stakes {
		total += stake.AvailableStake
	}
	return total
}

// DistributeSlashedFunds distributes slashed funds to reporters/DAO
func (sm *SlashingManager) DistributeSlashedFunds(ctx context.Context, evidence *SlashingEvidence, reporterAddress types.Address) (uint64, error) {
	if !evidence.Processed || evidence.SlashAmount == 0 {
		return 0, nil
	}

	// 50% to reporter, 50% to DAO treasury
	reporterShare := evidence.SlashAmount / 2
	// daoShare := evidence.SlashAmount - reporterShare

	// In production, these would be actual token transfers
	return reporterShare, nil
}
