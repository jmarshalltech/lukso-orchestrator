package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/lukso-network/lukso-orchestrator/shared/fileutil"
	contracts "github.com/lukso-network/lukso-orchestrator/tools/deposit-tx-sender/depositcontract"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"math/big"
	"os"
	"strings"
	"time"
)

const (
	depositGasLimit     = 4000000
	MaxEffectiveBalance = 32 * 1e9
	GweiPerEth          = 1000000000
	Endpoint            = "http://34.141.35.251:8545/"
	timeGapPerMiningTX  = 250 * time.Millisecond
)

var contractAddress = common.HexToAddress("0x000000000000000000000000000000000000cafE")

var (
	depositJSONFile = flag.String(
		"deposit-json-file",
		"",
		"Path to deposit_data.json file generated by the eth2.0-deposit-cli tool",
	)
	pandoraEndpoint = flag.String(
		"pandora-http-provider",
		"http://127.0.0.1:8545/",
		"A pandora string rpc endpoint. This is our pandora client http endpoint.",
	)
	keystoreFile = flag.String(
		"keystore-file",
		"",
		"Path to keystore.json file generated by pandora account tool",
	)
	walletPassword = flag.String(
		"keystore-password",
		"",
		"Password for keystore.json file",
	)
	chainId = flag.Int64(
		"pandora-chain-id",
		-1,
		"Chain id identifies chain of pandora network",
	)
	txInterval = flag.Int64(
		"tx-interval",
		10,
		"Transaction interval between consecutive two transactions")
)

type Deposit_Data struct {
	PublicKey             []byte
	WithdrawalCredentials []byte
	Amount                uint64
	Signature             []byte
	DepositRoot           [32]byte
}

// DepositDataJSON representing a json object of hex string and uint64 values for
// validators on eth2. This file can be generated using the official eth2.0-deposit-cli.
type DepositDataJSON struct {
	PubKey                string `json:"pubkey"`
	Amount                uint64 `json:"amount"`
	WithdrawalCredentials string `json:"withdrawal_credentials"`
	DepositDataRoot       string `json:"deposit_data_root"`
	Signature             string `json:"signature"`
}

// importDeposits
func importDeposits(inputFile string) (map[int64]*Deposit_Data, error) {
	expanded, err := fileutil.ExpandPath(inputFile)
	if err != nil {
		return nil, err
	}
	inputJSON, err := os.Open(expanded)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := inputJSON.Close(); err != nil {
		}
	}()
	enc, err := ioutil.ReadAll(inputJSON)
	if err != nil {
		return nil, err
	}
	var depositJSON []*DepositDataJSON
	if err := json.Unmarshal(enc, &depositJSON); err != nil {
		return nil, err
	}

	depositData := make(map[int64]*Deposit_Data, len(depositJSON))
	for i := 0; i < len(depositJSON); i++ {
		deposit, err := depositJSONToDepositData(depositJSON[i])
		if err != nil {
			return nil, err
		}
		depositData[int64(i)] = deposit
	}
	return depositData, nil
}

func depositJSONToDepositData(input *DepositDataJSON) (depositData *Deposit_Data, err error) {
	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(input.PubKey, "0x"))
	if err != nil {
		return
	}
	withdrawalBytes, err := hex.DecodeString(strings.TrimPrefix(input.WithdrawalCredentials, "0x"))
	if err != nil {
		return
	}
	signatureBytes, err := hex.DecodeString(strings.TrimPrefix(input.Signature, "0x"))
	if err != nil {
		return
	}
	dataRootBytes, err := hex.DecodeString(strings.TrimPrefix(input.DepositDataRoot, "0x"))
	if err != nil {
		return
	}
	var dataRoot [32]byte
	copy(dataRoot[:], dataRootBytes)
	return &Deposit_Data{
		PublicKey:             pubKeyBytes,
		WithdrawalCredentials: withdrawalBytes,
		Amount:                input.Amount,
		Signature:             signatureBytes,
		DepositRoot:           dataRoot,
	}, nil
}

