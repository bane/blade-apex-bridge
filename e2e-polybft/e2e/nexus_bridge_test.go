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

	ethwallet "github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
)

const (
	BatchFailed  = "FailedToExecuteOnDestination"
	BatchSuccess = "ExecutedOnDestination"
)

func TestE2E_ApexBridgeWithNexus(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
	)

	t.Run("Sanity check", func(t *testing.T) {
		sendAmount := uint64(1)
		expectedAmount := ethgo.Ether(sendAmount)

		user, err := apex.CreateAndFundNexusUser(ctx, sendAmount)
		require.NoError(t, err)

		ethBalance, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		require.NoError(t, err)
		require.NotZero(t, ethBalance)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, user, func(val *big.Int) bool {
			return val.Cmp(expectedAmount) == 0
		}, 10, 10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime", func(t *testing.T) {
		user := apex.CreateAndFundUser(t, ctx, uint64(5_000_000))

		txProviderPrime := apex.GetPrimeTxProvider()

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 10)
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), user.PrimeAddress)
		require.NoError(t, err)

		sendAmountDfm, sendAmountWei := convertToEthValues(uint64(1))

		// call SendTx command
		err = apex.Nexus.SendTxEvm(hex.EncodeToString(pkBytes), user.PrimeAddress, sendAmountWei)
		require.NoError(t, err)

		// check expected amount cardano
		expectedAmountOnPrime := prevAmount + sendAmountDfm
		err = cardanofw.WaitForAmount(ctx, txProviderPrime, user.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)

		newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)
		require.NotZero(t, newAmountOnPrime)
	})

	t.Run("From Prime to Nexus", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

		userPrime := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))
		require.NotNil(t, userPrime)

		user, err := apex.CreateAndFundNexusUser(ctx, sendAmount)
		require.NoError(t, err)

		ethBalance, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount %d\n", ethBalance)
		require.NoError(t, err)
		require.NotZero(t, ethBalance)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, user, func(val *big.Int) bool {
			return val.Cmp(sendAmountEth) == 0
		}, 10, 10)
		require.NoError(t, err)

		txProviderPrime := apex.GetPrimeTxProvider()

		nexusAddress := user.Address()

		receiverAddr := user.Address().String()
		fmt.Printf("ETH receiver Addr: %s\n", receiverAddr)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount BEFORE TX %d\n", ethBalanceBefore)
		require.NoError(t, err)

		relayerBalanceBefore, err := cardanofw.GetAddressEthAmount(ctx, apex.Nexus, apex.Bridge.GetRelayerWalletAddr())
		require.NoError(t, err)

		txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			nexusAddress.String(), sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddr)
		require.NoError(t, err)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, user, func(val *big.Int) bool {
			ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
			require.NoError(t, err)

			return ethBalanceBefore.Cmp(ethBalanceAfter) != 0
		}, 30, time.Second*30)
		require.NoError(t, err)

		ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount AFTER AFTER TX %d\n", ethBalanceAfter)
		require.NoError(t, err)

		relayerBalanceAfter, err := cardanofw.GetAddressEthAmount(ctx, apex.Nexus, apex.Bridge.GetRelayerWalletAddr())
		require.NoError(t, err)

		relayerBalanceGreater := relayerBalanceAfter.Cmp(relayerBalanceBefore) == 1
		require.True(t, relayerBalanceGreater)
	})
}

