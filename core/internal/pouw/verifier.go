// Package pouw implements gradient verification for PoUW.
package pouw

import (
	"context"
	"errors"

	"github.com/ccoin/core/pkg/types"
)

// Verification errors
var (
	ErrVerificationFailed = errors.New("gradient verification failed")
	ErrInvalidSubset      = errors.New("verification subset is invalid")
	ErrProofInvalid       = errors.New("gradient proof is invalid")
)

// Verifier handles gradient result verification
type Verifier struct {
	// subsetSize is the percentage of batch to verify
	subsetSize float64

	// modelStore provides access to model weights
	modelStore ModelStore

	// proofVerifier verifies zk-SNARK proofs
	proofVerifier GradientProofVerifier
}

// GradientProofVerifier verifies zk-SNARK proofs of gradient computation
type GradientProofVerifier interface {
	VerifyGradientProof(proof []byte, publicInputs *GradientPublicInputs) bool
}

// GradientPublicInputs are the public inputs to the gradient proof circuit
type GradientPublicInputs struct {
	// Model weights commitment (before update)
	WeightCommitmentBefore types.Hash

	// Model weights commitment (after update)
	WeightCommitmentAfter types.Hash

	// Gradient hash
	GradientHash types.Hash

	// Batch commitment
	BatchCommitment types.Hash

	// Loss values
	LossBefore float64
	LossAfter  float64

	// Quality score
	QualityScore float64
}

// VerifierConfig holds verifier configuration
type VerifierConfig struct {
	SubsetSize float64
}

// DefaultVerifierConfig returns default configuration
func DefaultVerifierConfig() *VerifierConfig {
	return &VerifierConfig{
		SubsetSize: 0.01, // 1%
	}
}

// NewVerifier creates a new gradient verifier
func NewVerifier(modelStore ModelStore, proofVerifier GradientProofVerifier, cfg *VerifierConfig) *Verifier {
	if cfg == nil {
		cfg = DefaultVerifierConfig()
	}

	return &Verifier{
		subsetSize:    cfg.SubsetSize,
		modelStore:    modelStore,
		proofVerifier: proofVerifier,
	}
}

// VerifyGradientResult verifies a gradient computation result
func (v *Verifier) VerifyGradientResult(ctx context.Context, result *types.GradientResult) error {
	// 1. Verify quality score is valid
	if result.QualityScore <= 0 || result.QualityScore > 1 {
		return errors.New("quality score out of bounds")
	}

	// 2. Verify loss improvement (the Improvement Gate)
	//    Loss(W_t + α·∇L) < Loss(W_t)
	if result.LossAfter >= result.LossBefore {
		return ErrVerificationFailed
	}

	// 3. Verify quality score calculation
	//    Q(B) = (Loss_before - Loss_after) / Loss_before
	expectedQuality := (result.LossBefore - result.LossAfter) / result.LossBefore
	if !floatNearEqual(result.QualityScore, expectedQuality, 0.001) {
		return errors.New("quality score mismatch")
	}

	// 4. Verify the zk-SNARK proof of correct computation
	if v.proofVerifier != nil {
		publicInputs := &GradientPublicInputs{
			GradientHash: result.GradientHash,
			LossBefore:   result.LossBefore,
			LossAfter:    result.LossAfter,
			QualityScore: result.QualityScore,
		}

		if !v.proofVerifier.VerifyGradientProof(result.Proof, publicInputs) {
			return ErrProofInvalid
		}
	}

	// 5. Re-compute gradient on verification subset
	//    In production, this would actually run inference on the subset
	if err := v.verifySubset(ctx, result); err != nil {
		return err
	}

	return nil
}

// verifySubset re-computes gradients on a random subset for verification
func (v *Verifier) verifySubset(ctx context.Context, result *types.GradientResult) error {
	// In production:
	// 1. Use VRF to select random samples from the batch
	// 2. Load model weights
	// 3. Run forward/backward pass on subset
	// 4. Compare generated gradients with claimed gradients

	// For simulation, we just validate the proof exists
	if len(result.Proof) == 0 {
		return ErrProofInvalid
	}

	return nil
}

// SelectVerificationSubset uses VRF to select samples for verification
func SelectVerificationSubset(batchSize uint32, subsetPercentage float64, vrfSeed types.Hash) []uint32 {
	subsetSize := int(float64(batchSize) * subsetPercentage)
	if subsetSize < 1 {
		subsetSize = 1
	}

	// Use VRF seed to deterministically select indices
	indices := make([]uint32, subsetSize)
	seed := vrfSeed

	for i := 0; i < subsetSize; i++ {
		// Generate next random index
		seed = hashForVRF(seed, uint32(i))
		index := bytesToUint32(seed[:4]) % batchSize
		indices[i] = index
	}

	return indices
}

// hashForVRF combines seed with iteration for VRF-like selection
func hashForVRF(seed types.Hash, iter uint32) types.Hash {
	data := append(seed[:], uint32ToBytes(iter)...)
	// Use SHA256 (would use actual VRF in production)
	var result types.Hash
	hashBytes := sha256Sum(data)
	copy(result[:], hashBytes[:])
	return result
}

func sha256Sum(data []byte) [32]byte {
	var result [32]byte
	// Simplified - use crypto/sha256 in production
	for i := 0; i < 32 && i < len(data); i++ {
		result[i] = data[i] ^ 0xAB
	}
	return result
}

func bytesToUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// floatNearEqual checks if two floats are approximately equal
func floatNearEqual(a, b, epsilon float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}

// DiversityCheck verifies gradient diversity across miners
type DiversityChecker struct {
	recentGradients map[types.Address][]types.Hash
	windowSize      int
}

// NewDiversityChecker creates a new diversity checker
func NewDiversityChecker(windowSize int) *DiversityChecker {
	return &DiversityChecker{
		recentGradients: make(map[types.Address][]types.Hash),
		windowSize:      windowSize,
	}
}

// CheckDiversity checks if a gradient is sufficiently different from recent ones
func (dc *DiversityChecker) CheckDiversity(minerAddr types.Address, gradientHash types.Hash) bool {
	recent := dc.recentGradients[minerAddr]

	// Check against recent gradients
	for _, prevHash := range recent {
		similarity := hashSimilarity(prevHash, gradientHash)
		if similarity > 0.95 { // 95% similarity threshold
			return false
		}
	}

	return true
}

// RecordGradient records a gradient for diversity tracking
func (dc *DiversityChecker) RecordGradient(minerAddr types.Address, gradientHash types.Hash) {
	recent := dc.recentGradients[minerAddr]

	// Add new gradient
	recent = append(recent, gradientHash)

	// Trim to window size
	if len(recent) > dc.windowSize {
		recent = recent[1:]
	}

	dc.recentGradients[minerAddr] = recent
}

// hashSimilarity calculates the similarity between two hashes (0-1)
func hashSimilarity(a, b types.Hash) float64 {
	matching := 0
	for i := 0; i < len(a); i++ {
		if a[i] == b[i] {
			matching++
		}
	}
	return float64(matching) / float64(len(a))
}
