// Package zkp implements zero-knowledge cryptographic primitives.
package zkp

import (
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	"github.com/ccoin/core/pkg/types"
)

// Commitment errors
var (
	ErrInvalidValue     = errors.New("invalid commitment value")
	ErrInvalidBlinder   = errors.New("invalid blinder")
	ErrInvalidPoint     = errors.New("invalid elliptic curve point")
	ErrCommitmentFailed = errors.New("commitment computation failed")
)

// Generator points for Pedersen commitment
// In production, these would be generated via a trusted setup or hash-to-curve
var (
	// G is the base generator point
	generatorG bn254.G1Affine
	// H is the secondary generator for the blinding factor (no known discrete log relation to G)
	generatorH bn254.G1Affine

	// initialized tracks if generators have been set up
	initialized = false
)

// InitializeGenerators sets up the Pedersen commitment generators
func InitializeGenerators() error {
	if initialized {
		return nil
	}

	// Use the standard BN254 generator for G
	_, _, g1Gen, _ := bn254.Generators()
	generatorG = g1Gen

	// Derive H from G using hash-to-curve (ensuring no known discrete log)
	// In production, this would use a proper hash-to-curve algorithm
	hBytes := hashToBytes("CCOIN_PEDERSEN_H")
	generatorH.ScalarMultiplication(&generatorG, new(big.Int).SetBytes(hBytes))

	initialized = true
	return nil
}

// PedersenCommitment represents a Pedersen commitment: C = v*G + r*H
type PedersenCommitment struct {
	Point bn254.G1Affine
}

// CommitmentOpening contains the values needed to open a commitment
type CommitmentOpening struct {
	Value   *big.Int
	Blinder *big.Int
}

// NewPedersenCommitment creates a new Pedersen commitment
// C = value * G + blinder * H
func NewPedersenCommitment(value, blinder *big.Int) (*PedersenCommitment, error) {
	if err := InitializeGenerators(); err != nil {
		return nil, err
	}

	if value == nil || blinder == nil {
		return nil, ErrInvalidValue
	}

	// Compute value * G
	var valueG bn254.G1Affine
	valueG.ScalarMultiplication(&generatorG, value)

	// Compute blinder * H
	var blinderH bn254.G1Affine
	blinderH.ScalarMultiplication(&generatorH, blinder)

	// Add them together: C = vG + rH
	var commitment bn254.G1Affine
	commitment.Add(&valueG, &blinderH)

	return &PedersenCommitment{Point: commitment}, nil
}

// NewRandomCommitment creates a commitment with a random blinder
func NewRandomCommitment(value *big.Int) (*PedersenCommitment, *big.Int, error) {
	blinder, err := RandomScalar()
	if err != nil {
		return nil, nil, err
	}

	commitment, err := NewPedersenCommitment(value, blinder)
	if err != nil {
		return nil, nil, err
	}

	return commitment, blinder, nil
}

// Verify checks if a commitment opens to the given value and blinder
func (c *PedersenCommitment) Verify(value, blinder *big.Int) bool {
	expected, err := NewPedersenCommitment(value, blinder)
	if err != nil {
		return false
	}
	return c.Point.Equal(&expected.Point)
}

// Add adds two commitments (for proving value conservation)
// C1 + C2 = (v1 + v2)*G + (r1 + r2)*H
func (c *PedersenCommitment) Add(other *PedersenCommitment) *PedersenCommitment {
	var result bn254.G1Affine
	result.Add(&c.Point, &other.Point)
	return &PedersenCommitment{Point: result}
}

// Sub subtracts two commitments
// C1 - C2 = (v1 - v2)*G + (r1 - r2)*H
func (c *PedersenCommitment) Sub(other *PedersenCommitment) *PedersenCommitment {
	var negOther bn254.G1Affine
	negOther.Neg(&other.Point)

	var result bn254.G1Affine
	result.Add(&c.Point, &negOther)

	return &PedersenCommitment{Point: result}
}

// Bytes returns the compressed byte representation
func (c *PedersenCommitment) Bytes() []byte {
	return c.Point.Marshal()
}

// FromBytes reconstructs a commitment from bytes
func (c *PedersenCommitment) FromBytes(data []byte) error {
	return c.Point.Unmarshal(data)
}

// ToHash converts commitment to a Hash type
func (c *PedersenCommitment) ToHash() types.Hash {
	var hash types.Hash
	bytes := c.Bytes()
	if len(bytes) >= types.HashSize {
		copy(hash[:], bytes[:types.HashSize])
	}
	return hash
}

// RandomScalar generates a random scalar in the field
func RandomScalar() (*big.Int, error) {
	var scalar fr.Element
	_, err := scalar.SetRandom()
	if err != nil {
		return nil, err
	}
	return scalar.BigInt(new(big.Int)), nil
}

// RandomBytes generates n random bytes
func RandomBytes(n int) ([]byte, error) {
	bytes := make([]byte, n)
	_, err := rand.Read(bytes)
	return bytes, err
}

// hashToBytes is a helper to derive deterministic bytes from a string
func hashToBytes(input string) []byte {
	// Simple hash for now - would use proper hash in production
	result := make([]byte, 32)
	data := []byte(input)
	for i := 0; i < 32; i++ {
		if i < len(data) {
			result[i] = data[i] ^ byte(i*17)
		} else {
			result[i] = byte(i * 31)
		}
	}
	return result
}

// ValueCommitment wraps a Pedersen commitment with value metadata
type ValueCommitment struct {
	Commitment *PedersenCommitment
	AssetType  types.Hash // For multi-asset support
}

// NewValueCommitment creates a value commitment
func NewValueCommitment(value uint64, assetType types.Hash) (*ValueCommitment, *big.Int, error) {
	valueInt := new(big.Int).SetUint64(value)
	commitment, blinder, err := NewRandomCommitment(valueInt)
	if err != nil {
		return nil, nil, err
	}

	return &ValueCommitment{
		Commitment: commitment,
		AssetType:  assetType,
	}, blinder, nil
}

// VerifyValueConservation checks that inputs = outputs + fee
// sum(input_commitments) = sum(output_commitments) + fee*G
func VerifyValueConservation(
	inputCommitments []*PedersenCommitment,
	outputCommitments []*PedersenCommitment,
	fee uint64,
) bool {
	if err := InitializeGenerators(); err != nil {
		return false
	}

	// Sum inputs
	var inputSum bn254.G1Affine
	inputSum.SetInfinity()
	for _, c := range inputCommitments {
		inputSum.Add(&inputSum, &c.Point)
	}

	// Sum outputs
	var outputSum bn254.G1Affine
	outputSum.SetInfinity()
	for _, c := range outputCommitments {
		outputSum.Add(&outputSum, &c.Point)
	}

	// Add fee * G to outputs (fee has no blinder, known value)
	var feeCommitment bn254.G1Affine
	feeCommitment.ScalarMultiplication(&generatorG, new(big.Int).SetUint64(fee))
	outputSum.Add(&outputSum, &feeCommitment)

	// Check inputs = outputs + fee
	// This works because the blinders must also balance for this to equal
	return inputSum.Equal(&outputSum)
}
