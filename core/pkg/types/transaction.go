// Package types defines core transaction structures for the CCoin blockchain.
// Transactions use zk-SNARKs for privacy with optional programmable disclosure.
package types

import (
	"crypto/sha256"
	"encoding/binary"
)

// Transaction represents a shielded transaction in the CCoin network.
// It uses zk-SNARKs to hide sender, receiver, and amount while proving validity.
type Transaction struct {
	// TxHash is the unique identifier for this transaction
	TxHash Hash

	// Version is the transaction format version
	Version uint32

	// Nullifiers are unique identifiers derived from spent outputs
	// Used to prevent double-spending without revealing which output is spent
	Nullifiers []Hash

	// Commitments are Pedersen commitments to new outputs
	// Each commitment hides (recipient, value, blinding factor)
	Commitments []Commitment

	// Proof is the zk-SNARK proof attesting to transaction validity
	Proof ZKProof

	// DisclosureFlags indicates which optional disclosure proofs are attached
	DisclosureFlags uint32

	// Disclosures contains optional selective disclosure proofs
	Disclosures []Disclosure

	// Fee is the transaction fee in base units
	Fee uint64

	// Memo is an optional encrypted memo field
	Memo []byte

	// Anchor is the Merkle root of the commitment tree at the time of creation
	Anchor Hash
}

// Commitment represents a Pedersen commitment to a transaction output
type Commitment struct {
	// Value is the commitment value: Commit(value, blinder) = g^value * h^blinder
	Value Hash

	// EncryptedNote contains the encrypted note data for the recipient
	EncryptedNote []byte
}

// ZKProof represents a zk-SNARK proof (Groth16 or PLONK)
type ZKProof struct {
	// ProofType indicates the proof system used (0 = Groth16, 1 = PLONK)
	ProofType uint8

	// ProofData contains the serialized proof
	ProofData []byte

	// PublicInputs contains the public inputs to the circuit
	PublicInputs []Hash
}

// DisclosureType represents the type of selective disclosure
type DisclosureType uint8

const (
	// DisclosureNone indicates no disclosure
	DisclosureNone DisclosureType = 0

	// DisclosureRange proves value is within a range [a, b]
	DisclosureRange DisclosureType = 1

	// DisclosureIdentity proves ownership of a valid credential
	DisclosureIdentity DisclosureType = 2

	// DisclosureSanctions proves non-membership in sanctions list
	DisclosureSanctions DisclosureType = 3

	// DisclosureTemporal proves funds held for minimum duration
	DisclosureTemporal DisclosureType = 4

	// DisclosureAggregate proves properties about a set of transactions
	DisclosureAggregate DisclosureType = 5
)

// Disclosure represents a programmable selective disclosure proof
type Disclosure struct {
	// Type indicates what property is being disclosed
	Type DisclosureType

	// Proof contains the zk-SNARK proof for this disclosure
	Proof ZKProof

	// PublicData contains any public data associated with the disclosure
	// For range: [min, max]
	// For identity: authority public key
	// For sanctions: sanctions list Merkle root
	// For temporal: minimum duration
	PublicData []byte
}

// RangeDisclosureData contains public data for a range disclosure
type RangeDisclosureData struct {
	Min uint64
	Max uint64
}

// TemporalDisclosureData contains public data for a temporal disclosure
type TemporalDisclosureData struct {
	MinDuration uint64 // Minimum seconds the funds must have been held
}

// NewTransaction creates a new transaction
func NewTransaction() *Transaction {
	return &Transaction{
		Version:     1,
		Nullifiers:  make([]Hash, 0),
		Commitments: make([]Commitment, 0),
		Disclosures: make([]Disclosure, 0),
	}
}

// ComputeHash calculates the transaction hash
func (tx *Transaction) ComputeHash() Hash {
	data := tx.serializeForHash()
	return sha256.Sum256(data)
}

// serializeForHash serializes transaction fields for hashing
func (tx *Transaction) serializeForHash() []byte {
	buf := make([]byte, 0, 4096)

	// Version
	versionBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(versionBytes, tx.Version)
	buf = append(buf, versionBytes...)

	// Nullifiers
	for _, nullifier := range tx.Nullifiers {
		buf = append(buf, nullifier[:]...)
	}

	// Commitments
	for _, commitment := range tx.Commitments {
		buf = append(buf, commitment.Value[:]...)
	}

	// Proof
	buf = append(buf, tx.Proof.ProofData...)

	// Fee
	feeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(feeBytes, tx.Fee)
	buf = append(buf, feeBytes...)

	// Anchor
	buf = append(buf, tx.Anchor[:]...)

	return buf
}

// HasDisclosure checks if the transaction has a specific disclosure type
func (tx *Transaction) HasDisclosure(dt DisclosureType) bool {
	for _, d := range tx.Disclosures {
		if d.Type == dt {
			return true
		}
	}
	return false
}

// GetDisclosure returns the disclosure of a specific type, or nil if not present
func (tx *Transaction) GetDisclosure(dt DisclosureType) *Disclosure {
	for i, d := range tx.Disclosures {
		if d.Type == dt {
			return &tx.Disclosures[i]
		}
	}
	return nil
}

// IsShielded returns true if this is a fully shielded transaction
func (tx *Transaction) IsShielded() bool {
	return len(tx.Nullifiers) > 0 && len(tx.Commitments) > 0
}

// TxSize returns the serialized size of the transaction in bytes
func (tx *Transaction) TxSize() int {
	size := 4 // Version
	size += len(tx.Nullifiers) * HashSize
	for _, c := range tx.Commitments {
		size += HashSize + len(c.EncryptedNote)
	}
	size += 1 + len(tx.Proof.ProofData) + len(tx.Proof.PublicInputs)*HashSize
	size += 4 // DisclosureFlags
	for _, d := range tx.Disclosures {
		size += 1 + len(d.Proof.ProofData) + len(d.PublicData)
	}
	size += 8 // Fee
	size += len(tx.Memo)
	size += HashSize // Anchor
	return size
}

// Note represents a decrypted transaction note (internal use)
type Note struct {
	// Value is the amount in base units
	Value uint64

	// RecipientAddress is the recipient's address
	RecipientAddress Address

	// Blinder is the random blinding factor
	Blinder Hash

	// Memo is the decrypted memo
	Memo []byte
}

// NullifierDerivation contains parameters for deriving a nullifier
type NullifierDerivation struct {
	// SpendingKey is the private spending key
	SpendingKey Hash

	// NoteCommitment is the commitment to the note being spent
	NoteCommitment Hash

	// Position is the position in the commitment tree
	Position uint64
}

// DeriveNullifier computes the nullifier for a note
func (nd *NullifierDerivation) DeriveNullifier() Hash {
	// nullifier = H(spending_key || note_commitment || position)
	buf := make([]byte, 0, HashSize*2+8)
	buf = append(buf, nd.SpendingKey[:]...)
	buf = append(buf, nd.NoteCommitment[:]...)
	posBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(posBytes, nd.Position)
	buf = append(buf, posBytes...)
	return sha256.Sum256(buf)
}
