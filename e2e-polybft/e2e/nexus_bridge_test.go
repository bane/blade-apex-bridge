package e2e

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	BatchFailed  = "FailedToExecuteOnDestination"
	BatchSuccess = "ExecutedOnDestination"
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

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	t.Run("From Nexus to Prime", func(t *testing.T) {
		user := apex.Users[0]

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		sendAmountDfm, sendAmountWei := convertToEthValues(uint64(1))

		// call SendTx command
		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
			user, sendAmountWei, user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		expectedAmount := getExpectedAmountFromNexus(sendAmountDfm, prevAmount, 1, 1)

		// check expected amount cardano
		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

		user := apex.Users[0]

		receiverAddr := user.GetAddress(cardanofw.ChainIDNexus)
		fmt.Printf("ETH receiver Addr: %s\n", receiverAddr)

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount BEFORE TX %d\n", ethBalanceBefore)
		require.NoError(t, err)

		relayerBalanceBefore, err := apex.GetChainMust(t, cardanofw.ChainIDNexus).GetAddressBalance(
			ctx, apex.NexusInfo.RelayerAddress.String())
		require.NoError(t, err)

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, new(big.Int).SetUint64(sendAmountDfm), user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		expectedAmount := new(big.Int).Add(sendAmountEth, ethBalanceBefore)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)

		ethBalanceAfter, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount AFTER TX %d\n", ethBalanceAfter)
		require.NoError(t, err)

		relayerBalanceAfter, err := apex.GetChainMust(t, cardanofw.ChainIDNexus).GetAddressBalance(
			ctx, apex.NexusInfo.RelayerAddress.String())
		require.NoError(t, err)

		relayerBalanceGreater := relayerBalanceAfter.Cmp(relayerBalanceBefore) == 1
		require.True(t, relayerBalanceGreater)
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

	sendAmountDfm, sendAmountWei := convertToEthValues(uint64(1))

	user := apex.Users[userCnt-1]

	t.Run("From Nexus to Prime one by one - wait for other side", func(t *testing.T) {
		const (
			instances = 5
		)

		for i := 0; i < instances; i++ {
			// check amount on prime
			prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
			require.NoError(t, err)

			// call SendTx command
			apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
				user, sendAmountWei, user,
			)

			expectedAmount := getExpectedAmountFromNexus(sendAmountDfm, prevAmount, 1, 1)

			// check expected amount cardano
			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
			require.NoError(t, err)
		}
	})

	t.Run("From Nexus to Prime one by one - don't wait", func(t *testing.T) {
		const (
			instances = 5
		)

		// check amount on prime
		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			// call SendTx command
			apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
				user, sendAmountWei, user,
			)
		}

		expectedAmount := getExpectedAmountFromNexus(sendAmountDfm, prevAmount, instances, 1)

		// check expected amount cardano
		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime - parallel", func(t *testing.T) {
		const (
			instances = 5
		)

		// check amount on prime
		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				// call SendTx command
				apex.SubmitBridgingRequest(t, ctx,
					cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
					apex.Users[idx], sendAmountWei, user,
				)
			}(i)
		}

		wg.Wait()

		expectedAmount := getExpectedAmountFromNexus(sendAmountDfm, prevAmount, instances, 1)

		// check expected amount cardano
		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime - sequential and parallel", func(t *testing.T) {
		const (
			instances         = 5
			parallelInstances = 10
		)

		// check amount on prime
		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		for sequenceIdx := 0; sequenceIdx < instances; sequenceIdx++ {
			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(idx int) {
					defer wg.Done()

					// call SendTx command
					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
						apex.Users[idx], sendAmountWei, user,
					)
				}(i)
			}

			wg.Wait()
		}

		expectedAmount := getExpectedAmountFromNexus(sendAmountDfm, prevAmount, instances, parallelInstances)

		// check expected amount cardano
		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime - sequential and parallel multiple receivers", func(t *testing.T) {
		const (
			instances         = 5
			parallelInstances = 10
			receivers         = 4
		)

		// create receivers and check their amount on prime
		var (
			destinationUsers            = make([]*cardanofw.TestApexUser, receivers)
			destinationUsersPrevAmounts = make([]*big.Int, receivers)
			err                         error
		)

		for i := 0; i < receivers; i++ {
			destinationUsers[i] = apex.Users[i]

			destinationUsersPrevAmounts[i], err = apex.GetBalance(ctx, destinationUsers[i], cardanofw.ChainIDPrime)
			require.NoError(t, err)
		}

		for sequenceIdx := 0; sequenceIdx < instances; sequenceIdx++ {
			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(idx int) {
					defer wg.Done()

					// call SendTx command
					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
						apex.Users[idx], sendAmountWei, destinationUsers...,
					)
				}(i)
			}

			wg.Wait()
		}

		// check expected amount cardano

		var wgResults sync.WaitGroup
		for i := 0; i < receivers; i++ {
			wgResults.Add(1)

			go func(receiverIdx int) {
				defer wgResults.Done()

				expectedAmount := getExpectedAmountFromNexus(
					sendAmountDfm, destinationUsersPrevAmounts[receiverIdx], instances, parallelInstances)

				err = apex.WaitForExactAmount(ctx, destinationUsers[receiverIdx], cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
			}(i)
		}

		wgResults.Wait()
	})

	t.Run("From Nexus to Prime - sequential and parallel, one node goes off in the middle", func(t *testing.T) {
		const (
			instances            = 5
			parallelInstances    = 10
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
		)

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.GetValidator(t, validatorStoppingIdx).Stop())
			}
		}()

		// check amount on prime
		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		for sequenceIdx := 0; sequenceIdx < instances; sequenceIdx++ {
			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(idx int) {
					defer wg.Done()

					// call SendTx command
					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
						apex.Users[idx], sendAmountWei, user,
					)
				}(i)
			}

			wg.Wait()
		}

		expectedAmount := getExpectedAmountFromNexus(sendAmountDfm, prevAmount, instances, parallelInstances)

		// check expected amount cardano
		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
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
	fee := new(big.Int).SetUint64(1000010000000000000)

	nexusAdminPkBytes, err := apex.NexusInfo.AdminKey.MarshallPrivateKey()
	require.NoError(t, err)

	nexusAdminPrivateKey := hex.EncodeToString(nexusAdminPkBytes)

	sendTxParams := func(txType, gatewayAddr, nexusUrl, privateKey, chainDst, receiver string, amount, fee *big.Int) error {
		return cardanofw.RunCommand(cardanofw.ResolveApexBridgeBinary(), []string{
			"sendtx",
			"--tx-type", txType,
			"--gateway-addr", gatewayAddr,
			"--nexus-url", nexusUrl,
			"--key", privateKey,
			"--chain-dst", chainDst,
			"--receiver", fmt.Sprintf("%s:%s", receiver, amount.String()),
			"--fee", fee.String(),
		}, os.Stdout)
	}

	t.Run("Wrong Tx-Type", func(t *testing.T) {
		sendAmountEth := uint64(1)
		sendAmountWei := ethgo.Ether(sendAmountEth)

		userPk, err := user.GetPrivateKey(cardanofw.ChainIDNexus)
		require.NoError(t, err)

		// call SendTx command
		err = sendTxParams("cardano", // "cardano" instead of "evm"
			apex.NexusInfo.GatewayAddress.String(),
			apex.NexusInfo.Node.JSONRPCAddr(),
			userPk, cardanofw.ChainIDPrime,
			user.GetAddress(cardanofw.ChainIDPrime),
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "failed to execute command")
	})

	t.Run("Wrong Nexus URL", func(t *testing.T) {
		sendAmountEth := uint64(1)
		sendAmountWei := ethgo.Ether(sendAmountEth)

		userPk, err := user.GetPrivateKey(cardanofw.ChainIDNexus)
		require.NoError(t, err)

		// call SendTx command
		err = sendTxParams("evm",
			apex.NexusInfo.GatewayAddress.String(),
			"localhost:1234",
			userPk, cardanofw.ChainIDPrime,
			user.GetAddress(cardanofw.ChainIDPrime),
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "Error: invalid --nexus-url flag")
	})

	t.Run("Sender not enough funds", func(t *testing.T) {
		sendAmountEth := uint64(2)
		sendAmountWei := ethgo.Ether(sendAmountEth)

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
		err = sendTxParams("evm",
			apex.NexusInfo.GatewayAddress.String(),
			apex.NexusInfo.Node.JSONRPCAddr(),
			unfundedUserPk, cardanofw.ChainIDPrime,
			unfundedUser.GetAddress(cardanofw.ChainIDPrime),
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "insufficient funds for execution")
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

		_, err = apex.GetChainMust(t, cardanofw.ChainIDNexus).SendTx(
			ctx, nexusAdminPrivateKey, unfundedUser.NexusAddress.String(), big.NewInt(10), nil)
		require.NoError(t, err)

		sendAmountEth := uint64(20) // Sender funded with 10 Eth
		sendAmountWei := ethgo.Ether(sendAmountEth)

		// call SendTx command
		err = sendTxParams("evm",
			apex.NexusInfo.GatewayAddress.String(),
			apex.NexusInfo.Node.JSONRPCAddr(),
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

	t.Run("From Prime to Nexus one by one - wait for other side", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		instances := 5

		for i := 0; i < instances; i++ {
			ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
			fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
			require.NoError(t, err)

			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				user, new(big.Int).SetUint64(sendAmountDfm), user,
			)

			fmt.Printf("Tx sent. hash: %s\n", txHash)

			ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, sendAmountEth)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
			require.NoError(t, err)
		}
	})

	t.Run("From Prime to Nexus one by one - don't wait for other side", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 5

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				user, new(big.Int).SetUint64(sendAmountDfm), user,
			)

			fmt.Printf("Tx sent. hash: %s\n", txHash)
		}

		transferedAmount := new(big.Int).SetInt64(int64(instances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 5

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash := apex.SubmitBridgingRequest(t, ctx,
					cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
					apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
				)

				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		transferedAmount := new(big.Int).SetInt64(int64(instances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)

		ethBalanceAfter, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus sequential and parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		sequentialInstances := 5
		parallelInstances := 10

		for sequenceIdx := 0; sequenceIdx < sequentialInstances; sequenceIdx++ {
			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(sequence, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)

					fmt.Printf("Seq: %v, Tx %v sent. hash: %s\n", sequence+1, idx+1, txHash)
				}(sequenceIdx, i)
			}

			wg.Wait()
		}

		transferedAmount := new(big.Int).SetInt64(int64(parallelInstances * sequentialInstances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)
		fmt.Printf("Expected ETH Amount after Txs %d\n", ethExpectedBalance)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)

		ethBalanceAfter, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus sequential and parallel with max receivers", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
			receivers           = 4
		)

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		var (
			destinationUsers            = make([]*cardanofw.TestApexUser, receivers)
			destinationUsersPrevAmounts = make([]*big.Int, receivers)
		)

		for i := 0; i < receivers; i++ {
			destinationUsers[i] = apex.Users[i]

			destinationUsersPrevAmounts[i], err = apex.GetBalance(ctx, destinationUsers[i], cardanofw.ChainIDNexus)
			require.NoError(t, err)
		}

		for sequenceIdx := 0; sequenceIdx < sequentialInstances; sequenceIdx++ {
			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(sequence, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), destinationUsers...,
					)

					fmt.Printf("Seq: %v, Tx %v sent. hash: %s\n", sequence+1, idx+1, txHash)
				}(sequenceIdx, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs\n", sequentialInstances*parallelInstances)

		var wgResults sync.WaitGroup

		transferedAmount := new(big.Int).SetInt64(int64(parallelInstances * sequentialInstances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)

		for i := 0; i < receivers; i++ {
			wgResults.Add(1)

			go func(receiverIdx int) {
				defer wgResults.Done()

				ethExpectedBalance := big.NewInt(0).Add(destinationUsersPrevAmounts[receiverIdx], transferedAmount)

				err = apex.WaitForExactAmount(ctx,
					destinationUsers[receiverIdx], cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
				require.NoError(t, err)
				fmt.Printf("%v receiver, %v TXs confirmed\n", receiverIdx, sequentialInstances*parallelInstances)
			}(i)
		}

		wgResults.Wait()
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

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.GetValidator(t, validatorStoppingIdx).Stop())
			}
		}()

		for sequenceIdx := 0; sequenceIdx < sequentialInstances; sequenceIdx++ {
			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(sequence, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)

					fmt.Printf("Seq: %v, Tx %v sent. hash: %s\n", sequence+1, idx+1, txHash)
				}(sequenceIdx, i)
			}

			wg.Wait()
		}

		transferedAmount := new(big.Int).SetInt64(int64(parallelInstances * sequentialInstances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)
		fmt.Printf("Expected ETH Amount after Txs %d\n", ethExpectedBalance)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)

		fmt.Printf("TXs on Nexus expected amount received, err: %v\n", err)

		// nothing else should be bridged for 2 minutes
		err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 12, time.Second*10)
		assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on prime")

		fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
	})

	t.Run("Both directions sequential", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sendAmount = uint64(1)
			instances  = 5
		)

		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

		prevAmountPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		prevAmountNexus, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				apex.Users[0], new(big.Int).SetUint64(sendAmountDfm), user,
			)

			fmt.Printf("prime tx sent. hash: %s\n", txHash)

			txHash = apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
				apex.Users[0], sendAmountEth, user,
			)

			fmt.Printf("nexus tx sent. hash: %s\n", txHash)
		}

		transferedAmountEth := new(big.Int).SetInt64(int64(instances))
		transferedAmountEth.Mul(transferedAmountEth, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(prevAmountNexus, transferedAmountEth)
		fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

		// check expected amount nexus
		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)

		// check expected amount prime
		transferedAmountDfm := new(big.Int).SetInt64(int64(instances))
		transferedAmountDfm.Mul(transferedAmountDfm, new(big.Int).SetUint64(sendAmountDfm))
		dfmExpectedBalance := big.NewInt(0).Add(prevAmountPrime, transferedAmountDfm)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, dfmExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("Both directions sequential and parallel", func(t *testing.T) {
		sequentialInstances := 5
		parallelInstances := 6

		sendAmount := uint64(1)
		sendAmountDfm, sendAmountWei := convertToEthValues(sendAmount)

		prevAmountPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		prevAmountNexus, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		for j := 0; j < sequentialInstances; j++ {
			var wg sync.WaitGroup

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)

					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
						apex.Users[idx], sendAmountWei, user,
					)

					fmt.Printf("run: %v. Nexus tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		// check expected amount nexus
		transferedAmountEth := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
		transferedAmountEth.Mul(transferedAmountEth, sendAmountWei)
		ethExpectedBalance := big.NewInt(0).Add(prevAmountNexus, transferedAmountEth)
		fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)

		// check expected amount prime
		transferedAmountDfm := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
		transferedAmountDfm.Mul(transferedAmountDfm, new(big.Int).SetUint64(sendAmountDfm))
		dfmExpectedBalance := big.NewInt(0).Add(prevAmountPrime, transferedAmountDfm)
		fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", dfmExpectedBalance)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, dfmExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("Both directions sequential and parallel - one node goes off in the midle", func(t *testing.T) {
		const (
			sequentialInstances  = 5
			parallelInstances    = 6
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
		)

		sendAmount := uint64(1)
		sendAmountDfm, sendAmountWei := convertToEthValues(sendAmount)

		prevAmountPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		prevAmountNexus, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.GetValidator(t, validatorStoppingIdx).Stop())
			}
		}()

		var wg sync.WaitGroup

		for i := 0; i < parallelInstances; i++ {
			wg.Add(2)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)

					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", j+1, idx+1, txHash)
				}
			}(i)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
						apex.Users[idx], sendAmountWei, user,
					)

					fmt.Printf("run: %v. Nexus tx %v sent. hash: %s\n", j+1, idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		wg.Add(2)

		go func() {
			defer wg.Done()

			// check expected amount nexus
			transferedAmountEth := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
			transferedAmountEth.Mul(transferedAmountEth, sendAmountWei)
			ethExpectedBalance := big.NewInt(0).Add(prevAmountNexus, transferedAmountEth)
			fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
			require.NoError(t, err)

			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on nexus")

			fmt.Printf("TXs on nexus finished with success: %v\n", err != nil)
		}()

		go func() {
			defer wg.Done()

			transferedAmountDfm := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
			transferedAmountDfm.Mul(transferedAmountDfm, new(big.Int).SetUint64(sendAmountDfm))
			dfmExpectedBalance := big.NewInt(0).Add(prevAmountPrime, transferedAmountDfm)
			fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", dfmExpectedBalance)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, dfmExpectedBalance, 100, time.Second*10)
			require.NoError(t, err)

			// nothing else should be bridged for 2 minutes
			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDPrime, dfmExpectedBalance, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on prime")

			fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
		}()

		wg.Wait()
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

		sendAmount := uint64(1)
		sendAmountDfm, sendAmountWei := convertToEthValues(sendAmount)

		prevAmountPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		prevAmountNexus, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.GetValidator(t, validatorStoppingIdx1).Stop())
				require.NoError(t, apex.GetValidator(t, validatorStoppingIdx2).Stop())
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(startAgainAfter):
				require.NoError(t, apex.GetValidator(t, validatorStoppingIdx1).Start(ctx, false))
			}
		}()

		var wg sync.WaitGroup

		for i := 0; i < parallelInstances; i++ {
			wg.Add(2)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)

					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", j+1, idx+1, txHash)
				}
			}(i)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDNexus, cardanofw.ChainIDPrime,
						apex.Users[idx], sendAmountWei, user,
					)

					fmt.Printf("run: %v. Nexus tx %v sent. hash: %s\n", j+1, idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		wg.Add(2)

		go func() {
			defer wg.Done()

			// check expected amount nexus
			transferedAmountEth := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
			transferedAmountEth.Mul(transferedAmountEth, sendAmountWei)
			ethExpectedBalance := big.NewInt(0).Add(prevAmountNexus, transferedAmountEth)
			fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
			require.NoError(t, err)

			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on nexus")

			fmt.Printf("TXs on nexus finished with success: %v\n", err != nil)
		}()

		go func() {
			defer wg.Done()

			transferedAmountDfm := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
			transferedAmountDfm.Mul(transferedAmountDfm, new(big.Int).SetUint64(sendAmountDfm))
			dfmExpectedBalance := big.NewInt(0).Add(prevAmountPrime, transferedAmountDfm)
			fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", dfmExpectedBalance)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, dfmExpectedBalance, 100, time.Second*10)
			require.NoError(t, err)

			// nothing else should be bridged for 2 minutes
			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDPrime, dfmExpectedBalance, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on prime")

			fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
		}()

		wg.Wait()
	})
}

