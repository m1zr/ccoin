// Package zkp implements programmable disclosure for selective transparency.
package zkp

import (
	"context"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Disclosure errors
var (
	ErrDisclosureTypeInvalid     = errors.New("invalid disclosure type")
	ErrDisclosureProofInvalid    = errors.New("disclosure proof is invalid")
	ErrDisclosureRequirementFailed = errors.New("disclosure requirement not met")
)

// DisclosureType defines the type of selective disclosure
type DisclosureType uint8

const (
	// DisclosureTypeNone - no disclosure
	DisclosureTypeNone DisclosureType = iota

	// DisclosureTypeRange - prove value is in range [min, max]
	DisclosureTypeRange

	// DisclosureTypeIdentity - prove ownership of credential from authority
	DisclosureTypeIdentity

	// DisclosureTypeTemporal - prove funds held for minimum duration
	DisclosureTypeTemporal

	// DisclosureTypeSanctions - prove not on sanctions list
	DisclosureTypeSanctions

	// DisclosureTypeThreshold - prove value above/below threshold
	DisclosureTypeThreshold

	// DisclosureTypeSource - prove source of funds (for compliance)
	DisclosureTypeSource
)

// DisclosureFlags are bitmask flags for disclosure requirements
type DisclosureFlags uint32

const (
	FlagNone          DisclosureFlags = 0
	FlagRangeRequired DisclosureFlags = 1 << 0
	FlagIdentityRequired DisclosureFlags = 1 << 1
	FlagTemporalRequired DisclosureFlags = 1 << 2
	FlagSanctionsRequired DisclosureFlags = 1 << 3
	FlagThresholdRequired DisclosureFlags = 1 << 4
	FlagSourceRequired DisclosureFlags = 1 << 5
)

// Disclosure represents a programmable disclosure proof
type Disclosure struct {
	Type       DisclosureType
	Proof      []byte
	PublicData []byte
	Metadata   map[string]string
}

// RangeDisclosure proves value is in range without revealing exact value
type RangeDisclosure struct {
	Commitment types.Hash
	MinValue   uint64
	MaxValue   uint64
	Proof      []byte
}

// IdentityDisclosure proves identity ownership from authority
type IdentityDisclosure struct {
	AuthorityPubKey      types.Hash
	CredentialCommitment types.Hash
	Proof                []byte
}

// TemporalDisclosure proves funds held for minimum duration
type TemporalDisclosure struct {
	Commitment   types.Hash
	MinDuration  uint64 // seconds
	ProofTime    uint64 // timestamp when proof was created
	Proof        []byte
}

// SanctionsDisclosure proves address is not on sanctions list
type SanctionsDisclosure struct {
	SanctionsListRoot types.Hash // Merkle root of sanctions list
	ProofOfNonMembership []byte
}

// DisclosureManager handles creation and verification of disclosures
type DisclosureManager struct {
	mu sync.RWMutex

	// Circuit manager for proof generation/verification
	circuits *CircuitManager

	// Known authorities for identity disclosures
	authorities map[types.Hash]Authority

	// Sanctions list root
	sanctionsRoot types.Hash
}

// Authority represents a credential issuer
type Authority struct {
	PublicKey types.Hash
	Name      string
	Domain    string
}

// NewDisclosureManager creates a new disclosure manager
func NewDisclosureManager(circuits *CircuitManager) *DisclosureManager {
	return &DisclosureManager{
		circuits:    circuits,
		authorities: make(map[types.Hash]Authority),
	}
}

// CreateRangeDisclosure creates a range disclosure proof
func (dm *DisclosureManager) CreateRangeDisclosure(
	ctx context.Context,
	value uint64,
	blinder []byte,
	commitment types.Hash,
	minValue, maxValue uint64,
) (*RangeDisclosure, error) {
	// Verify the value is actually in range
	if value < minValue || value > maxValue {
		return nil, ErrDisclosureRequirementFailed
	}

	// Create the circuit witness
	circuit := &RangeDisclosureCircuit{
		Commitment: commitment,
		MinValue:   minValue,
		MaxValue:   maxValue,
		Value:      value,
		// Blinder would be set from bytes
	}

	// Generate proof
	proofData, err := dm.circuits.GenerateProof(ctx, ProofTypeRangeDisclosure, circuit)
	if err != nil {
		return nil, err
	}

	return &RangeDisclosure{
		Commitment: commitment,
		MinValue:   minValue,
		MaxValue:   maxValue,
		Proof:      proofData.Proof,
	}, nil
}

// VerifyRangeDisclosure verifies a range disclosure proof
func (dm *DisclosureManager) VerifyRangeDisclosure(
	ctx context.Context,
	disclosure *RangeDisclosure,
) (bool, error) {
	proofData := &ProofData{
		ProofType: ProofTypeRangeDisclosure,
		Proof:     disclosure.Proof,
	}

	return dm.circuits.VerifyProof(ctx, proofData)
}

// CreateTemporalDisclosure creates a temporal disclosure proof
func (dm *DisclosureManager) CreateTemporalDisclosure(
	ctx context.Context,
	value uint64,
	blinder []byte,
	commitment types.Hash,
	creationTime uint64,
	currentTime uint64,
	minDuration uint64,
) (*TemporalDisclosure, error) {
	// Verify the duration requirement
	if currentTime-creationTime < minDuration {
		return nil, ErrDisclosureRequirementFailed
	}

	// Create circuit witness
	circuit := &TemporalDisclosureCircuit{
		CurrentTime:  currentTime,
		MinDuration:  minDuration,
		Commitment:   commitment,
		CreationTime: creationTime,
		Value:        value,
	}

	// Generate proof
	proofData, err := dm.circuits.GenerateProof(ctx, ProofTypeTemporalDisclosure, circuit)
	if err != nil {
		return nil, err
	}

	return &TemporalDisclosure{
		Commitment:  commitment,
		MinDuration: minDuration,
		ProofTime:   currentTime,
		Proof:       proofData.Proof,
	}, nil
}

// CreateIdentityDisclosure creates an identity disclosure proof
func (dm *DisclosureManager) CreateIdentityDisclosure(
	ctx context.Context,
	privateKey []byte,
	credential []byte,
	authorityPubKey types.Hash,
) (*IdentityDisclosure, error) {
	// Verify authority is known
	dm.mu.RLock()
	_, known := dm.authorities[authorityPubKey]
	dm.mu.RUnlock()

	if !known {
		return nil, errors.New("unknown authority")
	}

	// Create circuit witness
	circuit := &IdentityDisclosureCircuit{
		AuthorityPubKey: authorityPubKey,
		// Fill in other fields
	}

	// Generate proof
	proofData, err := dm.circuits.GenerateProof(ctx, ProofTypeIdentityDisclosure, circuit)
	if err != nil {
		return nil, err
	}

	// Create credential commitment
	credCommitment := hashBytes(credential)

	return &IdentityDisclosure{
		AuthorityPubKey:      authorityPubKey,
		CredentialCommitment: credCommitment,
		Proof:                proofData.Proof,
	}, nil
}

// RegisterAuthority registers a trusted credential authority
func (dm *DisclosureManager) RegisterAuthority(authority Authority) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.authorities[authority.PublicKey] = authority
}

