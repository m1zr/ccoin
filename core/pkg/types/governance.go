// Package types defines governance structures for the CCoin Research DAO.
package types

// ProposalType represents the type of governance proposal
type ProposalType uint8

const (
	// ProposalNewModel proposes training a new AI model
	ProposalNewModel ProposalType = 0

	// ProposalTaskPriority proposes changing task queue priorities
	ProposalTaskPriority ProposalType = 1

	// ProposalParameterAdjust proposes adjusting model training parameters
	ProposalParameterAdjust ProposalType = 2

	// ProposalLicenseChange proposes changing a model's license
	ProposalLicenseChange ProposalType = 3

	// ProposalTreasurySpend proposes spending from the treasury
	ProposalTreasurySpend ProposalType = 4

	// ProposalProtocolUpgrade proposes a protocol upgrade
	ProposalProtocolUpgrade ProposalType = 5
)

// ProposalStatus represents the status of a proposal
type ProposalStatus uint8

const (
	// ProposalStatusActive indicates voting is in progress
	ProposalStatusActive ProposalStatus = 0

	// ProposalStatusPassed indicates the proposal passed
	ProposalStatusPassed ProposalStatus = 1

	// ProposalStatusRejected indicates the proposal was rejected
	ProposalStatusRejected ProposalStatus = 2

	// ProposalStatusExecuted indicates the proposal was executed
	ProposalStatusExecuted ProposalStatus = 3

	// ProposalStatusCancelled indicates the proposal was cancelled
	ProposalStatusCancelled ProposalStatus = 4
)

// ProposalThresholds defines voting requirements for each proposal type
var ProposalThresholds = map[ProposalType]struct {
	Quorum            float64 // Percentage of tokens that must vote
	ApprovalThreshold float64 // Percentage of votes that must approve
	VotingPeriod      uint64  // Duration in blocks
}{
	ProposalNewModel:        {Quorum: 0.10, ApprovalThreshold: 0.50, VotingPeriod: 50400},  // ~7 days
	ProposalTaskPriority:    {Quorum: 0.05, ApprovalThreshold: 0.50, VotingPeriod: 21600},  // ~3 days
	ProposalParameterAdjust: {Quorum: 0.05, ApprovalThreshold: 0.50, VotingPeriod: 21600},  // ~3 days
	ProposalLicenseChange:   {Quorum: 0.15, ApprovalThreshold: 0.66, VotingPeriod: 100800}, // ~14 days
	ProposalTreasurySpend:   {Quorum: 0.10, ApprovalThreshold: 0.50, VotingPeriod: 50400},  // ~7 days
	ProposalProtocolUpgrade: {Quorum: 0.25, ApprovalThreshold: 0.75, VotingPeriod: 201600}, // ~28 days
}

// Proposal represents a governance proposal in the Research DAO
type Proposal struct {
	// ProposalID is the unique identifier
	ProposalID Hash

	// Type is the proposal type
	Type ProposalType

	// ProposerAddress is the address that submitted the proposal
	ProposerAddress Address

	// Title is a short description
	Title string

	// Description is the full proposal text
	Description string

	// Data contains type-specific proposal data
	Data ProposalData

	// VotesFor is the total voting power in favor
	VotesFor uint64

	// VotesAgainst is the total voting power against
	VotesAgainst uint64

	// QuorumRequired is the minimum participation required
	QuorumRequired float64

	// ApprovalThreshold is the minimum approval percentage
	ApprovalThreshold float64

	// VotingStartBlock is when voting begins
	VotingStartBlock uint64

	// VotingEndBlock is when voting ends
	VotingEndBlock uint64

	// Status is the current proposal status
	Status ProposalStatus

	// ExecutedAt is the block when the proposal was executed (if applicable)
	ExecutedAt uint64
}

// ProposalData is an interface for type-specific proposal data
type ProposalData interface {
	ProposalType() ProposalType
	Validate() error
}

// NewModelProposalData contains data for a new model proposal
type NewModelProposalData struct {
	Architecture    string
	TaskType        TaskType
	Domain          string
	DataSourceURL   string
	TargetAccuracy  float64
	ComputeBudget   uint64 // Maximum GPU-hours
	ValidationSetID Hash
}

func (d *NewModelProposalData) ProposalType() ProposalType { return ProposalNewModel }
func (d *NewModelProposalData) Validate() error           { return nil }

// TreasurySpendData contains data for a treasury spend proposal
type TreasurySpendData struct {
	Recipient   Address
	Amount      uint64
	Purpose     string
	MilestoneID Hash // Optional link to a milestone
}

func (d *TreasurySpendData) ProposalType() ProposalType { return ProposalTreasurySpend }
func (d *TreasurySpendData) Validate() error           { return nil }

// Vote represents a single vote on a proposal
type Vote struct {
	// ProposalID is the proposal being voted on
	ProposalID Hash

	// VoterAddress is the address casting the vote
	VoterAddress Address

	// VotePower is the calculated voting power
	VotePower uint64

	// InFavor is true if voting in favor, false if against
	InFavor bool

	// VotedAt is the block height when the vote was cast
	VotedAt uint64
}

// CalculateVotePower computes voting power using quadratic voting with reputation
// VotePower = sqrt(Staked) * (1 + 0.5 * Rep)
func CalculateVotePower(staked uint64, reputation float64) uint64 {
	// sqrt(staked) in fixed point (multiply by 1000 for precision)
	sqrtStaked := isqrt(staked * 1000000)
	
	// Reputation multiplier: 1 + 0.5 * Rep (scaled by 1000)
	repMultiplier := uint64(1000 + 500*reputation)
	
	// Final vote power
	return (sqrtStaked * repMultiplier) / 1000000
}

// isqrt computes integer square root
func isqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// HasReachedQuorum checks if a proposal has met its quorum requirement
func (p *Proposal) HasReachedQuorum(totalSupply uint64) bool {
	totalVotes := p.VotesFor + p.VotesAgainst
	participation := float64(totalVotes) / float64(totalSupply)
	return participation >= p.QuorumRequired
}

// HasPassed checks if a proposal has passed
func (p *Proposal) HasPassed(totalSupply uint64) bool {
	if !p.HasReachedQuorum(totalSupply) {
		return false
	}
	totalVotes := p.VotesFor + p.VotesAgainst
	if totalVotes == 0 {
		return false
	}
	approval := float64(p.VotesFor) / float64(totalVotes)
	return approval >= p.ApprovalThreshold
}

// NewProposal creates a new proposal with default thresholds
func NewProposal(proposalType ProposalType, proposer Address) *Proposal {
	thresholds := ProposalThresholds[proposalType]
	return &Proposal{
		Type:              proposalType,
		ProposerAddress:   proposer,
		QuorumRequired:    thresholds.Quorum,
		ApprovalThreshold: thresholds.ApprovalThreshold,
		Status:            ProposalStatusActive,
	}
}
