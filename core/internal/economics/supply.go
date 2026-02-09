// Package economics implements CCoin tokenomics.
package economics

import (
	"errors"
	"sync"
)

// Token supply constants
const (
	// MaxSupply is the maximum CCoin supply (210 million)
	MaxSupply uint64 = 210_000_000 * 1e8 // In base units (8 decimals)

	// InitialBlockReward is the initial reward per block (50 CCoin)
	InitialBlockReward uint64 = 50 * 1e8

	// HalvingInterval is the number of blocks between halvings
	HalvingInterval uint64 = 2_100_000

	// TailEmission is the minimum block reward
	TailEmission uint64 = 0.001 * 1e8 // 0.001 CCoin

	// TokenDecimals is the number of decimal places
	TokenDecimals = 8

	// TokenSymbol is the token symbol
	TokenSymbol = "CCOIN"

	// TokenName is the full token name
	TokenName = "CCoin"
)

// Tokenomics errors
var (
	ErrMaxSupplyReached = errors.New("maximum supply reached")
	ErrInvalidAmount    = errors.New("invalid amount")
)

// SupplyManager manages token supply and emissions
type SupplyManager struct {
	mu sync.RWMutex

	// Current circulating supply
	circulatingSupply uint64

	// Total minted (including burned)
	totalMinted uint64

	// Total burned
	totalBurned uint64

	// Current block height
	currentHeight uint64

	// Storage
	store SupplyStore
}

// SupplyStore defines persistence for supply data
type SupplyStore interface {
	GetCirculatingSupply() (uint64, error)
	SetCirculatingSupply(supply uint64) error
	GetTotalMinted() (uint64, error)
	SetTotalMinted(minted uint64) error
	GetTotalBurned() (uint64, error)
	SetTotalBurned(burned uint64) error
}

// NewSupplyManager creates a new supply manager
func NewSupplyManager(store SupplyStore) *SupplyManager {
	sm := &SupplyManager{store: store}

	// Load from store if available
	if store != nil {
		if supply, err := store.GetCirculatingSupply(); err == nil {
			sm.circulatingSupply = supply
		}
		if minted, err := store.GetTotalMinted(); err == nil {
			sm.totalMinted = minted
		}
		if burned, err := store.GetTotalBurned(); err == nil {
			sm.totalBurned = burned
		}
	}

	return sm
}

// CalculateBlockReward calculates the block reward at a given height
// Follows Bitcoin-like halving schedule
func CalculateBlockReward(height uint64) uint64 {
	halvings := height / HalvingInterval

	// After ~32 halvings, reward becomes negligible
	if halvings >= 32 {
		return TailEmission
	}

	// Calculate reward: InitialReward / 2^halvings
	reward := InitialBlockReward >> halvings

	// Never go below tail emission
	if reward < TailEmission {
		return TailEmission
	}

	return reward
}

// CalculateReputationMultiplier calculates the reputation-based reward multiplier
// Multiplier = 0.5 + 0.5 × Rep (where Rep ∈ [0.1, 3.0])
// Result: [0.55, 2.0]
func CalculateReputationMultiplier(reputation float64) float64 {
	return 0.5 + 0.5*reputation
}

// CalculateMinerReward calculates the total reward for a miner
// R = R_base × ReputationMultiplier
func CalculateMinerReward(height uint64, reputation float64) uint64 {
	baseReward := CalculateBlockReward(height)
	multiplier := CalculateReputationMultiplier(reputation)

	// Apply multiplier
	reward := float64(baseReward) * multiplier
	return uint64(reward)
}

// MintReward mints new tokens as block reward
func (sm *SupplyManager) MintReward(height uint64, amount uint64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check max supply
	if sm.circulatingSupply+amount > MaxSupply {
		// Reduce to max supply if close
		if sm.circulatingSupply >= MaxSupply {
			return ErrMaxSupplyReached
		}
		amount = MaxSupply - sm.circulatingSupply
	}

	sm.circulatingSupply += amount
	sm.totalMinted += amount
	sm.currentHeight = height

	if sm.store != nil {
		if err := sm.store.SetCirculatingSupply(sm.circulatingSupply); err != nil {
			return err
		}
		if err := sm.store.SetTotalMinted(sm.totalMinted); err != nil {
			return err
		}
	}

	return nil
}

