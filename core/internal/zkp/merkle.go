// Package zkp implements Merkle trees for commitment accumulation.
package zkp

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Merkle tree errors
var (
	ErrTreeFull        = errors.New("merkle tree is full")
	ErrLeafNotFound    = errors.New("leaf not found in tree")
	ErrInvalidPath     = errors.New("invalid merkle path")
	ErrInvalidPosition = errors.New("invalid position")
)

// TreeDepth is the fixed depth of the commitment tree
const TreeDepth = 32

// CommitmentTree is a Merkle tree for storing note commitments
type CommitmentTree struct {
	mu sync.RWMutex

	// Tree depth
	depth int

	// Current number of leaves
	size uint64

	// Root hash
	root types.Hash

	// Store for persistence
	store TreeStore

	// Cache of tree nodes
	nodeCache map[uint64]types.Hash
}

// TreeStore defines the interface for Merkle tree persistence
type TreeStore interface {
	// GetNode retrieves a node by position
	GetNode(ctx context.Context, level, index uint64) (types.Hash, error)

	// SetNode stores a node
	SetNode(ctx context.Context, level, index uint64, hash types.Hash) error

	// GetRoot returns the current root
	GetRoot(ctx context.Context) (types.Hash, error)

	// SetRoot updates the root
	SetRoot(ctx context.Context, root types.Hash) error

	// GetSize returns the number of leaves
	GetSize(ctx context.Context) (uint64, error)

	// SetSize updates the leaf count
	SetSize(ctx context.Context, size uint64) error
}

// MerklePath represents a path from a leaf to the root
type MerklePath struct {
	// Siblings are the sibling hashes along the path
	Siblings []types.Hash

	// PathBits indicates left (0) or right (1) at each level
	PathBits []bool

	// LeafPosition is the position of the leaf
	LeafPosition uint64
}

// NewCommitmentTree creates a new commitment tree
func NewCommitmentTree(store TreeStore, depth int) *CommitmentTree {
	if depth == 0 {
		depth = TreeDepth
	}

	return &CommitmentTree{
		depth:     depth,
		nodeCache: make(map[uint64]types.Hash),
		store:     store,
	}
}

// Initialize loads the tree state from storage
func (ct *CommitmentTree) Initialize(ctx context.Context) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	root, err := ct.store.GetRoot(ctx)
	if err != nil {
		// Start with empty tree
		ct.root = ct.emptyRoot()
		ct.size = 0
		return nil
	}

	ct.root = root

	size, err := ct.store.GetSize(ctx)
	if err != nil {
		ct.size = 0
	} else {
		ct.size = size
	}

	return nil
}

// AddCommitment adds a new commitment to the tree
func (ct *CommitmentTree) AddCommitment(ctx context.Context, commitment types.Hash) (uint64, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	maxLeaves := uint64(1) << ct.depth
	if ct.size >= maxLeaves {
		return 0, ErrTreeFull
	}

	position := ct.size
	ct.size++

	// Insert at leaf level
	if err := ct.store.SetNode(ctx, 0, position, commitment); err != nil {
		ct.size--
		return 0, err
	}

	// Update path to root
	currentHash := commitment
	currentIndex := position

	for level := 0; level < ct.depth; level++ {
		siblingIndex := currentIndex ^ 1 // XOR with 1 to get sibling
		siblingHash, err := ct.store.GetNode(ctx, uint64(level), siblingIndex)
		if err != nil {
			// Sibling doesn't exist, use empty hash
			siblingHash = ct.emptyHash(level)
		}

		var newHash types.Hash
		if currentIndex%2 == 0 {
			// Current is left child
			newHash = hashPair(currentHash, siblingHash)
		} else {
			// Current is right child
			newHash = hashPair(siblingHash, currentHash)
		}

		// Move to parent
		currentIndex /= 2
		currentHash = newHash

		// Store the new hash at parent level
		if err := ct.store.SetNode(ctx, uint64(level+1), currentIndex, currentHash); err != nil {
			return 0, err
		}
	}

	ct.root = currentHash
	if err := ct.store.SetRoot(ctx, ct.root); err != nil {
		return 0, err
	}
	if err := ct.store.SetSize(ctx, ct.size); err != nil {
		return 0, err
	}

	return position, nil
}

// GetRoot returns the current Merkle root
func (ct *CommitmentTree) GetRoot() types.Hash {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.root
}

// GetSize returns the number of commitments in the tree
func (ct *CommitmentTree) GetSize() uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.size
}

