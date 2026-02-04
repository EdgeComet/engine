package sharding

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"
)

// Distributor determines which EG instances should store a given cache entry
type Distributor interface {
	ComputeTargets(ctx context.Context, cacheKey string, renderingEgID string, replicationFactor int) ([]string, error)
	ComputeHashTargets(ctx context.Context, cacheKey string, replicationFactor int) ([]string, error)
}

// HashModuloDistributor implements deterministic distribution using hash modulo
type HashModuloDistributor struct {
	registry Registry
	logger   *zap.Logger
}

// NewHashModuloDistributor creates a new hash modulo distributor
func NewHashModuloDistributor(registry Registry, logger *zap.Logger) *HashModuloDistributor {
	return &HashModuloDistributor{
		registry: registry,
		logger:   logger,
	}
}

// ComputeTargets computes target EGs using hash modulo algorithm
// Algorithm:
// 1. Get healthy EGs and sort alphabetically (deterministic ordering)
// 2. Compute primary index: XXHash64(cacheKey) % numEGs
// 3. Select N consecutive EGs starting from primary (with wrap-around)
// 4. Ensure rendering EG is included in target list
func (d *HashModuloDistributor) ComputeTargets(ctx context.Context, cacheKey string, renderingEgID string, replicationFactor int) ([]string, error) {
	if replicationFactor <= 0 {
		return []string{renderingEgID}, nil
	}

	// Get all healthy EGs
	healthyEGs, err := d.registry.GetHealthyEGs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy EGs: %w", err)
	}

	if len(healthyEGs) == 0 {
		return []string{renderingEgID}, nil
	}

	// Extract EG IDs (already sorted alphabetically by registry)
	egIDs := make([]string, len(healthyEGs))
	for i, eg := range healthyEGs {
		egIDs[i] = eg.EgID
	}

	// If replication factor exceeds cluster size, cap it
	actualReplication := replicationFactor
	if actualReplication > len(egIDs) {
		actualReplication = len(egIDs)
	}

	// Compute hash and primary index
	hashValue := xxhash.Sum64String(cacheKey)
	primaryIndex := int(hashValue % uint64(len(egIDs)))

	// Select N consecutive EGs with wrap-around
	targets := make([]string, 0, actualReplication)
	for i := 0; i < actualReplication; i++ {
		targetIndex := (primaryIndex + i) % len(egIDs)
		targets = append(targets, egIDs[targetIndex])
	}

	// Ensure rendering EG is in target list
	renderingIncluded := false
	for _, target := range targets {
		if target == renderingEgID {
			renderingIncluded = true
			break
		}
	}

	if !renderingIncluded {
		// Replace first target with rendering EG
		if len(targets) > 0 {
			targets[0] = renderingEgID
		} else {
			targets = []string{renderingEgID}
		}
	}

	d.logger.Debug("Computed distribution targets",
		zap.String("cache_key", cacheKey),
		zap.String("rendering_eg", renderingEgID),
		zap.Int("replication_factor", replicationFactor),
		zap.Int("cluster_size", len(egIDs)),
		zap.Strings("targets", targets))

	return targets, nil
}

// ComputeHashTargets computes target EGs using ONLY hash distribution (no rendering EG override)
// Used for pull operations to check if an EG should store pulled cache based on pure hash distribution
func (d *HashModuloDistributor) ComputeHashTargets(ctx context.Context, cacheKey string, replicationFactor int) ([]string, error) {
	if replicationFactor <= 0 {
		return []string{}, nil
	}

	// Get all healthy EGs
	healthyEGs, err := d.registry.GetHealthyEGs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy EGs: %w", err)
	}

	if len(healthyEGs) == 0 {
		return []string{}, nil
	}

	// Extract EG IDs (already sorted alphabetically by registry)
	egIDs := make([]string, len(healthyEGs))
	for i, eg := range healthyEGs {
		egIDs[i] = eg.EgID
	}

	// If replication factor exceeds cluster size, cap it
	actualReplication := replicationFactor
	if actualReplication > len(egIDs) {
		actualReplication = len(egIDs)
	}

	// Compute hash and primary index
	hashValue := xxhash.Sum64String(cacheKey)
	primaryIndex := int(hashValue % uint64(len(egIDs)))

	// Select N consecutive EGs with wrap-around (NO rendering EG override)
	targets := make([]string, 0, actualReplication)
	for i := 0; i < actualReplication; i++ {
		targetIndex := (primaryIndex + i) % len(egIDs)
		targets = append(targets, egIDs[targetIndex])
	}

	d.logger.Debug("Computed hash-based targets (no rendering override)",
		zap.String("cache_key", cacheKey),
		zap.Int("replication_factor", replicationFactor),
		zap.Int("cluster_size", len(egIDs)),
		zap.Strings("targets", targets))

	return targets, nil
}

// RandomDistributor implements random distribution
type RandomDistributor struct {
	registry Registry
	logger   *zap.Logger
}

// NewRandomDistributor creates a new random distributor
func NewRandomDistributor(registry Registry, logger *zap.Logger) *RandomDistributor {
	return &RandomDistributor{
		registry: registry,
		logger:   logger,
	}
}