// Burn burns tokens from circulation
func (sm *SupplyManager) Burn(amount uint64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if amount > sm.circulatingSupply {
		return ErrInvalidAmount
	}

	sm.circulatingSupply -= amount
	sm.totalBurned += amount

	if sm.store != nil {
		if err := sm.store.SetCirculatingSupply(sm.circulatingSupply); err != nil {
			return err
		}
		if err := sm.store.SetTotalBurned(sm.totalBurned); err != nil {
			return err
		}
	}

	return nil
}

// GetCirculatingSupply returns the current circulating supply
func (sm *SupplyManager) GetCirculatingSupply() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.circulatingSupply
}

// GetTotalMinted returns the total minted amount
func (sm *SupplyManager) GetTotalMinted() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.totalMinted
}

// GetTotalBurned returns the total burned amount
func (sm *SupplyManager) GetTotalBurned() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.totalBurned
}

// GetInflationRate returns the current annual inflation rate
func (sm *SupplyManager) GetInflationRate(blocksPerYear uint64) float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.circulatingSupply == 0 {
		return 0
	}

	annualEmission := float64(CalculateBlockReward(sm.currentHeight)) * float64(blocksPerYear)
	return annualEmission / float64(sm.circulatingSupply) * 100
}

// CalculateHalvingBlock returns the next halving block
func CalculateHalvingBlock(currentHeight uint64) uint64 {
	currentHalving := currentHeight / HalvingInterval
	return (currentHalving + 1) * HalvingInterval
}

// GetHalvingCount returns the number of halvings that have occurred
func GetHalvingCount(height uint64) uint64 {
	return height / HalvingInterval
}

// FormatAmount formats a raw amount to human-readable string
func FormatAmount(amount uint64) string {
	whole := amount / 1e8
	frac := amount % 1e8
	return formatWithDecimals(whole, frac)
}

func formatWithDecimals(whole uint64, frac uint64) string {
	if frac == 0 {
		return formatUint(whole)
	}
	// Remove trailing zeros from fraction
	for frac > 0 && frac%10 == 0 {
		frac /= 10
	}
	return formatUint(whole) + "." + formatUint(frac)
}

func formatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 20)
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// ParseAmount parses a human-readable amount to raw units
func ParseAmount(s string) (uint64, error) {
	var whole, frac uint64
	decimals := 0
	inFrac := false

	for _, c := range s {
		if c == '.' {
			if inFrac {
				return 0, ErrInvalidAmount
			}
			inFrac = true
			continue
		}

		if c < '0' || c > '9' {
			return 0, ErrInvalidAmount
		}

		digit := uint64(c - '0')

		if inFrac {
			if decimals >= TokenDecimals {
				continue // Ignore extra decimals
			}
			frac = frac*10 + digit
			decimals++
		} else {
			whole = whole*10 + digit
		}
	}

	// Scale fraction to 8 decimals
	for decimals < TokenDecimals {
		frac *= 10
		decimals++
	}

	return whole*1e8 + frac, nil
}

// ProjectedSupplyAtHeight calculates the projected supply at a given height
func ProjectedSupplyAtHeight(targetHeight uint64) uint64 {
	var supply uint64
	currentHalving := uint64(0)

	for height := uint64(0); height < targetHeight; {
		nextHalving := (currentHalving + 1) * HalvingInterval
		if nextHalving > targetHeight {
			nextHalving = targetHeight
		}

		blocksInPeriod := nextHalving - height
		reward := CalculateBlockReward(height)
		supply += reward * blocksInPeriod

		if supply > MaxSupply {
			return MaxSupply
		}

		height = nextHalving
		currentHalving++
	}

	return supply
}

// EmissionSchedule returns the emission schedule
type EmissionEntry struct {
	HalvingNumber uint64
	BlockStart    uint64
	BlockEnd      uint64
	RewardPerBlock uint64
	TotalEmission uint64
}

// GetEmissionSchedule returns the full emission schedule
func GetEmissionSchedule() []EmissionEntry {
	schedule := make([]EmissionEntry, 0)
	var totalEmitted uint64

	for halving := uint64(0); halving < 32; halving++ {
		reward := CalculateBlockReward(halving * HalvingInterval)
		if reward <= TailEmission {
			break
		}

		periodEmission := reward * HalvingInterval
		totalEmitted += periodEmission

		schedule = append(schedule, EmissionEntry{
			HalvingNumber:  halving,
			BlockStart:     halving * HalvingInterval,
			BlockEnd:       (halving+1)*HalvingInterval - 1,
			RewardPerBlock: reward,
			TotalEmission:  periodEmission,
		})
	}

	return schedule
}
