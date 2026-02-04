package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
)

const (
	serviceKeyPrefix  = "service:render:"
	serviceListKey    = "services:render:list"
	RegistryTTL       = 3 * time.Second // TTL for service registration (allows 2 missed heartbeats)
	HeartbeatInterval = 1 * time.Second // Heartbeat update frequency
)

type ServiceRegistry struct {
	redis  *redis.Client
	logger *zap.Logger
}

type ServiceInfo struct {
	ID       string            `json:"id"`
	Address  string            `json:"address"`
	Port     int               `json:"port"`
	Capacity int               `json:"capacity"`
	Load     int               `json:"load"`
	LastSeen time.Time         `json:"last_seen"`
	Version  string            `json:"version,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (si *ServiceInfo) URL() string {
	return fmt.Sprintf("http://%s:%d", si.Address, si.Port)
}

func (si *ServiceInfo) IsHealthy() bool {
	return time.Now().UTC().Sub(si.LastSeen) < 1*RegistryTTL
}

func (si *ServiceInfo) LoadPercentage() float64 {
	if si.Capacity <= 0 {
		return 100.0
	}
	return float64(si.Load) / float64(si.Capacity) * 100.0
}

// SetMetadata populates the metadata map with pool stats and hostname
func (si *ServiceInfo) SetMetadata(poolSize int, available int, hostname string) {
	if si.Metadata == nil {
		si.Metadata = make(map[string]string)
	}
	si.Metadata["pool_size"] = fmt.Sprintf("%d", poolSize)
	si.Metadata["available"] = fmt.Sprintf("%d", available)
	si.Metadata["hostname"] = hostname
}

func NewServiceRegistry(redisClient *redis.Client, logger *zap.Logger) *ServiceRegistry {
	return &ServiceRegistry{
		redis:  redisClient,
		logger: logger,
	}
}

func (sr *ServiceRegistry) RegisterService(ctx context.Context, info *ServiceInfo) error {
	if info.ID == "" {
		return fmt.Errorf("service ID is required")
	}
	if info.Address == "" {
		return fmt.Errorf("service address is required")
	}
	if info.Port <= 0 {
		return fmt.Errorf("service port must be positive")
	}

	info.LastSeen = time.Now().UTC()

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal service info: %w", err)
	}

	serviceKey := serviceKeyPrefix + info.ID

	if err := sr.redis.Set(ctx, serviceKey, data, RegistryTTL); err != nil {
		sr.logger.Error("Failed to register service",
			zap.String("service_id", info.ID),
			zap.Error(err))
		return fmt.Errorf("failed to register service: %w", err)
	}

	if err := sr.redis.HSet(ctx, serviceListKey, info.ID, info.URL()); err != nil {
		sr.logger.Error("Failed to add service to list",
			zap.String("service_id", info.ID),
			zap.Error(err))
		return fmt.Errorf("failed to add service to list: %w", err)
	}

	/*sr.logger.Info("RS Service registered successfully",
	zap.String("service_id", info.ID),
	zap.String("url", info.URL()))*/

	return nil
}

func (sr *ServiceRegistry) UnregisterService(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return fmt.Errorf("service ID is required")
	}

	serviceKey := serviceKeyPrefix + serviceID

	exists, err := sr.redis.Exists(ctx, serviceKey)
	if err != nil {
		return fmt.Errorf("failed to check service existence: %w", err)
	}

	if !exists {
		sr.logger.Warn("Attempted to unregister non-existent service",
			zap.String("service_id", serviceID))
		return nil
	}

	if err := sr.redis.Del(ctx, serviceKey); err != nil {
		sr.logger.Error("Failed to delete service key",
			zap.String("service_id", serviceID),
			zap.Error(err))
		return fmt.Errorf("failed to delete service: %w", err)
	}

	if err := sr.redis.HSet(ctx, serviceListKey, serviceID, ""); err != nil {
		sr.logger.Error("Failed to remove service from list",
			zap.String("service_id", serviceID),
			zap.Error(err))
	}

	sr.logger.Info("Service unregistered successfully",
		zap.String("service_id", serviceID))

	return nil
}

func (sr *ServiceRegistry) GetService(ctx context.Context, serviceID string) (*ServiceInfo, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("service ID is required")
	}

	serviceKey := serviceKeyPrefix + serviceID
	data, err := sr.redis.Get(ctx, serviceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	if data == "" {
		return nil, nil
	}

	var info ServiceInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		sr.logger.Error("Failed to unmarshal service info",
			zap.String("service_id", serviceID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to unmarshal service info: %w", err)
	}

	return &info, nil
}

func (sr *ServiceRegistry) ListServices(ctx context.Context) ([]*ServiceInfo, error) {
	keys, err := sr.redis.Keys(ctx, serviceKeyPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("failed to list service keys: %w", err)
	}

	if len(keys) == 0 {
		return []*ServiceInfo{}, nil
	}

	services := make([]*ServiceInfo, 0, len(keys))

	for _, key := range keys {
		data, err := sr.redis.Get(ctx, key)
		if err != nil {
			sr.logger.Warn("Failed to get service data",
				zap.String("key", key),
				zap.Error(err))
			continue
		}

		if data == "" {
			continue
		}

		var info ServiceInfo
		if err := json.Unmarshal([]byte(data), &info); err != nil {
			sr.logger.Warn("Failed to unmarshal service info",
				zap.String("key", key),
				zap.Error(err))
			continue
		}

		services = append(services, &info)
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].ID < services[j].ID
	})

	return services, nil
}

func (sr *ServiceRegistry) ListHealthyServices(ctx context.Context) ([]*ServiceInfo, error) {
	allServices, err := sr.ListServices(ctx)
	if err != nil {
		return nil, err
	}

	healthyServices := make([]*ServiceInfo, 0, len(allServices))
	for _, service := range allServices {
		if service.IsHealthy() {
			healthyServices = append(healthyServices, service)
		} else {
			sr.logger.Debug("Filtering out unhealthy service",
				zap.String("service_id", service.ID),
				zap.Time("last_seen", service.LastSeen))
		}
	}

	return healthyServices, nil
}

func (sr *ServiceRegistry) Heartbeat(ctx context.Context, serviceID string, load int) error {
	if serviceID == "" {
		return fmt.Errorf("service ID is required")
	}

	info, err := sr.GetService(ctx, serviceID)
	if err != nil {
		return fmt.Errorf("failed to get current service info: %w", err)
	}

	if info == nil {
		return fmt.Errorf("service not found: %s", serviceID)
	}

	info.Load = load
	info.LastSeen = time.Now().UTC()

	return sr.RegisterService(ctx, info)
}

func (sr *ServiceRegistry) CleanupStaleServices(ctx context.Context) error {
	services, err := sr.ListServices(ctx)
	if err != nil {
		return err
	}

	staleThreshold := time.Now().UTC().Add(-1 * RegistryTTL)
	staleCount := 0

	for _, service := range services {
		if service.LastSeen.Before(staleThreshold) {
			if err := sr.UnregisterService(ctx, service.ID); err != nil {
				sr.logger.Warn("Failed to cleanup stale service",
					zap.String("service_id", service.ID),
					zap.Error(err))
			} else {
				staleCount++
			}
		}
	}

	if staleCount > 0 {
		sr.logger.Info("Cleaned up stale services", zap.Int("count", staleCount))
	}

	return nil
}
