// Package economics implements the treasury and fund management.
package economics

import (
	"context"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Treasury errors
var (
	ErrInsufficientFunds = errors.New("insufficient treasury funds")
	ErrUnauthorized      = errors.New("unauthorized treasury operation")
	ErrInvalidAllocation = errors.New("invalid allocation")
)

// Treasury manages the DAO treasury
type Treasury struct {
	mu sync.RWMutex

	// Treasury balance
	balance uint64

	// Allocation tracking
	allocations map[types.Hash]*Allocation

	// Historical transactions
	history []*TreasuryTx

	// Storage
	store TreasuryStore
}

// Allocation represents a fund allocation
type Allocation struct {
	AllocationID   types.Hash
	ProposalID     types.Hash // Governance proposal that approved this
	Recipient      types.Address
	Amount         uint64
	Purpose        string
	ApprovedAt     uint64
	ReleasedAt     uint64
	Status         AllocationStatus
}

// AllocationStatus tracks allocation progress
type AllocationStatus uint8

const (
	AllocationPending AllocationStatus = iota
	AllocationApproved
	AllocationReleased
	AllocationCancelled
)

// TreasuryTx represents a treasury transaction
type TreasuryTx struct {
	TxType      TreasuryTxType
	Amount      uint64
	BlockHeight uint64
	Reference   types.Hash // Block hash, proposal ID, etc.
}

// TreasuryTxType defines treasury transaction types
type TreasuryTxType uint8

const (
	TxTypeDeposit TreasuryTxType = iota
	TxTypeAllocation
	TxTypeSlashing
	TxTypeBurn
)

// TreasuryStore defines persistence for treasury
type TreasuryStore interface {
	GetBalance() (uint64, error)
	SetBalance(balance uint64) error
	SaveAllocation(ctx context.Context, alloc *Allocation) error
	GetAllocation(ctx context.Context, id types.Hash) (*Allocation, error)
}

// NewTreasury creates a new treasury
func NewTreasury(store TreasuryStore) *Treasury {
	t := &Treasury{
		allocations: make(map[types.Hash]*Allocation),
		history:     make([]*TreasuryTx, 0),
		store:       store,
	}

	if store != nil {
		if bal, err := store.GetBalance(); err == nil {
			t.balance = bal
		}
	}

	return t
}

// Deposit adds funds to the treasury
func (t *Treasury) Deposit(amount uint64, blockHeight uint64, reference types.Hash) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.balance += amount
	t.history = append(t.history, &TreasuryTx{
		TxType:      TxTypeDeposit,
		Amount:      amount,
		BlockHeight: blockHeight,
		Reference:   reference,
	})

	if t.store != nil {
		return t.store.SetBalance(t.balance)
	}

	return nil
}

// CreateAllocation creates a new allocation (pending governance approval)
func (t *Treasury) CreateAllocation(
	ctx context.Context,
	proposalID types.Hash,
	recipient types.Address,
	amount uint64,
	purpose string,
	blockHeight uint64,
) (*Allocation, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if amount > t.balance {
		return nil, ErrInsufficientFunds
	}

	alloc := &Allocation{
		ProposalID: proposalID,
		Recipient:  recipient,
		Amount:     amount,
		Purpose:    purpose,
		ApprovedAt: blockHeight,
		Status:     AllocationPending,
	}

	// Generate ID
	alloc.AllocationID = generateAllocationID(proposalID, recipient, amount)

	t.allocations[alloc.AllocationID] = alloc

	if t.store != nil {
		if err := t.store.SaveAllocation(ctx, alloc); err != nil {
			return nil, err
		}
	}

	return alloc, nil
}

// ApproveAllocation marks an allocation as approved
func (t *Treasury) ApproveAllocation(ctx context.Context, allocationID types.Hash) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	alloc, exists := t.allocations[allocationID]
	if !exists {
		return ErrInvalidAllocation
	}

	if alloc.Status != AllocationPending {
		return errors.New("allocation not pending")
	}

	alloc.Status = AllocationApproved

	if t.store != nil {
		return t.store.SaveAllocation(ctx, alloc)
	}

	return nil
}

