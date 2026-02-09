// Package types defines miner and reputation structures for CCoin.
package types

import (
	"math"
)

// Reputation constants as defined in the whitepaper
const (
	// DecayFactor (λ) for EWMA calculation
	DecayFactor = 0.9

	// MinReputation is the minimum allowed reputation
	MinReputation = 0.1

	// MaxReputation is the maximum allowed reputation
	MaxReputation = 3.0

	// InitialReputation is the starting reputation for new miners
	InitialReputation = 1.0

	// MinQualityScore is the minimum acceptable quality score
	MinQualityScore = 0.001
)

// Miner represents a miner in the CCoin network
type Miner struct {
	// Address is the miner's public key hash
	Address Address

	// ReputationScore is the current reputation (bounded [0.1, 3.0])
	ReputationScore float64

	// TotalBlocks is the total number of blocks mined
	TotalBlocks uint64

	// TotalQualityScore is the sum of all quality scores
	TotalQualityScore float64

	// EpochBlocks is the number of blocks mined in the current epoch
	EpochBlocks uint64

	// EpochQualitySum is the sum of quality scores in the current epoch
	EpochQualitySum float64

	// LastActiveEpoch is the last epoch where this miner was active
	LastActiveEpoch uint64

	// TotalRewards is the total rewards earned (in base units)
	TotalRewards uint64

	// StakedAmount is the amount staked for validation/inference
	StakedAmount uint64

	// IsBanned indicates if the miner is temporarily banned
	IsBanned bool

	// BanExpiresAt is the block height when the ban expires
	BanExpiresAt uint64
}

// NewMiner creates a new miner with initial reputation
func NewMiner(address Address) *Miner {
	return &Miner{
		Address:         address,
		ReputationScore: InitialReputation,
	}
}

// UpdateReputation updates the miner's reputation using EWMA
// Rep_t = λ * Rep_{t-1} + (1 - λ) * Q̄_epoch
func (m *Miner) UpdateReputation(epochQualityAverage float64) {
	newRep := DecayFactor*m.ReputationScore + (1-DecayFactor)*epochQualityAverage

	// Apply bounds
	m.ReputationScore = math.Max(MinReputation, math.Min(MaxReputation, newRep))
}

// RecordBlock records a new block mined by this miner
func (m *Miner) RecordBlock(qualityScore float64, epoch uint64) {
	m.TotalBlocks++
	m.TotalQualityScore += qualityScore

	// Reset epoch stats if new epoch
	if epoch != m.LastActiveEpoch {
		m.EpochBlocks = 0
		m.EpochQualitySum = 0
		m.LastActiveEpoch = epoch
	}

	m.EpochBlocks++
	m.EpochQualitySum += qualityScore
}

// EpochQualityAverage returns the average quality score for the current epoch
func (m *Miner) EpochQualityAverage() float64 {
	if m.EpochBlocks == 0 {
		return InitialReputation
	}
	return m.EpochQualitySum / float64(m.EpochBlocks)
}

// CalculateBlockReward calculates the reward for a block based on reputation
// R = R_base * (0.5 + 0.5 * Rep)
func (m *Miner) CalculateBlockReward(baseReward uint64) uint64 {
	multiplier := 0.5 + 0.5*m.ReputationScore
	return uint64(float64(baseReward) * multiplier)
}

// ApplyPenalty applies a reputation penalty
func (m *Miner) ApplyPenalty(severity PenaltySeverity) {
	switch severity {
	case PenaltyMinor:
		// Reduce reputation by 10%
		m.ReputationScore *= 0.9
	case PenaltyMajor:
		// Reduce reputation by 50%
		m.ReputationScore *= 0.5
	case PenaltySevere:
		// Slash to minimum
		m.ReputationScore = MinReputation
	}

	// Apply lower bound
	if m.ReputationScore < MinReputation {
		m.ReputationScore = MinReputation
	}
}

// Ban temporarily bans the miner
func (m *Miner) Ban(currentBlock, duration uint64) {
	m.IsBanned = true
	m.BanExpiresAt = currentBlock + duration
	m.ReputationScore = MinReputation
}

// CheckBan checks and clears expired bans
func (m *Miner) CheckBan(currentBlock uint64) bool {
	if m.IsBanned && currentBlock >= m.BanExpiresAt {
		m.IsBanned = false
	}
	return m.IsBanned
}

// PenaltySeverity represents the severity of a reputation penalty
type PenaltySeverity uint8

const (
	// PenaltyMinor is for low quality gradients
	PenaltyMinor PenaltySeverity = 0

	// PenaltyMajor is for checkpoint validation failures
	PenaltyMajor PenaltySeverity = 1

	// PenaltySevere is for invalid proof submission
	PenaltySevere PenaltySeverity = 2
)

// InferenceNode represents a node that serves AI inference requests
type InferenceNode struct {
	// Address is the node operator's address
	Address Address

	// StakedAmount is the minimum stake (1000 CCoin)
	StakedAmount uint64

	// HostedModels is the list of model IDs this node serves
	HostedModels []Hash

	// Uptime is the uptime percentage (0.0 - 1.0)
	Uptime float64

	// TotalQueries is the total inference queries served
	TotalQueries uint64

	// CorrectResults is the number of correct results
	CorrectResults uint64

	// LastActiveBlock is the last block where this node was active
	LastActiveBlock uint64

	// IsActive indicates if the node is currently active
	IsActive bool
}

// MinInferenceStake is the minimum stake to become an inference node
const MinInferenceStake = 1000_000_000_000 // 1000 CCoin

// NewInferenceNode creates a new inference node
func NewInferenceNode(address Address, stake uint64) *InferenceNode {
	return &InferenceNode{
		Address:      address,
		StakedAmount: stake,
		HostedModels: make([]Hash, 0),
		IsActive:     stake >= MinInferenceStake,
	}
}

// CanServe checks if the node can serve inference requests
func (n *InferenceNode) CanServe() bool {
	return n.IsActive && n.StakedAmount >= MinInferenceStake && len(n.HostedModels) > 0
}

// Accuracy returns the node's accuracy rate
func (n *InferenceNode) Accuracy() float64 {
	if n.TotalQueries == 0 {
		return 1.0
	}
	return float64(n.CorrectResults) / float64(n.TotalQueries)
}

// SelectionWeight returns the weight for node selection in oracle requests
func (n *InferenceNode) SelectionWeight() float64 {
	// Weight = stake * uptime * accuracy
	return float64(n.StakedAmount) * n.Uptime * n.Accuracy()
}
