package channelhealth

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
)

// Cluster coordination for the circuit breaker.
//
// EWMA and in-flight stay per-instance on purpose (soft signals; each replica
// balances its own view of latency/load, like Linkerd's per-proxy model). Only
// the circuit breaker's OPEN state is shared: a channel tripped by any instance
// must be excluded cluster-wide, otherwise other replicas keep hammering a dead
// channel. The local breaker still trips instantly for fast local reaction; the
// shared layer adds ~1s cluster convergence on top.
//
// Everything here is a no-op without Redis, so single-instance deployments are
// completely unaffected.

const (
	// sharedCircuitsKey is a Redis hash: field=channelID,
	// value=globalGeneration:openUntilUnixMilli.
	sharedCircuitsKey      = "new-api:channel_health:circuits:v2"
	sharedSequenceKey      = "new-api:channel_health:circuits:v2:sequence"
	sharedResetField       = "__reset_generation"
	clusterSyncInterval    = time.Second
	clusterMutationTimeout = time.Second
	// Sequence floors use Unix microseconds so they permanently dominate the
	// Unix-millisecond generations written by pre-sequence v2 instances during a
	// rolling deployment, while remaining exactly representable by Redis Lua.
	sharedGenerationScale int64 = 1000
	// sharedStaleGrace closes a shared entry this long past its openUntil, so an
	// instance that trips a channel then dies before recovery cannot pin it open.
	sharedStaleGrace = 5 * time.Minute
)

var (
	sharedCircuits        atomic.Value // map[int]int64 (channelID -> openUntilUnixMilli)
	clusterSyncOnce       sync.Once
	sharedMutationMu      sync.Mutex
	latestMutations       = make(map[int]sharedCircuitMutation)
	latestResetGeneration int64
)

type sharedCircuitMutation struct {
	generation int64
	openUntil  int64
}

// nextSharedGenerationScript allocates a cluster-global generation at the
// moment a transition occurs. If the sequence key was lost while the hash
// survived, it first recovers the greatest generation from the hash. The floor
// also places this allocator above legacy Unix-millisecond generations.
var nextSharedGenerationScript = redis.NewScript(`
local floor = tonumber(ARGV[1]) or 0
local reset_field = ARGV[2]
local current_raw = redis.call('GET', KEYS[1])
local current = tonumber(current_raw) or 0
if not current_raw then
  local entries = redis.call('HGETALL', KEYS[2])
  for i = 1, #entries, 2 do
    local field = entries[i]
    local raw = entries[i + 1]
    local entry_generation = 0
    if field == reset_field then
      entry_generation = tonumber(raw) or 0
    else
      local separator = string.find(raw, ':', 1, true)
      if separator then
        entry_generation = tonumber(string.sub(raw, 1, separator - 1)) or 0
      end
    end
    if entry_generation > current then
      current = entry_generation
    end
  end
end
if floor > current then
  current = floor
end
redis.call('SET', KEYS[1], string.format('%.0f', current))
return redis.call('INCR', KEYS[1])`)

// Shared values are generation:openUntil. Generation is assigned when the
// local transition occurs, not when its asynchronous Redis write runs, so a
// delayed old write cannot resurrect or shorten newer state. Legacy plain
// openUntil values are read as generation zero.
var publishOpenScript = redis.NewScript(`
local field = ARGV[1]
local generation_raw = ARGV[2]
local open_until_raw = ARGV[3]
local generation = tonumber(ARGV[2])
local open_until = tonumber(ARGV[3])
local reset_generation = tonumber(redis.call('HGET', KEYS[1], ARGV[4])) or -1
if generation <= reset_generation then
  return 0
end
local current = redis.call('HGET', KEYS[1], field)
local current_generation = -1
local current_generation_raw = '-1'
local current_until = 0
if current then
  local separator = string.find(current, ':', 1, true)
  if separator then
    current_generation_raw = string.sub(current, 1, separator - 1)
    current_generation = tonumber(current_generation_raw) or 0
    current_until = tonumber(string.sub(current, separator + 1)) or 0
  else
    current_generation = 0
    current_generation_raw = '0'
    current_until = tonumber(current) or 0
    if current_until > 100000000000000 then
      current_until = math.floor(current_until / 1000000)
    end
  end
end
if current_until <= 0 and generation > current_generation then
  redis.call('HSET', KEYS[1], field, generation_raw .. ':' .. open_until_raw)
  return 1
end
if current_until > 0 and open_until > current_until then
  local stored_generation_raw = generation_raw
  if current_generation > generation then
    stored_generation_raw = current_generation_raw
  end
  redis.call('HSET', KEYS[1], field, stored_generation_raw .. ':' .. open_until_raw)
  return 1
end
return 0`)

