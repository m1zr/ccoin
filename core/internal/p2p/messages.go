// Package p2p provides message serialization for network communication.
package p2p

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/ccoin/core/pkg/types"
)

// Message types
const (
	MsgTypeBlock       uint8 = 0x01
	MsgTypeTransaction uint8 = 0x02
	MsgTypeTask        uint8 = 0x03
	MsgTypeGetBlocks   uint8 = 0x10
	MsgTypeGetTxs      uint8 = 0x11
	MsgTypeStatus      uint8 = 0x20
	MsgTypePing        uint8 = 0x30
	MsgTypePong        uint8 = 0x31
)

// Message errors
var (
	ErrInvalidMessageType = errors.New("invalid message type")
	ErrMessageTooLarge    = errors.New("message too large")
	ErrInvalidChecksum    = errors.New("invalid checksum")
)

// MaxMessageSize is the maximum size of a network message
const MaxMessageSize = 32 * 1024 * 1024 // 32 MB

// Message represents a network message
type Message struct {
	Type    uint8
	Payload []byte
}

// BlockMessage wraps a block for network transmission
type BlockMessage struct {
	Block *types.Block
}

// TransactionMessage wraps a transaction for network transmission
type TransactionMessage struct {
	Transaction *types.Transaction
}

// TaskMessage wraps a task assignment for network transmission
type TaskMessage struct {
	Task *types.Task
}

// GetBlocksMessage requests blocks by hash
type GetBlocksMessage struct {
	StartHash types.Hash
	Count     uint32
}

// StatusMessage exchanges node status information
type StatusMessage struct {
	Version     uint32
	NetworkID   uint32
	Height      uint64
	BestHash    types.Hash
	GenesisHash types.Hash
}

// Encode serializes a message for network transmission
func (m *Message) Encode(w io.Writer) error {
	// Write message type
	if err := binary.Write(w, binary.BigEndian, m.Type); err != nil {
		return err
	}

	// Write payload length
	payloadLen := uint32(len(m.Payload))
	if err := binary.Write(w, binary.BigEndian, payloadLen); err != nil {
		return err
	}

	// Write payload
	if _, err := w.Write(m.Payload); err != nil {
		return err
	}

	return nil
}

// Decode deserializes a message from network data
func (m *Message) Decode(r io.Reader) error {
	// Read message type
	if err := binary.Read(r, binary.BigEndian, &m.Type); err != nil {
		return err
	}

	// Read payload length
	var payloadLen uint32
	if err := binary.Read(r, binary.BigEndian, &payloadLen); err != nil {
		return err
	}

	if payloadLen > MaxMessageSize {
		return ErrMessageTooLarge
	}

	// Read payload
	m.Payload = make([]byte, payloadLen)
	if _, err := io.ReadFull(r, m.Payload); err != nil {
		return err
	}

	return nil
}

// EncodeBlock serializes a block message
func EncodeBlock(block *types.Block) ([]byte, error) {
	// Serialize block header
	header := block.Header
	buf := make([]byte, 0, 1024)

	// Version
	buf = binary.BigEndian.AppendUint32(buf, header.Version)

	// Hash
	buf = append(buf, header.Hash[:]...)

	// Parents count and hashes
	buf = append(buf, byte(len(header.Parents)))
	for _, parent := range header.Parents {
		buf = append(buf, parent[:]...)
	}

	// Roots
	buf = append(buf, header.TxRoot[:]...)
	buf = append(buf, header.StateRoot[:]...)

	// PoUW
	buf = append(buf, header.PoUWResult[:]...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(header.PoUWProof)))
	buf = append(buf, header.PoUWProof...)
	buf = append(buf, header.TaskID[:]...)

	// Quality score as fixed point
	qualityFixed := uint64(header.QualityScore * 1e9)
	buf = binary.BigEndian.AppendUint64(buf, qualityFixed)

	// Miner info
	buf = append(buf, header.MinerAddress[:]...)
	repFixed := uint64(header.ReputationScore * 1e9)
	buf = binary.BigEndian.AppendUint64(buf, repFixed)

	// Difficulty (as big-endian bytes)
	diffBytes := header.Difficulty.Bytes()
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(diffBytes)))
	buf = append(buf, diffBytes...)

	// Nonce, timestamp, height
	buf = binary.BigEndian.AppendUint64(buf, header.Nonce)
	buf = binary.BigEndian.AppendUint64(buf, header.Timestamp)
	buf = binary.BigEndian.AppendUint64(buf, header.Height)

	// Cumulative score
	if header.CumulativeScore != nil {
		scoreStr := header.CumulativeScore.Text('f', 0)
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(scoreStr)))
		buf = append(buf, []byte(scoreStr)...)
	} else {
		buf = binary.BigEndian.AppendUint16(buf, 0)
	}

	// Extra data
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(header.ExtraData)))
	buf = append(buf, header.ExtraData...)

	// Transaction count
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(block.Transactions)))

	// Transactions (simplified - just hashes for now)
	for _, tx := range block.Transactions {
		txData, err := EncodeTransaction(tx)
		if err != nil {
			return nil, err
		}
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(txData)))
		buf = append(buf, txData...)
	}

	return buf, nil
}

