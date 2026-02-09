// Package types defines core data structures for the CCoin blockchain.
// This includes blocks, transactions, models, and other fundamental types.
package types

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"time"
)

// Constants for the CCoin protocol
const (
	// MaxParents is the maximum number of parent blocks a block can reference
	MaxParents = 64

	// MinParents is the minimum number of parent blocks (genesis has 0)
	MinParents = 1

	// HashSize is the size of a hash in bytes (SHA3-256)
	HashSize = 32

	// AddressSize is the size of an address in bytes
	AddressSize = 20

	// SignatureSize is the size of an ECDSA signature
	SignatureSize = 65

	// MaxTransactionsPerBlock is the maximum transactions in a single block
	MaxTransactionsPerBlock = 10000

	// EpochLength is the number of blocks in an epoch for difficulty/reputation adjustment
	EpochLength = 1000

	// HalvingInterval is the number of blocks between reward halvings
	HalvingInterval = 2100000

	// InitialBlockReward is the initial block reward (50 CCoin in base units)
	InitialBlockReward = 50_000_000_000 // 50 CCoin with 9 decimal places

	// TailEmission is the minimum block reward (0.001 CCoin)
	TailEmission = 1_000_000 // 0.001 CCoin

	// CoinbaseMaturity is the number of confirmations before coinbase can be spent
	CoinbaseMaturity = 100
)

// Hash represents a 32-byte hash (SHA3-256)
type Hash [HashSize]byte

// Address represents a 20-byte address (hash of public key)
type Address [AddressSize]byte

// Signature represents a 65-byte ECDSA signature
type Signature [SignatureSize]byte

// EmptyHash is the zero hash
var EmptyHash = Hash{}

// EmptyAddress is the zero address
var EmptyAddress = Address{}

// IsEmpty returns true if the hash is empty (all zeros)
func (h Hash) IsEmpty() bool {
	return h == EmptyHash
}

// Bytes returns the hash as a byte slice
func (h Hash) Bytes() []byte {
	return h[:]
}

// String returns the hex string representation of the hash
func (h Hash) String() string {
	return bytesToHex(h[:])
}

// HashFromBytes creates a Hash from a byte slice
func HashFromBytes(b []byte) Hash {
	var h Hash
	if len(b) >= HashSize {
		copy(h[:], b[:HashSize])
	}
	return h
}

// BlockHeader contains the metadata for a block in the DAG
type BlockHeader struct {
	// Hash is the SHA3-256 hash of this header (computed, not serialized)
	Hash Hash

	// Version is the block format version
	Version uint32

	// Parents contains hashes of 1..k parent blocks in the DAG
	Parents []Hash

	// TxRoot is the Merkle root of transactions in this block
	TxRoot Hash

	// StateRoot is the root hash of the state trie after applying this block
	StateRoot Hash

	// PoUWResult contains the hash of the Proof-of-Useful-Work computation result
	PoUWResult Hash

	// PoUWProof contains the verification data for the PoUW
	PoUWProof []byte

	// TaskID identifies which AI training task this block contributes to
	TaskID Hash

	// QualityScore measures the magnitude of model improvement (0, 1]
	QualityScore float64

	// MinerAddress is the address of the miner who created this block
	MinerAddress Address

	// ReputationScore is the miner's reputation at the time of mining
	ReputationScore float64

	// Difficulty is the difficulty target for this block
	Difficulty *big.Int

	// Nonce is the value found by the miner to satisfy the difficulty
	Nonce uint64

	// Timestamp is the Unix timestamp when this block was created
	Timestamp uint64

	// Height is the logical height in the DAG (max parent height + 1)
	Height uint64

	// CumulativeScore is the total reputation-weighted work up to this block
	CumulativeScore *big.Float

	// ExtraData is arbitrary data (max 32 bytes)
	ExtraData []byte
}

// Block represents a complete block including header and transactions
type Block struct {
	Header       *BlockHeader
	Transactions []*Transaction
}

// NewBlock creates a new block with the given header and transactions
func NewBlock(header *BlockHeader, txs []*Transaction) *Block {
	return &Block{
		Header:       header,
		Transactions: txs,
	}
}

// ComputeHash calculates the hash of the block header
func (h *BlockHeader) ComputeHash() Hash {
	data := h.serializeForHash()
	return sha256.Sum256(data)
}

// serializeForHash serializes header fields for hashing
func (h *BlockHeader) serializeForHash() []byte {
	buf := make([]byte, 0, 1024)

	// Version
	versionBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(versionBytes, h.Version)
	buf = append(buf, versionBytes...)

	// Parents
	for _, parent := range h.Parents {
		buf = append(buf, parent[:]...)
	}

	// TxRoot
	buf = append(buf, h.TxRoot[:]...)

	// StateRoot
	buf = append(buf, h.StateRoot[:]...)

	// PoUWResult
	buf = append(buf, h.PoUWResult[:]...)

	// TaskID
	buf = append(buf, h.TaskID[:]...)

	// MinerAddress
	buf = append(buf, h.MinerAddress[:]...)

	// Difficulty
	if h.Difficulty != nil {
		buf = append(buf, h.Difficulty.Bytes()...)
	}

	// Nonce
	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, h.Nonce)
	buf = append(buf, nonceBytes...)

	// Timestamp
	tsBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBytes, h.Timestamp)
	buf = append(buf, tsBytes...)

	return buf
}

// Work calculates the amount of work represented by this block
func (h *BlockHeader) Work() *big.Int {
	if h.Difficulty == nil || h.Difficulty.Sign() == 0 {
		return big.NewInt(0)
	}
	// Work = 2^256 / (difficulty + 1)
	maxTarget := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	divisor := new(big.Int).Add(h.Difficulty, big.NewInt(1))
	return new(big.Int).Div(maxTarget, divisor)
}

// IsGenesis returns true if this is the genesis block (no parents)
func (h *BlockHeader) IsGenesis() bool {
	return len(h.Parents) == 0
}

// Time returns the block timestamp as a time.Time
func (h *BlockHeader) Time() time.Time {
	return time.Unix(int64(h.Timestamp), 0)
}

// bytesToHex converts bytes to hex string
func bytesToHex(b []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(b)*2)
	for i, v := range b {
		result[i*2] = hexChars[v>>4]
		result[i*2+1] = hexChars[v&0x0f]
	}
	return string(result)
}
