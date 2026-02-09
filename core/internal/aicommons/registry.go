// Package aicommons implements the AI Commons model registry.
package aicommons

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Registry errors
var (
	ErrModelNotFound       = errors.New("model not found")
	ErrModelExists         = errors.New("model already exists")
	ErrInvalidContribution = errors.New("invalid contribution")
	ErrUnauthorized        = errors.New("unauthorized operation")
)

// ModelRegistry manages the AI Commons model registry
type ModelRegistry struct {
	mu sync.RWMutex

	// Models indexed by ID
	models map[types.Hash]*types.ModelEntry

	// Models indexed by task type
	modelsByTask map[types.TaskType][]types.Hash

	// Contribution records
	contributions map[types.Hash][]*Contribution

	// Storage backend
	store ModelStore
}

// Contribution records a contribution to a model
type Contribution struct {
	Contributor types.Address
	ModelID     types.Hash
	Epoch       uint64
	Compute     uint64 // Compute units contributed
	Quality     float64
	GradientHash types.Hash
	Timestamp   uint64
}

// ModelStore defines persistence for models
type ModelStore interface {
	SaveModel(ctx context.Context, model *types.ModelEntry) error
	GetModel(ctx context.Context, id types.Hash) (*types.ModelEntry, error)
	ListModels(ctx context.Context, taskType types.TaskType, limit int) ([]*types.ModelEntry, error)
	SaveContribution(ctx context.Context, contrib *Contribution) error
	GetContributions(ctx context.Context, modelID types.Hash) ([]*Contribution, error)
}

// NewModelRegistry creates a new model registry
func NewModelRegistry(store ModelStore) *ModelRegistry {
	return &ModelRegistry{
		models:        make(map[types.Hash]*types.ModelEntry),
		modelsByTask:  make(map[types.TaskType][]types.Hash),
		contributions: make(map[types.Hash][]*Contribution),
		store:         store,
	}
}

// RegisterModel registers a new model in the commons
func (r *ModelRegistry) RegisterModel(ctx context.Context, model *types.ModelEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.models[model.ModelID]; exists {
		return ErrModelExists
	}

	// Generate model ID if not set
	if model.ModelID == (types.Hash{}) {
		model.ModelID = r.generateModelID(model)
	}

	model.Status = types.ModelStatusActive
	model.Contributors = make(map[types.Address]uint64)

	r.models[model.ModelID] = model
	r.modelsByTask[model.TaskType] = append(r.modelsByTask[model.TaskType], model.ModelID)

	return r.store.SaveModel(ctx, model)
}

// generateModelID generates a unique model ID
func (r *ModelRegistry) generateModelID(model *types.ModelEntry) types.Hash {
	data := []byte(model.Architecture)
	data = append(data, []byte(model.Domain)...)
	data = append(data, byte(model.TaskType))

	hash := sha256.Sum256(data)
	var id types.Hash
	copy(id[:], hash[:])
	return id
}

// GetModel retrieves a model by ID
func (r *ModelRegistry) GetModel(ctx context.Context, modelID types.Hash) (*types.ModelEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	model, exists := r.models[modelID]
	if !exists {
		return r.store.GetModel(ctx, modelID)
	}
	return model, nil
}

// RecordContribution records a training contribution
func (r *ModelRegistry) RecordContribution(ctx context.Context, contrib *Contribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	model, exists := r.models[contrib.ModelID]
	if !exists {
		return ErrModelNotFound
	}

	// Update model statistics
	model.TotalCompute += contrib.Compute
	model.Contributors[contrib.Contributor] += contrib.Compute

	// Update accuracy based on contribution quality
	// This is simplified - real implementation would track model weights
	weightedQuality := float64(contrib.Compute) / float64(model.TotalCompute)
	model.Accuracy = model.Accuracy*(1-weightedQuality) + contrib.Quality*weightedQuality

	// Store contribution
	r.contributions[contrib.ModelID] = append(r.contributions[contrib.ModelID], contrib)

	if err := r.store.SaveContribution(ctx, contrib); err != nil {
		return err
	}

	return r.store.SaveModel(ctx, model)
}

