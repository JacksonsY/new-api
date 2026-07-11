package channelhealth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterSharedMutationRejectsOutOfOrderUpdates(t *testing.T) {
	const channelID = 930101
	sharedMutationMu.Lock()
	original := latestMutations
	originalResetGeneration := latestResetGeneration
	latestMutations = make(map[int]sharedCircuitMutation)
	latestResetGeneration = 0
	sharedMutationMu.Unlock()
	t.Cleanup(func() {
		sharedMutationMu.Lock()
		latestMutations = original
		latestResetGeneration = originalResetGeneration
		sharedMutationMu.Unlock()
	})

	require.True(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 10, openUntil: 1000}))
	assert.False(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 20, openUntil: 500}), "a shorter cooldown must not replace a live longer cooldown")

	require.True(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 30}))
	assert.False(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 10, openUntil: 2000}), "an old delayed open must not resurrect a recovered channel")
	require.True(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 40, openUntil: 500}))

	sharedMutationMu.Lock()
	got := latestMutations[channelID]
	sharedMutationMu.Unlock()
	assert.Equal(t, sharedCircuitMutation{generation: 40, openUntil: 500}, got)
}

func TestRegisterSharedResetInvalidatesOlderQueuedMutations(t *testing.T) {
	const channelID = 930102
	sharedMutationMu.Lock()
	original := latestMutations
	originalResetGeneration := latestResetGeneration
	latestMutations = make(map[int]sharedCircuitMutation)
	latestResetGeneration = 0
	sharedMutationMu.Unlock()
	t.Cleanup(func() {
		sharedMutationMu.Lock()
		latestMutations = original
		latestResetGeneration = originalResetGeneration
		sharedMutationMu.Unlock()
	})

	oldOpen := sharedCircuitMutation{generation: 10, openUntil: 1000}
	require.True(t, registerSharedMutation(channelID, oldOpen))
	require.True(t, sharedMutationIsCurrent(channelID, oldOpen))
	require.True(t, registerSharedReset(20))
	assert.False(t, sharedMutationIsCurrent(channelID, oldOpen), "reset-all must invalidate already queued work")
	assert.False(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 19, openUntil: 2000}))
	assert.False(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 20, openUntil: 2000}))
	assert.True(t, registerSharedMutation(channelID, sharedCircuitMutation{generation: 21, openUntil: 2000}))
}

func TestParseSharedCircuitValueSupportsVersionedStateAndLegacyOpen(t *testing.T) {
	generation, openUntil, ok := parseSharedCircuitValue("12:345")
	require.True(t, ok)
	assert.EqualValues(t, 12, generation)
	assert.EqualValues(t, 345, openUntil)

	generation, openUntil, ok = parseSharedCircuitValue("678")
	require.True(t, ok)
	assert.Zero(t, generation)
	assert.EqualValues(t, 678, openUntil)

	legacyNanos := time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC).UnixNano()
	generation, openUntil, ok = parseSharedCircuitValue(strconv.FormatInt(legacyNanos, 10))
	require.True(t, ok)
	assert.Zero(t, generation)
	assert.EqualValues(t, legacyNanos/int64(time.Millisecond), openUntil)

	generation, openUntil, ok = parseSharedCircuitValue("99:0")
	require.True(t, ok)
	assert.EqualValues(t, 99, generation)
	assert.Zero(t, openUntil, "closed state is retained as a generation tombstone")

	_, _, ok = parseSharedCircuitValue("invalid")
	assert.False(t, ok)
}

