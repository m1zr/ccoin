// Package mempool implements the transaction memory pool.
package mempool

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Mempool errors
var (
	ErrPoolFull         = errors.New("mempool is full")
	ErrTxAlreadyExists  = errors.New("transaction already in mempool")
	ErrTxExpired        = errors.New("transaction expired")
	ErrInsufficientFee  = errors.New("insufficient transaction fee")
	ErrDoubleSpend      = errors.New("nullifier already spent")
	ErrInvalidProof     = errors.New("invalid zk-SNARK proof")
)

// Mempool manages pending transactions
type Mempool struct {
	mu sync.RWMutex

	// Transactions indexed by hash
	txs map[types.Hash]*MempoolTx

	// Priority queue for transaction ordering
	queue []*MempoolTx

	// Nullifier index for double-spend prevention
	nullifiers map[types.Hash]types.Hash // nullifier -> tx hash

	// Config
	maxSize     int
	minFee      uint64
	maxTxPerBlock int
}

// MempoolTx wraps a transaction with mempool metadata
type MempoolTx struct {
	Tx        *types.Transaction
	AddedAt   uint64
	Priority  float64 // fee / size
	Size      int
	Validated bool
}

// Config holds mempool configuration
type Config struct {
	MaxSize       int
	MinFee        uint64
	MaxTxPerBlock int
}

// DefaultConfig returns default mempool configuration
func DefaultConfig() *Config {
	return &Config{
		MaxSize:       10000,
		MinFee:        1,
		MaxTxPerBlock: 1000,
	}
}

// NewMempool creates a new transaction mempool
func NewMempool(cfg *Config) *Mempool {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	return &Mempool{
		txs:         make(map[types.Hash]*MempoolTx),
		queue:       make([]*MempoolTx, 0),
		nullifiers:  make(map[types.Hash]types.Hash),
		maxSize:     cfg.MaxSize,
		minFee:      cfg.MinFee,
		maxTxPerBlock: cfg.MaxTxPerBlock,
	}
}

// Add adds a transaction to the mempool
func (m *Mempool) Add(tx *types.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.txs[tx.TxHash]; exists {
		return ErrTxAlreadyExists
	}

	// Check mempool size
	if len(m.txs) >= m.maxSize {
		// Try to evict low-priority transactions
		if !m.evictLowestPriority(tx.Fee) {
			return ErrPoolFull
		}
	}

	// Check minimum fee
	if tx.Fee < m.minFee {
		return ErrInsufficientFee
	}

	// Check for double-spend (nullifier already in pool)
	for _, nullifier := range tx.Nullifiers {
		if existingTx, exists := m.nullifiers[nullifier]; exists {
			return errors.New("nullifier conflicts with tx " + existingTx.String())
		}
	}

	// Calculate priority (fee rate)
	size := estimateTxSize(tx)
	priority := float64(tx.Fee) / float64(size)

	mpt := &MempoolTx{
		Tx:        tx,
		AddedAt:   uint64(currentTimestamp()),
		Priority:  priority,
		Size:      size,
		Validated: false,
	}

	// Add to index
	m.txs[tx.TxHash] = mpt

	// Add nullifiers
	for _, nullifier := range tx.Nullifiers {
		m.nullifiers[nullifier] = tx.TxHash
	}

	// Add to priority queue
	m.insertIntoQueue(mpt)

	return nil
}

// Remove removes a transaction from the mempool
func (m *Mempool) Remove(txHash types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mpt, exists := m.txs[txHash]
	if !exists {
		return
	}

	// Remove from index
	delete(m.txs, txHash)

	// Remove nullifiers
	for _, nullifier := range mpt.Tx.Nullifiers {
		delete(m.nullifiers, nullifier)
	}

	// Remove from queue
	m.removeFromQueue(txHash)
}

// Get retrieves a transaction from the mempool
func (m *Mempool) Get(txHash types.Hash) *types.Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if mpt, exists := m.txs[txHash]; exists {
		return mpt.Tx
	}
	return nil
}

// Has checks if a transaction is in the mempool
func (m *Mempool) Has(txHash types.Hash) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.txs[txHash]
	return exists
}

// HasNullifier checks if a nullifier is in the mempool
func (m *Mempool) HasNullifier(nullifier types.Hash) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.nullifiers[nullifier]
	return exists
}

