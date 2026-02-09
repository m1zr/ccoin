// Package dag implements the BlockDAG data structure and ordering algorithm.
package dag

import (
	"context"
	"errors"
	"math/big"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Common errors
var (
	ErrBlockNotFound    = errors.New("block not found")
	ErrInvalidParent    = errors.New("invalid parent reference")
	ErrDuplicateBlock   = errors.New("block already exists")
	ErrInvalidTimestamp = errors.New("invalid timestamp")
	ErrOrphanBlock      = errors.New("orphan block (missing parents)")
)

// DAG represents the BlockDAG structure
type DAG struct {
	mu sync.RWMutex

	// Store is the persistent storage backend
	store Store

	// Cache for recently accessed blocks
	cache *BlockCache

	// Index of block children for efficient traversal
	children map[types.Hash][]types.Hash

	// Genesis block hash
	genesisHash types.Hash

	// Current tips (blocks with no children)
	tips map[types.Hash]struct{}

	// Main chain tip (highest cumulative score)
	mainChainTip types.Hash

	// Current height (max height in DAG)
	height uint64

	// Current epoch
	epoch uint64
}

// Store defines the interface for DAG persistent storage
type Store interface {
	// GetBlock retrieves a block by hash
	GetBlock(ctx context.Context, hash types.Hash) (*types.Block, error)

	// GetBlockHeader retrieves a block header by hash
	GetBlockHeader(ctx context.Context, hash types.Hash) (*types.BlockHeader, error)

	// SaveBlock saves a block to storage
	SaveBlock(ctx context.Context, block *types.Block) error

	// GetBlocksByHeight returns all blocks at a given height
	GetBlocksByHeight(ctx context.Context, height uint64) ([]*types.BlockHeader, error)

	// GetChildren returns child block hashes
	GetChildren(ctx context.Context, hash types.Hash) ([]types.Hash, error)

	// GetMainChain returns the main chain blocks in order
	GetMainChain(ctx context.Context, fromHeight, toHeight uint64) ([]*types.BlockHeader, error)

	// UpdateMainChain marks blocks as on/off main chain
	UpdateMainChain(ctx context.Context, onChain, offChain []types.Hash) error

	// GetTips returns current DAG tips
	GetTips(ctx context.Context) ([]types.Hash, error)
}

// Config holds DAG configuration
type Config struct {
	// CacheSize is the number of blocks to cache in memory
	CacheSize int

	// MaxParents is the maximum number of parents per block
	MaxParents int
}

// DefaultConfig returns the default DAG configuration
func DefaultConfig() *Config {
	return &Config{
		CacheSize:  10000,
		MaxParents: types.MaxParents,
	}
}

// NewDAG creates a new DAG instance
func NewDAG(store Store, config *Config) *DAG {
	if config == nil {
		config = DefaultConfig()
	}

	return &DAG{
		store:    store,
		cache:    NewBlockCache(config.CacheSize),
		children: make(map[types.Hash][]types.Hash),
		tips:     make(map[types.Hash]struct{}),
	}
}

// Initialize loads the DAG state from storage
func (d *DAG) Initialize(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Load tips
	tips, err := d.store.GetTips(ctx)
	if err != nil {
		return err
	}

	for _, tip := range tips {
		d.tips[tip] = struct{}{}
	}

	// Find main chain tip (highest cumulative score)
	var maxScore *big.Float
	for tip := range d.tips {
		header, err := d.store.GetBlockHeader(ctx, tip)
		if err != nil {
			continue
		}

		if maxScore == nil || header.CumulativeScore.Cmp(maxScore) > 0 {
			maxScore = header.CumulativeScore
			d.mainChainTip = tip
			d.height = header.Height
		}
	}

	d.epoch = d.height / types.EpochLength

	return nil
}

// AddBlock adds a new block to the DAG
func (d *DAG) AddBlock(ctx context.Context, block *types.Block) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if block already exists
	if _, err := d.getBlockHeader(ctx, block.Header.Hash); err == nil {
		return ErrDuplicateBlock
	}

	// Validate parents exist
	for _, parentHash := range block.Header.Parents {
		if _, err := d.getBlockHeader(ctx, parentHash); err != nil {
			return ErrOrphanBlock
		}
	}

	// Calculate cumulative score
	block.Header.CumulativeScore = d.calculateCumulativeScore(ctx, block.Header)

	// Save block
	if err := d.store.SaveBlock(ctx, block); err != nil {
		return err
	}

	// Update cache
	d.cache.Add(block)

	// Update children index
	for _, parentHash := range block.Header.Parents {
		d.children[parentHash] = append(d.children[parentHash], block.Header.Hash)

		// Parent is no longer a tip
		delete(d.tips, parentHash)
	}

	// New block is a tip
	d.tips[block.Header.Hash] = struct{}{}

	// Update main chain if necessary
	if block.Header.CumulativeScore.Cmp(d.getMainChainScore(ctx)) > 0 {
		if err := d.updateMainChain(ctx, block.Header.Hash); err != nil {
			return err
		}
	}

	// Update height if necessary
	if block.Header.Height > d.height {
		d.height = block.Header.Height
		d.epoch = d.height / types.EpochLength
	}

	return nil
}

