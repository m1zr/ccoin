// Package zkp implements nullifier generation and tracking.
package zkp

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Nullifier errors
var (
	ErrNullifierSpent   = errors.New("nullifier already spent")
	ErrNullifierInvalid = errors.New("invalid nullifier")
)

// NullifierSet tracks spent nullifiers to prevent double-spending
type NullifierSet struct {
	mu sync.RWMutex

	// In-memory cache of recent nullifiers
	cache map[types.Hash]struct{}

	// Persistent storage
	store NullifierStore

	// Cache size limit
	maxCacheSize int
}

// NullifierStore defines the interface for persistent nullifier storage
type NullifierStore interface {
	// HasNullifier checks if a nullifier has been spent
	HasNullifier(ctx context.Context, nullifier types.Hash) (bool, error)

	// AddNullifier marks a nullifier as spent
	AddNullifier(ctx context.Context, nullifier types.Hash, txHash types.Hash, blockHeight uint64) error

	// GetNullifierInfo returns information about a spent nullifier
	GetNullifierInfo(ctx context.Context, nullifier types.Hash) (*NullifierInfo, error)
}

// NullifierInfo contains information about a spent nullifier
type NullifierInfo struct {
	Nullifier   types.Hash
	TxHash      types.Hash
	BlockHeight uint64
	SpentAt     uint64
}

// NullifierConfig holds configuration for the nullifier set
type NullifierConfig struct {
	MaxCacheSize int
}

// DefaultNullifierConfig returns default configuration
func DefaultNullifierConfig() *NullifierConfig {
	return &NullifierConfig{
		MaxCacheSize: 100000,
	}
}

// NewNullifierSet creates a new nullifier set
func NewNullifierSet(store NullifierStore, cfg *NullifierConfig) *NullifierSet {
	if cfg == nil {
		cfg = DefaultNullifierConfig()
	}

	return &NullifierSet{
		cache:        make(map[types.Hash]struct{}),
		store:        store,
		maxCacheSize: cfg.MaxCacheSize,
	}
}

// IsSpent checks if a nullifier has already been spent
func (ns *NullifierSet) IsSpent(ctx context.Context, nullifier types.Hash) (bool, error) {
	// Check cache first
	ns.mu.RLock()
	_, inCache := ns.cache[nullifier]
	ns.mu.RUnlock()

	if inCache {
		return true, nil
	}

	// Check persistent storage
	return ns.store.HasNullifier(ctx, nullifier)
}

// MarkSpent marks a nullifier as spent
func (ns *NullifierSet) MarkSpent(ctx context.Context, nullifier types.Hash, txHash types.Hash, blockHeight uint64) error {
	// Check if already spent
	spent, err := ns.IsSpent(ctx, nullifier)
	if err != nil {
		return err
	}
	if spent {
		return ErrNullifierSpent
	}

	// Add to persistent storage
	if err := ns.store.AddNullifier(ctx, nullifier, txHash, blockHeight); err != nil {
		return err
	}

	// Add to cache
	ns.mu.Lock()
	ns.cache[nullifier] = struct{}{}

	// Evict if cache is too large (simple random eviction)
	if len(ns.cache) > ns.maxCacheSize {
		// Remove first item found (not truly random but simple)
		for k := range ns.cache {
			delete(ns.cache, k)
			break
		}
	}
	ns.mu.Unlock()

	return nil
}

// BatchCheck checks multiple nullifiers at once
func (ns *NullifierSet) BatchCheck(ctx context.Context, nullifiers []types.Hash) ([]bool, error) {
	results := make([]bool, len(nullifiers))

	for i, nullifier := range nullifiers {
		spent, err := ns.IsSpent(ctx, nullifier)
		if err != nil {
			return nil, err
		}
		results[i] = spent
	}

	return results, nil
}

// DeriveNullifier derives a nullifier from a spending key and note
// nullifier = H(spending_key || commitment || position)
func DeriveNullifier(spendingKey []byte, commitment types.Hash, position uint64) types.Hash {
	hasher := sha256.New()
	hasher.Write(spendingKey)
	hasher.Write(commitment[:])
	hasher.Write(uint64ToBytes(position))

	var nullifier types.Hash
	copy(nullifier[:], hasher.Sum(nil))
	return nullifier
}

// DeriveNullifierFromNote derives nullifier from note components
func DeriveNullifierFromNote(
	spendingKey []byte,
	value uint64,
	blinder []byte,
	address types.Address,
	position uint64,
) types.Hash {
	// First compute the note commitment
	hasher := sha256.New()
	hasher.Write(uint64ToBytes(value))
	hasher.Write(blinder)
	hasher.Write(address[:])

	var commitment types.Hash
	copy(commitment[:], hasher.Sum(nil))

	// Then derive nullifier
	return DeriveNullifier(spendingKey, commitment, position)
}

// NullifierDerivationKey derives the nullifier key from a spending key
func NullifierDerivationKey(spendingKey []byte) []byte {
	hasher := sha256.New()
	hasher.Write([]byte("CCOIN_NULLIFIER_KEY"))
	hasher.Write(spendingKey)
	return hasher.Sum(nil)
}

// uint64ToBytes converts uint64 to big-endian bytes
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

// InMemoryNullifierStore is a simple in-memory implementation for testing
type InMemoryNullifierStore struct {
	mu         sync.RWMutex
	nullifiers map[types.Hash]*NullifierInfo
}

// NewInMemoryNullifierStore creates a new in-memory nullifier store
func NewInMemoryNullifierStore() *InMemoryNullifierStore {
	return &InMemoryNullifierStore{
		nullifiers: make(map[types.Hash]*NullifierInfo),
	}
}

// HasNullifier checks if a nullifier exists
func (s *InMemoryNullifierStore) HasNullifier(ctx context.Context, nullifier types.Hash) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.nullifiers[nullifier]
	return exists, nil
}

// AddNullifier adds a nullifier
func (s *InMemoryNullifierStore) AddNullifier(ctx context.Context, nullifier types.Hash, txHash types.Hash, blockHeight uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.nullifiers[nullifier]; exists {
		return ErrNullifierSpent
	}

	s.nullifiers[nullifier] = &NullifierInfo{
		Nullifier:   nullifier,
		TxHash:      txHash,
		BlockHeight: blockHeight,
	}
	return nil
}

// GetNullifierInfo returns info about a nullifier
func (s *InMemoryNullifierStore) GetNullifierInfo(ctx context.Context, nullifier types.Hash) (*NullifierInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.nullifiers[nullifier]
	if !exists {
		return nil, ErrNullifierInvalid
	}
	return info, nil
}

// Size returns the number of nullifiers
func (s *InMemoryNullifierStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.nullifiers)
}
