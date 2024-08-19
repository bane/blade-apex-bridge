package e2e

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
)

func TestE2E_ApexBridge_Nexus(t *testing.T) {
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
		require.NotNil(t, user)

		ethBalance, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		require.NoError(t, err)
		require.NotZero(t, ethBalance)

		err = cardanofw.WaitForEthAmount(context.Background(), apex.Nexus, user, func(val *big.Int) bool {
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

		sendAmountEth := uint64(1)
		sendAmountDfm := new(big.Int).SetUint64(sendAmountEth)
		expDfm := new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil)
		sendAmountDfm.Mul(sendAmountDfm, expDfm)

		sendAmountWei := ethgo.Ether(sendAmountEth)

		// call SendTx command
		err = apex.Nexus.SendTxEvm(string(pkBytes), user.PrimeAddress, sendAmountWei)
		require.NoError(t, err)

		// check expected amount cardano
		expectedAmountOnPrime := prevAmount + sendAmountDfm.Uint64()
		err = cardanofw.WaitForAmount(context.Background(), txProviderPrime, user.PrimeAddress, func(val uint64) bool {
			return val == expectedAmountOnPrime
		}, 100, time.Second*10)
		require.NoError(t, err)

		newAmountOnPrime, err := cardanofw.GetTokenAmount(ctx, txProviderPrime, user.PrimeAddress)
		require.NoError(t, err)
		require.NotZero(t, newAmountOnPrime)
	})

	t.Run("From Prime to Nexus", func(t *testing.T) {
		sendAmount := uint64(1)
		sendAmountDfm := new(big.Int).SetUint64(sendAmount)
		exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil)
		sendAmountDfm.Mul(sendAmountDfm, exp)

		expectedAmount := ethgo.Ether(sendAmount)

		userPrime := apex.CreateAndFundUser(t, ctx, uint64(500_000_000))
		require.NotNil(t, userPrime)

		user, err := apex.CreateAndFundNexusUser(ctx, sendAmount)
		require.NoError(t, err)
		require.NotNil(t, user)

		ethBalance, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount %d\n", ethBalance)
		require.NoError(t, err)
		require.NotZero(t, ethBalance)

		err = cardanofw.WaitForEthAmount(context.Background(), apex.Nexus, user, func(val *big.Int) bool {
			return val.Cmp(expectedAmount) == 0
		}, 10, 10)
		require.NoError(t, err)

		///////////////////////////////////////
		//////////// SEND TX //////////////////
		///////////////////////////////////////

		txProviderPrime := apex.GetPrimeTxProvider()

		nexusAddress := user.Address()

		receiverAddr := user.Address().String()
		fmt.Printf("ETH receiver Addr: %s\n", receiverAddr)

		ethBalanceBefore, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount BEFORE TX %d\n", ethBalanceBefore)
		require.NoError(t, err)

		txHash := userPrime.BridgeNexusAmount(t, ctx, txProviderPrime, apex.Bridge.PrimeMultisigAddr,
			nexusAddress.String(), sendAmountDfm.Uint64(), apex.PrimeCluster.NetworkConfig(), receiverAddr)

		ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount AFTER  TX %d\n", ethBalanceAfter)
		require.NoError(t, err)

		fmt.Printf("Tx sent. hash: %s\n", txHash)

		err = cardanofw.WaitForEthAmount(context.Background(), apex.Nexus, user, func(val *big.Int) bool {
			ethBalanceAfter, err := cardanofw.GetEthAmount(ctx, apex.Nexus, user)
			require.NoError(t, err)

			return ethBalanceBefore.Cmp(ethBalanceAfter) != 0
		}, 30, time.Second*30)
		require.NoError(t, err)

		ethBalanceAfter, err = cardanofw.GetEthAmount(ctx, apex.Nexus, user)
		fmt.Printf("ETH Amount AFTER AFTER TX %d\n", ethBalanceAfter)
		require.NoError(t, err)
	})
}
