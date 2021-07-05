package types

import (
	"github.com/ethereum/go-ethereum/common"
	"time"
)

const BLSSignatureSize = 96

type MinimalEpochConsensusInfo struct {
	Epoch            uint64        `json:"epoch"`
	ValidatorList    []string      `json:"validatorList"`
	EpochStartTime   uint64        `json:"epochTimeStart"`
	SlotTimeDuration time.Duration `json:"slotTimeDuration"`
}

// PandoraPendingHeaderFilter
type PandoraPendingHeaderFilter struct {
	FromBlockHash common.Hash `json:"fromBlockHash"`
}

type BlsSignatureBytes [BLSSignatureSize]byte

// Bytes gets the byte representation of the underlying hash.
func (h BlsSignatureBytes) Bytes() []byte { return h[:] }

type PanExtraDataWithBLSSig struct {
	ExtraData
	BlsSignatureBytes *BlsSignatureBytes
}
