// Package tests provides integration tests for AI Commons.
package tests

import (
	"context"
	"testing"

	"github.com/ccoin/core/internal/aicommons"
	"github.com/ccoin/core/pkg/types"
)

// Mock AI Commons store
type mockAICommonsStore struct {
	models        map[types.Hash]*aicommons.Model
	contributions map[types.Hash][]*aicommons.Contribution
	licenses      map[types.Hash]*aicommons.License
}

func newMockAICommonsStore() *mockAICommonsStore {
	return &mockAICommonsStore{
		models:        make(map[types.Hash]*aicommons.Model),
		contributions: make(map[types.Hash][]*aicommons.Contribution),
		licenses:      make(map[types.Hash]*aicommons.License),
	}
}

func (s *mockAICommonsStore) SaveModel(ctx context.Context, m *aicommons.Model) error {
	s.models[m.ModelID] = m
	return nil
}

func (s *mockAICommonsStore) GetModel(ctx context.Context, id types.Hash) (*aicommons.Model, error) {
	return s.models[id], nil
}

func (s *mockAICommonsStore) SaveContribution(ctx context.Context, c *aicommons.Contribution) error {
	s.contributions[c.ModelID] = append(s.contributions[c.ModelID], c)
	return nil
}

func (s *mockAICommonsStore) SaveLicense(ctx context.Context, l *aicommons.License) error {
	s.licenses[l.LicenseID] = l
	return nil
}

func (s *mockAICommonsStore) GetLicense(ctx context.Context, id types.Hash) (*aicommons.License, error) {
	return s.licenses[id], nil
}

// Test model registration
func TestModelRegistration(t *testing.T) {
	ctx := context.Background()
	store := newMockAICommonsStore()
	registry := aicommons.NewModelRegistry(store)

	proposer := types.Address{1, 2, 3}
	currentBlock := uint64(1000)

	model, err := registry.RegisterModel(
		ctx,
		"Test Model",
		"A test ML model",
		"ipfs://Qm...",
		aicommons.ModelClassification,
		proposer,
		currentBlock,
	)

	if err != nil {
		t.Fatalf("Failed to register model: %v", err)
	}

	if model.ModelID == (types.Hash{}) {
		t.Error("Model ID should not be empty")
	}

	if model.Status != aicommons.ModelActive {
		t.Error("New model should be active")
	}

	if model.Proposer != proposer {
		t.Error("Proposer should match")
	}
}

// Test contribution tracking
func TestContributionTracking(t *testing.T) {
	ctx := context.Background()
	store := newMockAICommonsStore()
	registry := aicommons.NewModelRegistry(store)

	// Register model
	proposer := types.Address{1}
	model, _ := registry.RegisterModel(
		ctx, "Model", "Desc", "ipfs://Qm", aicommons.ModelClassification, proposer, 1000,
	)

	// Add contributions
	contributors := []types.Address{
		{10, 10, 10},
		{20, 20, 20},
		{30, 30, 30},
	}

	computes := []uint64{100, 200, 300}

	for i, c := range contributors {
		contrib := &aicommons.Contribution{
			Contributor:  c,
			ModelID:      model.ModelID,
			Compute:      computes[i],
			Quality:      0.9,
			GradientHash: types.Hash{byte(i)},
			Timestamp:    1000 + uint64(i),
		}

		err := registry.RecordContribution(ctx, contrib)
		if err != nil {
			t.Fatalf("Failed to record contribution: %v", err)
		}
	}

	// Check total compute
	m := registry.GetModel(model.ModelID)
	expectedCompute := uint64(100 + 200 + 300)
	if m.TotalCompute != expectedCompute {
		t.Errorf("Total compute should be %d, got %d", expectedCompute, m.TotalCompute)
	}

	// Check contributor shares
	shares := registry.CalculateShares(model.ModelID)
	if len(shares) != 3 {
		t.Errorf("Should have 3 contributors, got %d", len(shares))
	}

	// Contributor with 300 compute should have 50% share
	expectedShare := float64(300) / float64(600)
	if shares[contributors[2]] != expectedShare {
		t.Errorf("Contributor share should be %.2f, got %.2f", expectedShare, shares[contributors[2]])
	}
}

