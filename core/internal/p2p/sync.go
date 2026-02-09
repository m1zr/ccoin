// Package p2p provides block synchronization functionality.
package p2p

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ccoin/core/internal/dag"
	"github.com/ccoin/core/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Sync errors
var (
	ErrNoSyncPeers    = errors.New("no peers available for sync")
	ErrSyncTimeout    = errors.New("sync timeout")
	ErrInvalidBlock   = errors.New("received invalid block")
	ErrOrphanReceived = errors.New("received orphan block")
)

// SyncManager handles blockchain synchronization
type SyncManager struct {
	mu sync.RWMutex

	node      *Node
	dag       *dag.DAG
	validator *dag.BlockValidator

	// Sync state
	syncing       bool
	syncTarget    uint64
	syncProgress  uint64
	lastSyncPeer  peer.ID

	// Pending blocks awaiting parents
	pending map[types.Hash]*types.Block

	// Request tracking
	pendingRequests map[types.Hash]time.Time
	requestTimeout  time.Duration

	// Config
	batchSize int
}

// SyncConfig holds synchronization configuration
type SyncConfig struct {
	BatchSize      int
	RequestTimeout time.Duration
}

// DefaultSyncConfig returns default sync configuration
func DefaultSyncConfig() *SyncConfig {
	return &SyncConfig{
		BatchSize:      100,
		RequestTimeout: 30 * time.Second,
	}
}

// NewSyncManager creates a new sync manager
func NewSyncManager(node *Node, d *dag.DAG, validator *dag.BlockValidator, cfg *SyncConfig) *SyncManager {
	if cfg == nil {
		cfg = DefaultSyncConfig()
	}

	return &SyncManager{
		node:            node,
		dag:             d,
		validator:       validator,
		pending:         make(map[types.Hash]*types.Block),
		pendingRequests: make(map[types.Hash]time.Time),
		requestTimeout:  cfg.RequestTimeout,
		batchSize:       cfg.BatchSize,
	}
}

// Start begins the sync process
func (sm *SyncManager) Start(ctx context.Context) error {
	// Find best peer to sync from
	bestPeer, bestHeight := sm.findBestPeer()
	if bestPeer == "" {
		return ErrNoSyncPeers
	}

	localHeight := sm.dag.GetHeight()
	if bestHeight <= localHeight {
		return nil // Already synced
	}

	sm.mu.Lock()
	sm.syncing = true
	sm.syncTarget = bestHeight
	sm.syncProgress = localHeight
	sm.lastSyncPeer = bestPeer
	sm.mu.Unlock()

	// Start sync loop
	go sm.syncLoop(ctx, bestPeer, localHeight, bestHeight)

	return nil
}

// syncLoop performs the main synchronization
func (sm *SyncManager) syncLoop(ctx context.Context, peerID peer.ID, start, target uint64) {
	defer func() {
		sm.mu.Lock()
		sm.syncing = false
		sm.mu.Unlock()
	}()

	current := start
	for current < target {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Request next batch
		endHeight := current + uint64(sm.batchSize)
		if endHeight > target {
			endHeight = target
		}

		// In production, this would send GetBlocks request and wait for response
		// For now, we simulate the flow
		time.Sleep(100 * time.Millisecond)

		sm.mu.Lock()
		sm.syncProgress = endHeight
		sm.mu.Unlock()

		current = endHeight
	}
}

// findBestPeer finds the peer with the highest block height
func (sm *SyncManager) findBestPeer() (peer.ID, uint64) {
	peers := sm.node.Peers()
	if len(peers) == 0 {
		return "", 0
	}

	var bestPeer peer.ID
	var bestHeight uint64

	for _, p := range peers {
		if p.Height > bestHeight {
			bestHeight = p.Height
			bestPeer = p.ID
		}
	}

	return bestPeer, bestHeight
}

// HandleBlock processes an incoming block
func (sm *SyncManager) HandleBlock(ctx context.Context, block *types.Block) error {
	// Validate block
	if err := sm.validator.ValidateBlock(ctx, block); err != nil {
		return err
	}

	// Try to add to DAG
	if err := sm.dag.AddBlock(ctx, block); err != nil {
		if errors.Is(err, dag.ErrOrphanBlock) {
			// Store in pending and request parents
			sm.addPending(block)
			sm.requestParents(ctx, block.Header.Parents)
			return nil
		}
		return err
	}

	// Block added successfully, check if any pending blocks can now be added
	sm.processPending(ctx)

	// Broadcast to peers
	data, err := EncodeBlock(block)
	if err != nil {
		return err
	}
	return sm.node.BroadcastBlock(data)
}

// addPending adds a block to the pending queue
func (sm *SyncManager) addPending(block *types.Block) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pending[block.Header.Hash] = block
}

// processPending tries to add pending blocks to the DAG
func (sm *SyncManager) processPending(ctx context.Context) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Keep trying until no progress is made
	for {
		progress := false

		for hash, block := range sm.pending {
			// Check if all parents are now in DAG
			allParentsExist := true
			for _, parent := range block.Header.Parents {
				if _, err := sm.dag.GetBlock(ctx, parent); err != nil {
					allParentsExist = false
					break
				}
			}

			if allParentsExist {
				if err := sm.dag.AddBlock(ctx, block); err == nil {
					delete(sm.pending, hash)
					progress = true
				}
			}
		}

		if !progress {
			break
		}
	}
}

// requestParents requests missing parent blocks
func (sm *SyncManager) requestParents(ctx context.Context, parents []types.Hash) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, parent := range parents {
		// Check if already requested
		if _, exists := sm.pendingRequests[parent]; exists {
			continue
		}

		// Check if already in DAG
		if _, err := sm.dag.GetBlock(ctx, parent); err == nil {
			continue
		}

		// Mark as requested
		sm.pendingRequests[parent] = time.Now()

		// In production, send GetBlock request to peers
		// For now, just mark it
	}
}

// IsSyncing returns whether sync is in progress
func (sm *SyncManager) IsSyncing() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.syncing
}

// Progress returns sync progress
func (sm *SyncManager) Progress() (current, target uint64) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.syncProgress, sm.syncTarget
}

// PendingCount returns the number of pending blocks
func (sm *SyncManager) PendingCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.pending)
}

// CleanupStale removes stale pending requests
func (sm *SyncManager) CleanupStale() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-sm.requestTimeout)
	for hash, requestTime := range sm.pendingRequests {
		if requestTime.Before(cutoff) {
			delete(sm.pendingRequests, hash)
		}
	}
}
