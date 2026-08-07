package registry

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
)

const (
	tabsKeyPrefix = "tabs:"

	// defaultReservationTTL bounds how long a reservation can hold a tab before
	// the render service's heartbeat pushes the tabs hash TTL back to RegistryTTL
	defaultReservationTTL = 2 * time.Second

	reasonNoServices = "no_services"
	reasonNoCapacity = "no_capacity"

	// availableTab marks a tab as free; a reserved tab holds the request ID
	availableTab = ""
)

var (
	// ErrNoServices means no render service is currently registered and alive
	ErrNoServices = errors.New("no healthy render services available")

	// ErrNoCapacity means every live render service is saturated
	ErrNoCapacity = errors.New("all render services at capacity")
)

// selectAndReserveScript atomically selects a healthy render service and reserves an available tab.
//
// Services are discovered through the serviceListKey index rather than a KEYS scan: the scan
// is O(keyspace) and, inside an atomic script, blocks every other Redis client for its full
// duration. The per-service keys remain the liveness source of truth via their TTL.
const selectAndReserveScript = `
-- Atomically selects a healthy render service and reserves an available tab
-- ARGV[1] = request_id
-- ARGV[2] = strategy ("least_loaded" or "most_available")
-- ARGV[3] = reservation TTL (seconds)

local request_id = ARGV[1]
local strategy = ARGV[2]
local reservation_ttl = tonumber(ARGV[3])

local index_key = KEYS[1]
local service_prefix = ARGV[4]
local tabs_prefix = ARGV[5]

local index = redis.call('HGETALL', index_key)

local candidates = {}
local live = 0

for i = 1, #index, 2 do
    local service_id = index[i]
    local url = index[i + 1]

    -- Empty URL is the soft-delete marker written by builds predating HDEL
    if url == '' then
        redis.call('HDEL', index_key, service_id)
    else
        local service_data = redis.call('GET', service_prefix .. service_id)

        if not service_data then
            -- Absent key means the TTL lapsed: the instance is dead. Unambiguous
            -- here because a Redis-level error aborts the script instead of
            -- returning a value we could misread as absence.
            redis.call('HDEL', index_key, service_id)
        else
            live = live + 1

            -- pcall, not a bare decode: a corrupt or half-written value would
            -- otherwise abort the whole script, so one bad record would fail
            -- selection for every service behind it in the index. The key exists,
            -- so this service is alive with a bad value; skip it and keep the
            -- index field, as the Go readers do. Its next heartbeat rewrites it.
            local decoded, service = pcall(cjson.decode, service_data)

            if decoded and type(service) == 'table' then
                -- tonumber, not a bare comparison: JSON null decodes to a truthy
                -- userdata that would raise on <, and address/port must be usable
                -- or the reply truncates and the reserved tab leaks
                local capacity = tonumber(service.capacity)
                local load = tonumber(service.load) or 0
                local port = tonumber(service.port)
                local address = service.address

                if capacity and capacity > 0 and load < capacity
                    and port and type(address) == 'string' and address ~= '' then
                    local tabs_key = tabs_prefix .. service_id
                    local tabs = redis.call('HGETALL', tabs_key)
                    local available_count = 0
                    local first_available = nil

                    for j = 1, #tabs, 2 do
                        if tabs[j + 1] == '' then
                            local tab_id = tonumber(tabs[j])
                            if tab_id then
                                available_count = available_count + 1
                                if first_available == nil then
                                    first_available = tab_id
                                end
                            end
                        end
                    end

                    if available_count > 0 then
                        table.insert(candidates, {
                            service_id = service_id,
                            address = address,
                            port = port,
                            tabs_key = tabs_key,
                            available_count = available_count,
                            first_available = first_available,
                            load_pct = load / capacity
                        })
                    end
                end
            end
        end
    end
end

if live == 0 then
    return {false, 'no_services'}
end

if #candidates == 0 then
    return {false, 'no_capacity'}
end

local selected = candidates[1]

if strategy == 'least_loaded' then
    for _, candidate in ipairs(candidates) do
        if candidate.load_pct < selected.load_pct then
            selected = candidate
        end
    end
elseif strategy == 'most_available' then
    for _, candidate in ipairs(candidates) do
        if candidate.available_count > selected.available_count then
            selected = candidate
        end
    end
end

local tab_id = selected.first_available
redis.call('HSET', selected.tabs_key, tostring(tab_id), request_id)
redis.call('EXPIRE', selected.tabs_key, reservation_ttl)

return {
    selected.service_id,
    tostring(tab_id),
    selected.address,
    tostring(selected.port)
}
`

