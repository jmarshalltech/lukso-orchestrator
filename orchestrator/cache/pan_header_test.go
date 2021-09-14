package cache

import (
	"context"
	eth1Types "github.com/ethereum/go-ethereum/core/types"
	"github.com/lukso-network/lukso-orchestrator/shared/testutil"
	"github.com/lukso-network/lukso-orchestrator/shared/testutil/assert"
	"github.com/lukso-network/lukso-orchestrator/shared/testutil/require"
	"math/rand"
	"testing"
)

var expectedPanHeaders map[uint64]*eth1Types.Header

func setup(num int) {
	expectedPanHeaders = make(map[uint64]*eth1Types.Header)
	for i := 0; i < num; i++ {
		header := testutil.NewEth1Header(uint64(i))
		expectedPanHeaders[uint64(i)] = header
	}
}

// Test_PandoraHeaderCache_Apis
func Test_PandoraHeaderCache_Apis(t *testing.T) {
	pc := NewPanHeaderCache()
	ctx := context.Background()
	setup(100)

	for slot := 0; slot < 100; slot++ {
		slotUint64 := uint64(slot)
		pc.Put(ctx, slotUint64, expectedPanHeaders[slotUint64])
		actualHeader, err := pc.Get(ctx, slotUint64)
		require.NoError(t, err)
		assert.DeepEqual(t, expectedPanHeaders[slotUint64], actualHeader)
	}
}

// Test_PandoraHeaderCache_Size
func Test_PandoraHeaderCache_Size(t *testing.T) {
	maxCacheSize = 10
	pc := NewPanHeaderCache()
	ctx := context.Background()
	setup(100)

	for slot := 0; slot < 100; slot++ {
		slotUint64 := uint64(slot)
		pc.Put(ctx, slotUint64, expectedPanHeaders[slotUint64])
	}

	// Should not found slot-0 because cache size is 10
	actualHeader, err := pc.Get(ctx, 88)
	require.ErrorContains(t, "Invalid slot", err, "Should not found because cache size is 10")

	actualHeader, err = pc.Get(ctx, 90)
	require.NoError(t, err, "Should be found slot 90")
	assert.DeepEqual(t, expectedPanHeaders[90], actualHeader)
}

func Test_PandoraHeaderRemoveCache(t *testing.T) {
	maxCacheSize = 1 << 10
	pc := NewPanHeaderCache()
	ctx := context.Background()
	setup(100)

	for slot := 0; slot < 100; slot++ {
		slotUint64 := uint64(slot)
		pc.Put(ctx, slotUint64, expectedPanHeaders[slotUint64])
	}
	// now remove a slot from the cache and check if previous slots are removed
	removedSlotNumber := uint64(rand.Int31n(100))
	// slot is removed
	pc.Remove(ctx, removedSlotNumber)

	// now all slots from removedSlotNumber to 0 is null
	for i := int(removedSlotNumber); i >= 0; i-- {
		_, err := pc.Get(ctx, uint64(i))
		require.ErrorContains(t, "Invalid slot", err, "Should not found because it is removed")
	}

	for i := int(removedSlotNumber) + 1; i < 100; i++ {
		actualHeader, err := pc.Get(ctx, uint64(i))
		require.NoError(t, err, "Should be found slot")
		assert.DeepEqual(t, expectedPanHeaders[uint64(i)], actualHeader)
	}
}

func Test_PandoraHeaderGetAll(t *testing.T) {
	maxCacheSize = 1 << 10
	pc := NewPanHeaderCache()
	ctx := context.Background()
	setup(100)

	for slot := 0; slot < 100; slot++ {
		slotUint64 := uint64(slot)
		pc.Put(ctx, slotUint64, expectedPanHeaders[slotUint64])
	}

	actualPanHeaders, err := pc.GetAll()
	require.NoError(t, err)
	assert.Equal(t, len(expectedPanHeaders), len(actualPanHeaders))
}
