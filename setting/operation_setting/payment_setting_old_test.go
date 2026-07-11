package operation_setting

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayMethodsUpdateIsAtomicAndSnapshotIsIsolated(t *testing.T) {
	original := PayMethods2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdatePayMethodsByJsonString(original))
	})

	require.NoError(t, UpdatePayMethodsByJsonString(`[{"name":"Card","type":"card"}]`))
	require.Error(t, UpdatePayMethodsByJsonString(`[{"name":`))

	snapshot := GetPayMethodsSnapshot()
	require.Len(t, snapshot, 1)
	assert.Equal(t, "card", snapshot[0]["type"])

	snapshot[0]["type"] = "mutated"
	snapshot = append(snapshot, map[string]string{"type": "extra"})
	assert.True(t, ContainsPayMethod("card"))
	assert.False(t, ContainsPayMethod("mutated"))
	assert.Len(t, GetPayMethodsSnapshot(), 1)
}

func TestPayMethodsConcurrentSnapshots(t *testing.T) {
	original := PayMethods2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdatePayMethodsByJsonString(original))
	})

	const iterations = 50
	require.NoError(t, UpdatePayMethodsByJsonString(`[{"name":"Initial","type":"initial"}]`))
	var wg sync.WaitGroup
	errCh := make(chan error, iterations*3)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := UpdatePayMethodsByJsonString(fmt.Sprintf(
				`[{"name":"Method %d","type":"method-%d"}]`, i, i,
			)); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			snapshot := GetPayMethodsSnapshot()
			if len(snapshot) != 1 {
				errCh <- fmt.Errorf("snapshot length = %d, want 1", len(snapshot))
				continue
			}
			if snapshot[0]["name"] == "" || snapshot[0]["type"] == "" {
				errCh <- fmt.Errorf("incomplete snapshot: %#v", snapshot[0])
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}
