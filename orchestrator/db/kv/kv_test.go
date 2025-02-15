package kv

import (
	"context"
	"github.com/ethereum/go-ethereum/common"
	"github.com/lukso-network/lukso-orchestrator/shared/testutil/assert"
	"github.com/lukso-network/lukso-orchestrator/shared/testutil/require"
	"testing"
)

// setupDB instantiates and returns a Store instance.
func setupDB(t testing.TB, useTempDir bool) *Store {
	var dbPath string
	if !useTempDir {
		dbPath = "./testdata/" + OrchestratorNodeDbDirName
	} else {
		dbPath = t.TempDir()
	}
	db, err := NewKVStore(context.Background(), dbPath, &Config{})
	require.NoError(t, err, "Failed to instantiate DB")
	if useTempDir {
		t.Cleanup(func() {
			require.NoError(t, db.Close(), "Failed to close database")
		})
	}
	return db
}

func TestKV_Start_Stop(t *testing.T) {
	kv := setupDB(t, false)
	defer kv.ClearDB()

	headerHash := common.HexToHash("093eff5a6f078a434dc239817cf9916ab7867152dbf713e9f0f2001b6c1eeb1d")
	kv.latestVerifiedSlot = 100
	kv.latestEpoch = 3
	kv.latestHeaderHash = headerHash

	require.NoError(t, kv.Close())
	kv = setupDB(t, false)
	assert.Equal(t, uint64(100), kv.latestVerifiedSlot)
	assert.Equal(t, uint64(3), kv.latestEpoch)
	assert.Equal(t, headerHash, kv.latestHeaderHash)
}