// ValidateDisclosures validates all disclosures on a transaction
func (dm *DisclosureManager) ValidateDisclosures(
	ctx context.Context,
	tx *types.Transaction,
	requiredFlags DisclosureFlags,
) error {
	// Check if required disclosures are present
	providedFlags := DisclosureFlags(tx.DisclosureFlags)

	if requiredFlags&FlagRangeRequired != 0 && providedFlags&FlagRangeRequired == 0 {
		return errors.New("range disclosure required but not provided")
	}

	if requiredFlags&FlagIdentityRequired != 0 && providedFlags&FlagIdentityRequired == 0 {
		return errors.New("identity disclosure required but not provided")
	}

	if requiredFlags&FlagTemporalRequired != 0 && providedFlags&FlagTemporalRequired == 0 {
		return errors.New("temporal disclosure required but not provided")
	}

	if requiredFlags&FlagSanctionsRequired != 0 && providedFlags&FlagSanctionsRequired == 0 {
		return errors.New("sanctions compliance disclosure required but not provided")
	}

	// Verify each provided disclosure
	for _, disclosure := range tx.Disclosures {
		if err := dm.verifyDisclosure(ctx, &disclosure); err != nil {
			return err
		}
	}

	return nil
}

// verifyDisclosure verifies a single disclosure
func (dm *DisclosureManager) verifyDisclosure(ctx context.Context, disclosure *types.Disclosure) error {
	switch disclosure.Type {
	case uint8(DisclosureTypeRange):
		// Parse and verify range disclosure
		rangeDisc := &RangeDisclosure{
			Proof: disclosure.Proof,
		}
		valid, err := dm.VerifyRangeDisclosure(ctx, rangeDisc)
		if err != nil {
			return err
		}
		if !valid {
			return ErrDisclosureProofInvalid
		}

	case uint8(DisclosureTypeIdentity):
		// Verify identity disclosure
		// (implementation similar to range)

	case uint8(DisclosureTypeTemporal):
		// Verify temporal disclosure

	case uint8(DisclosureTypeSanctions):
		// Verify sanctions compliance

	default:
		return ErrDisclosureTypeInvalid
	}

	return nil
}

// hashBytes is a helper to hash arbitrary bytes
func hashBytes(data []byte) types.Hash {
	var hash types.Hash
	// Simple hash - use proper crypto hash in production
	for i := 0; i < len(hash) && i < len(data); i++ {
		hash[i] = data[i]
	}
	return hash
}

// DisclosureRequirement defines what disclosures are required for a transaction
type DisclosureRequirement struct {
	Flags        DisclosureFlags
	RangeMin     uint64
	RangeMax     uint64
	MinHoldTime  uint64
	AuthorityID  types.Hash
}

// DefaultComplianceRequirement returns standard compliance disclosure requirements
func DefaultComplianceRequirement() *DisclosureRequirement {
	return &DisclosureRequirement{
		Flags:       FlagSanctionsRequired,
		RangeMin:    0,
		RangeMax:    0,
		MinHoldTime: 0,
	}
}

// HighValueRequirement returns requirements for high-value transactions
func HighValueRequirement(threshold uint64) *DisclosureRequirement {
	return &DisclosureRequirement{
		Flags:    FlagRangeRequired | FlagIdentityRequired | FlagSanctionsRequired,
		RangeMin: threshold,
		RangeMax: ^uint64(0), // Max uint64
	}
}
