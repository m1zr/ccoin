// Package zkp implements shielded transaction processing.
package zkp

import (
	"context"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Transaction processing errors
var (
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrInvalidNote         = errors.New("invalid note")
	ErrNoteAlreadySpent    = errors.New("note already spent")
	ErrInvalidAnchor       = errors.New("invalid merkle anchor")
	ErrProofFailed         = errors.New("transaction proof verification failed")
)

// Note represents a spendable output in the shielded pool
type Note struct {
	// Value of the note
	Value uint64

	// Address this note belongs to
	Address types.Address

	// Blinding factor for the commitment
	Blinder []byte

	// Commitment = H(value || address || blinder)
	Commitment types.Hash

	// Position in the commitment tree
	Position uint64

	// MerklePath to the root
	MerklePath *MerklePath

	// Has this note been spent?
	Spent bool

	// Block height when created
	CreatedAt uint64
}

// TransactionBuilder builds shielded transactions
type TransactionBuilder struct {
	// Input notes to spend
	inputs []*NoteInput

	// Output notes to create
	outputs []*NoteOutput

	// Fee to pay
	fee uint64

	// Memo field
	memo []byte

	// Disclosures
	disclosures []types.Disclosure

	// Circuit manager for proof generation
	circuits *CircuitManager
}

// NoteInput represents an input note to spend
type NoteInput struct {
	Note       *Note
	SpendingKey []byte
	MerklePath *MerklePath
}

// NoteOutput represents an output note to create
type NoteOutput struct {
	Value   uint64
	Address types.Address
	Memo    []byte
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(circuits *CircuitManager) *TransactionBuilder {
	return &TransactionBuilder{
		inputs:      make([]*NoteInput, 0),
		outputs:     make([]*NoteOutput, 0),
		disclosures: make([]types.Disclosure, 0),
		circuits:    circuits,
	}
}

// AddInput adds an input note to spend
func (tb *TransactionBuilder) AddInput(note *Note, spendingKey []byte, path *MerklePath) error {
	if note == nil {
		return ErrInvalidNote
	}
	if note.Spent {
		return ErrNoteAlreadySpent
	}

	tb.inputs = append(tb.inputs, &NoteInput{
		Note:        note,
		SpendingKey: spendingKey,
		MerklePath:  path,
	})

	return nil
}

// AddOutput adds an output note to create
func (tb *TransactionBuilder) AddOutput(value uint64, address types.Address, memo []byte) {
	tb.outputs = append(tb.outputs, &NoteOutput{
		Value:   value,
		Address: address,
		Memo:    memo,
	})
}

// SetFee sets the transaction fee
func (tb *TransactionBuilder) SetFee(fee uint64) {
	tb.fee = fee
}

// SetMemo sets the transaction memo
func (tb *TransactionBuilder) SetMemo(memo []byte) {
	tb.memo = memo
}

// AddDisclosure adds a programmable disclosure
func (tb *TransactionBuilder) AddDisclosure(disclosure types.Disclosure) {
	tb.disclosures = append(tb.disclosures, disclosure)
}

// Build creates the shielded transaction
func (tb *TransactionBuilder) Build(ctx context.Context, anchor types.Hash) (*types.Transaction, error) {
	// Verify value conservation
	var inputSum, outputSum uint64
	for _, input := range tb.inputs {
		inputSum += input.Note.Value
	}
	for _, output := range tb.outputs {
		outputSum += output.Value
	}

	if inputSum != outputSum+tb.fee {
		return nil, ErrInsufficientFunds
	}

	// Generate nullifiers
	nullifiers := make([]types.Hash, len(tb.inputs))
	for i, input := range tb.inputs {
		nullifiers[i] = DeriveNullifier(
			input.SpendingKey,
			input.Note.Commitment,
			input.Note.Position,
		)
	}

	// Generate output commitments
	commitments := make([]types.Commitment, len(tb.outputs))
	blinders := make([][]byte, len(tb.outputs))

	for i, output := range tb.outputs {
		blinder, err := RandomBytes(32)
		if err != nil {
			return nil, err
		}
		blinders[i] = blinder

		commitment := computeNoteCommitment(output.Value, output.Address, blinder)
		commitments[i] = types.Commitment{Value: commitment}
	}

	// Generate zk-SNARK proof
	proof, err := tb.generateProof(ctx, anchor, nullifiers, commitments)
	if err != nil {
		return nil, err
	}

	// Build transaction
	tx := &types.Transaction{
		Version:        1,
		Nullifiers:     nullifiers,
		Commitments:    commitments,
		Proof:          proof,
		Anchor:         anchor,
		Fee:            tb.fee,
		Memo:           tb.memo,
		DisclosureFlags: tb.computeDisclosureFlags(),
		Disclosures:    tb.disclosures,
	}

	// Compute transaction hash
	tx.TxHash = tx.ComputeHash()

	return tx, nil
}

// generateProof generates the zk-SNARK proof
func (tb *TransactionBuilder) generateProof(
	ctx context.Context,
	anchor types.Hash,
	nullifiers []types.Hash,
	commitments []types.Commitment,
) (types.ZKProof, error) {
	// Build circuit witness
	values := make([]uint64, len(tb.inputs)+len(tb.outputs))
	for i, input := range tb.inputs {
		values[i] = input.Note.Value
	}
	for i, output := range tb.outputs {
		values[len(tb.inputs)+i] = output.Value
	}

	// In production, this would create a proper gnark witness
	// and generate a real Groth16 proof

	// For now, return a simulated proof
	proofData := make([]byte, 192) // Groth16 proof size on BN254
	copy(proofData, "SIMULATED_PROOF")

	return types.ZKProof{
		ProofType: 1, // Groth16
		ProofData: proofData,
	}, nil
}

// computeDisclosureFlags computes the disclosure flags from disclosures
func (tb *TransactionBuilder) computeDisclosureFlags() uint32 {
	var flags uint32
	for _, d := range tb.disclosures {
		switch d.Type {
		case uint8(DisclosureTypeRange):
			flags |= uint32(FlagRangeRequired)
		case uint8(DisclosureTypeIdentity):
			flags |= uint32(FlagIdentityRequired)
		case uint8(DisclosureTypeTemporal):
			flags |= uint32(FlagTemporalRequired)
		case uint8(DisclosureTypeSanctions):
			flags |= uint32(FlagSanctionsRequired)
		}
	}
	return flags
}

// computeNoteCommitment computes the commitment for a note
func computeNoteCommitment(value uint64, address types.Address, blinder []byte) types.Hash {
	data := make([]byte, 0, 60)
	data = append(data, uint64ToBytes(value)...)
	data = append(data, address[:]...)
	data = append(data, blinder...)

	var hash types.Hash
	// Simple hash - use proper crypto in production
	for i := 0; i < types.HashSize && i < len(data); i++ {
		hash[i] = data[i] ^ byte(i*31)
	}
	return hash
}

// ShieldedPool manages the shielded transaction pool
type ShieldedPool struct {
	mu sync.RWMutex

	// Commitment tree
	commitmentTree *CommitmentTree

	// Nullifier set
	nullifierSet *NullifierSet

	// Circuit manager
	circuits *CircuitManager

	// Disclosure manager
	disclosures *DisclosureManager
}

// NewShieldedPool creates a new shielded pool
func NewShieldedPool(
	tree *CommitmentTree,
	nullifiers *NullifierSet,
	circuits *CircuitManager,
	disclosures *DisclosureManager,
) *ShieldedPool {
	return &ShieldedPool{
		commitmentTree: tree,
		nullifierSet:   nullifiers,
		circuits:       circuits,
		disclosures:    disclosures,
	}
}

// ProcessTransaction validates and processes a shielded transaction
func (sp *ShieldedPool) ProcessTransaction(ctx context.Context, tx *types.Transaction, blockHeight uint64) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Verify anchor is valid
	currentRoot := sp.commitmentTree.GetRoot()
	if tx.Anchor != currentRoot {
		// In production, we'd check if anchor is a recent root
		// For now, just check current
		return ErrInvalidAnchor
	}

	// Verify nullifiers are not spent
	for _, nullifier := range tx.Nullifiers {
		spent, err := sp.nullifierSet.IsSpent(ctx, nullifier)
		if err != nil {
			return err
		}
		if spent {
			return ErrNullifierSpent
		}
	}

	// Verify zk-SNARK proof
	// (In production, this would verify the actual proof)
	if len(tx.Proof.ProofData) < 10 {
		return ErrProofFailed
	}

	// Verify disclosures if required
	// (This would check against policy)

	// Mark nullifiers as spent
	for _, nullifier := range tx.Nullifiers {
		if err := sp.nullifierSet.MarkSpent(ctx, nullifier, tx.TxHash, blockHeight); err != nil {
			return err
		}
	}

	// Add commitments to tree
	for _, commitment := range tx.Commitments {
		if _, err := sp.commitmentTree.AddCommitment(ctx, commitment.Value); err != nil {
			return err
		}
	}

	return nil
}

// GetCurrentAnchor returns the current commitment tree root
func (sp *ShieldedPool) GetCurrentAnchor() types.Hash {
	return sp.commitmentTree.GetRoot()
}

// GetMerklePath returns the Merkle path for a commitment
func (sp *ShieldedPool) GetMerklePath(ctx context.Context, position uint64) (*MerklePath, error) {
	return sp.commitmentTree.GetPath(ctx, position)
}