// Test licensing
func TestLicensing(t *testing.T) {
	ctx := context.Background()
	store := newMockAICommonsStore()
	registry := aicommons.NewModelRegistry(store)
	lm := aicommons.NewLicenseManager(store, registry)

	// Register model
	proposer := types.Address{1}
	model, _ := registry.RegisterModel(
		ctx, "Model", "Desc", "ipfs://Qm", aicommons.ModelClassification, proposer, 1000,
	)

	// Grant license
	licensee := types.Address{50, 50, 50}
	payment := uint64(10000)

	license, err := lm.GrantLicense(
		ctx,
		model.ModelID,
		licensee,
		types.LicenseCommercial,
		payment,
		2000,
	)

	if err != nil {
		t.Fatalf("Failed to grant license: %v", err)
	}

	if license.LicenseID == (types.Hash{}) {
		t.Error("License ID should not be empty")
	}

	if license.LicenseeAddr != licensee {
		t.Error("Licensee should match")
	}

	// Verify license
	valid := lm.VerifyLicense(ctx, license.LicenseID, licensee, 2500)
	if !valid {
		t.Error("License should be valid")
	}

	// Verify with wrong address
	wrongAddr := types.Address{99, 99, 99}
	valid = lm.VerifyLicense(ctx, license.LicenseID, wrongAddr, 2500)
	if valid {
		t.Error("License should not be valid for wrong address")
	}
}

// Test revenue distribution
func TestRevenueDistribution(t *testing.T) {
	ctx := context.Background()
	store := newMockAICommonsStore()
	registry := aicommons.NewModelRegistry(store)
	lm := aicommons.NewLicenseManager(store, registry)

	// Register model
	proposer := types.Address{1}
	model, _ := registry.RegisterModel(
		ctx, "Model", "Desc", "ipfs://Qm", aicommons.ModelClassification, proposer, 1000,
	)

	// Add contributions
	c1 := types.Address{10}
	c2 := types.Address{20}

	_ = registry.RecordContribution(ctx, &aicommons.Contribution{
		Contributor: c1, ModelID: model.ModelID, Compute: 100, Quality: 1.0,
	})
	_ = registry.RecordContribution(ctx, &aicommons.Contribution{
		Contributor: c2, ModelID: model.ModelID, Compute: 100, Quality: 1.0,
	})

	// Grant license
	payment := uint64(10000)
	_, _ = lm.GrantLicense(ctx, model.ModelID, types.Address{50}, types.LicenseCommercial, payment, 2000)

	// Distribute revenue
	distribution := lm.DistributeRevenue(ctx, model.ModelID)

	// Each contributor should get half
	if distribution[c1] != 5000 || distribution[c2] != 5000 {
		t.Errorf("Revenue distribution incorrect: c1=%d, c2=%d", distribution[c1], distribution[c2])
	}
}

// Test task assignment
func TestTaskAssignment(t *testing.T) {
	ctx := context.Background()
	store := newMockAICommonsStore()
	registry := aicommons.NewModelRegistry(store)
	ta := aicommons.NewTaskAssigner(store, registry)

	// Register model
	proposer := types.Address{1}
	model, _ := registry.RegisterModel(
		ctx, "Model", "Desc", "ipfs://Qm", aicommons.ModelClassification, proposer, 1000,
	)

	// Create task
	task := &aicommons.TrainingTask{
		ModelID:    model.ModelID,
		DatasetCID: "ipfs://QmDataset",
		BatchStart: 0,
		BatchEnd:   1000,
	}

	err := ta.AddTask(ctx, task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Assign task
	miner := types.Address{100}
	vrfProof := make([]byte, 32)
	currentBlock := uint64(2000)

	assignment, err := ta.AssignTask(ctx, task.TaskID, miner, vrfProof, currentBlock)
	if err != nil {
		t.Fatalf("Failed to assign task: %v", err)
	}

	if assignment.AssignedTo != miner {
		t.Error("Task should be assigned to miner")
	}

	if assignment.Status != aicommons.StatusAssigned {
		t.Error("Assignment status should be assigned")
	}
}
