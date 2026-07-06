package channelhealth

import (
	"context"
	"strconv"
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
	// sharedCircuitsKey is a Redis hash: field=channelID, value=openUntilUnixNano.
	sharedCircuitsKey   = "new-api:channel_health:circuits:v1"
	clusterSyncInterval = time.Second
	// sharedStaleGrace drops a shared entry this long past its openUntil, so an
	// instance that trips a channel then dies before recovery cannot pin it open
	// forever.
	sharedStaleGrace = 5 * time.Minute
)

var (
	sharedCircuits  atomic.Value // map[int]int64 (channelID -> openUntilUnixNano)
	clusterSyncOnce sync.Once
)

// pruneStaleCircuitsScript deletes hash fields whose openUntil is older than the
// threshold, re-reading each value server-side so it is atomic w.r.t. a
// concurrent publishOpen — a freshly re-tripped channel is never wiped.
var pruneStaleCircuitsScript = redis.NewScript(`
local entries = redis.call('HGETALL', KEYS[1])
local threshold = tonumber(ARGV[1])
local removed = 0
for i = 1, #entries, 2 do
	local v = tonumber(entries[i + 1])
	if v == nil or v < threshold then
		redis.call('HDEL', KEYS[1], entries[i])
		removed = removed + 1
	end
end
return removed`)

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
func sharedCircuitState(channelID int, nowNano int64) (open bool, halfOpen bool) {
	openUntil, ok := loadSharedCircuits()[channelID]
	if !ok {
		return false, false
	}
	if openUntil > nowNano {
		return true, false
	}
	return false, true
}

// publishOpen broadcasts a trip to the cluster (fire-and-forget). No-op without Redis.
func publishOpen(channelID int, openUntil time.Time) {
	if !clusterEnabled() {
		return
	}
	gopool.Go(func() {
		common.RDB.HSet(context.Background(), sharedCircuitsKey,
			strconv.Itoa(channelID), openUntil.UnixNano())
	})
}

// publishClosed clears a channel's shared trip after local recovery. No-op without Redis.
func publishClosed(channelID int) {
	if !clusterEnabled() {
		return
	}
	gopool.Go(func() {
		common.RDB.HDel(context.Background(), sharedCircuitsKey, strconv.Itoa(channelID))
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
	common.RDB.HDel(context.Background(), sharedCircuitsKey, strconv.Itoa(channelID))
	syncSharedCircuits()
}

// clearAllShared wipes every shared trip (operator "clear all").
func clearAllShared() {
	if clusterEnabled() {
		common.RDB.Del(context.Background(), sharedCircuitsKey)
	}
	sharedCircuits.Store(map[int]int64{})
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
	staleBefore := time.Now().Add(-sharedStaleGrace).UnixNano()
	next := make(map[int]int64, len(res))
	for field, val := range res {
		id, err1 := strconv.Atoi(field)
		openUntil, err2 := strconv.ParseInt(val, 10, 64)
		if err1 != nil || err2 != nil || openUntil < staleBefore {
			continue // skip malformed / stale locally; Lua prunes them in Redis
		}
		next[id] = openUntil
	}
	sharedCircuits.Store(next)
	// Prune truly-stale fields server-side. A Lua script re-reads each value
	// inside Redis, so a field re-tripped (HSet with a fresh future openUntil)
	// concurrently with this cleanup is never wiped — unlike a value-blind HDel
	// keyed on the names read a moment earlier.
	_ = pruneStaleCircuitsScript.Run(
		context.Background(), common.RDB, []string{sharedCircuitsKey}, staleBefore,
	).Err()
}