func TestE2E_ApexBridgeWithNexus_PtN_InvalidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 15
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig := cardanofw.NewPrimeChainConfig()
	primeConfig.PremineAmount = 10_000_000

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

	t.Run("Submitter not enough funds", func(t *testing.T) {
		sendAmountDfm, _ := convertToEthValues(100)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm,
		}

		bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
			user.GetAddress(cardanofw.ChainIDPrime), receivers,
			cardanofw.ChainIDNexus, feeAmount)
		require.NoError(t, err)

		_, err = cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
			apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
		require.Error(t, err)
		require.ErrorContains(t, err, "not enough funds")
	})

	t.Run("Submitted invalid metadata", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm * 10,
		}

		bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
			user.GetAddress(cardanofw.ChainIDPrime), receivers, cardanofw.ChainIDNexus, feeAmount)
		require.NoError(t, err)

		// Send only half bytes of metadata making it invalid
		bridgingRequestMetadata = bridgingRequestMetadata[0 : len(bridgingRequestMetadata)/2]

		_, err = cardanofw.SendTx(ctx, txProviderPrime, user.PrimeWallet,
			sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
			apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
		require.Error(t, err)
	})

	t.Run("Submitted invalid metadata - wrong type", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm * 10,
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
				"d":  cardanofw.ChainIDNexus,
				"s":  cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
				"tx": transactions,
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, user.PrimeWallet,
			sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
			apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, cardanofw.ChainIDPrime, txHash)

		_, err = cardanofw.WaitForRequestStates(nil, ctx, requestURL, apiKey, 60)
		require.Error(t, err)
		require.ErrorContains(t, err, "Timeout")
	})

	t.Run("Submitted invalid metadata - invalid destination", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm * 10,
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
				"s":  cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
				"tx": transactions,
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, user.PrimeWallet,
			sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
			apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHash)
	})

	t.Run("Submitted invalid metadata - invalid sender", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm * 10,
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
				"d":  cardanofw.ChainIDNexus,
				"s":  "", // should be sender address (max len 40)
				"tx": transactions,
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, user.PrimeWallet,
			sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
			apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, cardanofw.ChainIDPrime, txHash)

		_, err = cardanofw.WaitForRequestStates(nil, ctx, requestURL, apiKey, 60)
		require.NoError(t, err)
	})

	t.Run("Submitted invalid metadata - empty tx", func(t *testing.T) {
		sendAmountDfm, _ := convertToEthValues(0)
		feeAmount := uint64(1_100_000)

		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0)

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "bridge",
				"d":  cardanofw.ChainIDNexus,
				"s":  cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
				"tx": transactions, // should not be empty
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, user.PrimeWallet,
			sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
			apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, cardanofw.ChainIDPrime, txHash)

		_, err = cardanofw.WaitForRequestStates(nil, ctx, requestURL, apiKey, 60)
		require.NoError(t, err)
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
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

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
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)
					fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
				} else {
					const feeAmount = 1_100_000

					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDNexus, feeAmount)
					require.NoError(t, err)

					txHash, err := cardanofw.SendTx(ctx, txProviderPrime, apex.Users[idx].PrimeWallet,
						sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)

					fmt.Printf("Tx %v sent without waiting for confirmation. hash: %s\n", idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := new(big.Int).SetInt64(succeededCount)
		expectedAmount.Mul(expectedAmount, sendAmountEth)
		expectedAmount.Add(expectedAmount, ethBalanceBefore)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, expectedAmount, 500, time.Second*10)
		require.NoError(t, err)

		newAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, ethBalanceBefore, newAmount, expectedAmount)
	})

	//nolint:dupl
	t.Run("From Prime to Nexus 1000x 20min 90%", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

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
						apex.Users[idx], new(big.Int).SetUint64(sendAmountDfm), user,
					)

					fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
				} else {
					const feeAmount = 1_100_000

					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDNexus): sendAmountDfm * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDNexus, feeAmount)
					require.NoError(t, err)

					txHash, err := cardanofw.SendTx(ctx, txProviderPrime, apex.Users[idx].PrimeWallet,
						sendAmountDfm+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)

					fmt.Printf("Tx %v sent without waiting for confirmation. hash: %s\n", idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := new(big.Int).SetInt64(succeededCount)
		expectedAmount.Mul(expectedAmount, sendAmountEth)
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

	sendAmountDfm, sendAmountEth := convertToEthValues(1)

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
			user, new(big.Int).SetUint64(sendAmountDfm), user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, true)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		err = apex.StopRelayer()
		require.NoError(t, err)

		err = cardanofw.UpdateJSONFile(
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

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

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
			user, new(big.Int).SetUint64(sendAmountDfm), user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, true)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		err = apex.StopRelayer()
		require.NoError(t, err)

		err = cardanofw.UpdateJSONFile(
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

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

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
			user, new(big.Int).SetUint64(sendAmountDfm), user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, true)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		err = apex.StopRelayer()
		require.NoError(t, err)

		err = cardanofw.UpdateJSONFile(
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

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

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

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, new(big.Int).SetUint64(sendAmountDfm), user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check batch failed
		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)
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

		txHash := apex.SubmitBridgingRequest(t, ctx,
			cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
			user, new(big.Int).SetUint64(sendAmountDfm), user,
		)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check batch failed
		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

		require.Equal(t, failedToExecute, 5)
		require.False(t, timeout)
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

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		ethExpectedBalance := big.NewInt(int64(instances))
		ethExpectedBalance.Mul(ethExpectedBalance, sendAmountEth)
		ethExpectedBalance.Add(ethExpectedBalance, ethBalanceBefore)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				user, new(big.Int).SetUint64(sendAmountDfm), user,
			)

			fmt.Printf("Tx %v sent. hash: %s\n", i, txHash)

			// Check batch failed
			failedToExecute[i], timeout[i] = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)
		}

		for i := 0; i < instances; i++ {
			require.Equal(t, failedToExecute[i], 1)
			require.False(t, timeout[i])
		}

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
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

		ethBalanceBefore, err := apex.GetBalance(ctx, user, cardanofw.ChainIDNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		ethExpectedBalance := big.NewInt(int64(instances))
		ethExpectedBalance.Mul(ethExpectedBalance, sendAmountEth)
		ethExpectedBalance.Add(ethExpectedBalance, ethBalanceBefore)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDNexus,
				user, new(big.Int).SetUint64(sendAmountDfm), user,
			)

			fmt.Printf("Tx %v sent. hash: %s\n", i, txHash)

			// Check batch failed
			failedToExecute[i], timeout[i] = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)
		}

		for i := 0; i < instances; i++ {
			if i%2 == 0 {
				require.Equal(t, failedToExecute[i], 1)
			}

			require.False(t, timeout[i])
		}

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDNexus, ethExpectedBalance, 100, time.Second*10)
		require.NoError(t, err)
	})
}

