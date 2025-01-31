package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/e2ehelper"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/stretchr/testify/require"
)

func TestE2E_ApexBridgeWithNexus(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithUserCnt(1),
	)
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	t.Run("From Nexus to Prime", func(t *testing.T) {
		srcChain, dstChain := cardanofw.ChainIDNexus, cardanofw.ChainIDPrime

		e2ehelper.ExecuteSingleBridging(
			t, ctx, apex, apex.Users[0], apex.Users[0], srcChain, dstChain, sendAmountDfm)
	})

	t.Run("From Prime to Nexus", func(t *testing.T) {
		srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus

		relayerBalanceBefore, err := apex.GetChainMust(t, dstChain).GetAddressBalance(
			ctx, apex.NexusInfo.RelayerAddress.String())
		require.NoError(t, err)

		e2ehelper.ExecuteSingleBridging(
			t, ctx, apex, apex.Users[0], apex.Users[0], srcChain, dstChain, sendAmountDfm)

		relayerBalanceAfter, err := apex.GetChainMust(t, dstChain).GetAddressBalance(
			ctx, apex.NexusInfo.RelayerAddress.String())
		require.NoError(t, err)

		require.True(t, relayerBalanceAfter.Cmp(relayerBalanceBefore) == 1)
	})
}

func TestE2E_ApexBridgeWithNexus_NtP_ValidScenarios(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const (
		apiKey  = "test_api_key"
		userCnt = 15
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))
	user := apex.Users[userCnt-1]
	srcChain := cardanofw.ChainIDNexus
	dstChain := cardanofw.ChainIDPrime

	t.Run("From Nexus to Prime one by one - wait for other side", func(t *testing.T) {
		const instances = 5

		e2ehelper.ExecuteBridgingOneByOneWaitOnOtherSide(
			t, ctx, apex, instances, user, srcChain, dstChain, sendAmountDfm)
	})

	t.Run("From Nexus to Prime one by one - don't wait", func(t *testing.T) {
		const instances = 5

		e2ehelper.ExecuteBridgingWaitAfterSubmits(
			t, ctx, apex, instances, user, srcChain, dstChain, sendAmountDfm)
	})

	t.Run("From Nexus to Prime - parallel", func(t *testing.T) {
		const instances = 5

		e2ehelper.ExecuteBridging(
			t, ctx, apex, 1,
			apex.Users[:instances],
			[]*cardanofw.TestApexUser{user},
			[]string{srcChain},
			map[string][]string{
				srcChain: {dstChain},
			},
			sendAmountDfm)
	})

	t.Run("From Nexus to Prime - sequential and parallel", func(t *testing.T) {
		const (
			instances         = 5
			parallelInstances = 10
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, instances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{srcChain},
			map[string][]string{
				srcChain: {dstChain},
			},
			sendAmountDfm)
	})

	t.Run("From Nexus to Prime - sequential and parallel multiple receivers", func(t *testing.T) {
		const (
			sequentialInstances = 5
			parallelInstances   = 10
		)

		NexusToPrimeSequentialAndParallelWithMaxReceivers(
			t, ctx, apex, sequentialInstances, parallelInstances, sendAmountDfm)
	})

	t.Run("From Nexus to Prime - sequential and parallel, one node goes off in the middle", func(t *testing.T) {
		const (
			instances            = 5
			parallelInstances    = 10
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, instances,
			apex.Users[:parallelInstances],
			apex.Users[len(apex.Users)-1:],
			[]string{srcChain},
			map[string][]string{
				srcChain: {dstChain},
			},
			sendAmountDfm,
			e2ehelper.WithRestartValidatorsConfig([]e2ehelper.RestartValidatorsConfig{
				{WaitTime: stopAfter, StopIndxs: []int{validatorStoppingIdx}},
			}))
	})
}

