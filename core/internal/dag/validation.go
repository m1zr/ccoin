// Package dag implements block validation for the BlockDAG.
package dag

import (
	"context"
	"errors"
	"time"

	"github.com/ccoin/core/pkg/types"
)

// Validation errors
var (
	ErrInvalidBlockVersion  = errors.New("invalid block version")
	ErrTooManyParents       = errors.New("too many parents")
	ErrNoParents            = errors.New("non-genesis block has no parents")
	ErrFutureTimestamp      = errors.New("block timestamp is in the future")
	ErrInvalidHeight        = errors.New("invalid block height")
	ErrInvalidDifficulty    = errors.New("block does not meet difficulty target")
	ErrInvalidTxRoot        = errors.New("invalid transaction root")
	ErrInvalidPoUW          = errors.New("invalid proof of useful work")
	ErrInvalidReputation    = errors.New("invalid reputation score")
	ErrInvalidQualityScore  = errors.New("invalid quality score")
	ErrMinerBanned          = errors.New("miner is banned")
	ErrParentTimestamp      = errors.New("block timestamp before parent")
)

// BlockValidator validates blocks before adding to the DAG
type BlockValidator struct {
	dag          *DAG
	maxFutureSec uint64 // Maximum seconds a timestamp can be in the future
}

// NewBlockValidator creates a new block validator
func NewBlockValidator(dag *DAG) *BlockValidator {
	return &BlockValidator{
		dag:          dag,
		maxFutureSec: 120, // Allow 2 minutes into the future
	}
}

// ValidateBlock performs full block validation
func (v *BlockValidator) ValidateBlock(ctx context.Context, block *types.Block) error {
	header := block.Header

	// Validate header
	if err := v.validateHeader(ctx, header); err != nil {
		return err
	}

	// Validate transactions
	if err := v.validateTransactions(ctx, block); err != nil {
		return err
	}

	// Validate PoUW
	if err := v.validatePoUW(ctx, block); err != nil {
		return err
	}

	return nil
}

// validateHeader validates the block header
func (v *BlockValidator) validateHeader(ctx context.Context, header *types.BlockHeader) error {
	// Version check
	if header.Version != 1 {
		return ErrInvalidBlockVersion
	}

	// Check hash is correctly computed
	computedHash := header.ComputeHash()
	if computedHash != header.Hash {
		return errors.New("block hash mismatch")
	}

	// Parent validation
	if !header.IsGenesis() {
		if len(header.Parents) == 0 {
			return ErrNoParents
		}
		if len(header.Parents) > types.MaxParents {
			return ErrTooManyParents
		}

		// Validate each parent exists
		var maxParentHeight uint64
		var maxParentTime uint64

		for _, parentHash := range header.Parents {
			parentHeader, err := v.dag.getBlockHeader(ctx, parentHash)
			if err != nil {
				return ErrOrphanBlock
			}

			if parentHeader.Height > maxParentHeight {
				maxParentHeight = parentHeader.Height
			}
			if parentHeader.Timestamp > maxParentTime {
				maxParentTime = parentHeader.Timestamp
			}
		}

		// Height must be max(parent heights) + 1
		if header.Height != maxParentHeight+1 {
			return ErrInvalidHeight
		}

		// Timestamp must be >= max parent timestamp
		if header.Timestamp < maxParentTime {
			return ErrParentTimestamp
		}
	}

	// Timestamp not too far in the future
	now := uint64(time.Now().Unix())
	if header.Timestamp > now+v.maxFutureSec {
		return ErrFutureTimestamp
	}

	// Reputation bounds
	if header.ReputationScore < types.MinReputation || header.ReputationScore > types.MaxReputation {
		return ErrInvalidReputation
	}

	// Quality score bounds (if present)
	if header.QualityScore != 0 {
		if header.QualityScore <= 0 || header.QualityScore > 1 {
			return ErrInvalidQualityScore
		}
	}

	// Difficulty validation
	if err := v.validateDifficulty(header); err != nil {
		return err
	}

	return nil
}

