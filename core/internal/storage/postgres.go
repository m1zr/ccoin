// Package storage implements the PostgreSQL storage layer for CCoin.
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ccoin/core/pkg/types"
)

// Common errors
var (
	ErrNotFound     = errors.New("not found")
	ErrDuplicate    = errors.New("duplicate entry")
	ErrInvalidData  = errors.New("invalid data")
	ErrDBConnection = errors.New("database connection error")
)

// PostgresStore implements persistent storage using PostgreSQL
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Config holds database configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
	MaxConns int32
}

// DefaultConfig returns default database configuration
func DefaultConfig() *Config {
	return &Config{
		Host:     "localhost",
		Port:     5432,
		User:     "ccoin",
		Password: "",
		Database: "ccoin",
		SSLMode:  "disable",
		MaxConns: 20,
	}
}

// NewPostgresStore creates a new PostgreSQL store
func NewPostgresStore(ctx context.Context, cfg *Config) (*PostgresStore, error) {
	connString := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s pool_max_conns=%d",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode, cfg.MaxConns,
	)

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDBConnection, err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDBConnection, err)
	}

	return &PostgresStore{pool: pool}, nil
}

// Close closes the database connection pool
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// ============================================
// Block Operations
// ============================================

// SaveBlock saves a block to the database
func (s *PostgresStore) SaveBlock(ctx context.Context, block *types.Block) error {
	header := block.Header

	query := `
		INSERT INTO blocks (
			hash, version, parents, tx_root, state_root, pouw_result, pouw_proof,
			task_id, quality_score, miner_address, reputation_score, difficulty,
			nonce, timestamp, height, cumulative_score, is_main_chain, extra_data
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (hash) DO NOTHING
	`

	// Convert parents to bytea array
	parents := make([][]byte, len(header.Parents))
	for i, p := range header.Parents {
		parents[i] = p[:]
	}

	// Convert cumulative score to string for DECIMAL storage
	var scoreStr string
	if header.CumulativeScore != nil {
		scoreStr = header.CumulativeScore.Text('f', 0)
	} else {
		scoreStr = "0"
	}

	_, err := s.pool.Exec(ctx, query,
		header.Hash[:],
		header.Version,
		parents,
		header.TxRoot[:],
		header.StateRoot[:],
		nullIfEmpty(header.PoUWResult[:]),
		header.PoUWProof,
		nullIfEmpty(header.TaskID[:]),
		nullIfZero(header.QualityScore),
		header.MinerAddress[:],
		header.ReputationScore,
		header.Difficulty.Bytes(),
		header.Nonce,
		header.Timestamp,
		header.Height,
		scoreStr,
		false, // is_main_chain
		header.ExtraData,
	)

	if err != nil {
		return fmt.Errorf("failed to save block: %w", err)
	}

	// Save transactions
	for i, tx := range block.Transactions {
		if err := s.saveTransaction(ctx, tx, header.Hash, i); err != nil {
			return fmt.Errorf("failed to save transaction: %w", err)
		}
	}

	return nil
}

// GetBlock retrieves a complete block by hash
func (s *PostgresStore) GetBlock(ctx context.Context, hash types.Hash) (*types.Block, error) {
	header, err := s.GetBlockHeader(ctx, hash)
	if err != nil {
		return nil, err
	}

	txs, err := s.getBlockTransactions(ctx, hash)
	if err != nil {
		return nil, err
	}

	return &types.Block{
		Header:       header,
		Transactions: txs,
	}, nil
}