func TestE2E_ApexBridgeWithNexus_NtP_InvalidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 1
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]
	fee := cardanofw.DfmToChainNativeTokenAmount(cardanofw.ChainIDNexus, new(big.Int).SetUint64(uint64(1_100_000)))

	nexusAdminUser := &cardanofw.TestApexUser{
		NexusWallet:    apex.NexusInfo.AdminKey,
		NexusAddress:   apex.NexusInfo.AdminKey.Address(),
		HasNexusWallet: true,
	}

	t.Run("Wrong Tx-Type", func(t *testing.T) {
		sendAmountWei := ethgo.Ether(uint64(1))

		userPk, err := user.GetPrivateKey(cardanofw.ChainIDNexus)
		require.NoError(t, err)

		// call SendTx command
		err = sendTxParamsNPInvalidScenarios("cardano", // "cardano" instead of "evm"
			apex.NexusInfo.GatewayAddress.String(),
			apex.NexusInfo.JSONRPCAddr,
			userPk, cardanofw.ChainIDPrime,
			user.GetAddress(cardanofw.ChainIDPrime),
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "failed to execute command")
	})

	t.Run("Wrong Nexus URL", func(t *testing.T) {
		sendAmountWei := ethgo.Ether(uint64(1))

		userPk, err := user.GetPrivateKey(cardanofw.ChainIDNexus)
		require.NoError(t, err)

		// call SendTx command
		err = sendTxParamsNPInvalidScenarios("evm",
			apex.NexusInfo.GatewayAddress.String(),
			"localhost:1234",
			userPk, cardanofw.ChainIDPrime,
			user.GetAddress(cardanofw.ChainIDPrime),
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "Error: invalid --nexus-url flag")
	})

	t.Run("Submitter not enough funds", func(t *testing.T) {
		NexusToPrimeSubmitterNotEnoughFunds(t, ctx, apex)
	})

	t.Run("Big receiver amount", func(t *testing.T) {
		unfundedUser, err := cardanofw.NewTestApexUser(
			apex.Config.PrimeConfig.NetworkType,
			apex.Config.VectorConfig.IsEnabled,
			apex.Config.VectorConfig.NetworkType,
			apex.Config.NexusConfig.IsEnabled,
		)
		require.NoError(t, err)

		unfundedUserPk, err := unfundedUser.GetPrivateKey(cardanofw.ChainIDNexus)
		require.NoError(t, err)

		_, err = apex.SubmitTx(
			ctx, cardanofw.ChainIDNexus, nexusAdminUser, unfundedUser.NexusAddress.String(), big.NewInt(10), nil)
		require.NoError(t, err)

		sendAmountWei := ethgo.Ether(uint64(20)) // try to send 20 ethers with users without enough funds

		// call SendTx command
		err = sendTxParamsNPInvalidScenarios("evm",
			apex.NexusInfo.GatewayAddress.String(),
			apex.NexusInfo.JSONRPCAddr,
			unfundedUserPk, cardanofw.ChainIDPrime,
			unfundedUser.GetAddress(cardanofw.ChainIDPrime),
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "insufficient funds for execution")
	})
}