// validateDifficulty checks that the block meets the difficulty target
func (v *BlockValidator) validateDifficulty(header *types.BlockHeader) error {
	if header.Difficulty == nil || header.Difficulty.Sign() <= 0 {
		return ErrInvalidDifficulty
	}

	// Compute hash and check against difficulty
	// H(Header || nonce || Hash(PoUWResult)) < Difficulty
	hashInt := hashToBigInt(header.Hash)

	if hashInt.Cmp(header.Difficulty) >= 0 {
		return ErrInvalidDifficulty
	}

	return nil
}

// validateTransactions validates all transactions in the block
func (v *BlockValidator) validateTransactions(ctx context.Context, block *types.Block) error {
	if len(block.Transactions) > types.MaxTransactionsPerBlock {
		return errors.New("too many transactions in block")
	}

	// Compute transaction root
	computedRoot := ComputeTxRoot(block.Transactions)
	if computedRoot != block.Header.TxRoot {
		return ErrInvalidTxRoot
	}

	// Validate each transaction
	for _, tx := range block.Transactions {
		if err := v.validateTransaction(ctx, tx); err != nil {
			return err
		}
	}

	return nil
}

// validateTransaction validates a single transaction
func (v *BlockValidator) validateTransaction(ctx context.Context, tx *types.Transaction) error {
	// Verify transaction hash
	computedHash := tx.ComputeHash()
	if computedHash != tx.TxHash {
		return errors.New("transaction hash mismatch")
	}

	// Verify nullifiers are not already spent
	// (This would check the nullifier set in production)

	// Verify zk-SNARK proof
	// (This would call the ZKP verifier in production)

	return nil
}

// validatePoUW validates the Proof-of-Useful-Work
func (v *BlockValidator) validatePoUW(ctx context.Context, block *types.Block) error {
	header := block.Header

	// Genesis block has no PoUW requirement
	if header.IsGenesis() {
		return nil
	}

	// Must have PoUW result
	if header.PoUWResult.IsEmpty() {
		return ErrInvalidPoUW
	}

	// Verify the quality score is valid
	if header.QualityScore <= 0 || header.QualityScore > 1 {
		return ErrInvalidQualityScore
	}

	// In production, this would verify:
	// 1. The gradient computation is valid
	// 2. Loss(W_t + α·∇L) < Loss(W_t) (Improvement Gate)
	// 3. The verification subset matches the VRF selection
	// 4. Gradient diversity check

	return nil
}

// ComputeTxRoot computes the Merkle root of transactions
func ComputeTxRoot(txs []*types.Transaction) types.Hash {
	if len(txs) == 0 {
		return types.EmptyHash
	}

	// Collect transaction hashes
	hashes := make([]types.Hash, len(txs))
	for i, tx := range txs {
		hashes[i] = tx.TxHash
	}

	// Build Merkle tree
	return computeMerkleRoot(hashes)
}

// computeMerkleRoot computes the Merkle root from a list of hashes
func computeMerkleRoot(hashes []types.Hash) types.Hash {
	if len(hashes) == 0 {
		return types.EmptyHash
	}
	if len(hashes) == 1 {
		return hashes[0]
	}

	// Pad to even number
	if len(hashes)%2 != 0 {
		hashes = append(hashes, hashes[len(hashes)-1])
	}

	// Build next level
	nextLevel := make([]types.Hash, len(hashes)/2)
	for i := 0; i < len(hashes); i += 2 {
		nextLevel[i/2] = hashPair(hashes[i], hashes[i+1])
	}

	return computeMerkleRoot(nextLevel)
}

// hashPair hashes two hashes together
func hashPair(a, b types.Hash) types.Hash {
	data := make([]byte, types.HashSize*2)
	copy(data[:types.HashSize], a[:])
	copy(data[types.HashSize:], b[:])

	result := types.Hash{}
	// In production, use SHA3-256
	copy(result[:], data[:types.HashSize]) // Simplified for now
	return result
}

// hashToBigInt converts a hash to a big.Int for comparison
func hashToBigInt(h types.Hash) *big.Int {
	return new(big.Int).SetBytes(h[:])
}

// Required import
import "math/big"
