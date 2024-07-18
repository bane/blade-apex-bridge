package e2e

import (
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/Ethernal-Tech/blockchain-event-tracker/store"
	"github.com/Ethernal-Tech/blockchain-event-tracker/tracker"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"

	"github.com/0xPolygon/polygon-edge/command/bridge/common"
	bridgeHelper "github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	checkpointSubmittedEventSig = new(contractsapi.CheckpointSubmittedEvent).Sig()
	exitProcessedEventSig       = new(contractsapi.ExitProcessedEvent).Sig()
	stateSyncEventSig           = new(contractsapi.StateSyncedEvent).Sig()
)

type EventHandler struct {
	stateSyncEventCount           uint16
	checkpointSubmittedEventCount uint16
	exitProcessedEventCount       uint16
}

func (eh *EventHandler) AddLog(eventLog *ethgo.Log) error {
	switch eventLog.Topics[0] {
	case stateSyncEventSig:
		eh.stateSyncEventCount++
		fmt.Println("stateSync Event Logged, event count: ", eh.stateSyncEventCount)
	case checkpointSubmittedEventSig:
		eh.checkpointSubmittedEventCount++
		fmt.Println("checkpointSubmitted Event Logged, event count: ", eh.checkpointSubmittedEventCount)
	case exitProcessedEventSig:
		eh.exitProcessedEventCount++
		fmt.Println("exitProcessed Event Logged, event count: ", eh.exitProcessedEventCount)
	default:
		fmt.Println("Unknown Event Logged ")
	}

	fmt.Println("Removed:		", eventLog.Removed)
	fmt.Println("LogIndex: 		", eventLog.LogIndex)
	fmt.Println("TransactionIndex: 	", eventLog.TransactionIndex)
	fmt.Println("TransactionHash: 	", eventLog.TransactionHash)
	fmt.Println("BlockHash: 		", eventLog.BlockHash)
	fmt.Println("BlockNumber: 		", eventLog.BlockNumber)
	fmt.Println("Address: 		", eventLog.Address)
	fmt.Println("Topics: 		", eventLog.Topics)
	fmt.Println("Data: 			", eventLog.Data)

	return nil
}