func TestE2E_ApexBridgeWithNexus_PtNandBoth_ValidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 15
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

	t.Run("From Prime to Nexus one by one - wait for other side", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const instances = 5

		e2ehelper.ExecuteBridgingOneByOneWaitOnOtherSide(
			t, ctx, apex, instances, user, cardanofw.ChainIDPrime, cardanofw.ChainIDNexus, sendAmountDfm)
	})

	t.Run("From Prime to Nexus one by one - don't wait for other side", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const instances = 5

		e2ehelper.ExecuteBridgingWaitAfterSubmits(
			t, ctx, apex, instances, user, cardanofw.ChainIDPrime, cardanofw.ChainIDNexus, sendAmountDfm)
	})

	t.Run("From Prime to Nexus parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const instances = 5

		e2ehelper.ExecuteBridging(
			t, ctx, apex, 1,
			apex.Users[:instances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
			},
			sendAmountDfm)
	})

	t.Run("From Prime to Nexus sequential and parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex,
			sequentialInstances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
			},
			sendAmountDfm)
	})

	t.Run("From Prime to Nexus sequential and parallel with max receivers", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
		)

		PrimeToNexusSequentialAndParallelWithMaxReceivers(
			t, ctx, apex, sequentialInstances, parallelInstances, sendAmountDfm)
	})

	t.Run("From Prime to Nexus sequential and parallel - one node goes off in the midle", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sequentialInstances  = 5
			parallelInstances    = 6
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex,
			sequentialInstances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
			},
			sendAmountDfm,
			e2ehelper.WithRestartValidatorsConfig([]e2ehelper.RestartValidatorsConfig{
				{WaitTime: stopAfter, StopIndxs: []int{validatorStoppingIdx}},
			}))
	})

	t.Run("Both directions sequential", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const instances = 5

		e2ehelper.ExecuteBridging(
			t, ctx, apex,
			instances,
			apex.Users[:1],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDNexus},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
				cardanofw.ChainIDNexus: {cardanofw.ChainIDPrime},
			},
			sendAmountDfm)
	})

	t.Run("Both directions sequential and parallel", func(t *testing.T) {
		const (
			sequentialInstances = 5
			parallelInstances   = 6
		)

		PrimeNexusBothDirectionsSequentialAndParallel(
			t, ctx, apex, user, sequentialInstances, parallelInstances, sendAmountDfm)
	})

	t.Run("Both directions sequential and parallel - one node goes off in the midle", func(t *testing.T) {
		const (
			sequentialInstances  = 5
			parallelInstances    = 6
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex,
			sequentialInstances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDNexus},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
				cardanofw.ChainIDNexus: {cardanofw.ChainIDPrime},
			},
			sendAmountDfm,
			e2ehelper.WithWaitForUnexpectedBridges(true),
			e2ehelper.WithRestartValidatorsConfig([]e2ehelper.RestartValidatorsConfig{
				{WaitTime: stopAfter, StopIndxs: []int{validatorStoppingIdx}},
			}))
	})

	t.Run("Both directions sequential and parallel - two nodes go off in the middle and then one comes back", func(t *testing.T) {
		const (
			sequentialInstances   = 5
			parallelInstances     = 10
			stopAfter             = time.Second * 60
			startAgainAfter       = time.Second * 120
			validatorStoppingIdx1 = 1
			validatorStoppingIdx2 = 2
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex,
			sequentialInstances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDNexus},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
				cardanofw.ChainIDNexus: {cardanofw.ChainIDPrime},
			},
			sendAmountDfm,
			e2ehelper.WithWaitForUnexpectedBridges(true),
			e2ehelper.WithRestartValidatorsConfig([]e2ehelper.RestartValidatorsConfig{
				{WaitTime: stopAfter, StopIndxs: []int{validatorStoppingIdx1, validatorStoppingIdx2}},
				{WaitTime: startAgainAfter, StartIndxs: []int{validatorStoppingIdx1}},
			}))
	})
}

