// Package economics implements transaction fee handling.
package economics

import (
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Fee constants
const (
	// MinFeePerByte is the minimum fee per byte
	MinFeePerByte uint64 = 1

	// BaseFee is the base transaction fee
	BaseFee uint64 = 1000 // 0.00001 CCoin

	// PriorityFeeMultiplier for high-priority transactions
	PriorityFeeMultiplier = 2.0

	// MaxFeeMultiplier caps fee increases during congestion
	MaxFeeMultiplier = 10.0

	// TargetBlockGas is the target gas per block
	TargetBlockGas uint64 = 1_000_000

	// MaxBlockGas is the maximum gas per block
	MaxBlockGas uint64 = 2_000_000

	// FeeUpdateDenominator controls fee adjustment speed
	FeeUpdateDenominator = 8
)

// Fee errors
var (
	ErrFeeTooLow       = errors.New("fee too low")
	ErrGasLimitExceeded = errors.New("gas limit exceeded")
)

// FeeMarket implements EIP-1559 style fee market
type FeeMarket struct {
	mu sync.RWMutex

	// Current base fee per gas
	baseFeePerGas uint64

	// Recent block gas usage for fee adjustment
	recentGasUsed []uint64

	// Window size for calculating average
	windowSize int
}

// FeeConfig holds fee market configuration
type FeeConfig struct {
	InitialBaseFee uint64
	WindowSize     int
}

// DefaultFeeConfig returns default configuration
func DefaultFeeConfig() *FeeConfig {
	return &FeeConfig{
		InitialBaseFee: BaseFee,
		WindowSize:     10,
	}
}

// NewFeeMarket creates a new fee market
func NewFeeMarket(config *FeeConfig) *FeeMarket {
	if config == nil {
		config = DefaultFeeConfig()
	}

	return &FeeMarket{
		baseFeePerGas: config.InitialBaseFee,
		recentGasUsed: make([]uint64, 0, config.WindowSize),
		windowSize:    config.WindowSize,
	}
}

// GetBaseFee returns the current base fee
func (fm *FeeMarket) GetBaseFee() uint64 {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.baseFeePerGas
}

// UpdateBaseFee updates the base fee based on block gas usage (EIP-1559 style)
func (fm *FeeMarket) UpdateBaseFee(blockGasUsed uint64) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Track recent gas usage
	fm.recentGasUsed = append(fm.recentGasUsed, blockGasUsed)
	if len(fm.recentGasUsed) > fm.windowSize {
		fm.recentGasUsed = fm.recentGasUsed[1:]
	}

	// Calculate new base fee
	if blockGasUsed > TargetBlockGas {
		// Increase fee when above target
		gasUsedDelta := blockGasUsed - TargetBlockGas
		feeDelta := fm.baseFeePerGas * gasUsedDelta / TargetBlockGas / FeeUpdateDenominator
		if feeDelta < 1 {
			feeDelta = 1
		}
		fm.baseFeePerGas += feeDelta
	} else if blockGasUsed < TargetBlockGas {
		// Decrease fee when below target
		gasUsedDelta := TargetBlockGas - blockGasUsed
		feeDelta := fm.baseFeePerGas * gasUsedDelta / TargetBlockGas / FeeUpdateDenominator
		if feeDelta > fm.baseFeePerGas-MinFeePerByte {
			feeDelta = fm.baseFeePerGas - MinFeePerByte
		}
		fm.baseFeePerGas -= feeDelta
	}

	// Enforce minimum
	if fm.baseFeePerGas < MinFeePerByte {
		fm.baseFeePerGas = MinFeePerByte
	}

	// Enforce maximum (prevent runaway fees)
	maxFee := BaseFee * uint64(MaxFeeMultiplier)
	if fm.baseFeePerGas > maxFee {
		fm.baseFeePerGas = maxFee
	}
}

