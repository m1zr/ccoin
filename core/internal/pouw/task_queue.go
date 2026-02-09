// Package pouw implements the task queue for PoUW mining.
package pouw

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/ccoin/core/pkg/types"
)

// TaskQueue errors
var (
	ErrNoAvailableTasks = errors.New("no tasks available")
	ErrTaskNotFound     = errors.New("task not found")
	ErrTaskAlreadyAssigned = errors.New("task already assigned")
	ErrMinerNotEligible = errors.New("miner not eligible for task")
)

// TaskQueue manages the on-chain task assignment queue
type TaskQueue struct {
	mu sync.RWMutex

	// Tasks indexed by ID
	tasks map[types.Hash]*types.Task

	// Available tasks (not assigned)
	available []*types.Task

	// Tasks by status
	pending   map[types.Hash]*types.Task
	active    map[types.Hash]*types.Task
	completed map[types.Hash]*types.Task

	// VRF seed for deterministic task assignment
	vrfSeed types.Hash

	// Task parameters
	taskTimeout time.Duration
}

// TaskQueueConfig holds task queue configuration
type TaskQueueConfig struct {
	TaskTimeout time.Duration
}

// DefaultTaskQueueConfig returns default configuration
func DefaultTaskQueueConfig() *TaskQueueConfig {
	return &TaskQueueConfig{
		TaskTimeout: 10 * time.Minute,
	}
}

// NewTaskQueue creates a new task queue
func NewTaskQueue(cfg *TaskQueueConfig) *TaskQueue {
	if cfg == nil {
		cfg = DefaultTaskQueueConfig()
	}

	return &TaskQueue{
		tasks:       make(map[types.Hash]*types.Task),
		available:   make([]*types.Task, 0),
		pending:     make(map[types.Hash]*types.Task),
		active:      make(map[types.Hash]*types.Task),
		completed:   make(map[types.Hash]*types.Task),
		taskTimeout: cfg.TaskTimeout,
	}
}

// AddTask adds a new task to the queue
func (q *TaskQueue) AddTask(ctx context.Context, task *types.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.tasks[task.TaskID]; exists {
		return errors.New("task already exists")
	}

	task.Status = types.TaskStatusPending
	q.tasks[task.TaskID] = task
	q.pending[task.TaskID] = task
	q.available = append(q.available, task)

	return nil
}

// GetNextTask assigns and returns the next available task for a miner
func (q *TaskQueue) GetNextTask(ctx context.Context, minerAddr types.Address) (*types.Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.available) == 0 {
		return nil, ErrNoAvailableTasks
	}

	// Use VRF-like selection based on miner address
	taskIndex := q.selectTaskForMiner(minerAddr)
	if taskIndex >= len(q.available) {
		taskIndex = 0
	}

	task := q.available[taskIndex]

	// Assign task
	task.Status = types.TaskStatusAssigned
	task.AssignedMiner = minerAddr
	task.AssignedAt = uint64(time.Now().Unix())
	task.Deadline = task.AssignedAt + uint64(q.taskTimeout.Seconds())

	// Move from available to active
	q.available = append(q.available[:taskIndex], q.available[taskIndex+1:]...)
	delete(q.pending, task.TaskID)
	q.active[task.TaskID] = task

	return task, nil
}

// selectTaskForMiner uses deterministic selection based on miner address
func (q *TaskQueue) selectTaskForMiner(minerAddr types.Address) int {
	// VRF-like selection: H(seed || miner_address) mod len(available)
	data := append(q.vrfSeed[:], minerAddr[:]...)
	hash := sha256.Sum256(data)

	// Use first 8 bytes as index
	index := uint64(hash[0]) | uint64(hash[1])<<8 | uint64(hash[2])<<16 | uint64(hash[3])<<24
	return int(index % uint64(len(q.available)))
}

// CompleteTask marks a task as completed
func (q *TaskQueue) CompleteTask(ctx context.Context, taskID types.Hash, result *types.GradientResult) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, exists := q.active[taskID]
	if !exists {
		return ErrTaskNotFound
	}

	task.Status = types.TaskStatusCompleted
	task.CompletedAt = uint64(time.Now().Unix())

	delete(q.active, taskID)
	q.completed[taskID] = task

	return nil
}

// FailTask marks a task as failed
func (q *TaskQueue) FailTask(ctx context.Context, taskID types.Hash, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, exists := q.active[taskID]
	if !exists {
		return ErrTaskNotFound
	}

	task.Status = types.TaskStatusFailed
	// Store failure reason if needed

	// Return task to available pool for reassignment
	delete(q.active, taskID)
	task.Status = types.TaskStatusPending
	task.AssignedMiner = types.Address{}
	task.AssignedAt = 0
	task.Deadline = 0
	q.pending[taskID] = task
	q.available = append(q.available, task)

	return nil
}

// CleanupExpired moves expired tasks back to the available pool
func (q *TaskQueue) CleanupExpired(ctx context.Context) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := uint64(time.Now().Unix())
	expired := 0

	for taskID, task := range q.active {
		if task.Deadline > 0 && now > task.Deadline {
			// Return to available pool
			delete(q.active, taskID)
			task.Status = types.TaskStatusPending
			task.AssignedMiner = types.Address{}
			task.AssignedAt = 0
			task.Deadline = 0
			q.pending[taskID] = task
			q.available = append(q.available, task)
			expired++
		}
	}

	return expired
}

// GetTask retrieves a task by ID
func (q *TaskQueue) GetTask(taskID types.Hash) (*types.Task, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if task, exists := q.tasks[taskID]; exists {
		return task, nil
	}
	return nil, ErrTaskNotFound
}

// GetMinerTasks returns all tasks assigned to a miner
func (q *TaskQueue) GetMinerTasks(minerAddr types.Address) []*types.Task {
	q.mu.RLock()
	defer q.mu.RUnlock()

	tasks := make([]*types.Task, 0)
	for _, task := range q.active {
		if task.AssignedMiner == minerAddr {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// Stats returns task queue statistics
type QueueStats struct {
	TotalTasks    int
	AvailableTasks int
	ActiveTasks   int
	CompletedTasks int
}

// GetStats returns queue statistics
func (q *TaskQueue) GetStats() *QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return &QueueStats{
		TotalTasks:     len(q.tasks),
		AvailableTasks: len(q.available),
		ActiveTasks:    len(q.active),
		CompletedTasks: len(q.completed),
	}
}

// UpdateVRFSeed updates the VRF seed (called at each block)
func (q *TaskQueue) UpdateVRFSeed(blockHash types.Hash) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Chain VRF seed with block hash
	data := append(q.vrfSeed[:], blockHash[:]...)
	hash := sha256.Sum256(data)
	copy(q.vrfSeed[:], hash[:])
}

// CreateTask creates a new task for a model
func CreateTask(modelID types.Hash, datasetCID string, batchStart, batchSize uint32, reward uint64) *types.Task {
	task := &types.Task{
		ModelID:    modelID,
		DatasetCID: datasetCID,
		BatchStart: batchStart,
		BatchSize:  batchSize,
		Status:     types.TaskStatusPending,
		Reward:     reward,
	}

	// Generate task ID from content
	data := append(modelID[:], []byte(datasetCID)...)
	data = append(data, uint32ToBytes(batchStart)...)
	data = append(data, uint32ToBytes(batchSize)...)
	hash := sha256.Sum256(data)
	copy(task.TaskID[:], hash[:])

	// Generate objective hash
	copy(task.Objective[:], hash[16:])

	return task
}

func uint32ToBytes(v uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
	return b
}