// ReleaseAllocation releases funds for an approved allocation
func (t *Treasury) ReleaseAllocation(ctx context.Context, allocationID types.Hash, blockHeight uint64) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	alloc, exists := t.allocations[allocationID]
	if !exists {
		return 0, ErrInvalidAllocation
	}

	if alloc.Status != AllocationApproved {
		return 0, errors.New("allocation not approved")
	}

	if alloc.Amount > t.balance {
		return 0, ErrInsufficientFunds
	}

	// Deduct from balance
	t.balance -= alloc.Amount
	alloc.ReleasedAt = blockHeight
	alloc.Status = AllocationReleased

	t.history = append(t.history, &TreasuryTx{
		TxType:      TxTypeAllocation,
		Amount:      alloc.Amount,
		BlockHeight: blockHeight,
		Reference:   alloc.AllocationID,
	})

	if t.store != nil {
		if err := t.store.SetBalance(t.balance); err != nil {
			return 0, err
		}
		if err := t.store.SaveAllocation(ctx, alloc); err != nil {
			return 0, err
		}
	}

	return alloc.Amount, nil
}

// ReceiveSlashedFunds receives funds from slashing
func (t *Treasury) ReceiveSlashedFunds(amount uint64, blockHeight uint64, evidenceHash types.Hash) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.balance += amount
	t.history = append(t.history, &TreasuryTx{
		TxType:      TxTypeSlashing,
		Amount:      amount,
		BlockHeight: blockHeight,
		Reference:   evidenceHash,
	})

	if t.store != nil {
		return t.store.SetBalance(t.balance)
	}

	return nil
}

// GetBalance returns the current treasury balance
func (t *Treasury) GetBalance() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.balance
}

// GetAllocation returns an allocation by ID
func (t *Treasury) GetAllocation(allocationID types.Hash) *Allocation {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.allocations[allocationID]
}

// GetPendingAllocations returns all pending allocations
func (t *Treasury) GetPendingAllocations() []*Allocation {
	t.mu.RLock()
	defer t.mu.RUnlock()

	pending := make([]*Allocation, 0)
	for _, a := range t.allocations {
		if a.Status == AllocationPending {
			pending = append(pending, a)
		}
	}
	return pending
}

// generateAllocationID generates a unique allocation ID
func generateAllocationID(proposalID types.Hash, recipient types.Address, amount uint64) types.Hash {
	var id types.Hash
	for i := 0; i < 20; i++ {
		id[i] = proposalID[i] ^ recipient[i%20]
	}
	// Mix in amount
	for i := 0; i < 8; i++ {
		id[20+i] = byte(amount >> (i * 8))
	}
	return id
}

// RewardDistribution defines how block rewards are distributed
type RewardDistribution struct {
	MinerShare     float64
	StakerShare    float64
	TreasuryShare  float64
	ProposerShare  float64
	BurnShare      float64
}

// DefaultRewardDistribution returns the default distribution
// 40% miners, 20% stakers, 25% treasury, 10% proposer, 5% burn
func DefaultRewardDistribution() *RewardDistribution {
	return &RewardDistribution{
		MinerShare:    0.40,
		StakerShare:   0.20,
		TreasuryShare: 0.25,
		ProposerShare: 0.10,
		BurnShare:     0.05,
	}
}

// CalculateDistribution calculates reward amounts for each recipient
func (rd *RewardDistribution) CalculateDistribution(totalReward uint64) (miner, staker, treasury, proposer, burn uint64) {
	miner = uint64(float64(totalReward) * rd.MinerShare)
	staker = uint64(float64(totalReward) * rd.StakerShare)
	treasury = uint64(float64(totalReward) * rd.TreasuryShare)
	proposer = uint64(float64(totalReward) * rd.ProposerShare)
	burn = totalReward - miner - staker - treasury - proposer // Remainder to burn
	return
}
