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
	"github.com/Ethernal-Tech/ethgo"
	"github.com/stretchr/testify/require"
)

const (
	funderUserPrimeAddr = "addr_test1qpcjca78u9rtjkjknuhhahcamqwly4z7mm93xfcp79lcf4rffsvqf8w2lst46f3vqm4vnaftsmeqtcuw3072de49g4ssz3477z"
)

var (
	chains = []string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector, cardanofw.ChainIDNexus}
)

func Test_E2E_TestnetDistributeFromPrimeToFunderWallets(t *testing.T) {
	const (
		apexAmountToBridge = 10_000
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	funderUser, err := getFunderUser(apex, funderUserPrimeAddr)
	require.NoError(t, err)

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
		apexToFund = 100
	)

	funderUser, err := getFunderUser(apex, funderUserPrimeAddr)
	require.NoError(t, err)

	var wg sync.WaitGroup

	balances := getUserBalances(ctx, apex, apex.Users)
	printUserBalances(apex.Users, balances)

	fmt.Printf("funding the wallets\n")

	for _, user := range apex.Users {
		if user == funderUser {
			continue
		}

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

func Test_E2E_SanityCheck(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	var (
		user             = apex.Users[0]
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

func TestE2E_ApexTestnetBridge_ValidScenarios(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	t.Run("From Prime to Cector sequential and parallel with max receivers", func(t *testing.T) {
		const (
			sequentialInstances = 5
			parallelInstances   = 10
		)

		PrimeToVectorSequentialAndParallelWithMaxReceivers(t, ctx, apex, sequentialInstances, parallelInstances)
	})

	t.Run("Prime and Vector both directions sequential and parallel", func(t *testing.T) {
		const (
			sequentialInstances = 3
			parallelInstances   = 6
		)

		receiverUser := apex.Users[parallelInstances]

		PrimeVectorBothDirectionsSequentialAndParallel(t, ctx, apex, receiverUser, sequentialInstances, parallelInstances)
	})

	t.Run("From Prime to Nexus sequential and parallel with max receivers", func(t *testing.T) {
		const (
			sequentialInstances = 3
			parallelInstances   = 10
		)

		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

		PrimeToNexusSequentialAndParallelWithMaxReceivers(t, ctx, apex, sequentialInstances, parallelInstances, sendAmountDfm)
	})

	t.Run("Prime and Nexus both directions sequential and parallel", func(t *testing.T) {
		const (
			sequentialInstances = 3
			parallelInstances   = 6
		)

		receiverUser := apex.Users[parallelInstances]
		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

		PrimeNexusBothDirectionsSequentialAndParallel(
			t, ctx, apex, receiverUser, sequentialInstances, parallelInstances, sendAmountDfm)
	})

	t.Run("From Nexus to Prime sequential and parallel max receivers", func(t *testing.T) {
		const (
			sequentialInstances = 3
			parallelInstances   = 10
		)

		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(1))

		NexusToPrimeSequentialAndParallelWithMaxReceivers(t, ctx, apex, sequentialInstances, parallelInstances, sendAmountDfm)
	})
}

func TestE2E_ApexTestnetBridge_InvalidScenarios(t *testing.T) {
	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex, err := cardanofw.SetupRemoteApexBridge(t, cardanofw.GetTestnetApexBridgeConfig())
	require.NoError(t, err)

	t.Run("Prime to Vector mismatch submitted and receiver amounts", func(t *testing.T) {
		PrimeToVectorMismatchSubmittedAndReceiverAmounts(t, ctx, apex, apex.Users[0])
	})

	t.Run("Prime to Vector submitted invalid metadata - sliced off", func(t *testing.T) {
		PrimeToVectorInvalidMetadataSlicedOff(t, ctx, apex, apex.Users[1])
	})

	t.Run("Prime to Vector submitted invalid metadata - wrong type", func(t *testing.T) {
		PrimeToVectorInvalidMetadataWrongType(t, ctx, apex, apex.Users[2])
	})

	t.Run("Prime to Vector submitted invalid metadata - invalid destination", func(t *testing.T) {
		PrimeToVectorInvalidMetadataInvalidDestination(t, ctx, apex, apex.Users[3])
	})

	t.Run("Prime to Vector submitted invalid metadata - invalid sender", func(t *testing.T) {
		PrimeToVectorInvalidMetadataInvalidSender(t, ctx, apex, apex.Users[4])
	})

	t.Run("Prime to Vector submitted invalid metadata - empty tx", func(t *testing.T) {
		PrimeToVectorInvalidMetadataInvalidTransactions(t, ctx, apex, apex.Users[5])
	})

	t.Run("Prime to Nexus submitter not enough funds", func(t *testing.T) {
		sendAmountDfm := cardanofw.WeiToDfm(ethgo.Ether(500_000))

		PrimeToNexusSubmitterNotEnoughFunds(t, ctx, apex, apex.Users[6], sendAmountDfm)
	})

	t.Run("Prime to Nexus submitted invalid metadata - sliced off", func(t *testing.T) {
		PrimeToNexusInvalidMetadataSlicedOff(t, ctx, apex, apex.Users[7])
	})

	t.Run("Prime to Nexus submitted invalid metadata - wrong type", func(t *testing.T) {
		PrimeToNexusInvalidMetadataWrongType(t, ctx, apex, apex.Users[8])
	})

	t.Run("Prime to Nexus submitted invalid metadata - invalid destination", func(t *testing.T) {
		PrimeToNexusInvalidMetadataInvalidDestination(t, ctx, apex, apex.Users[9])
	})

	t.Run("Prime to Nexus submitted invalid metadata - invalid sender", func(t *testing.T) {
		PrimeToNexusInvalidMetadataInvalidSender(t, ctx, apex, apex.Users[0])
	})

	t.Run("Prime to Nexus submitted invalid metadata - empty tx", func(t *testing.T) {
		PrimeToNexusInvalidMetadataInvalidTransactions(t, ctx, apex, apex.Users[1])
	})

	t.Run("Nexus to Prime submitter not enough funds", func(t *testing.T) {
		NexusToPrimeSubmitterNotEnoughFunds(t, ctx, apex)
	})
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

func getFunderUser(apex *cardanofw.ApexSystem, primeAddr string) (*cardanofw.TestApexUser, error) {
	for _, user := range apex.Users {
		if user.GetAddress(cardanofw.ChainIDPrime) == primeAddr {
			return user, nil
		}
	}

	return nil, fmt.Errorf("user with prime addr: %s not found", primeAddr)
}
