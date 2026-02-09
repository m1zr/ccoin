// Package aicommons implements licensing for AI Commons models.
package aicommons

import (
	"context"
	"errors"
	"sync"

	"github.com/ccoin/core/pkg/types"
)

// Licensing errors
var (
	ErrLicenseNotFound     = errors.New("license not found")
	ErrLicenseExpired      = errors.New("license expired")
	ErrInsufficientPayment = errors.New("insufficient payment for license")
	ErrCommercialNotAllowed = errors.New("commercial use not allowed")
)

// LicenseManager manages model licensing
type LicenseManager struct {
	mu sync.RWMutex

	// Active licenses
	licenses map[types.Hash]*License

	// License templates
	templates map[types.LicenseType]*LicenseTemplate

	// Revenue tracking
	revenue map[types.Hash]uint64 // modelID -> total revenue

	// Storage
	store LicenseStore
}

// License represents an active license
type License struct {
	LicenseID    types.Hash
	ModelID      types.Hash
	LicenseeAddr types.Address
	LicenseType  types.LicenseType
	GrantedAt    uint64
	ExpiresAt    uint64
	MaxInferences uint64
	UsedInferences uint64
	CommercialUse bool
	PaymentAmount uint64
}

// LicenseTemplate defines terms for a license type
type LicenseTemplate struct {
	Type          types.LicenseType
	Name          string
	CommercialUse bool
	Duration      uint64 // in blocks
	BaseFee       uint64
	InferenceFee  uint64 // per inference
	RevenueShare  float64 // 0.0 - 1.0, share to contributors
}

// LicenseStore defines persistence for licenses
type LicenseStore interface {
	SaveLicense(ctx context.Context, license *License) error
	GetLicense(ctx context.Context, id types.Hash) (*License, error)
	GetLicensesForModel(ctx context.Context, modelID types.Hash) ([]*License, error)
	GetLicensesForAddress(ctx context.Context, addr types.Address) ([]*License, error)
}

// NewLicenseManager creates a new license manager
func NewLicenseManager(store LicenseStore) *LicenseManager {
	lm := &LicenseManager{
		licenses:  make(map[types.Hash]*License),
		templates: make(map[types.LicenseType]*LicenseTemplate),
		revenue:   make(map[types.Hash]uint64),
		store:     store,
	}

	// Initialize default license templates
	lm.initDefaultTemplates()

	return lm
}

// initDefaultTemplates sets up the default license types
func (lm *LicenseManager) initDefaultTemplates() {
	lm.templates[types.LicenseOpenSource] = &LicenseTemplate{
		Type:          types.LicenseOpenSource,
		Name:          "Open Source",
		CommercialUse: false,
		Duration:      0, // Unlimited
		BaseFee:       0,
		InferenceFee:  0,
		RevenueShare:  0,
	}

	lm.templates[types.LicenseRestrictedCommercial] = &LicenseTemplate{
		Type:          types.LicenseRestrictedCommercial,
		Name:          "Restricted Commercial",
		CommercialUse: true,
		Duration:      100000, // ~11 days at 10s blocks
		BaseFee:       10000,  // Base license fee
		InferenceFee:  1,      // Per inference
		RevenueShare:  0.7,    // 70% to contributors
	}

	lm.templates[types.LicenseFullCommercial] = &LicenseTemplate{
		Type:          types.LicenseFullCommercial,
		Name:          "Full Commercial",
		CommercialUse: true,
		Duration:      1000000, // ~115 days
		BaseFee:       100000,
		InferenceFee:  0, // Unlimited inferences
		RevenueShare:  0.5,
	}

	lm.templates[types.LicenseResearchOnly] = &LicenseTemplate{
		Type:          types.LicenseResearchOnly,
		Name:          "Research Only",
		CommercialUse: false,
		Duration:      500000,
		BaseFee:       0,
		InferenceFee:  0,
		RevenueShare:  0,
	}
}