// GetBlockHeader retrieves a block header by hash
func (s *PostgresStore) GetBlockHeader(ctx context.Context, hash types.Hash) (*types.BlockHeader, error) {
	query := `
		SELECT hash, version, parents, tx_root, state_root, pouw_result, pouw_proof,
			   task_id, quality_score, miner_address, reputation_score, difficulty,
			   nonce, timestamp, height, cumulative_score, extra_data
		FROM blocks WHERE hash = $1
	`

	var header types.BlockHeader
	var hashBytes, txRoot, stateRoot, pouwResult, taskID, minerAddr, difficulty, extraData []byte
	var parents [][]byte
	var scoreStr string

	err := s.pool.QueryRow(ctx, query, hash[:]).Scan(
		&hashBytes,
		&header.Version,
		&parents,
		&txRoot,
		&stateRoot,
		&pouwResult,
		&header.PoUWProof,
		&taskID,
		&header.QualityScore,
		&minerAddr,
		&header.ReputationScore,
		&difficulty,
		&header.Nonce,
		&header.Timestamp,
		&header.Height,
		&scoreStr,
		&extraData,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get block header: %w", err)
	}

	// Convert bytes to types
	copy(header.Hash[:], hashBytes)
	copy(header.TxRoot[:], txRoot)
	copy(header.StateRoot[:], stateRoot)
	if pouwResult != nil {
		copy(header.PoUWResult[:], pouwResult)
	}
	if taskID != nil {
		copy(header.TaskID[:], taskID)
	}
	copy(header.MinerAddress[:], minerAddr)
	header.Difficulty = new(big.Int).SetBytes(difficulty)
	header.ExtraData = extraData

	// Convert parents
	header.Parents = make([]types.Hash, len(parents))
	for i, p := range parents {
		copy(header.Parents[i][:], p)
	}

	// Parse cumulative score
	header.CumulativeScore = new(big.Float)
	header.CumulativeScore.SetString(scoreStr)

	return &header, nil
}

// GetBlocksByHeight returns all blocks at a given height
func (s *PostgresStore) GetBlocksByHeight(ctx context.Context, height uint64) ([]*types.BlockHeader, error) {
	query := `SELECT hash FROM blocks WHERE height = $1`

	rows, err := s.pool.Query(ctx, query, height)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var headers []*types.BlockHeader
	for rows.Next() {
		var hashBytes []byte
		if err := rows.Scan(&hashBytes); err != nil {
			return nil, err
		}

		var hash types.Hash
		copy(hash[:], hashBytes)

		header, err := s.GetBlockHeader(ctx, hash)
		if err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}

	return headers, nil
}