func TestE2E_ApexBridgeWithNexus_NtP_ValidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
	)

	txProviderPrime := apex.GetPrimeTxProvider()

	sendAmountDfm, sendAmountWei := convertToEthValues(uint64(1))

	cardanoUser := apex.CreateAndFundUser(t, ctx, uint64(1_000_000))

	t.Run("From Nexus to Prime one by one - wait for other side", func(t *testing.T) {
		const (
			instances = 5
		)

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 20)
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		evmUserPk := hex.EncodeToString(pkBytes)

		for i := 0; i < instances; i++ {
			// check amount on prime
			prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), cardanoUser.PrimeAddress)
			require.NoError(t, err)

			// call SendTx command
			err = apex.Nexus.SendTxEvm(evmUserPk, cardanoUser.PrimeAddress, sendAmountWei)
			require.NoError(t, err)

			// check expected amount cardano
			expectedAmountOnPrime := prevAmount + sendAmountDfm
			err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, cardanoUser.PrimeAddress, func(val uint64) bool {
				return val == expectedAmountOnPrime
			}, 100, time.Second*10)
			require.NoError(t, err)
		}
	})

	t.Run("From Nexus to Prime one by one - don't wait", func(t *testing.T) {
		const (
			instances = 5
		)

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 20)
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		evmUserPk := hex.EncodeToString(pkBytes)

		// check amount on prime
		prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), cardanoUser.PrimeAddress)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			// call SendTx command
			err = apex.Nexus.SendTxEvm(evmUserPk, cardanoUser.PrimeAddress, sendAmountWei)
			require.NoError(t, err)
		}

		// check expected amount cardano
		expectedAmountOnPrime := prevAmount + sendAmountDfm*uint64(instances)
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, cardanoUser.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime - parallel", func(t *testing.T) {
		const (
			instances = 5
		)

		// check amount on prime
		prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), cardanoUser.PrimeAddress)
		require.NoError(t, err)

		evmUserPks := make([]string, instances)

		for i := 0; i < instances; i++ {
			// create and fund wallet on nexus
			evmUser, err := apex.CreateAndFundNexusUser(ctx, 5)
			require.NoError(t, err)
			pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
			require.NoError(t, err)

			evmUserPks[i] = hex.EncodeToString(pkBytes)
		}

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				// call SendTx command
				err = apex.Nexus.SendTxEvm(evmUserPks[idx], cardanoUser.PrimeAddress, sendAmountWei)
				require.NoError(t, err)
			}(i)
		}

		wg.Wait()

		// check expected amount cardano
		expectedAmountOnPrime := prevAmount + sendAmountDfm*uint64(instances)
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, cardanoUser.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime - sequential and parallel", func(t *testing.T) {
		const (
			instances         = 5
			parallelInstances = 10
		)

		// check amount on prime
		prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), cardanoUser.PrimeAddress)
		require.NoError(t, err)

		for sequenceIdx := 0; sequenceIdx < instances; sequenceIdx++ {
			evmUserPks := make([]string, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				// create and fund wallet on nexus
				evmUser, err := apex.CreateAndFundNexusUser(ctx, 5)
				require.NoError(t, err)
				pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
				require.NoError(t, err)

				evmUserPks[i] = hex.EncodeToString(pkBytes)
			}

			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(idx int) {
					defer wg.Done()

					// call SendTx command
					err = apex.Nexus.SendTxEvm(evmUserPks[idx], cardanoUser.PrimeAddress, sendAmountWei)
					require.NoError(t, err)
				}(i)
			}

			wg.Wait()
		}

		// check expected amount cardano
		expectedAmountOnPrime := prevAmount + sendAmountDfm*uint64(instances)*uint64(parallelInstances)
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, cardanoUser.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("From Nexus to Prime - sequential and parallel multiple receivers", func(t *testing.T) {
		const (
			instances         = 5
			parallelInstances = 10
			receivers         = 4
		)

		// create receivers and check their amount on prime
		cardanoAddresses := make([]string, receivers)
		prevAmounts := make([]uint64, receivers)

		for i := 0; i < receivers; i++ {
			cUser := apex.CreateAndFundUser(t, ctx, uint64(1_000_000))
			cardanoAddresses[i] = cUser.PrimeAddress

			prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), cardanoAddresses[i])
			require.NoError(t, err)

			prevAmounts[i] = prevAmount
		}

		for sequenceIdx := 0; sequenceIdx < instances; sequenceIdx++ {
			evmUserPks := make([]string, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				// create and fund wallet on nexus
				evmUser, err := apex.CreateAndFundNexusUser(ctx, 20)
				require.NoError(t, err)
				pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
				require.NoError(t, err)

				evmUserPks[i] = hex.EncodeToString(pkBytes)
			}

			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(idx int) {
					defer wg.Done()

					// call SendTx command
					err := apex.Nexus.SendTxEvmMultipleReceivers(evmUserPks[idx], cardanoAddresses, sendAmountWei)
					require.NoError(t, err)
				}(i)
			}

			wg.Wait()
		}

		// check expected amount cardano
		expectedAmountOnPrime := prevAmounts[0] + sendAmountDfm*uint64(instances)*uint64(parallelInstances)

		var wgResults sync.WaitGroup
		for i := 0; i < receivers; i++ {
			wgResults.Add(1)

			go func(receiverIdx int) {
				defer wgResults.Done()

				err := cardanofw.WaitForAmount(context.Background(), txProviderPrime, cardanoAddresses[receiverIdx], func(val uint64) bool {
					return val == expectedAmountOnPrime
				}, 100, time.Second*10)
				require.NoError(t, err)
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
				require.NoError(t, apex.Bridge.GetValidator(t, validatorStoppingIdx).Stop())
			}
		}()

		// check amount on prime
		prevAmount, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), cardanoUser.PrimeAddress)
		require.NoError(t, err)

		for sequenceIdx := 0; sequenceIdx < instances; sequenceIdx++ {
			evmUserPks := make([]string, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				// create and fund wallet on nexus
				evmUser, err := apex.CreateAndFundNexusUser(ctx, 5)
				require.NoError(t, err)
				pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
				require.NoError(t, err)

				evmUserPks[i] = hex.EncodeToString(pkBytes)
			}

			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(idx int) {
					defer wg.Done()

					// call SendTx command
					err = apex.Nexus.SendTxEvm(evmUserPks[idx], cardanoUser.PrimeAddress, sendAmountWei)
					require.NoError(t, err)
				}(i)
			}

			wg.Wait()
		}

		// check expected amount cardano
		expectedAmountOnPrime := prevAmount + sendAmountDfm*uint64(instances)*uint64(parallelInstances)
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, cardanoUser.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)
	})
}

func TestE2E_ApexBridgeWithNexus_NtP_InvalidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
	)

	fee := new(big.Int).SetUint64(1000010000000000000)

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
		cardanoUser := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 10)
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		sendAmountEth := uint64(1)
		sendAmountWei := ethgo.Ether(sendAmountEth)

		// call SendTx command
		err = sendTxParams("cardano", // "cardano" instead of "evm"
			apex.Nexus.GetGatewayAddress().String(),
			apex.Nexus.Cluster.Servers[0].JSONRPCAddr(),
			hex.EncodeToString(pkBytes), "prime",
			cardanoUser.PrimeAddress,
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "failed to execute command")
	})

	t.Run("Wrong Nexus URL", func(t *testing.T) {
		cardanoUser := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 10)
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		sendAmountEth := uint64(1)
		sendAmountWei := ethgo.Ether(sendAmountEth)

		// call SendTx command
		err = sendTxParams("evm",
			apex.Nexus.GetGatewayAddress().String(),
			"localhost:1234",
			hex.EncodeToString(pkBytes), "prime",
			cardanoUser.PrimeAddress,
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "Error: invalid --nexus-url flag")
	})

	t.Run("Sender not enough funds", func(t *testing.T) {
		cardanoUser := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 1) // sendAmountEth = 2
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		sendAmountEth := uint64(2)
		sendAmountWei := ethgo.Ether(sendAmountEth)

		// call SendTx command
		err = sendTxParams("evm",
			apex.Nexus.GetGatewayAddress().String(),
			apex.Nexus.Cluster.Servers[0].JSONRPCAddr(),
			hex.EncodeToString(pkBytes), "prime",
			cardanoUser.PrimeAddress,
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "insufficient funds for execution")
	})

	t.Run("Big receiver amount", func(t *testing.T) {
		cardanoUser := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 10)
		require.NoError(t, err)
		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		sendAmountEth := uint64(20) // Sender funded with 10 Eth
		sendAmountWei := ethgo.Ether(sendAmountEth)

		// call SendTx command
		err = sendTxParams("evm",
			apex.Nexus.GetGatewayAddress().String(),
			apex.Nexus.Cluster.Servers[0].JSONRPCAddr(),
			hex.EncodeToString(pkBytes), "prime",
			cardanoUser.PrimeAddress,
			sendAmountWei, fee,
		)
		require.ErrorContains(t, err, "insufficient funds for execution")
	})
}

