// Package tests provides integration tests for governance and DAO.
package tests

import (
	"context"
	"testing"

	"github.com/ccoin/core/internal/governance"
	"github.com/ccoin/core/pkg/types"
)

// Mock governance store
type mockGovernanceStore struct {
	proposals map[types.Hash]*governance.Proposal
	votes     map[types.Hash]map[types.Address]*governance.Vote
}

func newMockGovernanceStore() *mockGovernanceStore {
	return &mockGovernanceStore{
		proposals: make(map[types.Hash]*governance.Proposal),
		votes:     make(map[types.Hash]map[types.Address]*governance.Vote),
	}
}

func (s *mockGovernanceStore) SaveProposal(ctx context.Context, p *governance.Proposal) error {
	s.proposals[p.ProposalID] = p
	return nil
}

func (s *mockGovernanceStore) GetProposal(ctx context.Context, id types.Hash) (*governance.Proposal, error) {
	return s.proposals[id], nil
}

func (s *mockGovernanceStore) SaveVote(ctx context.Context, v *governance.Vote) error {
	if s.votes[v.ProposalID] == nil {
		s.votes[v.ProposalID] = make(map[types.Address]*governance.Vote)
	}
	s.votes[v.ProposalID][v.VoterAddress] = v
	return nil
}

func (s *mockGovernanceStore) GetVote(ctx context.Context, proposalID types.Hash, voter types.Address) (*governance.Vote, error) {
	if s.votes[proposalID] == nil {
		return nil, nil
	}
	return s.votes[proposalID][voter], nil
}

// Test proposal creation
func TestProposalCreation(t *testing.T) {
	ctx := context.Background()
	store := newMockGovernanceStore()
	gm := governance.NewGovernanceManager(store)

	proposer := types.Address{1, 2, 3}
	currentBlock := uint64(1000)

	proposal, err := gm.CreateProposal(
		ctx,
		proposer,
		governance.ProposalTypeTreasury,
		"Test Proposal",
		"This is a test proposal description",
		governance.TreasuryAction{
			Recipient: types.Address{10, 20, 30},
			Amount:    1000000,
		},
		currentBlock,
	)

	if err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	if proposal.ProposalID == (types.Hash{}) {
		t.Error("Proposal ID should not be empty")
	}

	if proposal.Status != governance.StatusPending {
		t.Error("New proposal should be pending")
	}

	if proposal.Proposer != proposer {
		t.Error("Proposer should match")
	}
}

// Test voting
func TestVoting(t *testing.T) {
	ctx := context.Background()
	store := newMockGovernanceStore()
	gm := governance.NewGovernanceManager(store)

	proposer := types.Address{1}
	currentBlock := uint64(1000)

	// Create proposal
	proposal, _ := gm.CreateProposal(
		ctx,
		proposer,
		governance.ProposalTypeParameter,
		"Parameter Change",
		"Change block size",
		nil,
		currentBlock,
	)

	// Vote for
	voter1 := types.Address{10}
	err := gm.CastVote(ctx, proposal.ProposalID, voter1, true, 100, "Support", currentBlock+100)
	if err != nil {
		t.Fatalf("Failed to cast vote: %v", err)
	}

	// Vote against
	voter2 := types.Address{20}
	err = gm.CastVote(ctx, proposal.ProposalID, voter2, false, 50, "Oppose", currentBlock+100)
	if err != nil {
		t.Fatalf("Failed to cast vote: %v", err)
	}

	// Check tallies
	p := gm.GetProposal(proposal.ProposalID)
	if p.VotesFor != 100 {
		t.Errorf("Votes for should be 100, got %d", p.VotesFor)
	}
	if p.VotesAgainst != 50 {
		t.Errorf("Votes against should be 50, got %d", p.VotesAgainst)
	}

	// Try double voting
	err = gm.CastVote(ctx, proposal.ProposalID, voter1, true, 100, "Double", currentBlock+100)
	if err != governance.ErrAlreadyVoted {
		t.Error("Should not allow double voting")
	}
}

// Test quorum
func TestQuorum(t *testing.T) {
	ctx := context.Background()
	store := newMockGovernanceStore()
	gm := governance.NewGovernanceManager(store)

	proposer := types.Address{1}
	currentBlock := uint64(1000)

	proposal, _ := gm.CreateProposal(
		ctx,
		proposer,
		governance.ProposalTypeTreasury,
		"Treasury Allocation",
		"Fund development",
		nil,
		currentBlock,
	)

	// Add some votes (less than quorum)
	voter := types.Address{10}
	_ = gm.CastVote(ctx, proposal.ProposalID, voter, true, 10, "", currentBlock+100)

	// Try to finalize - should fail quorum
	votingEnd := proposal.VotingEndBlock
	passed, err := gm.FinalizeProposal(ctx, proposal.ProposalID, votingEnd+1)
	
	if err != nil {
		t.Fatalf("Finalize error: %v", err)
	}
	if passed {
		t.Error("Proposal should not pass without quorum")
	}
}

// Test proposal execution
func TestProposalExecution(t *testing.T) {
	ctx := context.Background()
	store := newMockGovernanceStore()
	gm := governance.NewGovernanceManager(store)

	proposer := types.Address{1}
	currentBlock := uint64(1000)

	proposal, _ := gm.CreateProposal(
		ctx,
		proposer,
		governance.ProposalTypeParameter,
		"Simple Param",
		"Test",
		nil,
		currentBlock,
	)

	// Add enough votes
	for i := 0; i < 100; i++ {
		voter := types.Address{byte(i + 10)}
		_ = gm.CastVote(ctx, proposal.ProposalID, voter, true, 1000, "", currentBlock+100)
	}

	// Finalize
	p := gm.GetProposal(proposal.ProposalID)
	passed, _ := gm.FinalizeProposal(ctx, proposal.ProposalID, p.VotingEndBlock+1)
	
	if !passed {
		t.Error("Proposal should pass with enough votes")
	}

	// Execute after timelock
	p = gm.GetProposal(proposal.ProposalID)
	if p.Status != governance.StatusQueued {
		t.Error("Passed proposal should be queued")
	}
}