var publishClosedScript = redis.NewScript(`
local field = ARGV[1]
local generation_raw = ARGV[2]
local generation = tonumber(ARGV[2])
local reset_generation = tonumber(redis.call('HGET', KEYS[1], ARGV[3])) or -1
if generation <= reset_generation then
  return 0
end
local current = redis.call('HGET', KEYS[1], field)
local current_generation = -1
if current then
  local separator = string.find(current, ':', 1, true)
  if separator then
    current_generation = tonumber(string.sub(current, 1, separator - 1)) or 0
  else
    current_generation = 0
  end
end
if generation >= current_generation then
  redis.call('HSET', KEYS[1], field, generation_raw .. ':0')
  return 1
end
return 0`)

// pruneStaleCircuitsScript converts expired opens to closed tombstones. Retaining
// their generation prevents delayed pre-expiry writes from resurrecting them.
var pruneStaleCircuitsScript = redis.NewScript(`
local entries = redis.call('HGETALL', KEYS[1])
local threshold = tonumber(ARGV[1])
local reset_field = ARGV[2]
local removed = 0
for i = 1, #entries, 2 do
  local field = entries[i]
  local raw = entries[i + 1]
  local separator = string.find(raw, ':', 1, true)
  local generation_raw = '0'
  local open_until = 0
  if separator then
    generation_raw = string.sub(raw, 1, separator - 1)
    open_until = tonumber(string.sub(raw, separator + 1)) or 0
  else
    open_until = tonumber(raw) or 0
    if open_until > 100000000000000 then
      open_until = math.floor(open_until / 1000000)
    end
  end
  if field ~= reset_field and open_until > 0 and open_until < threshold then
    redis.call('HSET', KEYS[1], field, generation_raw .. ':0')
    removed = removed + 1
  end
end
return removed`)

// clearAllSharedScript atomically records a global reset generation and removes
// only per-channel states at or before it. A delayed old publish is rejected by
// the reset marker, while a genuinely newer trip survives regardless of Redis
// command arrival order.
var clearAllSharedScript = redis.NewScript(`
local reset_field = ARGV[1]
local generation_raw = ARGV[2]
local generation = tonumber(ARGV[2])
local current_reset = tonumber(redis.call('HGET', KEYS[1], reset_field)) or -1
if generation < current_reset then
  return 0
end
local entries = redis.call('HGETALL', KEYS[1])
for i = 1, #entries, 2 do
  local field = entries[i]
  if field ~= reset_field then
    local raw = entries[i + 1]
    local separator = string.find(raw, ':', 1, true)
    local entry_generation = 0
    if separator then
      entry_generation = tonumber(string.sub(raw, 1, separator - 1)) or 0
    end
    if entry_generation <= generation then
      redis.call('HDEL', KEYS[1], field)
    end
  end
end
redis.call('HSET', KEYS[1], reset_field, generation_raw)
return 1`)

func init() {
	sharedCircuits.Store(map[int]int64{})
}

func clusterEnabled() bool {
	return common.RedisEnabled && common.RDB != nil
}

func loadSharedCircuits() map[int]int64 {
	if v := sharedCircuits.Load(); v != nil {
		return v.(map[int]int64)
	}
	return nil
}

// sharedCircuitState reports the cluster-wide circuit view for a channel: open
// (cooldown not elapsed) or halfOpen (recently open, cooldown elapsed). It reads
// a lock-free snapshot map, so it is cheap on the hot path. An empty map (no
// Redis / single instance) yields both false, i.e. no effect.
func sharedCircuitState(channelID int, nowUnixMilli int64) (open bool, halfOpen bool) {
	openUntil, ok := loadSharedCircuits()[channelID]
	if !ok {
		return false, false
	}
	if openUntil > nowUnixMilli {
		return true, false
	}
	return false, true
}