// CalculateFee calculates the total fee for a transaction
func (fm *FeeMarket) CalculateFee(gasLimit uint64, priorityFee uint64) uint64 {
	fm.mu.RLock()
	baseFee := fm.baseFeePerGas
	fm.mu.RUnlock()

	return (baseFee + priorityFee) * gasLimit
}

// ValidateFee checks if a transaction fee is sufficient
func (fm *FeeMarket) ValidateFee(tx *types.Transaction, gasUsed uint64) error {
	requiredFee := fm.CalculateFee(gasUsed, 0)
	if tx.Fee < requiredFee {
		return ErrFeeTooLow
	}
	return nil
}

// EstimateGas estimates gas for a transaction
func EstimateGas(tx *types.Transaction) uint64 {
	// Base cost
	gas := uint64(21000)

	// Add cost per nullifier
	gas += uint64(len(tx.Nullifiers)) * 1000

	// Add cost per commitment
	gas += uint64(len(tx.Commitments)) * 1000

	// Add cost for proof verification
	gas += uint64(len(tx.Proof.ProofData)) * 10

	// Add cost for disclosures
	gas += uint64(len(tx.Disclosures)) * 5000

	// Add cost for memo
	gas += uint64(len(tx.Memo)) * 10

	return gas
}

// FeeDistribution defines how fees are distributed
type FeeDistribution struct {
	MinerShare    float64 // Share to block producer
	BurnShare     float64 // Share to burn
	TreasuryShare float64 // Share to DAO treasury
}

// DefaultFeeDistribution returns the default fee distribution
func DefaultFeeDistribution() *FeeDistribution {
	return &FeeDistribution{
		MinerShare:    0.50, // 50% to miner
		BurnShare:     0.30, // 30% burned
		TreasuryShare: 0.20, // 20% to treasury
	}
}

// DistributeFees distributes collected fees according to the distribution
func DistributeFees(totalFees uint64, dist *FeeDistribution) (miner, burn, treasury uint64) {
	miner = uint64(float64(totalFees) * dist.MinerShare)
	burn = uint64(float64(totalFees) * dist.BurnShare)
	treasury = totalFees - miner - burn // Remainder to treasury
	return
}

// GetAverageGasUsed returns the average gas used over the window
func (fm *FeeMarket) GetAverageGasUsed() uint64 {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	if len(fm.recentGasUsed) == 0 {
		return 0
	}

	var total uint64
	for _, gas := range fm.recentGasUsed {
		total += gas
	}

	return total / uint64(len(fm.recentGasUsed))
}

// GetCongestionLevel returns 0-100 representing network congestion
func (fm *FeeMarket) GetCongestionLevel() int {
	avgGas := fm.GetAverageGasUsed()
	if avgGas >= MaxBlockGas {
		return 100
	}
	return int(float64(avgGas) / float64(MaxBlockGas) * 100)
}

// PriorityCalculator calculates transaction priority for ordering
type PriorityCalculator struct {
	baseFee uint64
}

// NewPriorityCalculator creates a new priority calculator
func NewPriorityCalculator(baseFee uint64) *PriorityCalculator {
	return &PriorityCalculator{baseFee: baseFee}
}

// CalculatePriority calculates transaction priority
// Priority = effectiveFee / gasUsed
func (pc *PriorityCalculator) CalculatePriority(tx *types.Transaction, gasUsed uint64) float64 {
	if gasUsed == 0 {
		return 0
	}
	return float64(tx.Fee) / float64(gasUsed)
}

// SortByPriority sorts transactions by priority (highest first)
func (pc *PriorityCalculator) SortByPriority(txs []*types.Transaction) []*types.Transaction {
	// Simple bubble sort for now
	n := len(txs)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			p1 := pc.CalculatePriority(txs[j], EstimateGas(txs[j]))
			p2 := pc.CalculatePriority(txs[j+1], EstimateGas(txs[j+1]))
			if p1 < p2 {
				txs[j], txs[j+1] = txs[j+1], txs[j]
			}
		}
	}
	return txs
}