// GetPath returns the Merkle path for a given leaf position
func (ct *CommitmentTree) GetPath(ctx context.Context, position uint64) (*MerklePath, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if position >= ct.size {
		return nil, ErrInvalidPosition
	}

	siblings := make([]types.Hash, ct.depth)
	pathBits := make([]bool, ct.depth)

	currentIndex := position
	for level := 0; level < ct.depth; level++ {
		siblingIndex := currentIndex ^ 1
		siblingHash, err := ct.store.GetNode(ctx, uint64(level), siblingIndex)
		if err != nil {
			siblingHash = ct.emptyHash(level)
		}

		siblings[level] = siblingHash
		pathBits[level] = currentIndex%2 == 1 // true if right child

		currentIndex /= 2
	}

	return &MerklePath{
		Siblings:     siblings,
		PathBits:     pathBits,
		LeafPosition: position,
	}, nil
}

// VerifyPath verifies a Merkle path leads to the expected root
func (ct *CommitmentTree) VerifyPath(leaf types.Hash, path *MerklePath, expectedRoot types.Hash) bool {
	if len(path.Siblings) != ct.depth || len(path.PathBits) != ct.depth {
		return false
	}

	currentHash := leaf
	for i := 0; i < ct.depth; i++ {
		if path.PathBits[i] {
			// Current is right child
			currentHash = hashPair(path.Siblings[i], currentHash)
		} else {
			// Current is left child
			currentHash = hashPair(currentHash, path.Siblings[i])
		}
	}

	return currentHash == expectedRoot
}

// ContainsCommitment checks if a commitment exists in the tree
func (ct *CommitmentTree) ContainsCommitment(ctx context.Context, commitment types.Hash) (bool, uint64, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	for i := uint64(0); i < ct.size; i++ {
		leaf, err := ct.store.GetNode(ctx, 0, i)
		if err != nil {
			continue
		}
		if leaf == commitment {
			return true, i, nil
		}
	}

	return false, 0, nil
}

// emptyHash returns the empty hash for a given tree level
func (ct *CommitmentTree) emptyHash(level int) types.Hash {
	if level == 0 {
		return types.EmptyHash
	}

	// Recursively compute: H(empty[level-1] || empty[level-1])
	childEmpty := ct.emptyHash(level - 1)
	return hashPair(childEmpty, childEmpty)
}

// emptyRoot returns the root of an empty tree
func (ct *CommitmentTree) emptyRoot() types.Hash {
	return ct.emptyHash(ct.depth)
}

// hashPair hashes two hashes together
func hashPair(left, right types.Hash) types.Hash {
	hasher := sha256.New()
	hasher.Write(left[:])
	hasher.Write(right[:])

	var result types.Hash
	copy(result[:], hasher.Sum(nil))
	return result
}

// InMemoryTreeStore is a simple in-memory tree store for testing
type InMemoryTreeStore struct {
	mu    sync.RWMutex
	nodes map[uint64]map[uint64]types.Hash // level -> index -> hash
	root  types.Hash
	size  uint64
}

// NewInMemoryTreeStore creates a new in-memory tree store
func NewInMemoryTreeStore() *InMemoryTreeStore {
	return &InMemoryTreeStore{
		nodes: make(map[uint64]map[uint64]types.Hash),
	}
}

// GetNode retrieves a node
func (s *InMemoryTreeStore) GetNode(ctx context.Context, level, index uint64) (types.Hash, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	levelMap, exists := s.nodes[level]
	if !exists {
		return types.EmptyHash, ErrLeafNotFound
	}

	hash, exists := levelMap[index]
	if !exists {
		return types.EmptyHash, ErrLeafNotFound
	}

	return hash, nil
}

// SetNode stores a node
func (s *InMemoryTreeStore) SetNode(ctx context.Context, level, index uint64, hash types.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.nodes[level] == nil {
		s.nodes[level] = make(map[uint64]types.Hash)
	}
	s.nodes[level][index] = hash
	return nil
}

// GetRoot returns the root
func (s *InMemoryTreeStore) GetRoot(ctx context.Context) (types.Hash, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root, nil
}

// SetRoot sets the root
func (s *InMemoryTreeStore) SetRoot(ctx context.Context, root types.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root = root
	return nil
}

// GetSize returns the size
func (s *InMemoryTreeStore) GetSize(ctx context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size, nil
}

// SetSize sets the size
func (s *InMemoryTreeStore) SetSize(ctx context.Context, size uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.size = size
	return nil
}