func TestE2E_ApexBridgeWithNexus_PtNandBoth_ValidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
	)

	userPrime := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))
	require.NotNil(t, userPrime)

	txProviderPrime := apex.GetPrimeTxProvider()

	startAmountNexus := uint64(1)
	expectedAmountNexus := ethgo.Ether(startAmountNexus)

	userNexus, err := apex.CreateAndFundNexusUser(ctx, startAmountNexus)
	require.NoError(t, err)

	err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
		return val.Cmp(expectedAmountNexus) == 0
	}, 10, 10)
	require.NoError(t, err)

	fmt.Println("Nexus user created and funded")

	receiverAddrNexus := userNexus.Address().String()
	fmt.Printf("Nexus receiver Addr: %s\n", receiverAddrNexus)

	t.Run("From Prime to Nexus", func(t *testing.T) {
		sendAmountDfm, _ := convertToEthValues(1)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
		require.NoError(t, err)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethBalanceBefore) != 0
		}, 30, 30*time.Second)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus one by one - wait for other side", func(t *testing.T) {
		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		instances := 5

		for i := 0; i < instances; i++ {
			ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
			fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
			require.NoError(t, err)

			txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
			require.NoError(t, err)

			fmt.Printf("Tx sent. hash: %s\n", txHash)

			ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, sendAmountEth)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
				return val.Cmp(ethExpectedBalance) == 0
			}, 30, 30*time.Second)
			require.NoError(t, err)
		}
	})

	t.Run("From Prime to Nexus one by one - don't wait for other side", func(t *testing.T) {
		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 5

		for i := 0; i < instances; i++ {
			txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
			require.NoError(t, err)

			fmt.Printf("Tx sent. hash: %s\n", txHash)
		}

		transferedAmount := new(big.Int).SetInt64(int64(instances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 30, 30*time.Second)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus parallel", func(t *testing.T) {
		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		// Fund wallets
		instances := 5
		primeUsers := make([]*cardanofw.TestApexUser, instances)

		for i := 0; i < instances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(10_000_000))
			require.NotNil(t, primeUsers[i])
		}

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
					receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
				require.NoError(t, err)

				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		transferedAmount := new(big.Int).SetInt64(int64(instances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 30, 30*time.Second)
		require.NoError(t, err)

		ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus sequential and parallel", func(t *testing.T) {
		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		sequentialInstances := 5
		parallelInstances := 10

		for sequenceIdx := 0; sequenceIdx < sequentialInstances; sequenceIdx++ {
			primeUsers := make([]*cardanofw.TestApexUser, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(5_000_000))
				require.NotNil(t, primeUsers[i])
			}

			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(sequence, idx int) {
					defer wg.Done()

					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
					require.NoError(t, err)

					fmt.Printf("Seq: %v, Tx %v sent. hash: %s\n", sequence+1, idx+1, txHash)
				}(sequenceIdx, i)
			}

			wg.Wait()
		}

		transferedAmount := new(big.Int).SetInt64(int64(parallelInstances * sequentialInstances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)
		fmt.Printf("Expected ETH Amount after Txs %d\n", ethExpectedBalance)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 60, 30*time.Second)
		require.NoError(t, err)

		ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)
		require.NoError(t, err)
	})

	t.Run("From Prime to Nexus sequential and parallel with max receivers", func(t *testing.T) {
		const (
			sequentialInstances = 5
			parallelInstances   = 10
			receivers           = 4
		)

		startAmountNexus := uint64(100)
		startAmountNexusEth := ethgo.Ether(startAmountNexus)

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		nexusUsers := make([]*ethwallet.Account, receivers)
		receiverAddresses := make([]string, receivers)

		// Create receivers
		for i := 0; i < receivers; i++ {
			user, err := apex.CreateAndFundNexusUser(ctx, startAmountNexus)
			require.NoError(t, err)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, user, func(val *big.Int) bool {
				return val.Cmp(startAmountNexusEth) == 0
			}, 30, 30*time.Second)
			require.NoError(t, err)

			receiverAddresses[i] = user.Address().String()
			nexusUsers[i] = user
		}

		fmt.Println("Nexus user created and funded")

		for sequenceIdx := 0; sequenceIdx < sequentialInstances; sequenceIdx++ {
			primeUsers := make([]*cardanofw.TestApexUser, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(10_000_000))
				require.NotNil(t, primeUsers[i])
			}

			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(sequence, idx int) {
					defer wg.Done()

					txHash, err := cardanofw.BridgeAmountFullMultipleReceiversNexus(ctx, txProviderPrime,
						apex.PrimeCluster.NetworkConfig(), apex.Bridge.PrimeMultisigAddr, receiverAddrNexus,
						primeUsers[idx].PrimeWallet, receiverAddresses, sendAmountDfm)
					require.NoError(t, err)

					fmt.Printf("Seq: %v, Tx %v sent. hash: %s\n", sequence+1, idx+1, txHash)
				}(sequenceIdx, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs\n", sequentialInstances*parallelInstances)

		var wgResults sync.WaitGroup

		transferedAmount := new(big.Int).SetInt64(int64(parallelInstances * sequentialInstances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(startAmountNexusEth, transferedAmount)
		fmt.Printf("Expected ETH Amount after Txs %d\n", ethExpectedBalance)

		for i := 0; i < receivers; i++ {
			wgResults.Add(1)

			go func(receiverIdx int) {
				defer wgResults.Done()

				err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, nexusUsers[receiverIdx],
					func(val *big.Int) bool {
						return val.Cmp(ethExpectedBalance) == 0
					}, 60, 30*time.Second)
				require.NoError(t, err)
				fmt.Printf("%v receiver, %v TXs confirmed\n", receiverIdx, sequentialInstances*parallelInstances)
			}(i)
		}

		wgResults.Wait()
	})

	t.Run("From Prime to Nexus sequential and parallel - one node goes off in the midle", func(t *testing.T) {
		const (
			sequentialInstances  = 5
			parallelInstances    = 6
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
		)

		sendAmountDfm, sendAmountEth := convertToEthValues(1)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.Bridge.GetValidator(t, validatorStoppingIdx).Stop())
			}
		}()

		for sequenceIdx := 0; sequenceIdx < sequentialInstances; sequenceIdx++ {
			primeUsers := make([]*cardanofw.TestApexUser, parallelInstances)

			for i := 0; i < parallelInstances; i++ {
				primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(5_000_000))
				require.NotNil(t, primeUsers[i])
			}

			var wg sync.WaitGroup
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(sequence, idx int) {
					defer wg.Done()

					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
					require.NoError(t, err)

					fmt.Printf("Seq: %v, Tx %v sent. hash: %s\n", sequence+1, idx+1, txHash)
				}(sequenceIdx, i)
			}

			wg.Wait()
		}

		transferedAmount := new(big.Int).SetInt64(int64(parallelInstances * sequentialInstances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, transferedAmount)
		fmt.Printf("Expected ETH Amount after Txs %d\n", ethExpectedBalance)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 60, 30*time.Second)
		require.NoError(t, err)

		fmt.Printf("TXs on Nexus expected amount received, err: %v\n", err)

		// nothing else should be bridged for 2 minutes
		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) > 0
		}, 12, 10*time.Second)
		assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on prime")

		fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
	})

	t.Run("Both directions sequential", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

		// create and fund wallet on nexus
		evmUser, err := apex.CreateAndFundNexusUser(ctx, 50)
		require.NoError(t, err)

		pkBytes, err := evmUser.Ecdsa.MarshallPrivateKey()
		require.NoError(t, err)

		prevAmountNexus, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUser)
		require.NoError(t, err)
		require.NotZero(t, prevAmountNexus)

		// create and fund prime user
		userPrime := apex.CreateAndFundUser(t, ctx, uint64(100_000_000))
		require.NotNil(t, userPrime)

		prevAmountPrime, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), userPrime.PrimeAddress)
		require.NoError(t, err)

		txProviderPrime := apex.GetPrimeTxProvider()

		instances := 5

		sendAmountWei := ethgo.Ether(uint64(1))

		for i := 0; i < instances; i++ {
			txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				evmUser.Address().String(), sendAmountDfm, apex.PrimeCluster.NetworkConfig(), evmUser.Address().String())
			require.NoError(t, err)

			fmt.Printf("Tx sent. hash: %s\n", txHash)

			err = apex.Nexus.SendTxEvm(hex.EncodeToString(pkBytes), userPrime.PrimeAddress, sendAmountWei)
			require.NoError(t, err)
		}

		transferedAmount := new(big.Int).SetInt64(int64(instances))
		transferedAmount.Mul(transferedAmount, sendAmountEth)
		ethExpectedBalance := big.NewInt(0).Add(prevAmountNexus, transferedAmount)
		fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

		// check expected amount nexus
		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, evmUser, func(val *big.Int) bool {
			return val.Cmp(prevAmountNexus) != 0
		}, 30, 30*time.Second)
		require.NoError(t, err)

		ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUser)
		require.NoError(t, err)
		fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)

		// check expected amount prime
		expectedAmountOnPrime := prevAmountPrime + sendAmount // * wei?
		fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", expectedAmountOnPrime)

		err = cardanofw.WaitForAmount(ctx, txProviderPrime, userPrime.PrimeAddress, func(val uint64) bool {
			return val != prevAmountPrime
		}, 100, time.Second*10)
		require.NoError(t, err)
	})

	t.Run("Both directions sequential and parallel", func(t *testing.T) {
		sequentialInstances := 5
		parallelInstances := 6

		sendAmount := uint64(1)
		sendAmountDfm, sendAmountWei := convertToEthValues(sendAmount)

		// create and fund wallet on nexus
		evmUserReceiver, err := apex.CreateAndFundNexusUser(ctx, 100)
		require.NoError(t, err)

		prevAmountNexusReceiver, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUserReceiver)
		require.NoError(t, err)
		require.NotZero(t, prevAmountNexusReceiver)

		nexusUsers := make([]*ethwallet.Account, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			nexusUsers[i], err = apex.CreateAndFundNexusUser(ctx, 100)
			require.NoError(t, err)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, nexusUsers[i], func(val *big.Int) bool {
				return val.Cmp(big.NewInt(0)) != 0
			}, 30, 10*time.Second)
			require.NoError(t, err)
		}

		// create and fund prime user
		primeUserReceiver := apex.CreateAndFundUser(t, ctx, uint64(1_000_000))
		require.NotNil(t, primeUserReceiver)

		prevAmountPrime, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), primeUserReceiver.PrimeAddress)
		require.NoError(t, err)

		primeUsers := make([]*cardanofw.TestApexUser, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(100_000_000))
			require.NotNil(t, primeUsers[i])
		}

		txProviderPrime := apex.GetPrimeTxProvider()

		for j := 0; j < sequentialInstances; j++ {
			var wg sync.WaitGroup

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						evmUserReceiver.Address().String(), sendAmountDfm, apex.PrimeCluster.NetworkConfig(), evmUserReceiver.Address().String())
					require.NoError(t, err)

					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					pkBytesNexus, err := nexusUsers[idx].Ecdsa.MarshallPrivateKey()
					require.NoError(t, err)

					err = apex.Nexus.SendTxEvm(hex.EncodeToString(pkBytesNexus), primeUserReceiver.PrimeAddress, sendAmountWei)
					require.NoError(t, err)

					fmt.Printf("run: %v. Nexus tx %v sent.\n", run+1, idx+1)
				}(j, i)
			}

			wg.Wait()
		}

		// check expected amount nexus
		transferedAmount := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
		transferedAmount.Mul(transferedAmount, sendAmountWei)
		ethExpectedBalance := big.NewInt(0).Add(prevAmountNexusReceiver, transferedAmount)
		fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, evmUserReceiver, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 100, 30*time.Second)
		require.NoError(t, err)

		ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUserReceiver)
		require.NoError(t, err)
		fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)

		// check expected amount prime
		expectedAmountOnPrime := prevAmountPrime + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmountDfm
		fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", expectedAmountOnPrime)

		err = cardanofw.WaitForAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)

		newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress)
		require.NoError(t, err)
		require.NotZero(t, newAmountOnPrime)
		fmt.Printf("Prime amount after Tx %d\n", newAmountOnPrime)
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

		// create and fund wallet on nexus
		evmUserReceiver, err := apex.CreateAndFundNexusUser(ctx, 100)
		require.NoError(t, err)

		prevAmountNexusReceiver, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUserReceiver)
		require.NoError(t, err)
		require.NotZero(t, prevAmountNexusReceiver)

		nexusUsers := make([]*ethwallet.Account, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			nexusUsers[i], err = apex.CreateAndFundNexusUser(ctx, 100)
			require.NoError(t, err)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, nexusUsers[i], func(val *big.Int) bool {
				return val.Cmp(big.NewInt(0)) != 0
			}, 30, 10*time.Second)
			require.NoError(t, err)
		}

		// create and fund prime user
		primeUserReceiver := apex.CreateAndFundUser(t, ctx, uint64(1_000_000))
		require.NotNil(t, primeUserReceiver)

		prevAmountPrime, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), primeUserReceiver.PrimeAddress)
		require.NoError(t, err)

		primeUsers := make([]*cardanofw.TestApexUser, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(100_000_000))
			require.NotNil(t, primeUsers[i])
		}

		txProviderPrime := apex.GetPrimeTxProvider()

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.Bridge.GetValidator(t, validatorStoppingIdx).Stop())
			}
		}()

		var wg sync.WaitGroup

		for i := 0; i < parallelInstances; i++ {
			wg.Add(2)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						evmUserReceiver.Address().String(), sendAmountDfm, apex.PrimeCluster.NetworkConfig(), evmUserReceiver.Address().String())
					require.NoError(t, err)

					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", j+1, idx+1, txHash)
				}
			}(i)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					pkBytesNexus, err := nexusUsers[idx].Ecdsa.MarshallPrivateKey()
					require.NoError(t, err)

					err = apex.Nexus.SendTxEvm(hex.EncodeToString(pkBytesNexus), primeUserReceiver.PrimeAddress, sendAmountWei)
					require.NoError(t, err)

					fmt.Printf("run: %v. Nexus tx %v sent.\n", j+1, idx+1)
				}
			}(i)
		}

		wg.Wait()

		wg.Add(2)

		//nolint:dupl
		go func() {
			defer wg.Done()

			// check expected amount nexus
			transferedAmount := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
			transferedAmount.Mul(transferedAmount, sendAmountWei)
			ethExpectedBalance := big.NewInt(0).Add(prevAmountNexusReceiver, transferedAmount)
			fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, evmUserReceiver, func(val *big.Int) bool {
				return val.Cmp(ethExpectedBalance) == 0
			}, 100, 30*time.Second)
			require.NoError(t, err)

			ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUserReceiver)
			require.NoError(t, err)
			fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, evmUserReceiver, func(val *big.Int) bool {
				return val.Cmp(ethExpectedBalance) > 0
			}, 12, 10*time.Second)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on nexus")

			fmt.Printf("TXs on nexus finished with success: %v\n", err != nil)
		}()

		go func() {
			defer wg.Done()

			// check expected amount prime
			expectedAmountOnPrime := prevAmountPrime + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmountDfm
			fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", expectedAmountOnPrime)

			err = cardanofw.WaitForAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress, func(val uint64) bool {
				return val == expectedAmountOnPrime
			}, 100, time.Second*10)
			require.NoError(t, err)

			newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress)
			require.NoError(t, err)
			require.NotZero(t, newAmountOnPrime)
			fmt.Printf("Prime amount after Tx %d\n", newAmountOnPrime)

			fmt.Printf("TXs on prime expected amount received: %v\n", err)

			// nothing else should be bridged for 2 minutes
			err = cardanofw.WaitForAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress, func(val uint64) bool {
				return val > expectedAmountOnPrime
			}, 12, time.Second*10)
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

		// create and fund wallet on nexus
		evmUserReceiver, err := apex.CreateAndFundNexusUser(ctx, 100)
		require.NoError(t, err)

		prevAmountNexusReceiver, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUserReceiver)
		require.NoError(t, err)
		require.NotZero(t, prevAmountNexusReceiver)

		nexusUsers := make([]*ethwallet.Account, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			nexusUsers[i], err = apex.CreateAndFundNexusUser(ctx, 100)
			require.NoError(t, err)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, nexusUsers[i], func(val *big.Int) bool {
				return val.Cmp(big.NewInt(0)) != 0
			}, 30, 10*time.Second)
			require.NoError(t, err)
		}

		// create and fund prime user
		primeUserReceiver := apex.CreateAndFundUser(t, ctx, uint64(1_000_000))
		require.NotNil(t, primeUserReceiver)

		prevAmountPrime, err := cardanofw.GetTokenAmount(ctx, apex.GetPrimeTxProvider(), primeUserReceiver.PrimeAddress)
		require.NoError(t, err)

		primeUsers := make([]*cardanofw.TestApexUser, parallelInstances)

		for i := 0; i < parallelInstances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(100_000_000))
			require.NotNil(t, primeUsers[i])
		}

		txProviderPrime := apex.GetPrimeTxProvider()

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stopAfter):
				require.NoError(t, apex.Bridge.GetValidator(t, validatorStoppingIdx1).Stop())
				require.NoError(t, apex.Bridge.GetValidator(t, validatorStoppingIdx2).Stop())
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(startAgainAfter):
				require.NoError(t, apex.Bridge.GetValidator(t, validatorStoppingIdx1).Start(ctx, false))
			}
		}()

		var wg sync.WaitGroup

		for i := 0; i < parallelInstances; i++ {
			wg.Add(2)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						evmUserReceiver.Address().String(), sendAmountDfm, apex.PrimeCluster.NetworkConfig(), evmUserReceiver.Address().String())
					require.NoError(t, err)

					fmt.Printf("run: %v. Prime tx %v sent. hash: %s\n", j+1, idx+1, txHash)
				}
			}(i)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					pkBytesNexus, err := nexusUsers[idx].Ecdsa.MarshallPrivateKey()
					require.NoError(t, err)

					err = apex.Nexus.SendTxEvm(hex.EncodeToString(pkBytesNexus), primeUserReceiver.PrimeAddress, sendAmountWei)
					require.NoError(t, err)

					fmt.Printf("run: %v. Nexus tx %v sent.\n", j+1, idx+1)
				}
			}(i)
		}

		wg.Wait()

		wg.Add(2)

		//nolint:dupl
		go func() {
			defer wg.Done()

			// check expected amount nexus
			transferedAmount := new(big.Int).SetInt64(int64(sequentialInstances * parallelInstances))
			transferedAmount.Mul(transferedAmount, sendAmountWei)
			ethExpectedBalance := big.NewInt(0).Add(prevAmountNexusReceiver, transferedAmount)
			fmt.Printf("ETH ethExpectedBalance after Tx %d\n", ethExpectedBalance)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, evmUserReceiver, func(val *big.Int) bool {
				return val.Cmp(ethExpectedBalance) == 0
			}, 100, 30*time.Second)
			require.NoError(t, err)

			ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, evmUserReceiver)
			require.NoError(t, err)
			fmt.Printf("ETH Amount after Tx %d\n", ethBalanceAfter)

			err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, evmUserReceiver, func(val *big.Int) bool {
				return val.Cmp(ethExpectedBalance) > 0
			}, 12, 10*time.Second)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on nexus")

			fmt.Printf("TXs on nexus finished with success: %v\n", err != nil)
		}()

		go func() {
			defer wg.Done()

			// check expected amount prime
			expectedAmountOnPrime := prevAmountPrime + uint64(sequentialInstances)*uint64(parallelInstances)*sendAmountDfm
			fmt.Printf("prime expectedAmountOnPrime after Tx %d\n", expectedAmountOnPrime)

			err = cardanofw.WaitForAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress, func(val uint64) bool {
				return val == expectedAmountOnPrime
			}, 100, time.Second*10)
			require.NoError(t, err)

			newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress)
			require.NoError(t, err)
			require.NotZero(t, newAmountOnPrime)
			fmt.Printf("Prime amount after Tx %d\n", newAmountOnPrime)

			fmt.Printf("TXs on prime expected amount received: %v\n", err)

			// nothing else should be bridged for 2 minutes
			err = cardanofw.WaitForAmount(ctx, txProviderPrime, primeUserReceiver.PrimeAddress, func(val uint64) bool {
				return val > expectedAmountOnPrime
			}, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on vector")

			fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
		}()

		wg.Wait()
	})
}