func nextSharedMutationGeneration() int64 {
	if !clusterEnabled() {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), clusterMutationTimeout)
	defer cancel()
	floor := time.Now().UnixMilli() * sharedGenerationScale
	generation, err := nextSharedGenerationScript.Run(
		ctx,
		common.RDB,
		[]string{sharedSequenceKey, sharedCircuitsKey},
		floor,
		sharedResetField,
	).Int64()
	if err != nil || generation <= 0 {
		if err != nil {
			common.SysError("allocate shared circuit generation failed: " + err.Error())
		}
		return 0
	}
	return generation
}

func parseSharedCircuitValue(value string) (generation int64, openUntil int64, ok bool) {
	if before, after, found := strings.Cut(value, ":"); found {
		generation, errGeneration := strconv.ParseInt(before, 10, 64)
		openUntil, errOpenUntil := strconv.ParseInt(after, 10, 64)
		if errGeneration != nil || errOpenUntil != nil {
			return 0, 0, false
		}
		return generation, openUntil, true
	}
	openUntil, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	// v1 stored plain nanosecond timestamps. Normalize them to the millisecond
	// representation used by versioned values before exposing the snapshot.
	if openUntil > 100_000_000_000_000 {
		openUntil /= int64(time.Millisecond)
	}
	return 0, openUntil, true
}

// registerSharedMutation mirrors the Redis scripts' ordering rules locally so
// stale work can be dropped before entering the async pool. Opens only extend a
// live cooldown; a close leaves a versioned tombstone that rejects older opens.
func registerSharedMutation(channelID int, mutation sharedCircuitMutation) bool {
	sharedMutationMu.Lock()
	defer sharedMutationMu.Unlock()
	if mutation.generation <= latestResetGeneration {
		return false
	}
	current, exists := latestMutations[channelID]
	if !exists {
		latestMutations[channelID] = mutation
		return true
	}
	if mutation.openUntil <= 0 {
		if mutation.generation < current.generation {
			return false
		}
		latestMutations[channelID] = mutation
		return true
	}
	if current.openUntil <= 0 {
		if mutation.generation <= current.generation {
			return false
		}
		latestMutations[channelID] = mutation
		return true
	}
	if mutation.openUntil <= current.openUntil {
		return false
	}
	if mutation.generation < current.generation {
		mutation.generation = current.generation
	}
	latestMutations[channelID] = mutation
	return true
}

func registerSharedReset(generation int64) bool {
	if generation <= 0 {
		return false
	}
	sharedMutationMu.Lock()
	defer sharedMutationMu.Unlock()
	if generation < latestResetGeneration {
		return false
	}
	latestResetGeneration = generation
	for channelID, mutation := range latestMutations {
		if mutation.generation <= generation {
			delete(latestMutations, channelID)
		}
	}
	return true
}

func sharedMutationIsCurrent(channelID int, mutation sharedCircuitMutation) bool {
	sharedMutationMu.Lock()
	defer sharedMutationMu.Unlock()
	if mutation.generation <= latestResetGeneration {
		return false
	}
	current, exists := latestMutations[channelID]
	if !exists || current.openUntil != mutation.openUntil {
		return false
	}
	if mutation.openUntil <= 0 {
		return current.generation == mutation.generation
	}
	return current.generation >= mutation.generation
}

// publishOpen broadcasts a trip to the cluster (fire-and-forget). No-op without Redis.
func publishOpen(channelID int, openUntil time.Time, generation int64) {
	if !clusterEnabled() {
		return
	}
	if generation <= 0 {
		return
	}
	mutation := sharedCircuitMutation{generation: generation, openUntil: openUntil.UnixMilli()}
	if !registerSharedMutation(channelID, mutation) {
		return
	}
	gopool.Go(func() {
		if !sharedMutationIsCurrent(channelID, mutation) {
			return
		}
		_ = publishOpenScript.Run(context.Background(), common.RDB, []string{sharedCircuitsKey},
			strconv.Itoa(channelID), generation, openUntil.UnixMilli(), sharedResetField).Err()
	})
}

