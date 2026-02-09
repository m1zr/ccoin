// Package governance implements the Research DAO governance system.
package governance

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Governance errors
var (
	ErrProposalNotFound    = errors.New("proposal not found")
	ErrProposalClosed      = errors.New("proposal voting closed")
	ErrAlreadyVoted        = errors.New("already voted on this proposal")
	ErrInsufficientVotePower = errors.New("insufficient vote power")
	ErrQuorumNotMet        = errors.New("quorum not met")
	ErrThresholdNotMet     = errors.New("approval threshold not met")
)

// GovernanceManager manages the Research DAO
type GovernanceManager struct {
	mu sync.RWMutex

	// Active proposals
	proposals map[types.Hash]*types.Proposal

	// Vote records
	votes map[types.Hash]map[types.Address]*Vote

	// Proposal queue for execution
	executionQueue []*types.Proposal

	// Configuration
	config *GovernanceConfig

	// Storage
	store GovernanceStore
}

// Vote represents a vote on a proposal
type Vote struct {
	VoterAddress types.Address
	ProposalID   types.Hash
	Support      bool
	VotePower    uint64
	Reason       string
	CastAt       uint64
}

// GovernanceConfig holds governance parameters
type GovernanceConfig struct {
	// Minimum stake required to create a proposal
	MinProposalStake uint64

	// Voting period in blocks
	VotingPeriod uint64

	// Execution delay after approval (timelock)
	ExecutionDelay uint64

	// Default quorum (percentage of total stake)
	DefaultQuorum float64

	// Default approval threshold
	DefaultThreshold float64

	// Proposal type specific thresholds
	Thresholds map[types.ProposalType]ThresholdConfig
}

// ThresholdConfig holds threshold settings for a proposal type
type ThresholdConfig struct {
	QuorumRequired    float64
	ApprovalThreshold float64
}

// DefaultGovernanceConfig returns default configuration
func DefaultGovernanceConfig() *GovernanceConfig {
	return &GovernanceConfig{
		MinProposalStake: 10000,
		VotingPeriod:     10000, // ~1.15 days at 10s blocks
		ExecutionDelay:   1000,  // ~2.7 hours
		DefaultQuorum:    0.2,   // 20%
		DefaultThreshold: 0.5,   // 50%
		Thresholds: map[types.ProposalType]ThresholdConfig{
			types.ProposalTypeModelArchitecture: {
				QuorumRequired:    0.3,
				ApprovalThreshold: 0.6,
			},
			types.ProposalTypeParameterChange: {
				QuorumRequired:    0.2,
				ApprovalThreshold: 0.5,
			},
			types.ProposalTypeFundingAllocation: {
				QuorumRequired:    0.25,
				ApprovalThreshold: 0.6,
			},
			types.ProposalTypeEmergencyAction: {
				QuorumRequired:    0.4,
				ApprovalThreshold: 0.75,
			},
		},
	}
}

// GovernanceStore defines persistence for governance
type GovernanceStore interface {
	SaveProposal(ctx context.Context, proposal *types.Proposal) error
	GetProposal(ctx context.Context, id types.Hash) (*types.Proposal, error)
	SaveVote(ctx context.Context, vote *Vote) error
	GetVotes(ctx context.Context, proposalID types.Hash) ([]*Vote, error)
}

// NewGovernanceManager creates a new governance manager
func NewGovernanceManager(store GovernanceStore, config *GovernanceConfig) *GovernanceManager {
	if config == nil {
		config = DefaultGovernanceConfig()
	}

	return &GovernanceManager{
		proposals:      make(map[types.Hash]*types.Proposal),
		votes:          make(map[types.Hash]map[types.Address]*Vote),
		executionQueue: make([]*types.Proposal, 0),
		config:         config,
		store:          store,
	}
}

// CreateProposal creates a new governance proposal
func (gm *GovernanceManager) CreateProposal(
	ctx context.Context,
	proposalType types.ProposalType,
	proposer types.Address,
	title, description string,
	data types.ProposalData,
	currentBlock uint64,
) (*types.Proposal, error) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	// Get thresholds for proposal type
	threshold, exists := gm.config.Thresholds[proposalType]
	if !exists {
		threshold = ThresholdConfig{
			QuorumRequired:    gm.config.DefaultQuorum,
			ApprovalThreshold: gm.config.DefaultThreshold,
		}
	}

	proposal := &types.Proposal{
		Type:              proposalType,
		ProposerAddress:   proposer,
		Title:             title,
		Description:       description,
		Data:              data,
		VotesFor:          0,
		VotesAgainst:      0,
		QuorumRequired:    threshold.QuorumRequired,
		ApprovalThreshold: threshold.ApprovalThreshold,
		VotingStartBlock:  currentBlock,
		VotingEndBlock:    currentBlock + gm.config.VotingPeriod,
		Status:            types.ProposalStatusActive,
	}

	// Generate proposal ID
	proposal.ProposalID = gm.generateProposalID(proposal)

	gm.proposals[proposal.ProposalID] = proposal
	gm.votes[proposal.ProposalID] = make(map[types.Address]*Vote)

	return proposal, gm.store.SaveProposal(ctx, proposal)
}