func TestE2E_ApexBridgeWithNexus_PtN_InvalidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
	)

	userPrime := apex.CreateAndFundUser(t, ctx, uint64(10_000_000))
	require.NotNil(t, userPrime)

	txProviderPrime := apex.GetPrimeTxProvider()
	receiverAddrNexus := "0x999999cf1046e68e36E1aA2E0E07105eDDD1f08E"

	t.Run("Submitter not enough funds", func(t *testing.T) {
		sendAmountDfm, _ := convertToEthValues(100)

		_, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
		require.ErrorContains(t, err, "not enough funds")
	})

	t.Run("Multiple submitters - not enough funds", func(t *testing.T) {
		submitters := 5

		for i := 0; i < submitters; i++ {
			sendAmountDfm, _ := convertToEthValues(100)

			_, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
			require.ErrorContains(t, err, "not enough funds")
		}
	})

	t.Run("Multiple submitters - not enough funds parallel", func(t *testing.T) {
		sendAmountDfm, _ := convertToEthValues(100)

		// Fund wallets
		instances := 5
		primeUsers := make([]*cardanofw.TestApexUser, instances)

		for i := 0; i < instances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(1_000_000))
			require.NotNil(t, primeUsers[i])
		}

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				_, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
					receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
				require.ErrorContains(t, err, "not enough funds")
			}(i)
		}

		wg.Wait()
	})

	t.Run("Submitted invalid metadata", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)

		const feeAmount = 1_100_000

		receivers := map[string]uint64{
			receiverAddrNexus: sendAmount * 10, // 10Ada
		}

		bridgingRequestMetadata, err := cardanofw.CreateMetaData(
			userPrime.PrimeAddress, receivers, cardanofw.ChainIDNexus, feeAmount)
		require.NoError(t, err)

		// Send only half bytes of metadata making it invalid
		bridgingRequestMetadata = bridgingRequestMetadata[0 : len(bridgingRequestMetadata)/2]

		_, err = cardanofw.SendTx(ctx, txProviderPrime, userPrime.PrimeWallet,
			sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.Error(t, err)
	})

	t.Run("Submitted invalid metadata - wrong type", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)

		const feeAmount = 1_100_000

		receivers := map[string]uint64{
			receiverAddrNexus: sendAmount * 10, // 10Ada
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
				"s":  cardanofw.SplitString(userPrime.PrimeAddress, 40),
				"tx": transactions,
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, userPrime.PrimeWallet,
			sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		_, err = cardanofw.WaitForRequestState("InvalidState", ctx, requestURL, apiKey, 60)
		require.Error(t, err)
		require.ErrorContains(t, err, "Timeout")
	})

	t.Run("Submitted invalid metadata - invalid destination", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)

		const feeAmount = 1_100_000

		receivers := map[string]uint64{
			receiverAddrNexus: sendAmount * 10, // 10Ada
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
				"s":  cardanofw.SplitString(userPrime.PrimeAddress, 40),
				"tx": transactions,
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, userPrime.PrimeWallet,
			sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		_, err = cardanofw.WaitForRequestState("InvalidState", ctx, requestURL, apiKey, 60)
		require.Error(t, err)
		require.ErrorContains(t, err, "Timeout")
	})

	t.Run("Submitted invalid metadata - invalid sender", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, _ := convertToEthValues(sendAmount)

		const feeAmount = 1_100_000

		receivers := map[string]uint64{
			receiverAddrNexus: sendAmount * 10, // 10Ada
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

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, userPrime.PrimeWallet,
			sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		_, err = cardanofw.WaitForRequestState("InvalidState", ctx, requestURL, apiKey, 60)
		require.Error(t, err)
		require.ErrorContains(t, err, "Timeout")
	})

	t.Run("Submitted invalid metadata - emty tx", func(t *testing.T) {
		sendAmountDfm, _ := convertToEthValues(0)

		const feeAmount = 1_100_000

		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0)

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "bridge",
				"d":  cardanofw.ChainIDNexus,
				"s":  cardanofw.SplitString(userPrime.PrimeAddress, 40),
				"tx": transactions, // should not be empty
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(ctx, txProviderPrime, userPrime.PrimeWallet,
			sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		requestURL := fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

		_, err = cardanofw.WaitForRequestState("InvalidState", ctx, requestURL, apiKey, 60)
		require.Error(t, err)
		require.ErrorContains(t, err, "Timeout")
	})
}