func TestE2E_ApexBridgeWithNexus_PtN_InvalidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 15
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	const premineAmount = uint64(50_000_000)

	primeConfig := cardanofw.NewPrimeChainConfig()
	primeConfig.PremineAmount = premineAmount

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	t.Run("Submitter not enough funds", func(t *testing.T) {
		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(100))

		PrimeToNexusSubmitterNotEnoughFunds(t, ctx, apex, user, sendAmountDfm)
	})

	t.Run("Submitted invalid metadata - sliced off", func(t *testing.T) {
		PrimeToNexusInvalidMetadataSlicedOff(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - wrong type", func(t *testing.T) {
		PrimeToNexusInvalidMetadataWrongType(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - invalid destination", func(t *testing.T) {
		PrimeToNexusInvalidMetadataInvalidDestination(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - invalid sender", func(t *testing.T) {
		PrimeToNexusInvalidMetadataInvalidSender(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - empty tx", func(t *testing.T) {
		PrimeToNexusInvalidMetadataInvalidTransactions(t, ctx, apex, user)
	})
}

func TestE2E_ApexBridgeWithNexus_ValidScenarios_BigTest(t *testing.T) {
	if shouldRun := os.Getenv("RUN_E2E_BIG_TESTS"); shouldRun != "true" {
		t.Skip()
	}

	const (
		apiKey  = "test_api_key"
		userCnt = 1010
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig := cardanofw.NewPrimeChainConfig()
	primeConfig.PremineAmount = 100_000_000

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	txProviderPrime := apex.PrimeInfo.GetTxProvider()

	//nolint:dupl
	t.Run("From Prime to Nexus 200x 5min 90%", func(t *testing.T) {
		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 200

		maxWaitTime := 300
		successChance := 90 // 90%
		succeededCount := int64(0)

		fmt.Printf("Sending transactions\n")

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCount++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], sendAmountDfm, user,
					)
					fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
				} else {
					const feeAmount = 1_100_000

					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm.Uint64() * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDNexus, feeAmount)
					require.NoError(t, err)
					txHash, err := cardanofw.SendTx(ctx, txProviderPrime, apex.Users[idx].PrimeWallet,
						sendAmountDfm.Uint64()+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)

					fmt.Printf("Tx %v sent without waiting for confirmation. hash: %s\n", idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := new(big.Int).SetInt64(succeededCount)
		expectedAmount.Mul(expectedAmount, sendAmountDfm)
		expectedAmount.Add(expectedAmount, ethBalanceBefore)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, expectedAmount, 500, time.Second*10)
		require.NoError(t, err)

		newAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, ethBalanceBefore, newAmount, expectedAmount)
	})

	//nolint:dupl
	t.Run("From Prime to Nexus 1000x 20min 90%", func(t *testing.T) {
		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 1000

		maxWaitTime := 1200
		successChance := 90 // 90%
		succeededCount := int64(0)

		fmt.Printf("Sending transactions\n")

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCount++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], sendAmountDfm, user,
					)

					fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
				} else {
					const feeAmount = 1_100_000

					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm.Uint64() * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDNexus, feeAmount)
					require.NoError(t, err)

					txHash, err := cardanofw.SendTx(ctx, txProviderPrime, apex.Users[idx].PrimeWallet,
						sendAmountDfm.Uint64()+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)

					fmt.Printf("Tx %v sent without waiting for confirmation. hash: %s\n", idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := new(big.Int).SetInt64(succeededCount)
		expectedAmount.Mul(expectedAmount, sendAmountDfm)
		expectedAmount.Add(expectedAmount, ethBalanceBefore)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, expectedAmount, 500, time.Second*10)
		require.NoError(t, err)

		newAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, ethBalanceBefore, newAmount, expectedAmount)
	})
}

