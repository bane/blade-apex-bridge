package e2e

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/e2ehelper"
	infracommon "github.com/Ethernal-Tech/cardano-infrastructure/common"
	"github.com/stretchr/testify/require"
)

const (
	funderUserIdx = 20
)

var (
	chains = []string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector, cardanofw.ChainIDNexus}
)

func Test_E2E_TestnetDistributeFromPrimeToFunderWallets(t *testing.T) {
	const (
		apexAmountToBridge = 15_000_000
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(apex.Users), funderUserIdx+1)

	funderUser := apex.Users[funderUserIdx]

	balances := getUserBalances(ctx, apex, []*cardanofw.TestApexUser{funderUser})
	printUserBalances([]*cardanofw.TestApexUser{funderUser}, balances)

	sendAmountDfm := cardanofw.ApexToDfm(new(big.Int).SetUint64(apexAmountToBridge))

	fmt.Printf("bridging %v apex to vector\n", apexAmountToBridge)

	e2ehelper.ExecuteSingleBridging(
		t, ctx, apex, funderUser, funderUser, cardanofw.ChainIDPrime, cardanofw.ChainIDVector, sendAmountDfm)

	fmt.Printf("bridging %v apex to nexus\n", apexAmountToBridge)
	e2ehelper.ExecuteSingleBridging(
		t, ctx, apex, funderUser, funderUser, cardanofw.ChainIDPrime, cardanofw.ChainIDNexus, sendAmountDfm)

	balances = getUserBalances(ctx, apex, []*cardanofw.TestApexUser{funderUser})
	printUserBalances([]*cardanofw.TestApexUser{funderUser}, balances)
}

func Test_E2E_TestnetFund(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	const (
		apexToFund = 75
	)

	require.GreaterOrEqual(t, len(apex.Users), funderUserIdx+1)

	var (
		funderUser = apex.Users[funderUserIdx]
		wg         sync.WaitGroup
	)

	balances := getUserBalances(ctx, apex, apex.Users)
	printUserBalances(apex.Users, balances)

	fmt.Printf("funding the wallets\n")

	for _, user := range apex.Users {
		for _, chain := range chains {
			wg.Add(1)

			go func(user *cardanofw.TestApexUser, chain string) {
				defer wg.Done()

				addr := user.GetAddress(chain)

				fmt.Printf("Funding %s address: %s\n", chain, addr)

				_, err := apex.SubmitTx(ctx, chain, funderUser, addr, cardanofw.ApexToDfm(big.NewInt(apexToFund)), nil)
				if err != nil {
					fmt.Printf("error while funding %s address: %s, err: %v\n", chain, addr, err)
				}
			}(user, chain)
		}

		wg.Wait()
	}

	balances = getUserBalances(ctx, apex, apex.Users)
	printUserBalances(apex.Users, balances)

	fmt.Printf("done\n")
}

func Test_E2E_ApexTestnetBridge(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	var (
		user             = apex.Users[funderUserIdx]
		sendAmount       = cardanofw.ApexToDfm(big.NewInt(1))
		bridgingRequests = []struct {
			src  string
			dest string
		}{
			{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector},
			{src: cardanofw.ChainIDVector, dest: cardanofw.ChainIDPrime},
			{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime},
			{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDNexus},
		}
	)

	for _, dir := range bridgingRequests {
		fmt.Printf("bridging from %s to %s\n", dir.src, dir.dest)

		e2ehelper.ExecuteSingleBridging(
			t, ctx, apex, user, user, dir.src, dir.dest, sendAmount)
	}
}

func Test_E2E_TestnetPrintBalances(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	balances := getUserBalances(ctx, apex, apex.Users)
	printUserBalances(apex.Users, balances)
}

func printUserBalances(users []*cardanofw.TestApexUser, balances map[string]*big.Int) {
	for i, user := range users {
		fmt.Printf("=============================\n")
		fmt.Printf("user: %d\n", i)

		for _, chain := range chains {
			var (
				addr       = user.GetAddress(chain)
				balanceStr = "No data"
			)

			if balance, exists := balances[addr]; exists {
				balanceStr = balance.String()
			}

			fmt.Printf("%s addr: %s, balance: %s\n", chain, addr, balanceStr)
		}

		fmt.Printf("=============================\n")
	}
}

func getUserBalances(
	ctx context.Context, apex *cardanofw.ApexSystem,
	users []*cardanofw.TestApexUser,
) map[string]*big.Int {
	var (
		balances = make(map[string]*big.Int, len(users)*len(chains))
		wg       sync.WaitGroup
		mu       sync.Mutex
	)

	fmt.Printf("getting the balances\n")

	for _, user := range users {
		for _, chain := range chains {
			wg.Add(1)

			go func(u *cardanofw.TestApexUser, c string) {
				defer wg.Done()

				addr := user.GetAddress(chain)

				balance, err := infracommon.ExecuteWithRetry(
					ctx, func(ctx context.Context) (*big.Int, error) {
						return apex.GetBalance(ctx, u, c)
					},
				)
				if err != nil {
					fmt.Printf("error while getting balance of %s address: %s, err: %v\n", chain, addr, err)

					return
				}

				mu.Lock()
				defer mu.Unlock()

				balances[addr] = balance
			}(user, chain)
		}
	}

	wg.Wait()

	return balances
}
