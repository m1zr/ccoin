// Package aicommons implements task assignment for model training.
package aicommons

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Task assignment errors
var (
	ErrNoTasksAvailable = errors.New("no tasks available")
	ErrTaskAlreadyAssigned = errors.New("task already assigned")
	ErrInvalidGradient = errors.New("invalid gradient result")
)

// TaskAssigner manages task assignment for AI training
type TaskAssigner struct {
	mu sync.RWMutex

	// Model registry
	registry *ModelRegistry

	// Pending tasks per model
	tasks map[types.Hash][]*TrainingTask

	// Active assignments
	assignments map[types.Hash]*TaskAssignment

	// VRF seed for fair assignment
	vrfSeed types.Hash

	// Config
	config *TaskConfig
}

// TrainingTask represents a training task
type TrainingTask struct {
	TaskID      types.Hash
	ModelID     types.Hash
	DatasetCID  string
	BatchStart  uint32
	BatchEnd    uint32
	Objective   types.Hash
	Priority    uint32
	CreatedAt   uint64
	Deadline    uint64
	Reward      uint64
}

// TaskAssignment tracks an active task assignment
type TaskAssignment struct {
	Task          *TrainingTask
	AssignedTo    types.Address
	AssignedAt    uint64
	Deadline      uint64
	Status        AssignmentStatus
	GradientHash  types.Hash
	QualityScore  float64
	CompletedAt   uint64
}

// AssignmentStatus tracks task progress
type AssignmentStatus uint8

const (
	StatusPending AssignmentStatus = iota
	StatusAssigned
	StatusCompleted
	StatusFailed
	StatusExpired
)

// TaskConfig holds task assignment configuration
type TaskConfig struct {
	DefaultDeadline   uint64 // blocks
	MaxTasksPerMiner  int
	MinReputationScore float64
}

// DefaultTaskConfig returns default configuration
func DefaultTaskConfig() *TaskConfig {
	return &TaskConfig{
		DefaultDeadline:   600, // ~1.6 hours at 10s blocks
		MaxTasksPerMiner:  3,
		MinReputationScore: 0.5,
	}
}

// NewTaskAssigner creates a new task assigner
func NewTaskAssigner(registry *ModelRegistry, config *TaskConfig) *TaskAssigner {
	if config == nil {
		config = DefaultTaskConfig()
	}

	return &TaskAssigner{
		registry:    registry,
		tasks:       make(map[types.Hash][]*TrainingTask),
		assignments: make(map[types.Hash]*TaskAssignment),
		config:      config,
	}
}

// CreateTask creates a new training task
func (ta *TaskAssigner) CreateTask(
	ctx context.Context,
	modelID types.Hash,
	datasetCID string,
	batchStart, batchEnd uint32,
	reward uint64,
	currentBlock uint64,
) (*TrainingTask, error) {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	// Verify model exists
	_, err := ta.registry.GetModel(ctx, modelID)
	if err != nil {
		return nil, err
	}

	task := &TrainingTask{
		ModelID:    modelID,
		DatasetCID: datasetCID,
		BatchStart: batchStart,
		BatchEnd:   batchEnd,
		CreatedAt:  currentBlock,
		Deadline:   currentBlock + ta.config.DefaultDeadline,
		Reward:     reward,
	}

	// Generate task ID
	task.TaskID = ta.generateTaskID(task)
	task.Objective = ta.generateObjective(task)

	ta.tasks[modelID] = append(ta.tasks[modelID], task)

	return task, nil
}

// generateTaskID generates a unique task ID
func (ta *TaskAssigner) generateTaskID(task *TrainingTask) types.Hash {
	data := append(task.ModelID[:], []byte(task.DatasetCID)...)
	data = append(data, uint32ToBytes(task.BatchStart)...)
	data = append(data, uint32ToBytes(task.BatchEnd)...)
	data = append(data, uint64ToBytes(task.CreatedAt)...)

	hash := sha256.Sum256(data)
	var id types.Hash
	copy(id[:], hash[:])
	return id
}

// generateObjective generates the training objective hash
func (ta *TaskAssigner) generateObjective(task *TrainingTask) types.Hash {
	data := append(task.TaskID[:], task.ModelID[:]...)
	hash := sha256.Sum256(data)
	var obj types.Hash
	copy(obj[:], hash[:])
	return obj
}