func TestE2E_ApexBridgeWithNexus_BatchFailed(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 1
	)

	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

	t.Run("Test insufficient gas price dynamicTx=true", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(nil, func(mp map[string]interface{}) {
				block := cardanofw.GetMapFromInterfaceKey(mp, "chains", cardanofw.ChainIDNexus, "config")
				block["gasFeeCap"] = uint64(10)
				block["gasTipCap"] = uint64(11)
			}),
		)

		user := apex.Users[userCnt-1]

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, sendAmountDfm, user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		failedToExecute, timeout = cardanofw.WaitForBatchState(
			ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, true, false, cardanofw.BatchStateExecuted)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		require.NoError(t, apex.StopRelayer())

		err := cardanofw.UpdateJSONFile(
			apex.GetValidator(t, 0).GetRelayerConfig(),
			apex.GetValidator(t, 0).GetRelayerConfig(),
			func(mp map[string]interface{}) {
				block := cardanofw.GetMapFromInterfaceKey(mp, "chains", cardanofw.ChainIDNexus, "config")
				block["gasFeeCap"] = uint64(0)
				block["gasTipCap"] = uint64(0)
			},
			false,
		)
		require.NoError(t, err)

		err = apex.StartRelayer(ctx)
		require.NoError(t, err)

		failedToExecute, timeout = cardanofw.WaitForBatchState(
			ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)

		require.LessOrEqual(t, failedToExecute, 1)
		require.False(t, timeout)
	})

	t.Run("Test insufficient gas price dynamicTx=false", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(nil, func(mp map[string]interface{}) {
				block := cardanofw.GetMapFromInterfaceKey(mp, "chains", cardanofw.ChainIDNexus, "config")
				block["gasPrice"] = uint64(10)
				block["dynamicTx"] = bool(false)
			}),
		)

		user := apex.Users[userCnt-1]

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, sendAmountDfm, user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		failedToExecute, timeout = cardanofw.WaitForBatchState(
			ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, true, false, cardanofw.BatchStateExecuted)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		require.NoError(t, apex.StopRelayer())

		err := cardanofw.UpdateJSONFile(
			apex.GetValidator(t, 0).GetRelayerConfig(),
			apex.GetValidator(t, 0).GetRelayerConfig(),
			func(mp map[string]interface{}) {
				block := cardanofw.GetMapFromInterfaceKey(mp, "chains", cardanofw.ChainIDNexus, "config")
				block["gasPrice"] = uint64(0)
			},
			false,
		)
		require.NoError(t, err)

		err = apex.StartRelayer(ctx)
		require.NoError(t, err)

		failedToExecute, timeout = cardanofw.WaitForBatchState(
			ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)

		require.LessOrEqual(t, failedToExecute, 1)
		require.False(t, timeout)
	})

	t.Run("Test small fee", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(nil, func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "chains", cardanofw.ChainIDNexus, "config")["depositGasLimit"] = uint64(10)
			}),
		)

		user := apex.Users[userCnt-1]

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, sendAmountDfm, user)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		failedToExecute, timeout = cardanofw.WaitForBatchState(ctx,
			apex, cardanofw.ChainIDPrime, txHash, apiKey, true, false, cardanofw.BatchStateExecuted)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		require.NoError(t, apex.StopRelayer())

		err := cardanofw.UpdateJSONFile(
			apex.GetValidator(t, 0).GetRelayerConfig(),
			apex.GetValidator(t, 0).GetRelayerConfig(),
			func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "chains", cardanofw.ChainIDNexus, "config")["depositGasLimit"] = uint64(0)
			},
			false,
		)
		require.NoError(t, err)

		err = apex.StartRelayer(ctx)
		require.NoError(t, err)

		failedToExecute, timeout = cardanofw.WaitForBatchState(ctx,
			apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)

		require.LessOrEqual(t, failedToExecute, 1)
		require.False(t, timeout)
	})

	//nolint:dupl
	t.Run("Test failed batch", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", cardanofw.ChainIDNexus)["testMode"] = uint8(1)
			}, nil),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		user := apex.Users[userCnt-1]

		prevBalanceDfm, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("Dfm before Tx %d\n", prevBalanceDfm)

		expectedAmount := new(big.Int).Set(sendAmountDfm)
		expectedAmount = expectedAmount.Add(expectedAmount, prevBalanceDfm)

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, sendAmountDfm, user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check batch failed
		failedToExecute, timeout = cardanofw.WaitForBatchState(
			ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, expectedAmount, 3, time.Second*10)
		require.NoError(t, err)
	})

	//nolint:dupl
	t.Run("Test failed batch 5 times in a row", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", cardanofw.ChainIDNexus)["testMode"] = uint8(2)
			}, nil),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		user := apex.Users[userCnt-1]

		prevBalanceDfm, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("DFM Amount before Tx %d\n", prevBalanceDfm)

		expectedAmount := new(big.Int).Set(sendAmountDfm)
		expectedAmount = expectedAmount.Add(expectedAmount, prevBalanceDfm)

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, sendAmountDfm, user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check batch failed
		failedToExecute, timeout = cardanofw.WaitForBatchState(
			ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)

		require.Equal(t, failedToExecute, 5)
		require.False(t, timeout)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, expectedAmount, 3, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("Test multiple failed batches in a row", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		instances := 5
		failedToExecute := make([]int, instances)
		timeout := make([]bool, instances)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", cardanofw.ChainIDNexus)["testMode"] = uint8(3)
			}, nil),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		user := apex.Users[userCnt-1]

		prevBalanceDfm, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("DFM Amount before Tx %d\n", prevBalanceDfm)

		ethExpectedBalance := big.NewInt(int64(instances))
		ethExpectedBalance.Mul(ethExpectedBalance, sendAmountDfm)
		ethExpectedBalance.Add(ethExpectedBalance, prevBalanceDfm)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				user, sendAmountDfm, user,
			)

			fmt.Printf("Tx %v sent. hash: %s\n", i, txHash)

			failedToExecute[i], timeout[i] = cardanofw.WaitForBatchState(
				ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)
		}

		for i := 0; i < instances; i++ {
			require.Equal(t, failedToExecute[i], 1)
			require.False(t, timeout[i])
		}

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 20, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("Test failed batches at random", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		instances := 5
		failedToExecute := make([]int, instances)
		timeout := make([]bool, instances)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", cardanofw.ChainIDNexus)["testMode"] = uint8(4)
			}, nil),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		user := apex.Users[userCnt-1]

		prevBalanceDfm, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("DFM Amount before Tx %d\n", prevBalanceDfm)

		ethExpectedBalance := big.NewInt(int64(instances))
		ethExpectedBalance.Mul(ethExpectedBalance, sendAmountDfm)
		ethExpectedBalance.Add(ethExpectedBalance, prevBalanceDfm)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				user, sendAmountDfm, user,
			)

			fmt.Printf("Tx %v sent. hash: %s\n", i, txHash)

			// Check batch failed
			failedToExecute[i], timeout[i] = cardanofw.WaitForBatchState(
				ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, false, cardanofw.BatchStateExecuted)
		}

		for i := 0; i < instances; i++ {
			if i%2 == 0 {
				require.Equal(t, 1, failedToExecute[i])
			}

			require.False(t, timeout[i])
		}

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 3, time.Second*10)
		require.NoError(t, err)
	})
}

