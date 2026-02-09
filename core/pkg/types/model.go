// Package types defines AI model structures for the CCoin AI Commons.
package types

// ModelStatus represents the lifecycle status of a model
type ModelStatus uint8

const (
	// ModelStatusProposed indicates the model has been proposed but not yet approved
	ModelStatusProposed ModelStatus = 0

	// ModelStatusActive indicates the model is actively being trained
	ModelStatusActive ModelStatus = 1

	// ModelStatusCompleted indicates training has finished successfully
	ModelStatusCompleted ModelStatus = 2

	// ModelStatusDeprecated indicates the model is no longer in use
	ModelStatusDeprecated ModelStatus = 3
)

// TaskType represents the type of AI task
type TaskType uint8

const (
	// TaskClassification is for classification models
	TaskClassification TaskType = 0

	// TaskGeneration is for generative models
	TaskGeneration TaskType = 1

	// TaskRegression is for regression models
	TaskRegression TaskType = 2

	// TaskEmbedding is for embedding models
	TaskEmbedding TaskType = 3

	// TaskFolding is for protein folding
	TaskFolding TaskType = 4

	// TaskSimulation is for scientific simulations
	TaskSimulation TaskType = 5
)

// LicenseType represents the licensing model for a trained model
type LicenseType uint8

const (
	// LicenseOpen allows unrestricted use
	LicenseOpen LicenseType = 0

	// LicenseRestricted requires attribution
	LicenseRestricted LicenseType = 1

	// LicenseCommercial requires payment for commercial use
	LicenseCommercial LicenseType = 2
)

// ModelEntry represents an AI model in the AI Commons registry
type ModelEntry struct {
	// ModelID is the unique identifier for this model
	ModelID Hash

	// Architecture describes the model architecture (e.g., "transformer-7B")
	Architecture string

	// TaskType indicates the type of AI task
	TaskType TaskType

	// Domain describes the domain (e.g., "nlp", "medical-imaging", "climate")
	Domain string

	// CurrentWeights is the IPFS CID of the latest model weights
	CurrentWeights string

	// Accuracy is the latest validation accuracy (0.0 - 1.0)
	Accuracy float64

	// TotalCompute is the cumulative GPU-hours invested
	TotalCompute uint64

	// Contributors maps miner addresses to their contribution counts
	Contributors map[Address]uint64

	// Status is the current lifecycle status
	Status ModelStatus

	// License is the licensing model
	License LicenseType

	// ProposerAddress is the address that proposed this model
	ProposerAddress Address

	// GovernanceID links to the governance proposal that approved this model
	GovernanceID Hash

	// CreatedAt is the block height when this model was created
	CreatedAt uint64

	// LastUpdatedAt is the block height of the last update
	LastUpdatedAt uint64

	// ValidationMetrics contains detailed validation metrics
	ValidationMetrics *ValidationMetrics
}

// ValidationMetrics contains detailed metrics from model validation
type ValidationMetrics struct {
	// Accuracy is the primary accuracy metric
	Accuracy float64

	// Precision is the precision score
	Precision float64

	// Recall is the recall score
	Recall float64

	// F1Score is the F1 score
	F1Score float64

	// Loss is the validation loss
	Loss float64

	// ValidatedAt is the block height when validation occurred
	ValidatedAt uint64
}

// Task represents a computational task in the Task Queue
type Task struct {
	// TaskID is the unique identifier for this task
	TaskID Hash

	// ModelID links to the model being trained
	ModelID Hash

	// BatchIndex is the index of the data batch for this task
	BatchIndex uint64

	// DataHash is the hash of the training data batch
	DataHash Hash

	// CurrentWeightsHash is the hash of the model weights to use
	CurrentWeightsHash Hash

	// LearningRate is the learning rate for this batch
	LearningRate float64

	// DifficultyTarget is the mining difficulty for this task
	DifficultyTarget Hash

	// Status is the current task status
	Status TaskStatus

	// AssignedMiner is the miner assigned to this task (if any)
	AssignedMiner Address

	// AssignedAt is the block height when this task was assigned
	AssignedAt uint64

	// CompletedAt is the block height when this task was completed
	CompletedAt uint64
}

// TaskStatus represents the status of a task
type TaskStatus uint8

const (
	// TaskStatusPending indicates the task is waiting to be assigned
	TaskStatusPending TaskStatus = 0

	// TaskStatusAssigned indicates the task has been assigned to a miner
	TaskStatusAssigned TaskStatus = 1

	// TaskStatusCompleted indicates the task has been completed
	TaskStatusCompleted TaskStatus = 2

	// TaskStatusFailed indicates the task failed validation
	TaskStatusFailed TaskStatus = 3
)

// GradientResult represents the result of a PoUW gradient computation
type GradientResult struct {
	// TaskID identifies the task this gradient contributes to
	TaskID Hash

	// GradientHash is the hash of the computed gradient
	GradientHash Hash

	// Gradient contains the serialized gradient data (for verification)
	Gradient []byte

	// OldLoss is the loss before applying the gradient
	OldLoss float64

	// NewLoss is the loss after applying the gradient
	NewLoss float64

	// QualityScore is (OldLoss - NewLoss) / OldLoss
	QualityScore float64

	// VerificationSubset contains the subset of computation for verification
	VerificationSubset []byte

	// VRFSeed is the seed used to select the verification subset
	VRFSeed Hash
}

// NewModelEntry creates a new model entry
func NewModelEntry() *ModelEntry {
	return &ModelEntry{
		Contributors: make(map[Address]uint64),
		Status:       ModelStatusProposed,
		License:      LicenseOpen,
	}
}

// AddContribution records a contribution from a miner
func (m *ModelEntry) AddContribution(miner Address, qualityScore float64) {
	if m.Contributors == nil {
		m.Contributors = make(map[Address]uint64)
	}
	// Weight contribution by quality score (scaled to uint64)
	contribution := uint64(qualityScore * 1000000)
	m.Contributors[miner] += contribution
}

// TotalContributions returns the sum of all contributions
func (m *ModelEntry) TotalContributions() uint64 {
	var total uint64
	for _, c := range m.Contributors {
		total += c
	}
	return total
}

// ContributorShare calculates a miner's share of revenue (0.0 - 1.0)
func (m *ModelEntry) ContributorShare(miner Address) float64 {
	total := m.TotalContributions()
	if total == 0 {
		return 0
	}
	return float64(m.Contributors[miner]) / float64(total)
}