// calculateCumulativeScore computes S(B) = Work(B) * Rep(m) + sum(S(parents))
func (d *DAG) calculateCumulativeScore(ctx context.Context, header *types.BlockHeader) *big.Float {
	// Work * Reputation
	work := new(big.Float).SetInt(header.Work())
	rep := big.NewFloat(header.ReputationScore)
	score := new(big.Float).Mul(work, rep)

	// Add parent scores (we use max parent score for simplicity in DAG)
	var maxParentScore *big.Float
	for _, parentHash := range header.Parents {
		parentHeader, err := d.getBlockHeader(ctx, parentHash)
		if err != nil {
			continue
		}
		if maxParentScore == nil || parentHeader.CumulativeScore.Cmp(maxParentScore) > 0 {
			maxParentScore = parentHeader.CumulativeScore
		}
	}

	if maxParentScore != nil {
		score.Add(score, maxParentScore)
	}

	return score
}

// getMainChainScore returns the current main chain tip's cumulative score
func (d *DAG) getMainChainScore(ctx context.Context) *big.Float {
	if d.mainChainTip.IsEmpty() {
		return big.NewFloat(0)
	}

	header, err := d.getBlockHeader(ctx, d.mainChainTip)
	if err != nil {
		return big.NewFloat(0)
	}

	return header.CumulativeScore
}

// updateMainChain updates the main chain to end at the new tip
func (d *DAG) updateMainChain(ctx context.Context, newTip types.Hash) error {
	// Find common ancestor and update on/off chain status
	oldPath := d.getPathToGenesis(ctx, d.mainChainTip)
	newPath := d.getPathToGenesis(ctx, newTip)

	// Find divergence point
	oldSet := make(map[types.Hash]struct{})
	for _, h := range oldPath {
		oldSet[h] = struct{}{}
	}

	var onChain, offChain []types.Hash
	for _, h := range newPath {
		if _, exists := oldSet[h]; !exists {
			onChain = append(onChain, h)
		}
	}

	newSet := make(map[types.Hash]struct{})
	for _, h := range newPath {
		newSet[h] = struct{}{}
	}

	for _, h := range oldPath {
		if _, exists := newSet[h]; !exists {
			offChain = append(offChain, h)
		}
	}

	if err := d.store.UpdateMainChain(ctx, onChain, offChain); err != nil {
		return err
	}

	d.mainChainTip = newTip
	return nil
}