func TestE2E_NexusFundAmount(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const (
		apiKey     = "test_api_key"
		userCnt    = 10
		fundAmount = 100_000_000
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig := cardanofw.NewPrimeChainConfig()
	primeConfig.FundAmount = 1_000_000

	nexusConfig := cardanofw.NewNexusChainConfig(true)
	nexusConfig.FundAmount = big.NewInt(1)

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithNexusConfig(nexusConfig),
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("nexus user addr: ", user.NexusAddress)
	fmt.Println("prime multisig addr: ", apex.PrimeInfo.MultisigAddr)
	fmt.Println("prime fee addr: ", apex.PrimeInfo.FeeAddr)
	fmt.Println("nexus gateway addr ", apex.NexusInfo.GatewayAddress)

	testCases := []struct {
		name          string
		sendAmountDfm *big.Int
		fromChain     cardanofw.ChainID
		toChain       cardanofw.ChainID
		fundAmountDfm *big.Int
	}{
		{
			name:          "From nexus to prime - not enough funds",
			sendAmountDfm: cardanofw.WeiToDfm(ethgo.Ether(5)),
			fromChain:     cardanofw.ChainIDNexus,
			toChain:       cardanofw.ChainIDPrime,
			fundAmountDfm: new(big.Int).SetUint64(fundAmount),
		},
		{
			name:          "From prime to nexus - not enough funds",
			sendAmountDfm: cardanofw.WeiToDfm(ethgo.Ether(15)),
			fromChain:     cardanofw.ChainIDPrime,
			toChain:       cardanofw.ChainIDNexus,
			fundAmountDfm: new(big.Int).SetUint64(fundAmount),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prevAmount, err := apex.GetBalance(ctx, user, tc.toChain)
			require.NoError(t, err)

			fmt.Printf("prevAmount %v\n", prevAmount)

			expectedAmount := new(big.Int).Set(tc.sendAmountDfm)
			expectedAmount = expectedAmount.Add(expectedAmount, prevAmount)

			txHash := apex.SubmitBridgingRequest(t, ctx,
				tc.fromChain, tc.toChain,
				user, tc.sendAmountDfm, user,
			)

			fmt.Printf("Tx sent. hash: %s. %v - expectedAmount\n", txHash, expectedAmount)

			err = apex.WaitForExactAmount(ctx, user, tc.toChain, expectedAmount, 20, time.Second*10)
			require.Error(t, err)

			require.NoError(t, apex.FundChainHotWallet(ctx, tc.toChain, tc.fundAmountDfm))

			txHash = apex.SubmitBridgingRequest(t, ctx,
				tc.fromChain, tc.toChain,
				user, tc.sendAmountDfm, user,
			)

			fmt.Printf("Tx sent. hash: %s. %v - expectedAmount\n", txHash, expectedAmount)

			err = apex.WaitForExactAmount(ctx, user, tc.toChain, expectedAmount, 20, time.Second*10)
			require.NoError(t, err)
		})
	}
}