func TestE2E_EVM_EventTracker(t *testing.T) {
	const (
		transfersCount        = 5
		numBlockConfirmations = 2
		epochSize             = 5
		sprintSize            = uint64(5)
	)

	var (
		bridgeAmount = ethgo.Ether(2)
	)

	receiversAddrs := make([]types.Address, transfersCount)
	receivers := make([]string, transfersCount)
	amounts := make([]string, transfersCount)
	receiverKeys := make([]string, transfersCount)

	for i := 0; i < transfersCount; i++ {
		key, err := crypto.GenerateECDSAKey()
		require.NoError(t, err)

		rawKey, err := key.MarshallPrivateKey()
		require.NoError(t, err)

		receiverKeys[i] = hex.EncodeToString(rawKey)
		receiversAddrs[i] = key.Address()
		receivers[i] = key.Address().String()
		amounts[i] = fmt.Sprintf("%d", bridgeAmount)

		t.Logf("Receiver#%d=%s\n", i+1, receivers[i])
	}

	cluster := framework.NewTestCluster(t, 5,
		framework.WithTestRewardToken(),
		framework.WithNumBlockConfirmations(numBlockConfirmations),
		framework.WithEpochSize(epochSize),
		framework.WithBridge(),
	)

	defer cluster.Stop()

	cluster.WaitForReady(t)

	polybftCfg, err := polybft.LoadPolyBFTConfig(path.Join(cluster.Config.TmpDir, chainConfigFileName))
	require.NoError(t, err)

	// Init Event Tracker
	store, err := store.NewBoltDBEventTrackerStore("./test.db")
	if err != nil {
		t.Log("error intitiating event tracker db", err)
	}

	eventHandler := &EventHandler{
		stateSyncEventCount:           uint16(0),
		checkpointSubmittedEventCount: uint16(0),
		exitProcessedEventCount:       uint16(0),
	}

	eventTracker, err := tracker.NewEventTracker(
		&tracker.EventTrackerConfig{
			Logger:                hclog.Default().Named("test logger"),
			EventSubscriber:       eventHandler,
			RPCEndpoint:           cluster.Bridge.JSONRPCAddr(),
			SyncBatchSize:         10,
			NumBlockConfirmations: 100,
			LogFilter: map[ethgo.Address][]ethgo.Hash{
				ethgo.Address(polybftCfg.Bridge.CheckpointManagerAddr): {checkpointSubmittedEventSig},
				ethgo.Address(polybftCfg.Bridge.ExitHelperAddr):        {exitProcessedEventSig},
				ethgo.Address(polybftCfg.Bridge.StateSenderAddr):       {stateSyncEventSig},
			},
		},
		store, 0,
	)

	if err != nil {
		t.Log("failed to init new event tracker: ", err)
	}

	if err := eventTracker.Start(); err != nil {
		t.Log("failed to start the tracker")

		return
	}
	defer eventTracker.Close()

	rootchainTxRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithIPAddress(cluster.Bridge.JSONRPCAddr()))
	require.NoError(t, err)

	deployerKey, err := bridgeHelper.DecodePrivateKey("")
	require.NoError(t, err)

	deployTx := types.NewTx(types.NewLegacyTx(
		types.WithTo(nil),
		types.WithInput(contractsapi.RootERC20.Bytecode),
	))

	receipt, err := rootchainTxRelayer.SendTransaction(deployTx, deployerKey)
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint64(types.ReceiptSuccess), receipt.Status)

	rootERC20Token := types.Address(receipt.ContractAddress)
	t.Log("Rootchain token address:", rootERC20Token)

	// wait for a couple of sprints
	finalBlockNum := 1 * sprintSize
	require.NoError(t, cluster.WaitForBlock(finalBlockNum, 2*time.Minute))

	t.Run("track ERC20 token deposit", func(t *testing.T) {
		// DEPOSIT ERC20 TOKENS
		// send a few transactions to the bridge
		require.NoError(t,
			cluster.Bridge.Deposit(
				common.ERC20,
				rootERC20Token,
				polybftCfg.Bridge.RootERC20PredicateAddr,
				bridgeHelper.TestAccountPrivKey,
				strings.Join(receivers, ","),
				strings.Join(amounts, ","),
				"",
				cluster.Bridge.JSONRPCAddr(),
				bridgeHelper.TestAccountPrivKey,
				false,
			))

		require.NoError(t, cluster.WaitUntil(time.Minute*10, time.Second*2, func() bool {
			return eventHandler.stateSyncEventCount > uint16(transfersCount)
		}))
	})

	t.Run("track multiple deposits", func(t *testing.T) {
		const (
			depositsSubset = 1
		)

		require.NoError(t, cluster.Bridge.Deposit(
			common.ERC20,
			rootERC20Token,
			polybftCfg.Bridge.RootERC20PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join(receivers[:depositsSubset], ","),
			strings.Join(amounts[:depositsSubset], ","),
			"",
			cluster.Bridge.JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
		)

		require.NoError(t, cluster.Bridge.Deposit(
			common.ERC20,
			rootERC20Token,
			polybftCfg.Bridge.RootERC20PredicateAddr,
			bridgeHelper.TestAccountPrivKey,
			strings.Join(receivers[depositsSubset:], ","),
			strings.Join(amounts[depositsSubset:], ","),
			"",
			cluster.Bridge.JSONRPCAddr(),
			bridgeHelper.TestAccountPrivKey,
			false),
		)

		require.NoError(t, cluster.WaitUntil(time.Minute*10, time.Second*2, func() bool {
			return eventHandler.stateSyncEventCount > uint16(2*transfersCount)
		}))
	})

	if _, err = os.Stat("./test.db"); err == nil {
		os.RemoveAll("./test.db")
	}
}