// publishClosed clears a channel's shared trip after local recovery. No-op without Redis.
func publishClosed(channelID int, generation int64) {
	if !clusterEnabled() {
		return
	}
	if generation <= 0 {
		return
	}
	mutation := sharedCircuitMutation{generation: generation}
	if !registerSharedMutation(channelID, mutation) {
		return
	}
	gopool.Go(func() {
		if !sharedMutationIsCurrent(channelID, mutation) {
			return
		}
		_ = publishClosedScript.Run(context.Background(), common.RDB, []string{sharedCircuitsKey},
			strconv.Itoa(channelID), generation, sharedResetField).Err()
	})
}

// StartClusterSync launches the background loop that mirrors the shared circuit
// hash into the local snapshot every clusterSyncInterval. Call once at startup.
// No-op without Redis.
func StartClusterSync() {
	if !clusterEnabled() {
		return
	}
	clusterSyncOnce.Do(func() {
		gopool.Go(func() {
			for {
				time.Sleep(clusterSyncInterval)
				syncSharedCircuits()
			}
		})
	})
}

// clearShared removes one channel's shared trip synchronously and refreshes the
// local snapshot so the reset takes effect on this instance immediately (other
// replicas converge on the next sync). Without Redis it drops the local entry.
func clearShared(channelID int) {
	if !clusterEnabled() {
		dropLocalShared(channelID)
		return
	}
	generation := nextSharedMutationGeneration()
	if generation <= 0 {
		dropLocalShared(channelID)
		return
	}
	registerSharedMutation(channelID, sharedCircuitMutation{generation: generation})
	_ = publishClosedScript.Run(context.Background(), common.RDB, []string{sharedCircuitsKey},
		strconv.Itoa(channelID), generation, sharedResetField).Err()
	syncSharedCircuits()
}

// clearAllShared wipes every shared trip (operator "clear all").
func clearAllShared() {
	generation := nextSharedMutationGeneration()
	sharedCircuits.Store(map[int]int64{})
	if generation > 0 {
		registerSharedReset(generation)
		_ = clearAllSharedScript.Run(context.Background(), common.RDB, []string{sharedCircuitsKey},
			sharedResetField, generation).Err()
		syncSharedCircuits()
	}
}

// dropLocalShared removes a single channel from the local snapshot (copy-on-write).
func dropLocalShared(channelID int) {
	cur := loadSharedCircuits()
	if _, ok := cur[channelID]; !ok {
		return
	}
	next := make(map[int]int64, len(cur))
	for k, v := range cur {
		if k != channelID {
			next[k] = v
		}
	}
	sharedCircuits.Store(next)
}

func syncSharedCircuits() {
	res, err := common.RDB.HGetAll(context.Background(), sharedCircuitsKey).Result()
	if err != nil {
		return
	}
	if resetRaw, exists := res[sharedResetField]; exists {
		if resetGeneration, parseErr := strconv.ParseInt(resetRaw, 10, 64); parseErr == nil {
			registerSharedReset(resetGeneration)
		}
	}
	staleBefore := time.Now().Add(-sharedStaleGrace).UnixMilli()
	next := make(map[int]int64, len(res))
	for field, val := range res {
		id, err1 := strconv.Atoi(field)
		_, openUntil, valueOK := parseSharedCircuitValue(val)
		if err1 != nil || !valueOK || openUntil <= 0 || openUntil < staleBefore {
			continue // skip malformed / stale locally; Lua prunes them in Redis
		}
		next[id] = openUntil
	}
	sharedCircuits.Store(next)
	// Close truly-stale fields server-side while retaining their generations. A
	// Lua script re-reads each value inside Redis, so a concurrently re-tripped
	// field is never replaced by the tombstone.
	_ = pruneStaleCircuitsScript.Run(
		context.Background(), common.RDB, []string{sharedCircuitsKey}, staleBefore, sharedResetField,
	).Err()
}