func PrimeToNexusSequentialAndParallelWithMaxReceivers(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem,
	sequentialInstances, parallelInstances int, sendAmountDfm *big.Int,
) {
	t.Helper()

	const (
		receivers = 4
	)

	e2ehelper.ExecuteBridging(
		t, ctx, apex,
		sequentialInstances,
		apex.Users[:parallelInstances],
		apex.Users[:receivers],
		[]string{cardanofw.ChainIDPrime},
		map[string][]string{
			cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
		},
		sendAmountDfm)
}

func PrimeNexusBothDirectionsSequentialAndParallel(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, receiverUser *cardanofw.TestApexUser,
	sequentialInstances, parallelInstances int, sendAmountDfm *big.Int,
) {
	t.Helper()

	const ()

	e2ehelper.ExecuteBridging(
		t, ctx, apex,
		sequentialInstances,
		apex.Users[:parallelInstances],
		[]*cardanofw.TestApexUser{receiverUser},
		[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDNexus},
		map[string][]string{
			cardanofw.ChainIDPrime: {cardanofw.ChainIDNexus},
			cardanofw.ChainIDNexus: {cardanofw.ChainIDPrime},
		},
		sendAmountDfm)
}

func NexusToPrimeSequentialAndParallelWithMaxReceivers(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem,
	sequentialInstances, parallelInstances int, sendAmountDfm *big.Int,
) {
	t.Helper()

	const (
		receivers = 4
	)

	e2ehelper.ExecuteBridging(
		t, ctx, apex, sequentialInstances,
		apex.Users[:parallelInstances],
		apex.Users[:receivers],
		[]string{cardanofw.ChainIDNexus},
		map[string][]string{
			cardanofw.ChainIDNexus: {cardanofw.ChainIDPrime},
		},
		sendAmountDfm)
}

func PrimeToNexusSubmitterNotEnoughFunds(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
	sendAmountDfm *big.Int,
) {
	t.Helper()

	srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus
	receiverAddr := apex.PrimeInfo.MultisigAddr
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(dstChain): sendAmountDfm.Uint64(),
	}

	bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
		user.GetAddress(srcChain), receivers, dstChain, feeAmount)
	require.NoError(t, err)

	_, err = apex.SubmitTx(
		ctx, srcChain, user, receiverAddr, sendAmountDfm, bridgingRequestMetadata)

	require.Error(t, err)
	require.ErrorContains(t, err, "not enough funds")
}

func PrimeToNexusInvalidMetadataSlicedOff(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus
	receiverAddr := apex.PrimeInfo.MultisigAddr
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(dstChain): sendAmountDfm.Uint64() * 10,
	}

	bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
		user.GetAddress(srcChain), receivers, dstChain, feeAmount)
	require.NoError(t, err)

	// Send only half bytes of metadata making it invalid
	bridgingRequestMetadata = bridgingRequestMetadata[0 : len(bridgingRequestMetadata)/2]

	_, err = apex.SubmitTx(
		ctx, srcChain, user, receiverAddr, sendAmountDfm, bridgingRequestMetadata)
	require.Error(t, err)
}

func PrimeToNexusInvalidMetadataWrongType(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus
	receiverAddr := apex.PrimeInfo.MultisigAddr
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(dstChain): sendAmountDfm.Uint64() * 10,
	}

	var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0, len(receivers))
	for addr, amount := range receivers {
		transactions = append(transactions, cardanofw.BridgingRequestMetadataTransaction{
			Address: cardanofw.SplitString(addr, 40),
			Amount:  amount,
		})
	}

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "transaction", // should be "bridge"
			"d":  dstChain,
			"s":  cardanofw.SplitString(user.GetAddress(srcChain), 40),
			"tx": transactions,
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := apex.SubmitTx(
		ctx, srcChain, user, receiverAddr,
		sendAmountDfm.Add(sendAmountDfm, new(big.Int).SetUint64(feeAmount)), bridgingRequestMetadata)
	require.NoError(t, err)

	_, err = cardanofw.WaitForRequestStates(ctx, apex, srcChain, txHash, apex.Config.APIKey, nil, 60)
	require.Error(t, err)
	require.ErrorContains(t, err, "timeout")
}