// TabReservation identifies a tab reserved on a specific render service
type TabReservation struct {
	ServiceID string
	TabID     int
	Address   string
	Port      int
}

// TabSelector picks a render service with free capacity and reserves one of its tabs.
// Selection and reservation happen in a single atomic script so concurrent Edge Gateways
// cannot hand out the same tab twice.
type TabSelector struct {
	redis  *redis.Client
	logger *zap.Logger
}

func NewTabSelector(redisClient *redis.Client, logger *zap.Logger) *TabSelector {
	return &TabSelector{
		redis:  redisClient,
		logger: logger,
	}
}

// SelectAndReserve reserves a tab on the service chosen by the given strategy.
// Returns ErrNoServices when nothing is registered and alive, ErrNoCapacity when every
// live service is saturated.
func (ts *TabSelector) SelectAndReserve(ctx context.Context, requestID string, strategy string) (*TabReservation, error) {
	result, err := ts.redis.Eval(
		ctx,
		selectAndReserveScript,
		[]string{serviceListKey},
		requestID,
		strategy,
		int(defaultReservationTTL.Seconds()),
		serviceKeyPrefix,
		tabsKeyPrefix,
	)
	if err != nil {
		// Covers a WRONGTYPE on the index, which no heartbeat can repair, so it
		// must not stay invisible behind a bare error return
		ts.logger.Error("Service selection script failed",
			zap.String("request_id", requestID),
			zap.String("index_key", serviceListKey),
			zap.Error(err))
		return nil, fmt.Errorf("failed to execute service selection script: %w", err)
	}

	fields, ok := result.([]interface{})
	if !ok || len(fields) < 2 {
		ts.logger.Error("Malformed service selection result",
			zap.String("request_id", requestID),
			zap.Any("result", result))
		return nil, fmt.Errorf("invalid service selection result")
	}

	// Saturation and an empty registry are expected states the caller reports;
	// everything below this point is a contract violation worth an Error
	if fields[0] == nil || fields[0] == false {
		return nil, selectionFailure(fields)
	}

	if len(fields) < 4 {
		ts.logger.Error("Incomplete service selection result",
			zap.String("request_id", requestID),
			zap.Int("field_count", len(fields)))
		return nil, fmt.Errorf("incomplete service selection result: %d fields", len(fields))
	}

	serviceID, _ := fields[0].(string)
	tabIDStr, _ := fields[1].(string)
	address, _ := fields[2].(string)
	portStr, _ := fields[3].(string)

	tabID, err := strconv.Atoi(tabIDStr)
	if err != nil {
		// The script already reserved this tab, so a parse failure here strands it
		ts.logger.Error("Service selection returned an unparseable tab_id",
			zap.String("request_id", requestID),
			zap.String("service_id", serviceID),
			zap.String("tab_id", tabIDStr),
			zap.Error(err))
		return nil, fmt.Errorf("invalid tab_id %q: %w", tabIDStr, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		ts.logger.Error("Service selection returned an unparseable port",
			zap.String("request_id", requestID),
			zap.String("service_id", serviceID),
			zap.String("port", portStr),
			zap.Error(err))
		return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
	}

	return &TabReservation{
		ServiceID: serviceID,
		TabID:     tabID,
		Address:   address,
		Port:      port,
	}, nil
}

// selectionFailure maps the script's reason code to a sentinel error
func selectionFailure(fields []interface{}) error {
	reason, _ := fields[1].(string)

	switch reason {
	case reasonNoServices:
		return ErrNoServices
	case reasonNoCapacity:
		return ErrNoCapacity
	default:
		return fmt.Errorf("service selection failed: %s", reason)
	}
}

// Release marks the reserved tab available again. The Edge Gateway owns tab lifecycle,
// so this runs on every exit path of a render.
func (ts *TabSelector) Release(ctx context.Context, reservation *TabReservation) error {
	if reservation == nil {
		return nil
	}

	tabsKey := tabsKeyPrefix + reservation.ServiceID
	if err := ts.redis.HSet(ctx, tabsKey, strconv.Itoa(reservation.TabID), availableTab); err != nil {
		return fmt.Errorf("failed to release tab reservation: %w", err)
	}

	return nil
}