func TestE2E_ApexBridgeWithNexus_ValidScenarios_BigTest(t *testing.T) {
	if shouldRun := os.Getenv("RUN_E2E_BIG_TESTS"); shouldRun != "true" {
		t.Skip()
	}

	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.RunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithVectorEnabled(false),
		cardanofw.WithNexusEnabled(true),
	)

	userPrime := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))
	require.NotNil(t, userPrime)

	txProviderPrime := apex.GetPrimeTxProvider()

	startAmountNexus := uint64(1)
	expectedAmountNexus := ethgo.Ether(startAmountNexus)

	userNexus, err := apex.CreateAndFundNexusUser(ctx, startAmountNexus)
	require.NoError(t, err)

	err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
		return val.Cmp(expectedAmountNexus) == 0
	}, 10, 10)
	require.NoError(t, err)

	fmt.Println("Nexus user created and funded")

	receiverAddrNexus := userNexus.Address().String()
	fmt.Printf("Nexus receiver Addr: %s\n", receiverAddrNexus)

	//nolint:dupl
	t.Run("From Prime to Nexus 200x 5min 90%", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 200
		primeUsers := make([]*cardanofw.TestApexUser, instances)

		maxWaitTime := 300
		successChance := 90 // 90%
		succeededCount := int64(0)

		fmt.Printf("Funding %v Wallets\n", instances)

		for i := 0; i < instances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(10_000_000))
			require.NotNil(t, primeUsers[i])
		}

		fmt.Printf("Funding complete\n")
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

					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
					require.NoError(t, err)
					fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
				} else {
					const feeAmount = 1_100_000

					receivers := map[string]uint64{
						receiverAddrNexus: sendAmount * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateMetaData(
						primeUsers[idx].PrimeAddress, receivers, cardanofw.ChainIDNexus, feeAmount)
					require.NoError(t, err)

					txHash, err := cardanofw.SendTx(ctx, txProviderPrime, primeUsers[idx].PrimeWallet,
						sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
						apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
					require.NoError(t, err)

					fmt.Printf("Tx %v sent without waiting for confirmation. hash: %s\n", idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		var newAmount *big.Int

		expectedAmount := new(big.Int).SetInt64(succeededCount)
		expectedAmount.Mul(expectedAmount, sendAmountEth)
		expectedAmount.Add(expectedAmount, ethBalanceBefore)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(expectedAmount) >= 0
		}, 20, time.Second*6)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, ethBalanceBefore, newAmount, expectedAmount)
	})

	//nolint:dupl
	t.Run("From Prime to Nexus 1000x 20min 90%", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm, sendAmountEth := convertToEthValues(sendAmount)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		instances := 1000
		primeUsers := make([]*cardanofw.TestApexUser, instances)

		maxWaitTime := 1200
		successChance := 90 // 90%
		succeededCount := int64(0)

		fmt.Printf("Funding %v Wallets\n", instances)

		for i := 0; i < instances; i++ {
			primeUsers[i] = apex.CreateAndFundUser(t, ctx, uint64(10_000_000))
			require.NotNil(t, primeUsers[i])
		}

		fmt.Printf("Funding complete\n")
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

					txHash, err := primeUsers[idx].BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
						receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
					require.NoError(t, err)
					fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
				} else {
					const feeAmount = 1_100_000

					receivers := map[string]uint64{
						receiverAddrNexus: sendAmount * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateMetaData(
						primeUsers[idx].PrimeAddress, receivers, cardanofw.ChainIDNexus, feeAmount)
					require.NoError(t, err)

					txHash, err := cardanofw.SendTx(ctx, txProviderPrime, primeUsers[idx].PrimeWallet,
						sendAmountDfm+feeAmount, apex.Bridge.PrimeMultisigAddr,
						apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
					require.NoError(t, err)

					fmt.Printf("Tx %v sent without waiting for confirmation. hash: %s\n", idx+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		var newAmount *big.Int

		expectedAmount := new(big.Int).SetInt64(succeededCount)
		expectedAmount.Mul(expectedAmount, sendAmountEth)
		expectedAmount.Add(expectedAmount, ethBalanceBefore)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(expectedAmount) >= 0
		}, 20, time.Second*6)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, ethBalanceBefore, newAmount, expectedAmount)
	})
}