// SendAndMineDeposits sends the requested amount of deposits and mines the chain after to ensure the deposits are seen.
func SendAndMineDeposits(
	keystorePath string,
	allDeposits map[int64]*Deposit_Data,
	password string,
	chainId int64,
	txInterval int64,
) error {
	client, err := rpc.DialHTTP(*pandoraEndpoint)
	if err != nil {
		return err
	}
	defer client.Close()
	web3 := ethclient.NewClient(client)

	keystoreBytes, err := ioutil.ReadFile(keystorePath)
	if err != nil {
		return err
	}
	if err = sendDeposits(web3, keystoreBytes, allDeposits, password, chainId, txInterval); err != nil {
		return err
	}
	return nil
}

// sendDeposits uses the passed in web3 and keystore bytes to send the requested deposits.
func sendDeposits(
	web3 *ethclient.Client,
	keystoreBytes []byte,
	allDeposits map[int64]*Deposit_Data,
	password string,
	chainId int64,
	txInterval int64,
) error {

	txOps, err := bind.NewTransactorWithChainID(bytes.NewReader(keystoreBytes), password, big.NewInt(chainId))
	if err != nil {
		return err
	}
	txOps.GasLimit = depositGasLimit
	txOps.Context = context.Background()
	nonce, err := web3.PendingNonceAt(context.Background(), txOps.From)
	if err != nil {
		return err
	}
	txOps.Nonce = big.NewInt(int64(nonce))

	contract, err := contracts.NewDepositContract(contractAddress, web3)
	if err != nil {
		return err
	}

	for i := 0; i < len(allDeposits); i++ {
		dd := allDeposits[int64(i)]
		depositInGwei := big.NewInt(int64(MaxEffectiveBalance))
		txOps.Value = depositInGwei.Mul(depositInGwei, big.NewInt(int64(GweiPerEth)))

		log.WithField("amount", dd.Amount).
			WithField("pubkey", hexutil.Encode(dd.PublicKey)).
			WithField("signature", hexutil.Encode(dd.Signature)).
			WithField("withdrawalCredentials", hexutil.Encode(dd.WithdrawalCredentials)).
			WithField("depositDataRoot", hexutil.Encode(dd.DepositRoot[:])).Info("Deposit info")

		tx, err := contract.Deposit(txOps, dd.PublicKey, dd.WithdrawalCredentials, dd.Signature, dd.DepositRoot)
		if err != nil {
			return errors.Wrap(err, "unable to send transaction to contract")
		}
		log.WithField("txHash", tx.Hash()).
			WithField("nonce", txOps.Nonce).
			Info("Successfully send transaction to pandora chain. Waiting for validating.")
		txOps.Nonce = txOps.Nonce.Add(txOps.Nonce, big.NewInt(1))
		time.Sleep(time.Duration(txInterval) * time.Second)
	}
	return nil
}

func main() {
	flag.Parse()
	if *depositJSONFile == "" {
		log.Fatalf("Missed the deposit.json file path!")
	}
	if *pandoraEndpoint == "http://127.0.0.1:8545/" {
		log.WithField("pandoraEndpoint", *pandoraEndpoint).Warn("You are running pandora endpoint at localhost!")
	}
	if *walletPassword == "" {
		log.Fatalf("Missed password for the keystore.json file!")
	}
	if *keystoreFile == "" {
		log.Fatalf("Missed keystore.json file!")
	}
	if *chainId == -1 {
		log.Fatalf("Missed pandora chain id!")
	}
	inputFile := *depositJSONFile
	deposits, err := importDeposits(inputFile)
	if err != nil {
		log.WithError(err).Fatalf("Failed to read deposit json")
	}
	keyStorePath := *keystoreFile
	if err := SendAndMineDeposits(keyStorePath, deposits, *walletPassword, *chainId, *txInterval); err != nil {
		log.WithError(err).Fatalf("Failed to send transactions")
	}
}