// EncodeTransaction serializes a transaction
func EncodeTransaction(tx *types.Transaction) ([]byte, error) {
	buf := make([]byte, 0, 512)

	// Version and hash
	buf = binary.BigEndian.AppendUint32(buf, tx.Version)
	buf = append(buf, tx.TxHash[:]...)

	// Nullifiers
	buf = append(buf, byte(len(tx.Nullifiers)))
	for _, n := range tx.Nullifiers {
		buf = append(buf, n[:]...)
	}

	// Commitments
	buf = append(buf, byte(len(tx.Commitments)))
	for _, c := range tx.Commitments {
		buf = append(buf, c.Value[:]...)
	}

	// Proof
	buf = append(buf, byte(tx.Proof.ProofType))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(tx.Proof.ProofData)))
	buf = append(buf, tx.Proof.ProofData...)

	// Disclosure flags
	buf = binary.BigEndian.AppendUint32(buf, tx.DisclosureFlags)

	// Anchor
	buf = append(buf, tx.Anchor[:]...)

	// Fee
	buf = binary.BigEndian.AppendUint64(buf, tx.Fee)

	// Memo
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(tx.Memo)))
	buf = append(buf, tx.Memo...)

	return buf, nil
}

// EncodeTask serializes a task assignment
func EncodeTask(task *types.Task) ([]byte, error) {
	buf := make([]byte, 0, 256)

	// Task ID
	buf = append(buf, task.TaskID[:]...)

	// Model and dataset
	buf = append(buf, task.ModelID[:]...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(task.DatasetCID)))
	buf = append(buf, []byte(task.DatasetCID)...)

	// Batch info
	buf = binary.BigEndian.AppendUint32(buf, task.BatchStart)
	buf = binary.BigEndian.AppendUint32(buf, task.BatchSize)

	// Objective and status
	buf = append(buf, task.Objective[:]...)
	buf = append(buf, byte(task.Status))

	// Assigned miner
	buf = append(buf, task.AssignedMiner[:]...)

	// Timestamps
	buf = binary.BigEndian.AppendUint64(buf, task.AssignedAt)
	buf = binary.BigEndian.AppendUint64(buf, task.Deadline)
	buf = binary.BigEndian.AppendUint64(buf, task.CompletedAt)

	// Reward
	buf = binary.BigEndian.AppendUint64(buf, task.Reward)

	return buf, nil
}

// EncodeStatus serializes a status message
func EncodeStatus(status *StatusMessage) ([]byte, error) {
	buf := make([]byte, 0, 80)

	buf = binary.BigEndian.AppendUint32(buf, status.Version)
	buf = binary.BigEndian.AppendUint32(buf, status.NetworkID)
	buf = binary.BigEndian.AppendUint64(buf, status.Height)
	buf = append(buf, status.BestHash[:]...)
	buf = append(buf, status.GenesisHash[:]...)

	return buf, nil
}

// DecodeStatus deserializes a status message
func DecodeStatus(data []byte) (*StatusMessage, error) {
	if len(data) < 80 {
		return nil, errors.New("status message too short")
	}

	status := &StatusMessage{
		Version:   binary.BigEndian.Uint32(data[0:4]),
		NetworkID: binary.BigEndian.Uint32(data[4:8]),
		Height:    binary.BigEndian.Uint64(data[8:16]),
	}
	copy(status.BestHash[:], data[16:48])
	copy(status.GenesisHash[:], data[48:80])

	return status, nil
}