func waitForBatchSuccess(
	ctx context.Context, txHash string, apiURL string, apiKey string, breakAfterFail bool,
) (int, bool) {
	var (
		prevStatus           string
		currentStatus        string
		failedToExecuteCount int
		timeout              bool
	)

	timeoutTimer := time.NewTimer(time.Second * 300)
	defer timeoutTimer.Stop()

	requestURL := fmt.Sprintf(
		"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, cardanofw.ChainIDPrime, txHash)

	for {
		select {
		case <-timeoutTimer.C:
			timeout = true

			fmt.Printf("Timeout\n")

			return failedToExecuteCount, timeout
		case <-ctx.Done():
			return failedToExecuteCount, timeout
		case <-time.After(time.Millisecond * 500):
		}

		currentState, err := cardanofw.GetBridgingRequestState(ctx, requestURL, apiKey)
		if err != nil || currentState == nil {
			continue
		}

		prevStatus = currentStatus
		currentStatus = currentState.Status

		if prevStatus != currentStatus {
			fmt.Printf("currentStatus = %s\n", currentStatus)

			if currentStatus == BatchFailed {
				failedToExecuteCount++

				if breakAfterFail {
					return failedToExecuteCount, timeout
				}
			}

			if currentStatus == BatchSuccess {
				return failedToExecuteCount, timeout
			}
		}
	}
}

func convertToEthValues(sendAmount uint64) (uint64, *big.Int) {
	sendAmountDfm := new(big.Int).SetUint64(sendAmount)
	exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil)
	sendAmountDfm.Mul(sendAmountDfm, exp)

	return sendAmountDfm.Uint64(), ethgo.Ether(sendAmount)
}

func getExpectedAmountFromNexus(
	sendAmountDfm uint64, prevAmount *big.Int, instances, parallelInstances uint64,
) *big.Int {
	expectedAmount := new(big.Int).SetUint64(sendAmountDfm)
	expectedAmount.Mul(expectedAmount, new(big.Int).SetUint64(instances))
	expectedAmount.Mul(expectedAmount, new(big.Int).SetUint64(parallelInstances))

	return expectedAmount.Add(expectedAmount, prevAmount)
}