// GetChildren returns child block hashes for a given block
func (s *PostgresStore) GetChildren(ctx context.Context, hash types.Hash) ([]types.Hash, error) {
	query := `SELECT hash FROM blocks WHERE $1 = ANY(parents)`

	rows, err := s.pool.Query(ctx, query, hash[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var children []types.Hash
	for rows.Next() {
		var hashBytes []byte
		if err := rows.Scan(&hashBytes); err != nil {
			return nil, err
		}

		var childHash types.Hash
		copy(childHash[:], hashBytes)
		children = append(children, childHash)
	}

	return children, nil
}

// GetMainChain returns main chain blocks in height order
func (s *PostgresStore) GetMainChain(ctx context.Context, fromHeight, toHeight uint64) ([]*types.BlockHeader, error) {
	query := `
		SELECT hash FROM blocks 
		WHERE is_main_chain = TRUE AND height >= $1 AND height <= $2
		ORDER BY height ASC
	`

	rows, err := s.pool.Query(ctx, query, fromHeight, toHeight)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var headers []*types.BlockHeader
	for rows.Next() {
		var hashBytes []byte
		if err := rows.Scan(&hashBytes); err != nil {
			return nil, err
		}

		var hash types.Hash
		copy(hash[:], hashBytes)

		header, err := s.GetBlockHeader(ctx, hash)
		if err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}

	return headers, nil
}

// UpdateMainChain updates main chain status for blocks
func (s *PostgresStore) UpdateMainChain(ctx context.Context, onChain, offChain []types.Hash) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Mark blocks as on main chain
	for _, hash := range onChain {
		_, err := tx.Exec(ctx, "UPDATE blocks SET is_main_chain = TRUE WHERE hash = $1", hash[:])
		if err != nil {
			return err
		}
	}

	// Mark blocks as off main chain
	for _, hash := range offChain {
		_, err := tx.Exec(ctx, "UPDATE blocks SET is_main_chain = FALSE WHERE hash = $1", hash[:])
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetTips returns current DAG tips (blocks with no children)
func (s *PostgresStore) GetTips(ctx context.Context) ([]types.Hash, error) {
	query := `
		SELECT b.hash FROM blocks b
		WHERE NOT EXISTS (
			SELECT 1 FROM blocks c WHERE b.hash = ANY(c.parents)
		)
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tips []types.Hash
	for rows.Next() {
		var hashBytes []byte
		if err := rows.Scan(&hashBytes); err != nil {
			return nil, err
		}

		var hash types.Hash
		copy(hash[:], hashBytes)
		tips = append(tips, hash)
	}

	return tips, nil
}

// ============================================
// Transaction Operations
// ============================================

func (s *PostgresStore) saveTransaction(ctx context.Context, tx *types.Transaction, blockHash types.Hash, index int) error {
	query := `
		INSERT INTO transactions (
			tx_hash, block_hash, version, nullifiers, commitments, proof_type,
			proof, anchor, disclosure_flags, disclosures, fee, memo, tx_index
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (tx_hash) DO UPDATE SET block_hash = $2, tx_index = $13
	`

	// Convert nullifiers
	nullifiers := make([][]byte, len(tx.Nullifiers))
	for i, n := range tx.Nullifiers {
		nullifiers[i] = n[:]
	}

	// Convert commitments
	commitments := make([][]byte, len(tx.Commitments))
	for i, c := range tx.Commitments {
		commitments[i] = c.Value[:]
	}

	_, err := s.pool.Exec(ctx, query,
		tx.TxHash[:],
		blockHash[:],
		tx.Version,
		nullifiers,
		commitments,
		tx.Proof.ProofType,
		tx.Proof.ProofData,
		tx.Anchor[:],
		tx.DisclosureFlags,
		nil, // disclosures serialization
		tx.Fee,
		tx.Memo,
		index,
	)

	if err != nil {
		return err
	}

	// Save nullifiers
	for _, nullifier := range tx.Nullifiers {
		if err := s.saveNullifier(ctx, nullifier, tx.TxHash); err != nil {
			return err
		}
	}

	return nil
}

func (s *PostgresStore) saveNullifier(ctx context.Context, nullifier types.Hash, txHash types.Hash) error {
	query := `
		INSERT INTO nullifiers (nullifier, tx_hash, block_height)
		SELECT $1, $2, b.height FROM blocks b
		JOIN transactions t ON t.block_hash = b.hash
		WHERE t.tx_hash = $2
		ON CONFLICT (nullifier) DO NOTHING
	`

	_, err := s.pool.Exec(ctx, query, nullifier[:], txHash[:])
	return err
}

func (s *PostgresStore) getBlockTransactions(ctx context.Context, blockHash types.Hash) ([]*types.Transaction, error) {
	query := `
		SELECT tx_hash, version, nullifiers, commitments, proof_type, proof,
			   anchor, disclosure_flags, fee, memo
		FROM transactions WHERE block_hash = $1
		ORDER BY tx_index ASC
	`

	rows, err := s.pool.Query(ctx, query, blockHash[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transactions []*types.Transaction
	for rows.Next() {
		var tx types.Transaction
		var txHash, anchor []byte
		var nullifiers, commitments [][]byte

		if err := rows.Scan(
			&txHash,
			&tx.Version,
			&nullifiers,
			&commitments,
			&tx.Proof.ProofType,
			&tx.Proof.ProofData,
			&anchor,
			&tx.DisclosureFlags,
			&tx.Fee,
			&tx.Memo,
		); err != nil {
			return nil, err
		}

		copy(tx.TxHash[:], txHash)
		copy(tx.Anchor[:], anchor)

		tx.Nullifiers = make([]types.Hash, len(nullifiers))
		for i, n := range nullifiers {
			copy(tx.Nullifiers[i][:], n)
		}

		tx.Commitments = make([]types.Commitment, len(commitments))
		for i, c := range commitments {
			copy(tx.Commitments[i].Value[:], c)
		}

		transactions = append(transactions, &tx)
	}

	return transactions, nil
}

// ============================================
// Helper Functions
// ============================================

func nullIfEmpty(b []byte) interface{} {
	for _, v := range b {
		if v != 0 {
			return b
		}
	}
	return nil
}

func nullIfZero(f float64) interface{} {
	if f == 0 {
		return nil
	}
	return f
}

// Required import
import "math/big"