// CalculateRewardShare calculates a contributor's share of rewards
// Share_i = Compute_i / Σ Compute_j
func (r *ModelRegistry) CalculateRewardShare(modelID types.Hash, contributor types.Address) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	model, exists := r.models[modelID]
	if !exists {
		return 0
	}

	contributed := model.Contributors[contributor]
	if model.TotalCompute == 0 {
		return 0
	}

	return float64(contributed) / float64(model.TotalCompute)
}

// ListModelsByTask lists models for a specific task type
func (r *ModelRegistry) ListModelsByTask(ctx context.Context, taskType types.TaskType, limit int) ([]*types.ModelEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	modelIDs, exists := r.modelsByTask[taskType]
	if !exists {
		return nil, nil
	}

	result := make([]*types.ModelEntry, 0, len(modelIDs))
	for _, id := range modelIDs {
		if model, exists := r.models[id]; exists {
			result = append(result, model)
			if len(result) >= limit {
				break
			}
		}
	}

	return result, nil
}

// GetTopContributors returns top contributors for a model
func (r *ModelRegistry) GetTopContributors(modelID types.Hash, limit int) []ContributorRank {
	r.mu.RLock()
	defer r.mu.RUnlock()

	model, exists := r.models[modelID]
	if !exists {
		return nil
	}

	ranks := make([]ContributorRank, 0, len(model.Contributors))
	for addr, compute := range model.Contributors {
		share := float64(compute) / float64(model.TotalCompute)
		ranks = append(ranks, ContributorRank{
			Address: addr,
			Compute: compute,
			Share:   share,
		})
	}

	// Sort by compute (simple bubble sort)
	for i := 0; i < len(ranks)-1; i++ {
		for j := i + 1; j < len(ranks); j++ {
			if ranks[j].Compute > ranks[i].Compute {
				ranks[i], ranks[j] = ranks[j], ranks[i]
			}
		}
	}

	if len(ranks) > limit {
		ranks = ranks[:limit]
	}

	return ranks
}

// ContributorRank represents a contributor's ranking
type ContributorRank struct {
	Address types.Address
	Compute uint64
	Share   float64
}

// UpdateModelWeights updates the model's weight CID after training
func (r *ModelRegistry) UpdateModelWeights(ctx context.Context, modelID types.Hash, newWeightsCID string, newAccuracy float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	model, exists := r.models[modelID]
	if !exists {
		return ErrModelNotFound
	}

	model.CurrentWeights = newWeightsCID
	model.Accuracy = newAccuracy
	// Update LastUpdatedAt would be done here

	return r.store.SaveModel(ctx, model)
}

// DeprecateModel marks a model as deprecated
func (r *ModelRegistry) DeprecateModel(ctx context.Context, modelID types.Hash, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	model, exists := r.models[modelID]
	if !exists {
		return ErrModelNotFound
	}

	model.Status = types.ModelStatusDeprecated
	return r.store.SaveModel(ctx, model)
}

// GetModelStats returns statistics for a model
type ModelStats struct {
	ModelID           types.Hash
	TotalContributors int
	TotalCompute      uint64
	CurrentAccuracy   float64
	ContributionCount int
}

// GetStats returns model statistics
func (r *ModelRegistry) GetStats(modelID types.Hash) *ModelStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	model, exists := r.models[modelID]
	if !exists {
		return nil
	}

	contribCount := len(r.contributions[modelID])

	return &ModelStats{
		ModelID:           modelID,
		TotalContributors: len(model.Contributors),
		TotalCompute:      model.TotalCompute,
		CurrentAccuracy:   model.Accuracy,
		ContributionCount: contribCount,
	}
}
