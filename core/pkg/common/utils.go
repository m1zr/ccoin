// Package common provides shared utilities for the CCoin blockchain.
package common

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"time"
)

// Common errors
var (
	ErrInvalidHash      = errors.New("invalid hash")
	ErrInvalidAddress   = errors.New("invalid address")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrInvalidProof     = errors.New("invalid proof")
	ErrDoubleSpend      = errors.New("double spend detected")
	ErrInsufficientFund = errors.New("insufficient funds")
)

// HexToBytes converts a hex string to bytes
func HexToBytes(s string) ([]byte, error) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	return hex.DecodeString(s)
}

// BytesToHex converts bytes to a hex string with 0x prefix
func BytesToHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

// RandomBytes generates n random bytes
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// Now returns the current Unix timestamp
func Now() uint64 {
	return uint64(time.Now().Unix())
}

// NowNano returns the current Unix timestamp in nanoseconds
func NowNano() uint64 {
	return uint64(time.Now().UnixNano())
}

// TimestampToTime converts a Unix timestamp to time.Time
func TimestampToTime(ts uint64) time.Time {
	return time.Unix(int64(ts), 0)
}

// BigIntToBytes converts a big.Int to a fixed-size byte slice
func BigIntToBytes(n *big.Int, size int) []byte {
	if n == nil {
		return make([]byte, size)
	}
	b := n.Bytes()
	if len(b) >= size {
		return b[:size]
	}
	// Pad with leading zeros
	result := make([]byte, size)
	copy(result[size-len(b):], b)
	return result
}

// BytesToBigInt converts a byte slice to big.Int
func BytesToBigInt(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

// Uint64ToBytes converts uint64 to bytes (big endian)
func Uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

// BytesToUint64 converts bytes to uint64 (big endian)
func BytesToUint64(b []byte) uint64 {
	if len(b) < 8 {
		padded := make([]byte, 8)
		copy(padded[8-len(b):], b)
		b = padded
	}
	return binary.BigEndian.Uint64(b)
}

// Min returns the minimum of two uint64 values
func Min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two uint64 values
func Max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// MinInt returns the minimum of two int values
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaxInt returns the maximum of two int values
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// AbsDiff returns the absolute difference between two uint64 values
func AbsDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

// Clamp constrains a value to a range
func Clamp(value, min, max uint64) uint64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampFloat constrains a float64 value to a range
func ClampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// IsZeroBytes checks if all bytes are zero
func IsZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// CopyBytes returns a copy of a byte slice
func CopyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// ConcatBytes concatenates multiple byte slices
func ConcatBytes(slices ...[]byte) []byte {
	totalLen := 0
	for _, s := range slices {
		totalLen += len(s)
	}
	result := make([]byte, totalLen)
	offset := 0
	for _, s := range slices {
		copy(result[offset:], s)
		offset += len(s)
	}
	return result
}