func PrimeToNexusInvalidMetadataInvalidDestination(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus
	receiverAddr := apex.PrimeInfo.MultisigAddr
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(dstChain): sendAmountDfm.Uint64() * 10,
	}

	var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0, len(receivers))
	for addr, amount := range receivers {
		transactions = append(transactions, cardanofw.BridgingRequestMetadataTransaction{
			Address: cardanofw.SplitString(addr, 40),
			Amount:  amount,
		})
	}

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "bridge",
			"d":  "", // should be destination chain address
			"s":  cardanofw.SplitString(user.GetAddress(srcChain), 40),
			"tx": transactions,
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := apex.SubmitTx(
		ctx, srcChain, user, receiverAddr,
		sendAmountDfm.Add(sendAmountDfm, new(big.Int).SetUint64(feeAmount)), bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, srcChain, txHash, apex.Config.APIKey)
}

func PrimeToNexusInvalidMetadataInvalidSender(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus
	receiverAddr := apex.PrimeInfo.MultisigAddr
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(dstChain): sendAmountDfm.Uint64() * 10,
	}

	var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0, len(receivers))
	for addr, amount := range receivers {
		transactions = append(transactions, cardanofw.BridgingRequestMetadataTransaction{
			Address: cardanofw.SplitString(addr, 40),
			Amount:  amount,
		})
	}

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "bridge",
			"d":  dstChain,
			"s":  "", // should be sender address (max len 40)
			"tx": transactions,
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := apex.SubmitTx(
		ctx, srcChain, user, receiverAddr,
		sendAmountDfm.Add(sendAmountDfm, new(big.Int).SetUint64(feeAmount)), bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, srcChain, txHash, apex.Config.APIKey)
}

func PrimeToNexusInvalidMetadataInvalidTransactions(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	srcChain, dstChain := cardanofw.ChainIDPrime, cardanofw.ChainIDNexus
	receiverAddr := apex.PrimeInfo.MultisigAddr
	sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))
	feeAmount := uint64(1_100_000)

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "bridge",
			"d":  dstChain,
			"s":  cardanofw.SplitString(user.GetAddress(srcChain), 40),
			"tx": []cardanofw.BridgingRequestMetadataTransaction{}, // should not be empty
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := apex.SubmitTx(
		ctx, srcChain, user, receiverAddr,
		sendAmountDfm.Add(sendAmountDfm, new(big.Int).SetUint64(feeAmount)), bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, srcChain, txHash, apex.Config.APIKey)
}

func NexusToPrimeSubmitterNotEnoughFunds(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem,
) {
	t.Helper()

	fee := cardanofw.DfmToChainNativeTokenAmount(cardanofw.ChainIDNexus, new(big.Int).SetUint64(uint64(1_100_000)))
	sendAmountWei := ethgo.Ether(uint64(2))

	unfundedUser, err := cardanofw.NewTestApexUser(
		apex.Config.PrimeConfig.NetworkType,
		apex.Config.VectorConfig.IsEnabled,
		apex.Config.VectorConfig.NetworkType,
		apex.Config.NexusConfig.IsEnabled,
	)
	require.NoError(t, err)

	unfundedUserPk, err := unfundedUser.GetPrivateKey(cardanofw.ChainIDNexus)
	require.NoError(t, err)

	// call SendTx command
	err = sendTxParamsNPInvalidScenarios("evm",
		apex.NexusInfo.GatewayAddress.String(),
		apex.NexusInfo.JSONRPCAddr,
		unfundedUserPk, cardanofw.ChainIDPrime,
		unfundedUser.GetAddress(cardanofw.ChainIDPrime),
		sendAmountWei, fee,
	)
	require.ErrorContains(t, err, "insufficient funds for execution")
}

func sendTxParamsNPInvalidScenarios(txType, gatewayAddr, nexusURL, privateKey, chainDst, receiver string, amount, fee *big.Int) error {
	return cardanofw.RunCommand(cardanofw.ResolveApexBridgeBinary(), []string{
		"sendtx",
		"--tx-type", txType,
		"--gateway-addr", gatewayAddr,
		"--nexus-url", nexusURL,
		"--key", privateKey,
		"--chain-dst", chainDst,
		"--receiver", fmt.Sprintf("%s:%s", receiver, amount.String()),
		"--fee", fee.String(),
	}, os.Stdout)
}