// generateProposalID generates a unique proposal ID
func (gm *GovernanceManager) generateProposalID(proposal *types.Proposal) types.Hash {
	data := append(proposal.ProposerAddress[:], []byte(proposal.Title)...)
	data = append(data, uint64ToBytes(proposal.VotingStartBlock)...)
	data = append(data, byte(proposal.Type))

	hash := sha256.Sum256(data)
	var id types.Hash
	copy(id[:], hash[:])
	return id
}

// CastVote casts a vote on a proposal
func (gm *GovernanceManager) CastVote(
	ctx context.Context,
	proposalID types.Hash,
	voter types.Address,
	support bool,
	votePower uint64,
	reason string,
	currentBlock uint64,
) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	proposal, exists := gm.proposals[proposalID]
	if !exists {
		return ErrProposalNotFound
	}

	// Check voting period
	if currentBlock > proposal.VotingEndBlock {
		return ErrProposalClosed
	}

	// Check if already voted
	if _, voted := gm.votes[proposalID][voter]; voted {
		return ErrAlreadyVoted
	}

	// Record vote
	vote := &Vote{
		VoterAddress: voter,
		ProposalID:   proposalID,
		Support:      support,
		VotePower:    votePower,
		Reason:       reason,
		CastAt:       currentBlock,
	}

	gm.votes[proposalID][voter] = vote

	// Update proposal tallies
	if support {
		proposal.VotesFor += votePower
	} else {
		proposal.VotesAgainst += votePower
	}

	if err := gm.store.SaveVote(ctx, vote); err != nil {
		return err
	}

	return gm.store.SaveProposal(ctx, proposal)
}

// FinalizeProposal finalizes voting on a proposal
func (gm *GovernanceManager) FinalizeProposal(
	ctx context.Context,
	proposalID types.Hash,
	totalStake uint64,
	currentBlock uint64,
) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	proposal, exists := gm.proposals[proposalID]
	if !exists {
		return ErrProposalNotFound
	}

	// Must be past voting period
	if currentBlock <= proposal.VotingEndBlock {
		return errors.New("voting period not ended")
	}

	if proposal.Status != types.ProposalStatusActive {
		return errors.New("proposal already finalized")
	}

	// Calculate results
	totalVotes := proposal.VotesFor + proposal.VotesAgainst
	quorumReached := float64(totalVotes) >= float64(totalStake)*proposal.QuorumRequired

	if !quorumReached {
		proposal.Status = types.ProposalStatusRejected
		return gm.store.SaveProposal(ctx, proposal)
	}

	// Check approval threshold
	approvalRatio := float64(proposal.VotesFor) / float64(totalVotes)
	if approvalRatio >= proposal.ApprovalThreshold {
		proposal.Status = types.ProposalStatusPassed
		// Add to execution queue
		gm.executionQueue = append(gm.executionQueue, proposal)
	} else {
		proposal.Status = types.ProposalStatusRejected
	}

	return gm.store.SaveProposal(ctx, proposal)
}

// ExecuteProposal executes an approved proposal
func (gm *GovernanceManager) ExecuteProposal(
	ctx context.Context,
	proposalID types.Hash,
	currentBlock uint64,
) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	proposal, exists := gm.proposals[proposalID]
	if !exists {
		return ErrProposalNotFound
	}

	if proposal.Status != types.ProposalStatusPassed {
		return errors.New("proposal not approved")
	}

	// Check timelock
	timelockEnd := proposal.VotingEndBlock + gm.config.ExecutionDelay
	if currentBlock < timelockEnd {
		return errors.New("timelock not expired")
	}

	// Execute based on proposal type
	if err := gm.executeProposalAction(ctx, proposal); err != nil {
		return err
	}

	proposal.Status = types.ProposalStatusExecuted
	proposal.ExecutedAt = currentBlock

	// Remove from queue
	for i, p := range gm.executionQueue {
		if p.ProposalID == proposalID {
			gm.executionQueue = append(gm.executionQueue[:i], gm.executionQueue[i+1:]...)
			break
		}
	}

	return gm.store.SaveProposal(ctx, proposal)
}

// executeProposalAction executes the action for a proposal
func (gm *GovernanceManager) executeProposalAction(ctx context.Context, proposal *types.Proposal) error {
	switch proposal.Type {
	case types.ProposalTypeModelArchitecture:
		// Execute model architecture change
		// This would call into the model registry
		return nil

	case types.ProposalTypeParameterChange:
		// Execute parameter change
		return nil

	case types.ProposalTypeFundingAllocation:
		// Execute funding allocation
		return nil

	case types.ProposalTypeEmergencyAction:
		// Execute emergency action
		return nil

	default:
		return errors.New("unknown proposal type")
	}
}

// GetProposal returns a proposal by ID
func (gm *GovernanceManager) GetProposal(proposalID types.Hash) *types.Proposal {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.proposals[proposalID]
}

// GetActiveProposals returns all active proposals
func (gm *GovernanceManager) GetActiveProposals() []*types.Proposal {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	active := make([]*types.Proposal, 0)
	for _, p := range gm.proposals {
		if p.Status == types.ProposalStatusActive {
			active = append(active, p)
		}
	}
	return active
}

// GetVoteCount returns the vote count for a proposal
func (gm *GovernanceManager) GetVoteCount(proposalID types.Hash) (uint64, uint64) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	proposal, exists := gm.proposals[proposalID]
	if !exists {
		return 0, 0
	}
	return proposal.VotesFor, proposal.VotesAgainst
}

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}
