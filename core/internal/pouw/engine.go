// Package pouw implements Proof-of-Useful-Work consensus.
package pouw

import (
	"context"
	"crypto/sha256"
	"errors"
	"math/big"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// PoUW errors
var (
	ErrNoTask            = errors.New("no task assigned")
	ErrTaskExpired       = errors.New("task has expired")
	ErrInvalidGradient   = errors.New("invalid gradient computation")
	ErrImprovementFailed = errors.New("loss improvement check failed")
	ErrQualityTooLow     = errors.New("quality score too low")
	ErrDifficultyNotMet  = errors.New("difficulty target not met")
)

// Engine implements the Proof-of-Useful-Work mining engine
type Engine struct {
	mu sync.RWMutex

	// Task queue
	taskQueue *TaskQueue

	// Model registry
	modelStore ModelStore

	// Current mining state
	mining        bool
	currentTask   *types.Task
	minerAddress  types.Address

	// Verification parameters
	verificationSubsetSize float64 // Percentage of batch to verify
}

// ModelStore defines the interface for model weight storage
type ModelStore interface {
	GetWeights(ctx context.Context, modelID types.Hash) ([]byte, error)
	GetWeightsCID(ctx context.Context, modelID types.Hash) (string, error)
	SaveWeights(ctx context.Context, modelID types.Hash, weights []byte) error
}

// Config holds PoUW engine configuration
type Config struct {
	VerificationSubsetSize float64
}

// DefaultConfig returns default PoUW configuration
func DefaultConfig() *Config {
	return &Config{
		VerificationSubsetSize: 0.01, // 1% of batch
	}
}

// NewEngine creates a new PoUW engine
func NewEngine(taskQueue *TaskQueue, modelStore ModelStore, cfg *Config) *Engine {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	return &Engine{
		taskQueue:              taskQueue,
		modelStore:             modelStore,
		verificationSubsetSize: cfg.VerificationSubsetSize,
	}
}

// StartMining begins the PoUW mining process
func (e *Engine) StartMining(ctx context.Context, minerAddr types.Address) error {
	e.mu.Lock()
	if e.mining {
		e.mu.Unlock()
		return nil
	}
	e.mining = true
	e.minerAddress = minerAddr
	e.mu.Unlock()

	go e.miningLoop(ctx)
	return nil
}

// StopMining stops the mining process
func (e *Engine) StopMining() {
	e.mu.Lock()
	e.mining = false
	e.mu.Unlock()
}

// miningLoop is the main mining loop
func (e *Engine) miningLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e.mu.RLock()
		if !e.mining {
			e.mu.RUnlock()
			return
		}
		e.mu.RUnlock()

		// Get next task
		task, err := e.taskQueue.GetNextTask(ctx, e.minerAddress)
		if err != nil {
			continue
		}

		e.mu.Lock()
		e.currentTask = task
		e.mu.Unlock()

		// Perform useful work
		result, err := e.performWork(ctx, task)
		if err != nil {
			e.taskQueue.FailTask(ctx, task.TaskID, err.Error())
			continue
		}

		// Submit result
		_ = result // Result would be used to create a new block

		e.mu.Lock()
		e.currentTask = nil
		e.mu.Unlock()
	}
}

// PoUWResult contains the result of PoUW computation
type PoUWResult struct {
	GradientHash   types.Hash
	QualityScore   float64
	LossBefore     float64
	LossAfter      float64
	Nonce          uint64
	Proof          []byte
}