// ComputeTargets computes target EGs using random selection
// Algorithm:
// 1. Get healthy EGs
// 2. Randomly select N EGs
// 3. Ensure rendering EG is included
func (d *RandomDistributor) ComputeTargets(ctx context.Context, cacheKey string, renderingEgID string, replicationFactor int) ([]string, error) {
	if replicationFactor <= 0 {
		return []string{renderingEgID}, nil
	}

	// Get all healthy EGs
	healthyEGs, err := d.registry.GetHealthyEGs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy EGs: %w", err)
	}

	if len(healthyEGs) == 0 {
		return []string{renderingEgID}, nil
	}

	// Extract EG IDs
	egIDs := make([]string, len(healthyEGs))
	for i, eg := range healthyEGs {
		egIDs[i] = eg.EgID
	}

	// If replication factor exceeds cluster size, use all EGs
	actualReplication := replicationFactor
	if actualReplication > len(egIDs) {
		actualReplication = len(egIDs)
	}

	// Randomly shuffle EG IDs
	shuffled := make([]string, len(egIDs))
	copy(shuffled, egIDs)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Select first N
	targets := shuffled[:actualReplication]

	// Ensure rendering EG is in target list
	renderingIncluded := false
	for _, target := range targets {
		if target == renderingEgID {
			renderingIncluded = true
			break
		}
	}

	if !renderingIncluded {
		// Replace first target with rendering EG
		if len(targets) > 0 {
			targets[0] = renderingEgID
		} else {
			targets = []string{renderingEgID}
		}
	}

	d.logger.Debug("Computed random distribution targets",
		zap.String("cache_key", cacheKey),
		zap.String("rendering_eg", renderingEgID),
		zap.Int("replication_factor", replicationFactor),
		zap.Int("cluster_size", len(egIDs)),
		zap.Strings("targets", targets))

	return targets, nil
}

// ComputeHashTargets for random distributor (random selection without rendering EG override)
func (d *RandomDistributor) ComputeHashTargets(ctx context.Context, cacheKey string, replicationFactor int) ([]string, error) {
	if replicationFactor <= 0 {
		return []string{}, nil
	}

	// Get all healthy EGs
	healthyEGs, err := d.registry.GetHealthyEGs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy EGs: %w", err)
	}

	if len(healthyEGs) == 0 {
		return []string{}, nil
	}

	// Extract EG IDs
	egIDs := make([]string, len(healthyEGs))
	for i, eg := range healthyEGs {
		egIDs[i] = eg.EgID
	}

	// If replication factor exceeds cluster size, use all EGs
	actualReplication := replicationFactor
	if actualReplication > len(egIDs) {
		actualReplication = len(egIDs)
	}

	// Randomly shuffle EG IDs
	shuffled := make([]string, len(egIDs))
	copy(shuffled, egIDs)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Select first N (NO rendering EG override)
	targets := shuffled[:actualReplication]

	d.logger.Debug("Computed random targets (no rendering override)",
		zap.String("cache_key", cacheKey),
		zap.Int("replication_factor", replicationFactor),
		zap.Int("cluster_size", len(egIDs)),
		zap.Strings("targets", targets))

	return targets, nil
}

// PrimaryOnlyDistributor implements no distribution (only rendering EG stores cache)
type PrimaryOnlyDistributor struct {
	logger *zap.Logger
}

// NewPrimaryOnlyDistributor creates a new primary-only distributor
func NewPrimaryOnlyDistributor(logger *zap.Logger) *PrimaryOnlyDistributor {
	return &PrimaryOnlyDistributor{
		logger: logger,
	}
}

// ComputeTargets returns only the rendering EG
func (d *PrimaryOnlyDistributor) ComputeTargets(ctx context.Context, cacheKey string, renderingEgID string, replicationFactor int) ([]string, error) {
	d.logger.Debug("Primary-only distribution",
		zap.String("cache_key", cacheKey),
		zap.String("rendering_eg", renderingEgID))

	return []string{renderingEgID}, nil
}

// ComputeHashTargets for primary-only distributor (no distribution, no pull-and-store)
func (d *PrimaryOnlyDistributor) ComputeHashTargets(ctx context.Context, cacheKey string, replicationFactor int) ([]string, error) {
	// Primary-only: only rendering EG stores, no pull-and-store allowed
	d.logger.Debug("Primary-only distribution (no pull-and-store)",
		zap.String("cache_key", cacheKey))

	return []string{}, nil
}

// DistributorFactory creates distributors based on strategy name
func DistributorFactory(strategy string, registry Registry, logger *zap.Logger) (Distributor, error) {
	// Default to hash_modulo if empty
	if strategy == "" {
		strategy = "hash_modulo"
	}

	switch strategy {
	case "hash_modulo":
		return NewHashModuloDistributor(registry, logger), nil
	case "random":
		return NewRandomDistributor(registry, logger), nil
	case "primary_only":
		return NewPrimaryOnlyDistributor(logger), nil
	default:
		return nil, fmt.Errorf("unknown distribution strategy: %s", strategy)
	}
}

// EGIDsToString converts a list of EG IDs to a comma-separated string for storage
func EGIDsToString(egIDs []string) string {
	// Sort for consistent storage format
	sorted := make([]string, len(egIDs))
	copy(sorted, egIDs)
	sort.Strings(sorted)

	result := ""
	for i, id := range sorted {
		if i > 0 {
			result += ","
		}
		result += id
	}
	return result
}

// StringToEGIDs converts a comma-separated string to a list of EG IDs
func StringToEGIDs(egIDsStr string) []string {
	if egIDsStr == "" {
		return []string{}
	}

	parts := strings.Split(egIDsStr, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}