// AssignTask assigns a task to a miner using VRF selection
func (ta *TaskAssigner) AssignTask(
	ctx context.Context,
	minerAddr types.Address,
	minerReputation float64,
	currentBlock uint64,
) (*TaskAssignment, error) {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	// Check reputation threshold
	if minerReputation < ta.config.MinReputationScore {
		return nil, errors.New("reputation too low for task assignment")
	}

	// Select task using VRF-like selection
	task := ta.selectTask(minerAddr)
	if task == nil {
		return nil, ErrNoTasksAvailable
	}

	// Check if already assigned
	if _, exists := ta.assignments[task.TaskID]; exists {
		return nil, ErrTaskAlreadyAssigned
	}

	assignment := &TaskAssignment{
		Task:       task,
		AssignedTo: minerAddr,
		AssignedAt: currentBlock,
		Deadline:   currentBlock + ta.config.DefaultDeadline,
		Status:     StatusAssigned,
	}

	ta.assignments[task.TaskID] = assignment

	// Remove from pending
	ta.removeTaskFromPending(task.ModelID, task.TaskID)

	return assignment, nil
}

// selectTask selects a task using VRF-based selection
func (ta *TaskAssigner) selectTask(minerAddr types.Address) *TrainingTask {
	// Combine VRF seed with miner address for deterministic selection
	data := append(ta.vrfSeed[:], minerAddr[:]...)
	hash := sha256.Sum256(data)
	index := int(hash[0])

	// Find available tasks
	for _, tasks := range ta.tasks {
		if len(tasks) > 0 {
			return tasks[index%len(tasks)]
		}
	}

	return nil
}

// removeTaskFromPending removes a task from the pending queue
func (ta *TaskAssigner) removeTaskFromPending(modelID, taskID types.Hash) {
	tasks := ta.tasks[modelID]
	for i, t := range tasks {
		if t.TaskID == taskID {
			ta.tasks[modelID] = append(tasks[:i], tasks[i+1:]...)
			return
		}
	}
}

// SubmitResult submits a completed task result
func (ta *TaskAssigner) SubmitResult(
	ctx context.Context,
	taskID types.Hash,
	minerAddr types.Address,
	gradientHash types.Hash,
	qualityScore float64,
	currentBlock uint64,
) error {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	assignment, exists := ta.assignments[taskID]
	if !exists {
		return errors.New("assignment not found")
	}

	if assignment.AssignedTo != minerAddr {
		return errors.New("not assigned to this miner")
	}

	if currentBlock > assignment.Deadline {
		assignment.Status = StatusExpired
		return errors.New("deadline expired")
	}

	// Validate quality score
	if qualityScore <= 0 || qualityScore > 1 {
		return ErrInvalidGradient
	}

	assignment.GradientHash = gradientHash
	assignment.QualityScore = qualityScore
	assignment.CompletedAt = currentBlock
	assignment.Status = StatusCompleted

	// Record contribution to model
	contrib := &Contribution{
		Contributor:  minerAddr,
		ModelID:      assignment.Task.ModelID,
		Compute:      uint64(assignment.Task.BatchEnd - assignment.Task.BatchStart),
		Quality:      qualityScore,
		GradientHash: gradientHash,
		Timestamp:    currentBlock,
	}

	return ta.registry.RecordContribution(ctx, contrib)
}

// UpdateVRFSeed updates the VRF seed for task selection
func (ta *TaskAssigner) UpdateVRFSeed(blockHash types.Hash) {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	data := append(ta.vrfSeed[:], blockHash[:]...)
	hash := sha256.Sum256(data)
	copy(ta.vrfSeed[:], hash[:])
}

// CleanupExpired cleans up expired assignments
func (ta *TaskAssigner) CleanupExpired(currentBlock uint64) int {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	cleaned := 0
	for taskID, assignment := range ta.assignments {
		if assignment.Status == StatusAssigned && currentBlock > assignment.Deadline {
			assignment.Status = StatusExpired
			// Return task to pool
			ta.tasks[assignment.Task.ModelID] = append(ta.tasks[assignment.Task.ModelID], assignment.Task)
			delete(ta.assignments, taskID)
			cleaned++
		}
	}

	return cleaned
}

// GetPendingTasks returns pending tasks for a model
func (ta *TaskAssigner) GetPendingTasks(modelID types.Hash) []*TrainingTask {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	return ta.tasks[modelID]
}

// GetAssignment returns an assignment by task ID
func (ta *TaskAssigner) GetAssignment(taskID types.Hash) *TaskAssignment {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	return ta.assignments[taskID]
}

func uint32ToBytes(v uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
	return b
}
