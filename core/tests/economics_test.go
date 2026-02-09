// Package tests provides tests for economics components.
package tests

import (
	"testing"

	"github.com/ccoin/core/internal/economics"
)

// Test block reward calculation
func TestBlockReward(t *testing.T) {
	testCases := []struct {
		height   uint64
		expected uint64
	}{
		{0, 50 * 1e8},                     // Genesis epoch
		{1, 50 * 1e8},                     // Still first epoch
		{economics.HalvingInterval - 1, 50 * 1e8}, // End of first epoch
		{economics.HalvingInterval, 25 * 1e8},     // Second epoch
		{economics.HalvingInterval * 2, 12.5 * 1e8}, // Third epoch (rounded)
	}

	for _, tc := range testCases {
		reward := economics.CalculateBlockReward(tc.height)
		if reward != tc.expected {
			t.Errorf("Height %d: expected %d, got %d", tc.height, tc.expected, reward)
		}
	}
}

// Test reputation multiplier
func TestReputationMultiplier(t *testing.T) {
	testCases := []struct {
		reputation float64
		expected   float64
	}{
		{1.0, 1.0},   // Default reputation
		{0.1, 0.55},  // Minimum reputation
		{3.0, 2.0},   // Maximum reputation
		{2.0, 1.5},   // Mid-high reputation
	}

	for _, tc := range testCases {
		mult := economics.CalculateReputationMultiplier(tc.reputation)
		if mult != tc.expected {
			t.Errorf("Reputation %.1f: expected %.2f, got %.2f", tc.reputation, tc.expected, mult)
		}
	}
}

// Test miner reward calculation
func TestMinerReward(t *testing.T) {
	height := uint64(1000)
	reputation := 1.5
	
	reward := economics.CalculateMinerReward(height, reputation)
	expected := uint64(float64(50*1e8) * 1.25) // 50 * (0.5 + 0.5 * 1.5)
	
	if reward != expected {
		t.Errorf("Expected %d, got %d", expected, reward)
	}
}

// Test supply manager
func TestSupplyManager(t *testing.T) {
	sm := economics.NewSupplyManager(nil)

	// Initial supply should be 0
	if sm.GetCirculatingSupply() != 0 {
		t.Error("Initial supply should be 0")
	}

	// Mint some tokens
	err := sm.MintReward(1, 100*1e8)
	if err != nil {
		t.Fatalf("Failed to mint: %v", err)
	}

	if sm.GetCirculatingSupply() != 100*1e8 {
		t.Error("Supply should be 100 after mint")
	}

	// Burn some tokens
	err = sm.Burn(10 * 1e8)
	if err != nil {
		t.Fatalf("Failed to burn: %v", err)
	}

	if sm.GetCirculatingSupply() != 90*1e8 {
		t.Error("Supply should be 90 after burn")
	}

	if sm.GetTotalBurned() != 10*1e8 {
		t.Error("Burned amount should be 10")
	}
}

// Test fee market
func TestFeeMarket(t *testing.T) {
	fm := economics.NewFeeMarket(nil)

	initialFee := fm.GetBaseFee()
	if initialFee != economics.BaseFee {
		t.Errorf("Initial fee should be %d, got %d", economics.BaseFee, initialFee)
	}

	// Simulate high congestion
	fm.UpdateBaseFee(economics.TargetBlockGas * 2) // Double target
	newFee := fm.GetBaseFee()

	if newFee <= initialFee {
		t.Error("Fee should increase with high congestion")
	}

	// Simulate low congestion
	fm.UpdateBaseFee(economics.TargetBlockGas / 2) // Half target
	lowFee := fm.GetBaseFee()

	if lowFee >= newFee {
		t.Error("Fee should decrease with low congestion")
	}
}

// Test fee distribution
func TestFeeDistribution(t *testing.T) {
	totalFees := uint64(1000)
	dist := economics.DefaultFeeDistribution()

	miner, burn, treasury := economics.DistributeFees(totalFees, dist)

	// Check proportions
	if miner != 500 {
		t.Errorf("Miner share should be 500, got %d", miner)
	}
	if burn != 300 {
		t.Errorf("Burn share should be 300, got %d", burn)
	}
	if treasury != 200 {
		t.Errorf("Treasury share should be 200, got %d", treasury)
	}

	// Check total
	if miner+burn+treasury != totalFees {
		t.Error("Distribution should sum to total fees")
	}
}

// Test reward distribution
func TestRewardDistribution(t *testing.T) {
	rd := economics.DefaultRewardDistribution()
	totalReward := uint64(10000)

	miner, staker, treasury, proposer, burn := rd.CalculateDistribution(totalReward)

	// Check proportions
	if miner != 4000 {
		t.Errorf("Miner share should be 4000, got %d", miner)
	}
	if staker != 2000 {
		t.Errorf("Staker share should be 2000, got %d", staker)
	}
	if treasury != 2500 {
		t.Errorf("Treasury share should be 2500, got %d", treasury)
	}
	if proposer != 1000 {
		t.Errorf("Proposer share should be 1000, got %d", proposer)
	}
	// Burn gets remainder
	if miner+staker+treasury+proposer+burn != totalReward {
		t.Error("Distribution should sum to total reward")
	}
}

// Test amount parsing
func TestAmountParsing(t *testing.T) {
	testCases := []struct {
		input    string
		expected uint64
	}{
		{"1", 1 * 1e8},
		{"1.0", 1 * 1e8},
		{"0.5", 0.5 * 1e8},
		{"100.12345678", 10012345678},
		{"0.00000001", 1},
	}

	for _, tc := range testCases {
		amount, err := economics.ParseAmount(tc.input)
		if err != nil {
			t.Errorf("Failed to parse %s: %v", tc.input, err)
			continue
		}
		if amount != tc.expected {
			t.Errorf("Parse %s: expected %d, got %d", tc.input, tc.expected, amount)
		}
	}
}

// Test next halving calculation
func TestNextHalving(t *testing.T) {
	testCases := []struct {
		current  uint64
		expected uint64
	}{
		{0, economics.HalvingInterval},
		{1, economics.HalvingInterval},
		{economics.HalvingInterval - 1, economics.HalvingInterval},
		{economics.HalvingInterval, economics.HalvingInterval * 2},
		{economics.HalvingInterval + 1, economics.HalvingInterval * 2},
	}

	for _, tc := range testCases {
		next := economics.CalculateHalvingBlock(tc.current)
		if next != tc.expected {
			t.Errorf("Current %d: expected next %d, got %d", tc.current, tc.expected, next)
		}
	}
}