// getPathToGenesis returns the path from a block to genesis following highest-score parents
func (d *DAG) getPathToGenesis(ctx context.Context, hash types.Hash) []types.Hash {
	path := []types.Hash{}
	current := hash

	for !current.IsEmpty() {
		path = append(path, current)

		header, err := d.getBlockHeader(ctx, current)
		if err != nil || len(header.Parents) == 0 {
			break
		}

		// Follow the highest-score parent
		var bestParent types.Hash
		var bestScore *big.Float

		for _, parentHash := range header.Parents {
			parentHeader, err := d.getBlockHeader(ctx, parentHash)
			if err != nil {
				continue
			}
			if bestScore == nil || parentHeader.CumulativeScore.Cmp(bestScore) > 0 {
				bestScore = parentHeader.CumulativeScore
				bestParent = parentHash
			}
		}

		current = bestParent
	}

	return path
}

// getBlockHeader retrieves a block header, checking cache first
func (d *DAG) getBlockHeader(ctx context.Context, hash types.Hash) (*types.BlockHeader, error) {
	// Check cache
	if block := d.cache.Get(hash); block != nil {
		return block.Header, nil
	}

	// Load from storage
	return d.store.GetBlockHeader(ctx, hash)
}

// GetBlock retrieves a block by hash
func (d *DAG) GetBlock(ctx context.Context, hash types.Hash) (*types.Block, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Check cache
	if block := d.cache.Get(hash); block != nil {
		return block, nil
	}

	return d.store.GetBlock(ctx, hash)
}

// GetTips returns the current DAG tips
func (d *DAG) GetTips() []types.Hash {
	d.mu.RLock()
	defer d.mu.RUnlock()

	tips := make([]types.Hash, 0, len(d.tips))
	for tip := range d.tips {
		tips = append(tips, tip)
	}
	return tips
}

// GetMainChainTip returns the current main chain tip
func (d *DAG) GetMainChainTip() types.Hash {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.mainChainTip
}

// GetHeight returns the current DAG height
func (d *DAG) GetHeight() uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.height
}

// GetEpoch returns the current epoch
func (d *DAG) GetEpoch() uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.epoch
}

// SelectParents selects optimal parents for a new block
func (d *DAG) SelectParents(ctx context.Context, maxParents int) ([]types.Hash, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.tips) == 0 {
		return nil, nil // Genesis block
	}

	// Select tips with highest cumulative scores
	type tipScore struct {
		hash  types.Hash
		score *big.Float
	}

	tips := make([]tipScore, 0, len(d.tips))
	for tip := range d.tips {
		header, err := d.getBlockHeader(ctx, tip)
		if err != nil {
			continue
		}
		tips = append(tips, tipScore{hash: tip, score: header.CumulativeScore})
	}

	// Sort by score (descending)
	for i := 0; i < len(tips)-1; i++ {
		for j := i + 1; j < len(tips); j++ {
			if tips[j].score.Cmp(tips[i].score) > 0 {
				tips[i], tips[j] = tips[j], tips[i]
			}
		}
	}

	// Take top maxParents
	result := make([]types.Hash, 0, maxParents)
	for i := 0; i < len(tips) && i < maxParents; i++ {
		result = append(result, tips[i].hash)
	}

	return result, nil
}

// BlockCache is a simple LRU cache for blocks
type BlockCache struct {
	mu      sync.RWMutex
	blocks  map[types.Hash]*types.Block
	order   []types.Hash
	maxSize int
}

// NewBlockCache creates a new block cache
func NewBlockCache(maxSize int) *BlockCache {
	return &BlockCache{
		blocks:  make(map[types.Hash]*types.Block),
		order:   make([]types.Hash, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add adds a block to the cache
func (c *BlockCache) Add(block *types.Block) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := block.Header.Hash

	// Already in cache
	if _, exists := c.blocks[hash]; exists {
		return
	}

	// Evict oldest if at capacity
	if len(c.order) >= c.maxSize {
		oldest := c.order[0]
		delete(c.blocks, oldest)
		c.order = c.order[1:]
	}

	c.blocks[hash] = block
	c.order = append(c.order, hash)
}

// Get retrieves a block from the cache
func (c *BlockCache) Get(hash types.Hash) *types.Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.blocks[hash]
}