func TestE2E_ApexBridgeWithNexus_BatchFailed(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	var (
		userPrime         *cardanofw.TestApexUser
		txProviderPrime   wallet.ITxProvider
		sendAmountDfm     uint64
		sendAmountEth     *big.Int
		userNexus         *ethwallet.Account
		receiverAddrNexus string

		err error
	)

	initApex := func(ctx context.Context, apex *cardanofw.ApexSystem) {
		userPrime = apex.CreateAndFundUser(t, ctx, uint64(500_000_000))
		require.NotNil(t, userPrime)

		txProviderPrime = apex.GetPrimeTxProvider()

		startAmountNexus := uint64(1)
		expectedAmountNexus := ethgo.Ether(startAmountNexus)

		userNexus, err = apex.CreateAndFundNexusUser(ctx, startAmountNexus)
		require.NoError(t, err)

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(expectedAmountNexus) == 0
		}, 10, 10)
		require.NoError(t, err)

		fmt.Println("Nexus user created and funded")

		receiverAddrNexus = userNexus.Address().String()
		fmt.Printf("Nexus receiver Addr: %s\n", receiverAddrNexus)

		sendAmountDfm, sendAmountEth = convertToEthValues(1)
	}

	t.Run("Test small fee", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.RunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithCustomConfigHandlers(nil, func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "chains", "nexus", "config")["depositGasLimit"] = uint64(10)
			}),
		)

		initApex(ctx, apex)

		txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
		require.NoError(t, err)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check relay failed
		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, true)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)

		// Restart relayer after config fix
		err = apex.Bridge.StopRelayer()
		require.NoError(t, err)

		err = cardanofw.UpdateJSONFile(
			apex.Bridge.GetValidator(t, 0).GetRelayerConfig(),
			apex.Bridge.GetValidator(t, 0).GetRelayerConfig(),
			func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "chains", "nexus", "config")["depositGasLimit"] = uint64(0)
			},
			false,
		)
		require.NoError(t, err)

		err = apex.Bridge.StartRelayer(ctx)
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

		require.LessOrEqual(t, failedToExecute, 1)
		require.False(t, timeout)
	})

	//nolint:dupl
	t.Run("Test failed batch", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.RunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", "nexus")["testMode"] = uint8(1)
			}, nil),
		)

		initApex(ctx, apex)

		txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
		require.NoError(t, err)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check batch failed
		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

		require.Equal(t, failedToExecute, 1)
		require.False(t, timeout)
	})

	//nolint:dupl
	t.Run("Test failed batch 5 times in a row", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		var (
			failedToExecute int
			timeout         bool
		)

		apex := cardanofw.RunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", "nexus")["testMode"] = uint8(2)
			}, nil),
		)

		initApex(ctx, apex)

		txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
		require.NoError(t, err)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		// Check batch failed
		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		failedToExecute, timeout = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)

		require.Equal(t, failedToExecute, 5)
		require.False(t, timeout)
	})

	t.Run("Test multiple failed batches in a row", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		instances := 5
		failedToExecute := make([]int, instances)
		timeout := make([]bool, instances)

		apex := cardanofw.RunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", "nexus")["testMode"] = uint8(3)
			}, nil),
		)

		initApex(ctx, apex)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, sendAmountEth.Mul(sendAmountEth, big.NewInt(5)))

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
			require.NoError(t, err)

			fmt.Printf("Tx %v sent. hash: %s\n", i, txHash)

			// Check batch failed
			failedToExecute[i], timeout[i] = waitForBatchSuccess(ctx, txHash, apiURL, apiKey, false)
		}

		for i := 0; i < instances; i++ {
			require.Equal(t, failedToExecute[i], 1)
			require.False(t, timeout[i])
		}

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 5, 10*time.Second)
		require.NoError(t, err)
	})

	t.Run("Test failed batches at 'random'", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		instances := 5
		failedToExecute := make([]int, instances)
		timeout := make([]bool, instances)

		apex := cardanofw.RunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithVectorEnabled(false),
			cardanofw.WithNexusEnabled(true),
			cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
				cardanofw.GetMapFromInterfaceKey(mp, "ethChains", "nexus")["testMode"] = uint8(4)
			}, nil),
		)

		initApex(ctx, apex)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, userNexus)
		fmt.Printf("ETH Amount before Tx %d\n", ethBalanceBefore)
		require.NoError(t, err)

		ethExpectedBalance := big.NewInt(0).Add(ethBalanceBefore, sendAmountEth.Mul(sendAmountEth, big.NewInt(5)))

		apiURL, err := apex.Bridge.GetBridgingAPI()
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash, err := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
				receiverAddrNexus, sendAmountDfm, apex.PrimeCluster.NetworkConfig(), receiverAddrNexus)
			require.NoError(t, err)

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

		err = cardanofw.WaitForEthAmount(ctx, apex.Nexus, userNexus, func(val *big.Int) bool {
			return val.Cmp(ethExpectedBalance) == 0
		}, 5, 10*time.Second)
		require.NoError(t, err)
	})
}

func convertToEthValues(sendAmount uint64) (uint64, *big.Int) {
	sendAmountDfm := new(big.Int).SetUint64(sendAmount)
	exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil)
	sendAmountDfm.Mul(sendAmountDfm, exp)

	return sendAmountDfm.Uint64(), ethgo.Ether(sendAmount)
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
		"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

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