// performWork performs the useful work computation
func (e *Engine) performWork(ctx context.Context, task *types.Task) (*PoUWResult, error) {
	// In production, this would:
	// 1. Load model weights from IPFS
	// 2. Load batch data
	// 3. Compute gradients (actual AI training)
	// 4. Calculate loss improvement
	// 5. Generate zk-SNARK proof of correct computation

	// Simulated computation
	result := &PoUWResult{}

	// Simulate gradient computation
	gradientHash := e.simulateGradientComputation(task)
	result.GradientHash = gradientHash

	// Simulate loss calculation
	result.LossBefore = 0.5 // Simulated
	result.LossAfter = 0.45  // 10% improvement

	// Calculate quality score
	result.QualityScore = (result.LossBefore - result.LossAfter) / result.LossBefore
	if result.QualityScore <= 0 {
		return nil, ErrImprovementFailed
	}

	// Find nonce that meets difficulty
	nonce, err := e.findValidNonce(ctx, task, gradientHash)
	if err != nil {
		return nil, err
	}
	result.Nonce = nonce

	// Generate proof (simulated)
	result.Proof = e.simulateProofGeneration(gradientHash, nonce)

	return result, nil
}

// simulateGradientComputation simulates gradient computation
func (e *Engine) simulateGradientComputation(task *types.Task) types.Hash {
	// In production: actual gradient computation using PyTorch/TensorFlow
	// For simulation, we hash the task parameters
	data := append(task.TaskID[:], task.ModelID[:]...)
	hash := sha256.Sum256(data)
	
	var result types.Hash
	copy(result[:], hash[:])
	return result
}

// findValidNonce searches for a nonce that meets difficulty target
func (e *Engine) findValidNonce(ctx context.Context, task *types.Task, gradientHash types.Hash) (uint64, error) {
	// H(Header || nonce || Hash(R)) < Difficulty
	// For simulation, we just iterate until we find a valid nonce

	difficulty := big.NewInt(1)
	difficulty.Lsh(difficulty, 200) // Simulated difficulty

	var nonce uint64
	for nonce = 0; nonce < 1000000; nonce++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		hash := computePoUWHash(task.TaskID, nonce, gradientHash)
		hashInt := new(big.Int).SetBytes(hash[:])

		if hashInt.Cmp(difficulty) < 0 {
			return nonce, nil
		}
	}

	return 0, ErrDifficultyNotMet
}

// computePoUWHash computes H(Header || nonce || Hash(R))
func computePoUWHash(taskID types.Hash, nonce uint64, gradientHash types.Hash) types.Hash {
	data := make([]byte, 0, 72)
	data = append(data, taskID[:]...)
	data = append(data, uint64ToBytes(nonce)...)
	data = append(data, gradientHash[:]...)

	hash := sha256.Sum256(data)
	var result types.Hash
	copy(result[:], hash[:])
	return result
}

// simulateProofGeneration simulates zk-SNARK proof generation
func (e *Engine) simulateProofGeneration(gradientHash types.Hash, nonce uint64) []byte {
	// In production: actual zk-SNARK proof using gnark
	// For simulation, we just return a hash
	data := append(gradientHash[:], uint64ToBytes(nonce)...)
	hash := sha256.Sum256(data)
	return hash[:]
}

// ValidatePoUW validates a PoUW result
func (e *Engine) ValidatePoUW(ctx context.Context, block *types.Block) error {
	header := block.Header

	// Check difficulty target
	hash := computePoUWHash(header.TaskID, header.Nonce, header.PoUWResult)
	hashInt := new(big.Int).SetBytes(hash[:])

	if header.Difficulty == nil || hashInt.Cmp(header.Difficulty) >= 0 {
		return ErrDifficultyNotMet
	}

	// Check quality score bounds
	if header.QualityScore <= 0 || header.QualityScore > 1 {
		return ErrQualityTooLow
	}

	// In production: verify the zk-SNARK proof
	// This would validate:
	// 1. Gradient was computed correctly
	// 2. Loss improvement is genuine
	// 3. Correct verification subset was used

	return nil
}

// CalculateQualityScore calculates Q(B) = (Loss_before - Loss_after) / Loss_before
func CalculateQualityScore(lossBefore, lossAfter float64) float64 {
	if lossBefore == 0 {
		return 0
	}
	return (lossBefore - lossAfter) / lossBefore
}

// uint64ToBytes converts uint64 to bytes
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

// IsMining returns whether mining is active
func (e *Engine) IsMining() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mining
}

// CurrentTask returns the current task being worked on
func (e *Engine) CurrentTask() *types.Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentTask
}