// GrantLicense grants a new license
func (lm *LicenseManager) GrantLicense(
	ctx context.Context,
	modelID types.Hash,
	licensee types.Address,
	licenseType types.LicenseType,
	paymentAmount uint64,
	currentBlock uint64,
) (*License, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	template, exists := lm.templates[licenseType]
	if !exists {
		return nil, ErrLicenseNotFound
	}

	// Check payment
	if paymentAmount < template.BaseFee {
		return nil, ErrInsufficientPayment
	}

	// Calculate expiry
	var expiresAt uint64
	if template.Duration > 0 {
		expiresAt = currentBlock + template.Duration
	}

	// Generate license ID
	licenseID := lm.generateLicenseID(modelID, licensee, currentBlock)

	license := &License{
		LicenseID:     licenseID,
		ModelID:       modelID,
		LicenseeAddr:  licensee,
		LicenseType:   licenseType,
		GrantedAt:     currentBlock,
		ExpiresAt:     expiresAt,
		MaxInferences: 0, // Unlimited unless specified
		CommercialUse: template.CommercialUse,
		PaymentAmount: paymentAmount,
	}

	lm.licenses[licenseID] = license
	lm.revenue[modelID] += paymentAmount

	return license, lm.store.SaveLicense(ctx, license)
}

// generateLicenseID generates a unique license ID
func (lm *LicenseManager) generateLicenseID(modelID types.Hash, licensee types.Address, block uint64) types.Hash {
	data := append(modelID[:], licensee[:]...)
	data = append(data, uint64ToBytes(block)...)

	var id types.Hash
	for i := 0; i < types.HashSize && i < len(data); i++ {
		id[i] = data[i] ^ byte(i*17)
	}
	return id
}

// CheckLicense verifies a license is valid for use
func (lm *LicenseManager) CheckLicense(ctx context.Context, licenseID types.Hash, currentBlock uint64) error {
	lm.mu.RLock()
	license, exists := lm.licenses[licenseID]
	lm.mu.RUnlock()

	if !exists {
		// Try to load from store
		var err error
		license, err = lm.store.GetLicense(ctx, licenseID)
		if err != nil || license == nil {
			return ErrLicenseNotFound
		}
	}

	// Check expiry
	if license.ExpiresAt > 0 && currentBlock > license.ExpiresAt {
		return ErrLicenseExpired
	}

	// Check inference limit
	if license.MaxInferences > 0 && license.UsedInferences >= license.MaxInferences {
		return errors.New("inference limit exceeded")
	}

	return nil
}

// RecordInference records an inference against a license
func (lm *LicenseManager) RecordInference(ctx context.Context, licenseID types.Hash) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	license, exists := lm.licenses[licenseID]
	if !exists {
		return ErrLicenseNotFound
	}

	license.UsedInferences++

	// Charge inference fee if applicable
	template := lm.templates[license.LicenseType]
	if template != nil && template.InferenceFee > 0 {
		lm.revenue[license.ModelID] += template.InferenceFee
	}

	return lm.store.SaveLicense(ctx, license)
}

// GetModelRevenue returns total revenue for a model
func (lm *LicenseManager) GetModelRevenue(modelID types.Hash) uint64 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.revenue[modelID]
}

// DistributeRevenue calculates revenue distribution to contributors
func (lm *LicenseManager) DistributeRevenue(
	modelID types.Hash,
	registry *ModelRegistry,
) map[types.Address]uint64 {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	totalRevenue := lm.revenue[modelID]
	if totalRevenue == 0 {
		return nil
	}

	// Get model and calculate shares
	model, err := registry.GetModel(context.Background(), modelID)
	if err != nil {
		return nil
	}

	// Get license template for revenue share calculation
	template := lm.templates[model.License]
	if template == nil {
		return nil
	}

	distributableRevenue := uint64(float64(totalRevenue) * template.RevenueShare)

	// Calculate per-contributor distribution
	distribution := make(map[types.Address]uint64)
	for addr, compute := range model.Contributors {
		share := float64(compute) / float64(model.TotalCompute)
		distribution[addr] = uint64(float64(distributableRevenue) * share)
	}

	// Reset revenue after distribution
	lm.revenue[modelID] = totalRevenue - distributableRevenue

	return distribution
}

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

// GetLicenseTemplate returns a license template
func (lm *LicenseManager) GetLicenseTemplate(licenseType types.LicenseType) *LicenseTemplate {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.templates[licenseType]
}

// GetActiveLicenses returns active licenses for an address
func (lm *LicenseManager) GetActiveLicenses(ctx context.Context, addr types.Address, currentBlock uint64) ([]*License, error) {
	all, err := lm.store.GetLicensesForAddress(ctx, addr)
	if err != nil {
		return nil, err
	}

	active := make([]*License, 0)
	for _, lic := range all {
		if lic.ExpiresAt == 0 || currentBlock <= lic.ExpiresAt {
			active = append(active, lic)
		}
	}

	return active, nil
}