// SelectTransactions selects transactions for a new block
func (m *Mempool) SelectTransactions(maxCount int, maxSize int) []*types.Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if maxCount > m.maxTxPerBlock {
		maxCount = m.maxTxPerBlock
	}

	selected := make([]*types.Transaction, 0, maxCount)
	totalSize := 0
	usedNullifiers := make(map[types.Hash]bool)

	for _, mpt := range m.queue {
		if len(selected) >= maxCount {
			break
		}
		if totalSize+mpt.Size > maxSize {
			continue
		}

		// Check for internal conflicts
		conflict := false
		for _, nullifier := range mpt.Tx.Nullifiers {
			if usedNullifiers[nullifier] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}

		// Add transaction
		selected = append(selected, mpt.Tx)
		totalSize += mpt.Size

		// Mark nullifiers as used
		for _, nullifier := range mpt.Tx.Nullifiers {
			usedNullifiers[nullifier] = true
		}
	}

	return selected
}

// RemoveConfirmed removes transactions that have been confirmed in a block
func (m *Mempool) RemoveConfirmed(block *types.Block) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tx := range block.Transactions {
		if mpt, exists := m.txs[tx.TxHash]; exists {
			// Remove from index
			delete(m.txs, tx.TxHash)

			// Remove nullifiers
			for _, nullifier := range mpt.Tx.Nullifiers {
				delete(m.nullifiers, nullifier)
			}

			// Remove from queue
			m.removeFromQueue(tx.TxHash)
		}

		// Also remove any conflicting transactions
		for _, nullifier := range tx.Nullifiers {
			if conflictingTxHash, exists := m.nullifiers[nullifier]; exists {
				if mpt, exists := m.txs[conflictingTxHash]; exists {
					delete(m.txs, conflictingTxHash)
					for _, n := range mpt.Tx.Nullifiers {
						delete(m.nullifiers, n)
					}
					m.removeFromQueue(conflictingTxHash)
				}
			}
		}
	}
}

// Size returns the number of transactions in the mempool
func (m *Mempool) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}

// TotalFees returns the total fees of all transactions
func (m *Mempool) TotalFees() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var total uint64
	for _, mpt := range m.txs {
		total += mpt.Tx.Fee
	}
	return total
}

// Pending returns all pending transactions
func (m *Mempool) Pending() []*types.Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	txs := make([]*types.Transaction, 0, len(m.queue))
	for _, mpt := range m.queue {
		txs = append(txs, mpt.Tx)
	}
	return txs
}

// insertIntoQueue inserts a transaction into the priority queue
func (m *Mempool) insertIntoQueue(mpt *MempoolTx) {
	// Find insertion point (maintain sorted order by priority descending)
	idx := sort.Search(len(m.queue), func(i int) bool {
		return m.queue[i].Priority < mpt.Priority
	})

	// Insert at position
	m.queue = append(m.queue, nil)
	copy(m.queue[idx+1:], m.queue[idx:])
	m.queue[idx] = mpt
}

// removeFromQueue removes a transaction from the priority queue
func (m *Mempool) removeFromQueue(txHash types.Hash) {
	for i, mpt := range m.queue {
		if mpt.Tx.TxHash == txHash {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

// evictLowestPriority evicts the lowest priority transaction if new tx has higher priority
func (m *Mempool) evictLowestPriority(newFee uint64) bool {
	if len(m.queue) == 0 {
		return false
	}

	lowest := m.queue[len(m.queue)-1]
	if newFee > lowest.Tx.Fee {
		m.Remove(lowest.Tx.TxHash)
		return true
	}
	return false
}

// estimateTxSize estimates the serialized size of a transaction
func estimateTxSize(tx *types.Transaction) int {
	// Base size
	size := 100

	// Nullifiers: 32 bytes each
	size += len(tx.Nullifiers) * 32

	// Commitments: 32 bytes each
	size += len(tx.Commitments) * 32

	// Proof
	size += len(tx.Proof.ProofData)

	// Memo
	size += len(tx.Memo)

	return size
}

// currentTimestamp returns current unix timestamp
func currentTimestamp() int64 {
	return int64(0) // Placeholder - use time.Now().Unix() in production
}

// Validate validates a transaction's zk-SNARK proof
func (m *Mempool) Validate(ctx context.Context, tx *types.Transaction, verifier ProofVerifier) error {
	// Verify the proof
	if !verifier.Verify(tx.Proof, tx.Nullifiers, tx.Commitments) {
		return ErrInvalidProof
	}

	// Mark as validated
	m.mu.Lock()
	if mpt, exists := m.txs[tx.TxHash]; exists {
		mpt.Validated = true
	}
	m.mu.Unlock()

	return nil
}

// ProofVerifier interface for zk-SNARK verification
type ProofVerifier interface {
	Verify(proof types.ZKProof, nullifiers []types.Hash, commitments []types.Commitment) bool
}