func TestRedisScriptsProtectResetAndPrunedGenerations(t *testing.T) {
	client := startClusterTestRedis(t)
	ctx := context.Background()
	key := sharedCircuitsKey + ":ordering-test"
	field := "930103"
	future := time.Now().Add(time.Hour).UnixMilli()

	result, err := clearAllSharedScript.Run(ctx, client, []string{key}, sharedResetField, 200).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)

	result, err = publishOpenScript.Run(ctx, client, []string{key}, field, 100, future, sharedResetField).Int()
	require.NoError(t, err)
	assert.Zero(t, result, "an open queued before reset-all must not recreate the field")
	exists, err := client.HExists(ctx, key, field).Result()
	require.NoError(t, err)
	assert.False(t, exists)

	result, err = publishOpenScript.Run(ctx, client, []string{key}, field, 201, future, sharedResetField).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result, "a post-reset trip must be accepted")

	result, err = clearAllSharedScript.Run(ctx, client, []string{key}, sharedResetField, 202).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)

	staleUntil := time.Now().Add(-2 * sharedStaleGrace).UnixMilli()
	result, err = publishOpenScript.Run(ctx, client, []string{key}, field, 203, staleUntil, sharedResetField).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)
	result, err = pruneStaleCircuitsScript.Run(ctx, client, []string{key}, time.Now().Add(-sharedStaleGrace).UnixMilli(), sharedResetField).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)
	raw, err := client.HGet(ctx, key, field).Result()
	require.NoError(t, err)
	assert.Equal(t, "203:0", raw, "pruning must preserve a generation tombstone")

	result, err = publishOpenScript.Run(ctx, client, []string{key}, field, 203, future, sharedResetField).Int()
	require.NoError(t, err)
	assert.Zero(t, result, "the pruned generation cannot resurrect itself")
	result, err = publishOpenScript.Run(ctx, client, []string{key}, field, 204, future, sharedResetField).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result, "a genuinely newer trip may reopen the channel")

	result, err = clearAllSharedScript.Run(ctx, client, []string{key}, sharedResetField, 203).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)
	raw, err = client.HGet(ctx, key, field).Result()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(204, 10)+":"+strconv.FormatInt(future, 10), raw,
		"a delayed older reset-all must retain newer trips")
}

func TestRedisGenerationSequenceOrdersAcrossClockSkew(t *testing.T) {
	client := startClusterTestRedis(t)
	ctx := context.Background()
	hashKey := sharedCircuitsKey + ":generation-test"
	sequenceKey := sharedSequenceKey + ":generation-test"

	fastClockGeneration, err := nextSharedGenerationScript.Run(
		ctx, client, []string{sequenceKey, hashKey}, 2_000_000, sharedResetField,
	).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 2_000_001, fastClockGeneration)

	// A later event from a replica whose wall clock is far behind still receives
	// a greater generation from Redis.
	slowClockGeneration, err := nextSharedGenerationScript.Run(
		ctx, client, []string{sequenceKey, hashKey}, 1_000_000, sharedResetField,
	).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, fastClockGeneration+1, slowClockGeneration)

	// Equal wall-clock samples are also globally unique, so an open immediately
	// after a reset cannot collide with the reset generation.
	equalClockGeneration, err := nextSharedGenerationScript.Run(
		ctx, client, []string{sequenceKey, hashKey}, 1_000_000, sharedResetField,
	).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, slowClockGeneration+1, equalClockGeneration)

	field := "930105"
	future := time.Now().Add(time.Hour).UnixMilli()
	result, err := clearAllSharedScript.Run(
		ctx, client, []string{hashKey}, sharedResetField, slowClockGeneration,
	).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)
	result, err = publishOpenScript.Run(
		ctx, client, []string{hashKey}, field, equalClockGeneration, future, sharedResetField,
	).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result, "the open allocated after reset must be accepted")
	result, err = publishOpenScript.Run(
		ctx, client, []string{hashKey}, field, fastClockGeneration, future+60_000, sharedResetField,
	).Int()
	require.NoError(t, err)
	assert.Zero(t, result, "a delayed pre-reset open must remain rejected even with a longer cooldown")
	raw, err := client.HGet(ctx, hashKey, field).Result()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(equalClockGeneration, 10)+":"+strconv.FormatInt(future, 10), raw)

	// If the sequence key is lost, recover above the greatest generation already
	// persisted in the hash instead of restarting from the wall-clock floor.
	require.NoError(t, client.Del(ctx, sequenceKey).Err())
	require.NoError(t, client.HSet(ctx, hashKey, "930104", "3000000:0").Err())
	recoveredGeneration, err := nextSharedGenerationScript.Run(
		ctx, client, []string{sequenceKey, hashKey}, 100, sharedResetField,
	).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 3_000_001, recoveredGeneration)
}

func startClusterTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("redis-server unix socket test is not supported on Windows")
	}
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed")
	}

	socketDir, err := os.MkdirTemp("/tmp", "new-api-redis-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "redis.sock")
	cmd := exec.Command(binary,
		"--port", "0",
		"--save", "",
		"--appendonly", "no",
		"--unixsocket", socketPath,
		"--unixsocketperm", "700",
	)
	require.NoError(t, cmd.Start())
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socketPath})
	t.Cleanup(func() {
		_ = client.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitCh
		}
	})

	require.Eventually(t, func() bool {
		return client.Ping(context.Background()).Err() == nil
	}, 3*time.Second, 10*time.Millisecond, "redis-server failed to start")
	return client
}
