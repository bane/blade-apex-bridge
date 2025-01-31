package e2e

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/e2ehelper"
	infrawallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

// Download Cardano executables from https://github.com/IntersectMBO/cardano-node/releases/tag/8.7.3 and unpack tar.gz file
// Add directory where unpacked files are located to the $PATH (in example bellow `~/Apps/cardano`)
// eq add line `export PATH=$PATH:~/Apps/cardano` to  `~/.bashrc`
// cd e2e-polybft/e2e
// ONLY_RUN_APEX_BRIDGE=true go test -v -timeout 0 -run ^Test_OnlyRunApexBridge_WithNexusAndVector$ github.com/0xPolygon/polygon-edge/e2e-polybft/e2e
func Test_OnlyRunApexBridge_WithNexusAndVector(t *testing.T) {
	if !cardanofw.IsEnvVarTrue("ONLY_RUN_APEX_BRIDGE") {
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
		cardanofw.WithVectorEnabled(true),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithUserCnt(1),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	oracleAPI, err := apex.GetBridgingAPI()
	require.NoError(t, err)

	fmt.Printf("oracle API: %s\n", oracleAPI)
	fmt.Printf("oracle API key: %s\n", apiKey)

	fmt.Printf("prime network url: %s\n", apex.PrimeInfo.NetworkAddress)
	fmt.Printf("prime ogmios url: %s\n", apex.PrimeInfo.OgmiosURL)
	fmt.Printf("prime bridging addr: %s\n", apex.PrimeInfo.MultisigAddr)
	fmt.Printf("prime fee addr: %s\n", apex.PrimeInfo.FeeAddr)
	fmt.Printf("prime socket path: %s\n", apex.PrimeInfo.SocketPath)

	fmt.Printf("vector network url: %s\n", apex.VectorInfo.NetworkAddress)
	fmt.Printf("vector ogmios url: %s\n", apex.VectorInfo.OgmiosURL)
	fmt.Printf("vector bridging addr: %s\n", apex.VectorInfo.MultisigAddr)
	fmt.Printf("vector fee addr: %s\n", apex.VectorInfo.FeeAddr)
	fmt.Printf("vector socket path: %s\n", apex.VectorInfo.SocketPath)

	user := apex.Users[0]
	userPrimeSK, err := user.GetPrivateKey(cardanofw.ChainIDPrime)
	require.NoError(t, err)
	userVectorSK, err := user.GetPrivateKey(cardanofw.ChainIDVector)
	require.NoError(t, err)
	userNexusPK, err := user.GetPrivateKey(cardanofw.ChainIDNexus)
	require.NoError(t, err)

	nexusAdminKeyRaw, err := apex.NexusInfo.AdminKey.MarshallPrivateKey()
	require.NoError(t, err)

	fmt.Printf("user prime addr: %s\n", user.GetAddress(cardanofw.ChainIDPrime))
	fmt.Printf("user prime signing key hex: %s\n", userPrimeSK)
	fmt.Printf("user vector addr: %s\n", user.GetAddress(cardanofw.ChainIDVector))
	fmt.Printf("user vector signing key hex: %s\n", userVectorSK)

	jsonRPCClient, err := cardanofw.JSONRPCClient(apex.NexusInfo.JSONRPCAddr)
	require.NoError(t, err)

	nexusChainID, err := jsonRPCClient.ChainID()
	require.NoError(t, err)

	fmt.Printf("nexus user addr: %s\n", user.GetAddress(cardanofw.ChainIDNexus))
	fmt.Printf("nexus user signing key: %s\n", userNexusPK)
	fmt.Printf("nexus url: %s\n", apex.NexusInfo.JSONRPCAddr)
	fmt.Printf("nexus gateway sc addr: %s\n", apex.NexusInfo.GatewayAddress)
	fmt.Printf("nexus chainID: %v\n", nexusChainID)
	fmt.Printf("nexus admin key: %v\n", hex.EncodeToString(nexusAdminKeyRaw))

	proxyAdminPrivateKeyRaw, err := apex.GetBridgeProxyAdmin().MarshallPrivateKey()
	require.NoError(t, err)

	privateKeyRaw, err := apex.GetBridgeAdmin().MarshallPrivateKey()
	require.NoError(t, err)

	fmt.Printf("bridge url: %s\n", apex.GetBridgeDefaultJSONRPCAddr())
	fmt.Printf("bridge admin key: %s\n", hex.EncodeToString(privateKeyRaw))
	fmt.Printf("bridge admin address: %s\n", apex.GetBridgeAdmin().Address())
	fmt.Printf("bridge proxy admin key: %s\n", hex.EncodeToString(proxyAdminPrivateKeyRaw))
	fmt.Printf("bridge proxy admin address: %s\n", apex.GetBridgeProxyAdmin().Address())

	signalChannel := make(chan os.Signal, 1)
	// Notify the signalChannel when the interrupt signal is received (Ctrl+C)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	<-signalChannel
}

func TestE2E_ApexBridge_CardanoOracleState(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const apiKey = "my_api_key"

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithAPIValidatorID(-1),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	apiURLs, err := apex.GetBridgingAPIs()
	require.NoError(t, err)

	require.Equal(t, apex.GetValidatorsCount(), len(apiURLs))

	ticker := time.NewTicker(time.Second * 4)
	defer ticker.Stop()

	goodOraclesCount := 0

	for goodOraclesCount < apex.GetValidatorsCount() {
		select {
		case <-ctx.Done():
			t.Fatal("timeout")
		case <-ticker.C:
		}

		goodOraclesCount = 0

	outerLoop:
		for _, apiURL := range apiURLs {
			for _, chainID := range []string{cardanofw.ChainIDVector, cardanofw.ChainIDPrime} {
				requestURL := fmt.Sprintf("%s/api/OracleState/Get?chainId=%s", apiURL, chainID)

				currentState, err := cardanofw.GetOracleState(ctx, requestURL, apiKey)
				if err != nil || currentState == nil {
					break outerLoop
				}

				multisigAddr, feeAddr := "", ""
				sumMultisig, sumFee, desiredAmount := uint64(0), uint64(0), uint64(0)

				switch chainID {
				case cardanofw.ChainIDPrime:
					multisigAddr, feeAddr = apex.PrimeInfo.MultisigAddr, apex.PrimeInfo.FeeAddr
					desiredAmount = apex.Config.PrimeConfig.FundAmount
				case cardanofw.ChainIDVector:
					multisigAddr, feeAddr = apex.VectorInfo.MultisigAddr, apex.VectorInfo.FeeAddr
					desiredAmount = apex.Config.VectorConfig.FundAmount
				}

				for _, utxo := range currentState.Utxos {
					switch utxo.Address {
					case multisigAddr:
						sumMultisig += utxo.Amount
					case feeAddr:
						sumFee += utxo.Amount
					}
				}

				if sumMultisig != 0 || sumFee != 0 {
					fmt.Printf("%s sums: %d, %d\n", requestURL, sumMultisig, sumFee)
				}

				if sumMultisig != desiredAmount || sumFee != desiredAmount || currentState.BlockSlot == 0 {
					break outerLoop
				} else {
					goodOraclesCount++
				}
			}
		}
	}
}

func TestE2E_ApexBridge(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig, vectorConfig := cardanofw.NewPrimeChainConfig(), cardanofw.NewVectorChainConfig(true)
	primeConfig.PremineAmount = 500_000_000
	vectorConfig.PremineAmount = 500_000_000

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithUserCnt(1),
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithVectorConfig(vectorConfig),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	sendAmountDfm := cardanofw.ApexToDfm(big.NewInt(1))

	e2ehelper.ExecuteSingleBridging(
		t, ctx, apex, apex.Users[0], apex.Users[0], cardanofw.ChainIDPrime, cardanofw.ChainIDVector, sendAmountDfm)
}

func TestE2E_ApexBridge_BatchRecreated(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig, vectorConfig := cardanofw.NewPrimeChainConfig(), cardanofw.NewVectorChainConfig(true)
	primeConfig.FundAmount = 500_000_000
	vectorConfig.FundAmount = 500_000_000
	primeConfig.TTLInc, primeConfig.SlotRoundingThreshold = 1, 20
	vectorConfig.TTLInc, vectorConfig.SlotRoundingThreshold = 1, 30

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithVectorConfig(vectorConfig),
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(1),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[0]

	sendAmount := uint64(1_000_000)

	// Initiate bridging PRIME -> VECTOR
	txHash := apex.SubmitBridgingRequest(t, ctx,
		cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
		user, new(big.Int).SetUint64(sendAmount), user,
	)

	_, timeout := cardanofw.WaitForBatchState(
		ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey, false, true,
		cardanofw.BatchStateIncludedInBatch, cardanofw.BatchStateSubmittedToDestination)

	require.False(t, timeout)
}

func TestE2E_ApexBridge_Over_Max_Allowed_To_Bridge(t *testing.T) {
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
		cardanofw.WithUserCnt(1),
		cardanofw.WithNexusEnabled(true),
		cardanofw.WithCustomConfigHandlers(func(mp map[string]interface{}) {
			setting := cardanofw.GetMapFromInterfaceKey(mp, "bridgingSettings")
			setting["maxAmountAllowedToBridge"] = new(big.Int).SetUint64(5_000_000)
		}, nil),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	var (
		user             = apex.Users[0]
		apexSendAmount   = cardanofw.ApexToDfm(big.NewInt(10))
		bridgingRequests = []struct {
			src    string
			dest   string
			sender *cardanofw.TestApexUser
		}{
			{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector, sender: apex.Users[0]},
			{src: cardanofw.ChainIDVector, dest: cardanofw.ChainIDPrime, sender: apex.Users[0]},
			{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime, sender: apex.Users[0]},
		}
		txHashes = make([]string, len(bridgingRequests))
	)

	var wg sync.WaitGroup

	for idx, br := range bridgingRequests {
		wg.Add(1)

		go func(i int, src string, dest string, sender *cardanofw.TestApexUser) {
			defer wg.Done()

			txHashes[i] = apex.SubmitBridgingRequest(t, ctx, src, dest, sender, apexSendAmount, user)
			fmt.Printf("Bridging request: %v to %v sent. hash: %s\n", src, dest, txHashes[i])
		}(idx, br.src, br.dest, br.sender)
	}

	wg.Wait()

	for idx, br := range bridgingRequests {
		wg.Add(1)

		go func() {
			defer wg.Done()

			cardanofw.WaitForInvalidState(t, ctx, apex, br.src, txHashes[idx], apiKey)
		}()
	}

	wg.Wait()
}

func TestE2E_FundAmount(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const (
		apiKey           = "test_api_key"
		userCnt          = 10
		fundAmountPrime  = 100_000_000
		fundAmountVector = 100_000_000
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig, vectorConfig := cardanofw.NewPrimeChainConfig(), cardanofw.NewVectorChainConfig(true)
	primeConfig.FundAmount = 1_000_000
	vectorConfig.FundAmount = 1_000_000

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithVectorConfig(vectorConfig),
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	testCases := []struct {
		name       string
		sendAmount *big.Int
		fromChain  cardanofw.ChainID
		toChain    cardanofw.ChainID
		fundAmount int64
	}{
		{
			name:       "From prime to vector - not enough funds",
			sendAmount: new(big.Int).SetUint64(5_000_000),
			fromChain:  cardanofw.ChainIDPrime,
			toChain:    cardanofw.ChainIDVector,
			fundAmount: fundAmountVector,
		},
		{
			name:       "From vector to prime - not enough funds",
			sendAmount: new(big.Int).SetUint64(15_000_000),
			fromChain:  cardanofw.ChainIDVector,
			toChain:    cardanofw.ChainIDPrime,
			fundAmount: fundAmountPrime,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prevAmount, err := apex.GetBalance(ctx, user, tc.toChain)
			require.NoError(t, err)

			fmt.Printf("prevAmount %v\n", prevAmount)

			expectedAmount := new(big.Int).Set(tc.sendAmount)
			expectedAmount.Add(expectedAmount, prevAmount)

			txHash := apex.SubmitBridgingRequest(t, ctx,
				tc.fromChain, tc.toChain,
				user, tc.sendAmount, user,
			)

			fmt.Printf("Tx sent. hash: %s. %v - expectedAmount\n", txHash, expectedAmount)

			err = apex.WaitForExactAmount(ctx, user, tc.toChain, expectedAmount, 20, time.Second*10)
			require.Error(t, err)

			require.NoError(t, apex.FundChainHotWallet(ctx, tc.toChain, big.NewInt(tc.fundAmount)))

			txHash = apex.SubmitBridgingRequest(t, ctx,
				tc.fromChain, tc.toChain,
				user, tc.sendAmount, user,
			)

			fmt.Printf("Tx sent. hash: %s. %v - expectedAmount\n", txHash, expectedAmount)

			err = apex.WaitForExactAmount(ctx, user, tc.toChain, expectedAmount, 20, time.Second*10)
			require.NoError(t, err)
		})
	}
}

func TestE2E_ApexBridge_InvalidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 15
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig, vectorConfig := cardanofw.NewPrimeChainConfig(), cardanofw.NewVectorChainConfig(true)
	primeConfig.PremineAmount = 500_000_000
	vectorConfig.PremineAmount = 500_000_000

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(userCnt),
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithVectorConfig(vectorConfig),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[0]

	txProviderPrime := apex.PrimeInfo.GetTxProvider()

	t.Run("Mismatch submitted and receiver amounts", func(t *testing.T) {
		PrimeToVectorMismatchSubmittedAndReceiverAmounts(t, ctx, apex, user)
	})

	t.Run("Multiple submitters mismatch submitted and receiver amounts", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			sendAmount := uint64(1_000_000)
			feeAmount := uint64(1_100_000)

			receivers := map[string]uint64{
				apex.Users[i].GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
			}

			bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
				apex.Users[i].GetAddress(cardanofw.ChainIDPrime), receivers,
				cardanofw.ChainIDVector, feeAmount)
			require.NoError(t, err)

			txHash, err := cardanofw.SendTx(
				ctx, txProviderPrime, apex.Users[i].PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
				apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
			require.NoError(t, err)

			cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey)
		}
	})

	t.Run("Multiple submitters mismatch submitted and receiver amounts parallel", func(t *testing.T) {
		instances := 5
		txHashes := make([]string, instances)

		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		var wg sync.WaitGroup

		for i := 0; i < instances; i++ {
			idx := i
			receivers := map[string]uint64{
				apex.Users[idx].GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
			}

			wg.Add(1)

			go func() {
				defer wg.Done()

				testUser := apex.Users[idx]

				bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
					testUser.GetAddress(cardanofw.ChainIDPrime), receivers,
					cardanofw.ChainIDVector, feeAmount)
				require.NoError(t, err)

				txHashes[idx], err = cardanofw.SendTx(
					ctx, txProviderPrime, testUser.PrimeWallet,
					sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
					apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
				require.NoError(t, err)
			}()
		}

		wg.Wait()

		for i := 0; i < instances; i++ {
			cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHashes[i], apiKey)
		}
	})

	t.Run("Submitted invalid metadata - sliced off", func(t *testing.T) {
		PrimeToVectorInvalidMetadataSlicedOff(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - wrong type", func(t *testing.T) {
		PrimeToVectorInvalidMetadataWrongType(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - invalid destination", func(t *testing.T) {
		PrimeToVectorInvalidMetadataInvalidDestination(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - invalid sender", func(t *testing.T) {
		PrimeToVectorInvalidMetadataInvalidSender(t, ctx, apex, user)
	})

	t.Run("Submitted invalid metadata - empty tx", func(t *testing.T) {
		PrimeToVectorInvalidMetadataInvalidTransactions(t, ctx, apex, user)
	})

	t.Run("Submitted with tokens to bridging addr", func(t *testing.T) {
		sendAmount := uint64(5_000_000)
		feeAmount := uint64(1_100_000)

		minterUser := apex.Users[userCnt-1]

		brSubmitterUser, err := cardanofw.NewTestApexUser(
			apex.Config.PrimeConfig.NetworkType, false, 0, false)
		require.NoError(t, err)

		tokensFunded, err := cardanofw.FundUserWithToken(
			ctx, cardanofw.ChainIDPrime, apex.Config.PrimeConfig.NetworkType, txProviderPrime,
			minterUser, brSubmitterUser, uint64(10_000_000), uint64(1_000_000))
		require.NoError(t, err)

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t": "bridge",
				"d": cardanofw.ChainIDVector,
				"s": cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
				"tx": []cardanofw.BridgingRequestMetadataTransaction{{
					Address: cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDVector), 40),
					Amount:  sendAmount - feeAmount,
				}},
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		brSubmitterWallet, _ := brSubmitterUser.GetCardanoWallet(cardanofw.ChainIDPrime)

		txHash, err := cardanofw.SendTxWithTokens(ctx, apex.Config.PrimeConfig.NetworkType, txProviderPrime,
			brSubmitterWallet, apex.PrimeInfo.MultisigAddr,
			sendAmount, []infrawallet.TokenAmount{*tokensFunded}, bridgingRequestMetadata,
		)
		require.NoError(t, err)

		cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey)
	})
}

func TestE2E_ApexBridge_ValidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 20
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(userCnt),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("vector user addr: ", user.VectorAddress)
	fmt.Println("prime multisig addr: ", apex.PrimeInfo.MultisigAddr)
	fmt.Println("prime fee addr: ", apex.PrimeInfo.FeeAddr)
	fmt.Printf("prime socket path: %s\n", apex.PrimeInfo.SocketPath)
	fmt.Println("vector multisig addr: ", apex.VectorInfo.MultisigAddr)
	fmt.Println("vector fee addr: ", apex.VectorInfo.FeeAddr)
	fmt.Printf("vector socket path: %s\n", apex.VectorInfo.SocketPath)

	t.Run("Submitter has tokens", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		sendAmountDfm := big.NewInt(5_000_000)
		txProviderPrime := apex.PrimeInfo.GetTxProvider()
		minterUser := apex.Users[userCnt-2]

		brSubmitterUser, err := cardanofw.NewTestApexUser(
			apex.Config.PrimeConfig.NetworkType, false, 0, false)
		require.NoError(t, err)

		_, err = cardanofw.FundUserWithToken(
			ctx, cardanofw.ChainIDPrime, apex.Config.PrimeConfig.NetworkType, txProviderPrime,
			minterUser, brSubmitterUser, uint64(10_000_000), uint64(1_000_000))
		require.NoError(t, err)

		e2ehelper.ExecuteSingleBridging(
			t, ctx, apex, brSubmitterUser, user, cardanofw.ChainIDPrime, cardanofw.ChainIDVector, sendAmountDfm)
	})

	t.Run("Submitted with tokens to bridging addr - confirming that batcher functions", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		sendAmount := uint64(5_000_000)
		feeAmount := uint64(1_100_000)
		txProviderPrime := apex.PrimeInfo.GetTxProvider()

		minterUser := apex.Users[userCnt-3]

		brSubmitterUser, err := cardanofw.NewTestApexUser(
			apex.Config.PrimeConfig.NetworkType, false, 0, false)
		require.NoError(t, err)

		tokensFunded, err := cardanofw.FundUserWithToken(
			ctx, cardanofw.ChainIDPrime, apex.Config.PrimeConfig.NetworkType, txProviderPrime,
			minterUser, brSubmitterUser, uint64(10_000_000), uint64(1_000_000))
		require.NoError(t, err)

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t": "bridge",
				"d": cardanofw.ChainIDVector,
				"s": cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
				"tx": []cardanofw.BridgingRequestMetadataTransaction{{
					Address: cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDVector), 40),
					Amount:  sendAmount - feeAmount,
				}},
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		brSubmitterWallet, _ := brSubmitterUser.GetCardanoWallet(cardanofw.ChainIDPrime)

		txHash, err := cardanofw.SendTxWithTokens(ctx, apex.Config.PrimeConfig.NetworkType, txProviderPrime,
			brSubmitterWallet, apex.PrimeInfo.MultisigAddr,
			sendAmount, []infrawallet.TokenAmount{*tokensFunded}, bridgingRequestMetadata,
		)
		require.NoError(t, err)

		cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apiKey)

		const (
			sendAmountVec = uint64(1_000_000)
			instances     = 10
		)

		e2ehelper.ExecuteBridgingWaitAfterSubmits(
			t, ctx, apex, instances, minterUser,
			cardanofw.ChainIDVector, cardanofw.ChainIDPrime, new(big.Int).SetUint64(sendAmountVec))
	})

	t.Run("From prime to vector wait for each submit", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		e2ehelper.ExecuteBridgingOneByOneWaitOnOtherSide(
			t, ctx, apex, instances, user, cardanofw.ChainIDPrime, cardanofw.ChainIDVector, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("From prime to vector one by one", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			instances  = 5
			sendAmount = uint64(1_000_005)
		)

		e2ehelper.ExecuteBridgingWaitAfterSubmits(
			t, ctx, apex, instances, user, cardanofw.ChainIDPrime, cardanofw.ChainIDVector, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("From prime to vector parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, 1, apex.Users[:instances], []*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDVector},
			}, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("From vector to prime one by one", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		e2ehelper.ExecuteBridgingWaitAfterSubmits(
			t, ctx, apex, instances, user, cardanofw.ChainIDVector, cardanofw.ChainIDPrime, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("From vector to prime parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			instances  = 5
			sendAmount = uint64(1_000_000)
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, 1, apex.Users[:instances], []*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDVector},
			map[string][]string{
				cardanofw.ChainIDVector: {cardanofw.ChainIDPrime},
			}, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("From prime to vector sequential and parallel", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
			sendAmount          = uint64(1_000_000)
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, sequentialInstances, apex.Users[:parallelInstances], []*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime},
			map[string][]string{
				cardanofw.ChainIDPrime: {cardanofw.ChainIDVector},
			}, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("From prime to vector sequential and parallel with max receivers", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
		)

		PrimeToVectorSequentialAndParallelWithMaxReceivers(t, ctx, apex, sequentialInstances, parallelInstances)
	})

	t.Run("Both directions sequential", func(t *testing.T) {
		if cardanofw.ShouldSkipE2RRedundantTests() {
			t.Skip()
		}

		const (
			instances  = 5
			sendAmount = uint64(1_000_000)
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, instances,
			[]*cardanofw.TestApexUser{apex.Users[0]},
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector},
			map[string][]string{
				cardanofw.ChainIDPrime:  {cardanofw.ChainIDVector},
				cardanofw.ChainIDVector: {cardanofw.ChainIDPrime},
			}, new(big.Int).SetUint64(sendAmount))
	})

	t.Run("Both directions sequential and parallel - one node goes off in the middle", func(t *testing.T) {
		const (
			sequentialInstances  = 5
			parallelInstances    = 6
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
			sendAmount           = uint64(1_000_000)
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, sequentialInstances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector},
			map[string][]string{
				cardanofw.ChainIDPrime:  {cardanofw.ChainIDVector},
				cardanofw.ChainIDVector: {cardanofw.ChainIDPrime},
			}, new(big.Int).SetUint64(sendAmount),
			e2ehelper.WithWaitForUnexpectedBridges(true),
			e2ehelper.WithRestartValidatorsConfig([]e2ehelper.RestartValidatorsConfig{
				{WaitTime: stopAfter, StopIndxs: []int{validatorStoppingIdx}},
			}))
	})

	t.Run("Both directions sequential and parallel - two nodes goes off in the middle and then one comes back", func(t *testing.T) {
		const (
			sequentialInstances   = 5
			parallelInstances     = 10
			stopAfter             = time.Second * 60
			startAgainAfter       = time.Second * 120
			validatorStoppingIdx1 = 1
			validatorStoppingIdx2 = 2
			sendAmount            = uint64(1_000_000)
		)

		e2ehelper.ExecuteBridging(
			t, ctx, apex, sequentialInstances,
			apex.Users[:parallelInstances],
			[]*cardanofw.TestApexUser{user},
			[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector},
			map[string][]string{
				cardanofw.ChainIDPrime:  {cardanofw.ChainIDVector},
				cardanofw.ChainIDVector: {cardanofw.ChainIDPrime},
			}, new(big.Int).SetUint64(sendAmount),
			e2ehelper.WithWaitForUnexpectedBridges(true),
			e2ehelper.WithRestartValidatorsConfig([]e2ehelper.RestartValidatorsConfig{
				{WaitTime: stopAfter, StopIndxs: []int{validatorStoppingIdx1, validatorStoppingIdx2}},
				{WaitTime: startAgainAfter, StartIndxs: []int{validatorStoppingIdx1}},
			}))
	})

	t.Run("Both directions sequential and parallel", func(t *testing.T) {
		const (
			sequentialInstances = 5
			parallelInstances   = 6
		)

		PrimeVectorBothDirectionsSequentialAndParallel(t, ctx, apex, user, sequentialInstances, parallelInstances)
	})
}

func TestE2E_ApexBridge_Fund_Defund(t *testing.T) {
	if cardanofw.ShouldSkipE2RRedundantTests() {
		t.Skip()
	}

	const (
		apiKey       = "test_api_key"
		userCnt      = 10
		feeAmountDfm = 1_100_000
	)

	var (
		err error
	)

	type chainStageKey struct {
		chain    string
		receiver uint
	}

	type bridingRequest struct {
		src         string
		dest        string
		sender      *cardanofw.TestApexUser
		amount      *big.Int
		receiverIdx uint
	}

	createBridgingData := func(ctx context.Context, apex *cardanofw.ApexSystem,
		bridgingRequests []*bridingRequest, receivers map[uint]*cardanofw.TestApexUser,
		defundReceiver *cardanofw.TestApexUser, defundAmount *big.Int) (
		map[chainStageKey]*big.Int, map[chainStageKey]*big.Int,
		map[chainStageKey]*cardanofw.TestApexUser,
		map[chainStageKey]*big.Int, map[chainStageKey]*big.Int,
		map[chainStageKey]*cardanofw.TestApexUser,
	) {
		var (
			chainPrevAmounts     = make(map[chainStageKey]*big.Int)
			chainExpectedAmounts = make(map[chainStageKey]*big.Int)
			chainReceivers       = make(map[chainStageKey]*cardanofw.TestApexUser)

			defundReceiversPrevAmount     = make(map[chainStageKey]*big.Int)
			defundReceiversExpectedAmount = make(map[chainStageKey]*big.Int)
			defundReceivers               = make(map[chainStageKey]*cardanofw.TestApexUser)
		)

		for _, br := range bridgingRequests {
			key := chainStageKey{chain: br.dest, receiver: br.receiverIdx}
			if _, exists := chainPrevAmounts[key]; !exists {
				prevAmount, err := apex.GetBalance(ctx, receivers[br.receiverIdx], br.dest)
				require.NoError(t, err)

				chainPrevAmounts[key] = prevAmount
			}

			if _, exists := chainExpectedAmounts[key]; !exists {
				chainExpectedAmounts[key] = big.NewInt(0)
			}

			chainExpectedAmounts[key].Add(chainExpectedAmounts[key], cardanofw.ApexToDfm(br.amount))

			if _, exists := chainReceivers[key]; !exists {
				chainReceivers[key] = receivers[br.receiverIdx]
			}

			if defundAmount != nil && defundReceiver != nil {
				if _, exists := defundReceiversPrevAmount[key]; !exists {
					prevAmount, err := apex.GetBalance(ctx, defundReceiver, br.dest)
					require.NoError(t, err)

					defundReceiversPrevAmount[key] = prevAmount
				}

				if _, exist := defundReceiversExpectedAmount[key]; !exist {
					defundReceiversExpectedAmount[key] = big.NewInt(0)
				}

				defundReceiversExpectedAmount[key].Add(defundReceiversExpectedAmount[key], cardanofw.ApexToDfm(defundAmount))

				if _, exists := defundReceivers[key]; !exists {
					defundReceivers[key] = defundReceiver
				}
			}
		}

		return chainPrevAmounts, chainExpectedAmounts, chainReceivers, defundReceiversPrevAmount, defundReceiversExpectedAmount, defundReceivers
	}

	bridgeTransactions := func(ctx context.Context, apex *cardanofw.ApexSystem,
		bridgingRequests []*bridingRequest, receivers map[uint]*cardanofw.TestApexUser,
	) {
		var wg sync.WaitGroup

		for _, br := range bridgingRequests {
			wg.Add(1)

			go func(src string, dest string, sender *cardanofw.TestApexUser, receiver *cardanofw.TestApexUser, amount *big.Int) {
				defer wg.Done()

				txHash := apex.SubmitBridgingRequest(t, ctx, src, dest, sender, amount, receiver)
				fmt.Printf("Bridging request: %v to %v sent. hash: %s\n", src, dest, txHash)
			}(br.src, br.dest, br.sender, receivers[br.receiverIdx], cardanofw.ApexToDfm(br.amount))
		}

		wg.Wait()
	}

	waitOnDestination := func(
		ctx context.Context, apex *cardanofw.ApexSystem,
		chainPrevAmounts map[chainStageKey]*big.Int, chainExpectedAmounts map[chainStageKey]*big.Int,
		chainReceivers map[chainStageKey]*cardanofw.TestApexUser, numRetries int, waitTime time.Duration,
	) map[chainStageKey]error {
		var (
			wg           sync.WaitGroup
			errsPerChain = make(map[chainStageKey]error, len(chainPrevAmounts))
			mu           sync.Mutex
		)

		for chainKey, prevAmount := range chainPrevAmounts {
			wg.Add(1)

			go func() {
				defer wg.Done()

				fmt.Printf("Waiting for %v Amount on %v\n", chainExpectedAmounts[chainKey], chainKey.chain)

				expectedAmount := new(big.Int).Set(chainExpectedAmounts[chainKey])
				expectedAmount.Add(expectedAmount, prevAmount)

				err = apex.WaitForExactAmount(
					ctx, chainReceivers[chainKey], chainKey.chain, expectedAmount, numRetries, waitTime)

				mu.Lock()
				defer mu.Unlock()

				errsPerChain[chainKey] = err
			}()
		}

		wg.Wait()

		return errsPerChain
	}

	fundWallets := func(
		ctx context.Context, apex *cardanofw.ApexSystem,
		fundAmountApex *big.Int,
	) error {
		fmt.Printf("Funding hot wallets\n")

		for _, chain := range []string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector, cardanofw.ChainIDNexus} {
			if err = apex.FundChainHotWallet(ctx, chain, cardanofw.ApexToDfm(fundAmountApex)); err != nil {
				return err
			}
		}

		fmt.Printf("Hot wallets have been funded\n")

		return nil
	}

	defundWallets := func(
		ctx context.Context, apex *cardanofw.ApexSystem,
		defundReceiver *cardanofw.TestApexUser, defundAmountApex *big.Int,
		defundReceiverPrevAmounts map[chainStageKey]*big.Int, defundReceiverExpectedAmounts map[chainStageKey]*big.Int,
		defundReceivers map[chainStageKey]*cardanofw.TestApexUser,
	) {
		fmt.Printf("Defunding hot wallets\n")

		defundAmount := cardanofw.ApexToDfm(defundAmountApex)

		require.NoError(t, apex.DefundHotWallet(
			cardanofw.ChainIDPrime, defundReceiver.GetAddress(cardanofw.ChainIDPrime), defundAmount))

		require.NoError(t, apex.DefundHotWallet(
			cardanofw.ChainIDVector, defundReceiver.GetAddress(cardanofw.ChainIDVector), defundAmount))

		require.NoError(t, apex.DefundHotWallet(
			cardanofw.ChainIDNexus, defundReceiver.GetAddress(cardanofw.ChainIDNexus), defundAmount))

		errsPerChain := waitOnDestination(ctx, apex,
			defundReceiverPrevAmounts, defundReceiverExpectedAmounts, defundReceivers,
			200, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.NoError(t, err)
			fmt.Printf("Defund on %v confirmed\n", chainKey.chain)
		}
	}

	t.Run("Fund_Parallel_Send_BRs_Then_Full_Fund", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		primeConfig, vectorConfig, nexusConfig := cardanofw.NewPrimeChainConfig(),
			cardanofw.NewVectorChainConfig(true), cardanofw.NewNexusChainConfig(true)
		primeConfig.FundAmount = 0
		vectorConfig.FundAmount = 0
		nexusConfig.FundAmount = big.NewInt(0)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithPrimeConfig(primeConfig),
			cardanofw.WithVectorConfig(vectorConfig),
			cardanofw.WithNexusConfig(nexusConfig),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		var (
			bridgingRequests = []*bridingRequest{
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector, sender: apex.Users[0], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDNexus, sender: apex.Users[1], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDVector, dest: cardanofw.ChainIDPrime, sender: apex.Users[0], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime, sender: apex.Users[0], amount: big.NewInt(1), receiverIdx: 0},
			}

			receivers = map[uint]*cardanofw.TestApexUser{
				0: apex.Users[userCnt-1],
			}
		)

		chainPrevAmounts, chainExpectedAmounts, chainReceivers, _, _, _ := createBridgingData(ctx, apex, bridgingRequests, receivers, nil, nil)

		bridgeTransactions(ctx, apex, bridgingRequests, receivers)

		fmt.Printf("Confirming that bridging requests will not be processed\n")

		errsPerChain := waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 30, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.Error(t, err)
			fmt.Printf("As intended, %v TXs on %v not yet arrived\n", chainExpectedAmounts[chainKey], chainKey.chain)
		}

		require.NoError(t, fundWallets(ctx, apex, big.NewInt(100)))

		errsPerChain = waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 200, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.NoError(t, err)
			fmt.Printf("%v TXs on %v confirmed\n", chainExpectedAmounts[chainKey], chainKey)
		}
	})

	t.Run("Fund_Parallel_Send_BRs_Then_Fund_Twice", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		primeConfig, vectorConfig, nexusConfig := cardanofw.NewPrimeChainConfig(),
			cardanofw.NewVectorChainConfig(true), cardanofw.NewNexusChainConfig(true)
		primeConfig.FundAmount = 0
		vectorConfig.FundAmount = 0
		nexusConfig.FundAmount = big.NewInt(0)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithPrimeConfig(primeConfig),
			cardanofw.WithVectorConfig(vectorConfig),
			cardanofw.WithNexusConfig(nexusConfig),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		var (
			bridgingRequests = []*bridingRequest{
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector, sender: apex.Users[0], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector, sender: apex.Users[1], amount: big.NewInt(100), receiverIdx: 1},
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDNexus, sender: apex.Users[2], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDNexus, sender: apex.Users[3], amount: big.NewInt(100), receiverIdx: 1},
				{src: cardanofw.ChainIDVector, dest: cardanofw.ChainIDPrime, sender: apex.Users[0], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDVector, dest: cardanofw.ChainIDPrime, sender: apex.Users[1], amount: big.NewInt(100), receiverIdx: 1},
				{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime, sender: apex.Users[0], amount: big.NewInt(1), receiverIdx: 0},
				{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime, sender: apex.Users[1], amount: big.NewInt(100), receiverIdx: 1},
			}

			receivers = map[uint]*cardanofw.TestApexUser{
				0: apex.Users[userCnt-1],
				1: apex.Users[userCnt-2],
			}
		)

		chainPrevAmounts, chainExpectedAmounts, chainReceivers, _, _, _ := createBridgingData(ctx, apex, bridgingRequests, receivers, nil, nil)

		bridgeTransactions(ctx, apex, bridgingRequests, receivers)

		fmt.Printf("Confirming that bridging requests will not be processed\n")

		errsPerChain := waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 30, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.Error(t, err)
			fmt.Printf("As intended, %v TXs on %v not yet arrived\n", chainExpectedAmounts[chainKey], chainKey.chain)
		}

		require.NoError(t, fundWallets(ctx, apex, big.NewInt(10)))

		errsPerChain = waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 30, time.Second*10)
		for chainKey, err := range errsPerChain {
			if chainKey.receiver == 1 {
				require.Error(t, err)
				fmt.Printf("As intended, %v TXs on %v not yet arrived\n", chainExpectedAmounts[chainKey], chainKey)
			} else {
				require.NoError(t, err)
				fmt.Printf("%v TXs on %v confirmed\n", chainExpectedAmounts[chainKey], chainKey.chain)
			}
		}

		require.NoError(t, fundWallets(ctx, apex, big.NewInt(1000)))

		errsPerChain = waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 200, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.NoError(t, err)
			fmt.Printf("%v TXs on %v confirmed\n", chainExpectedAmounts[chainKey], chainKey.chain)
		}
	})

	t.Run("Basic defund test", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		initialFundInDfm := cardanofw.ApexToDfm(big.NewInt(100))

		primeConfig, vectorConfig, nexusConfig := cardanofw.NewPrimeChainConfig(),
			cardanofw.NewVectorChainConfig(true), cardanofw.NewNexusChainConfig(true)
		primeConfig.FundAmount = cardanofw.DfmToChainNativeTokenAmount(
			cardanofw.ChainIDPrime, initialFundInDfm).Uint64()
		vectorConfig.FundAmount = cardanofw.DfmToChainNativeTokenAmount(
			cardanofw.ChainIDVector, initialFundInDfm).Uint64()
		nexusConfig.FundAmount = cardanofw.DfmToChainNativeTokenAmount(
			cardanofw.ChainIDNexus, initialFundInDfm)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithPrimeConfig(primeConfig),
			cardanofw.WithVectorConfig(vectorConfig),
			cardanofw.WithNexusConfig(nexusConfig),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		// give time for oracles to submit hot wallet increment claims for initial fundings
		select {
		case <-ctx.Done():
			return
		case <-time.After(90 * time.Second):
		}

		var (
			defundReceiver          = apex.Users[userCnt-2]
			apexDefundAndFundAmount = big.NewInt(70)
			apexSendAmount          = big.NewInt(50)

			bridgingRequests = []*bridingRequest{
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector, sender: apex.Users[0], amount: apexSendAmount, receiverIdx: 0},
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDNexus, sender: apex.Users[1], amount: apexSendAmount, receiverIdx: 0},
				{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime, sender: apex.Users[0], amount: apexSendAmount, receiverIdx: 0},
			}

			receivers = map[uint]*cardanofw.TestApexUser{
				0: apex.Users[userCnt-1],
			}
		)

		require.True(t,
			cardanofw.ApexToDfm(apexSendAmount).Uint64()+feeAmountDfm < initialFundInDfm.Uint64())

		chainPrevAmounts, chainExpectedAmounts, chainReceivers,
			defundReceiversPrevAmount, defundReceiversExpectedAmount, defundReceivers :=
			createBridgingData(ctx, apex, bridgingRequests, receivers, defundReceiver, apexDefundAndFundAmount)

		defundWallets(ctx, apex, defundReceiver, apexDefundAndFundAmount,
			defundReceiversPrevAmount, defundReceiversExpectedAmount, defundReceivers)

		bridgeTransactions(ctx, apex, bridgingRequests, receivers)

		fmt.Printf("Confirming that bridging requests will not be processed\n")

		errsPerChain := waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 30, time.Second*10)
		for chain, err := range errsPerChain {
			require.Error(t, err)
			fmt.Printf("As intended, %v TXs on %v not yet arrived\n", chainExpectedAmounts[chain], chain)
		}

		require.NoError(t, fundWallets(ctx, apex, apexDefundAndFundAmount))

		errsPerChain = waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 200, time.Second*10)
		for chain, err := range errsPerChain {
			require.NoError(t, err)
			fmt.Printf("%v TXs on %v confirmed\n", chainExpectedAmounts[chain], chain)
		}
	})

	t.Run("Defund after bridging request is sent", func(t *testing.T) {
		ctx, cncl := context.WithCancel(context.Background())
		defer cncl()

		initialFundInDfm := cardanofw.ApexToDfm(big.NewInt(100))

		primeConfig, vectorConfig, nexusConfig := cardanofw.NewPrimeChainConfig(),
			cardanofw.NewVectorChainConfig(true), cardanofw.NewNexusChainConfig(true)
		primeConfig.FundAmount = cardanofw.DfmToChainNativeTokenAmount(
			cardanofw.ChainIDPrime, initialFundInDfm).Uint64()
		vectorConfig.FundAmount = cardanofw.DfmToChainNativeTokenAmount(
			cardanofw.ChainIDVector, initialFundInDfm).Uint64()
		nexusConfig.FundAmount = cardanofw.DfmToChainNativeTokenAmount(
			cardanofw.ChainIDNexus, initialFundInDfm)

		apex := cardanofw.SetupAndRunApexBridge(
			t, ctx,
			cardanofw.WithAPIKey(apiKey),
			cardanofw.WithUserCnt(userCnt),
			cardanofw.WithPrimeConfig(primeConfig),
			cardanofw.WithVectorConfig(vectorConfig),
			cardanofw.WithNexusConfig(nexusConfig),
		)

		defer require.True(t, apex.ApexBridgeProcessesRunning())

		// give time for oracles to submit hot wallet increment claims for initial fundings
		select {
		case <-ctx.Done():
			return
		case <-time.After(90 * time.Second):
		}

		var (
			defundReceiver          = apex.Users[userCnt-2]
			apexDefundAndFundAmount = big.NewInt(70)
			apexSendAmount          = big.NewInt(50)

			bridgingRequests = []*bridingRequest{
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDVector, sender: apex.Users[0], amount: apexSendAmount, receiverIdx: 0},
				{src: cardanofw.ChainIDPrime, dest: cardanofw.ChainIDNexus, sender: apex.Users[1], amount: apexSendAmount, receiverIdx: 0},
				{src: cardanofw.ChainIDNexus, dest: cardanofw.ChainIDPrime, sender: apex.Users[0], amount: big.NewInt(150), receiverIdx: 0},
			}

			receivers = map[uint]*cardanofw.TestApexUser{
				0: apex.Users[userCnt-1],
			}
		)

		require.True(t,
			cardanofw.ApexToDfm(apexSendAmount).Uint64()+feeAmountDfm < initialFundInDfm.Uint64())

		chainPrevAmounts, chainExpectedAmounts, chainReceivers, _, _, _ :=
			createBridgingData(ctx, apex, bridgingRequests, receivers, defundReceiver, apexDefundAndFundAmount)

		for _, request := range bridgingRequests {
			bridgeTransactions(ctx, apex, []*bridingRequest{request}, receivers)

			require.NoError(t, apex.DefundHotWallet(
				request.dest, defundReceiver.GetAddress(request.dest), cardanofw.ApexToDfm(apexDefundAndFundAmount)))
		}

		fmt.Printf("Confirming that bridging requests will not be processed\n")

		errsPerChain := waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 30, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.Error(t, err)
			fmt.Printf("As intended, %v TX on %v not yet arrived\n", chainExpectedAmounts[chainKey], chainKey.chain)
		}

		require.NoError(t, fundWallets(ctx, apex, apexDefundAndFundAmount))

		errsPerChain = waitOnDestination(ctx, apex, chainPrevAmounts, chainExpectedAmounts, chainReceivers, 200, time.Second*10)
		for chainKey, err := range errsPerChain {
			require.NoError(t, err)
			fmt.Printf("%v TX on %v confirmed\n", chainExpectedAmounts[chainKey], chainKey.chain)
		}
	})
}

func TestE2E_ApexBridge_ValidScenarios_BigTests(t *testing.T) {
	if shouldRun := os.Getenv("RUN_E2E_BIG_TESTS"); shouldRun != "true" {
		t.Skip()
	}

	const (
		apiKey  = "test_api_key"
		userCnt = 1010
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	primeConfig, vectorConfig := cardanofw.NewPrimeChainConfig(), cardanofw.NewVectorChainConfig(true)
	primeConfig.PremineAmount = 30_000_000_000
	vectorConfig.PremineAmount = 30_000_000_000

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(userCnt),
		cardanofw.WithPrimeConfig(primeConfig),
		cardanofw.WithVectorConfig(vectorConfig),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	txProviderPrime := apex.PrimeInfo.GetTxProvider()
	txProviderVector := apex.VectorInfo.GetTxProvider()

	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("vector user addr: ", user.VectorAddress)
	fmt.Println("prime multisig addr: ", apex.PrimeInfo.MultisigAddr)
	fmt.Println("prime fee addr: ", apex.PrimeInfo.FeeAddr)
	fmt.Println("vector multisig addr: ", apex.VectorInfo.MultisigAddr)
	fmt.Println("vector fee addr: ", apex.VectorInfo.FeeAddr)

	//nolint:dupl
	t.Run("From prime to vector 200x 5min 90%", func(t *testing.T) {
		instances := 200
		maxWaitTime := 300
		sendAmount := uint64(1_000_000)
		successChance := 90 // 90%
		succeededCount := int64(0)

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		fmt.Printf("Sending %v transactions in %v seconds\n", instances, maxWaitTime)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCount++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDVector, feeAmount)
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderPrime, apex.Users[idx].PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(succeededCount))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 500, time.Second*10)
		require.NoError(t, err)

		newAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, prevAmount, newAmount, expectedAmount)
	})

	//nolint:dupl
	t.Run("From prime to vector 1000x 20min 90%", func(t *testing.T) {
		instances := 1000
		maxWaitTime := 1200
		sendAmount := uint64(1_000_000)
		successChance := 90 // 90%
		succeededCount := int64(0)

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		fmt.Printf("Sending %v transactions in %v seconds\n", instances, maxWaitTime)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCount++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDVector, feeAmount)
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderPrime, apex.Users[idx].PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(succeededCount))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 500, time.Second*10)
		require.NoError(t, err)

		newAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		fmt.Printf("Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCount, prevAmount, newAmount, expectedAmount)
	})

	t.Run("Both directions 1000x 60min 90%", func(t *testing.T) {
		instances := 1000
		maxWaitTime := 3600
		sendAmount := uint64(1_000_000)
		successChance := 90 // 90%
		succeededCountPrime := int64(0)
		succeededCountVector := int64(0)

		prevAmountOnVector, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)
		prevAmountOnPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		fmt.Printf("Sending %v transactions in %v seconds\n", instances*2, maxWaitTime)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(2)

			//nolint:dupl
			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCountPrime++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDPrime), receivers,
						cardanofw.ChainIDVector, feeAmount)
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderPrime, apex.Users[idx].PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
						apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)

			//nolint:dupl
			go func(idx int) {
				defer wg.Done()

				if successChance > rand.Intn(100) {
					succeededCountVector++
					sleepTime := rand.Intn(maxWaitTime)
					time.Sleep(time.Second * time.Duration(sleepTime))

					apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)
				} else {
					feeAmount := uint64(1_100_000)
					receivers := map[string]uint64{
						user.GetAddress(cardanofw.ChainIDPrime): sendAmount * 10, // 10Ada
					}

					bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
						apex.Users[idx].GetAddress(cardanofw.ChainIDVector), receivers,
						cardanofw.ChainIDPrime, feeAmount)
					require.NoError(t, err)

					_, err = cardanofw.SendTx(
						ctx, txProviderVector, apex.Users[idx].VectorWallet, sendAmount+feeAmount, apex.VectorInfo.MultisigAddr,
						apex.Config.VectorConfig.NetworkType, bridgingRequestMetadata)
					require.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		fmt.Printf("All tx sent, waiting for confirmation.\n")

		expectedAmountOnVector := new(big.Int).SetUint64(sendAmount)
		expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(succeededCountVector))
		expectedAmountOnVector.Add(expectedAmountOnVector, prevAmountOnVector)

		expectedAmountOnPrime := new(big.Int).SetUint64(sendAmount)
		expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(succeededCountPrime))
		expectedAmountOnPrime.Add(expectedAmountOnPrime, prevAmountOnPrime)

		errs := make([]error, 2)

		wg.Add(2)

		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on vector\n", succeededCountVector)

			errs[0] = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 500, time.Second*10)
		}()

		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on prime\n", succeededCountPrime)

			errs[1] = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 500, time.Second*10)
		}()

		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		fmt.Printf("%v TXs on vector confirmed\n", succeededCountVector)
		fmt.Printf("%v TXs on prime confirmed\n", succeededCountPrime)

		newAmountOnVector, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)
		newAmountOnPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		fmt.Printf("Vector - Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCountVector, prevAmountOnVector, newAmountOnVector, expectedAmountOnVector)
		fmt.Printf("Prime - Success count: %v. prevAmount: %v. newAmount: %v. expectedAmount: %v\n", succeededCountPrime, prevAmountOnPrime, newAmountOnPrime, expectedAmountOnPrime)
	})
}

func PrimeToVectorSequentialAndParallelWithMaxReceivers(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, sequentialInstances, parallelInstances int,
) {
	t.Helper()

	const (
		receivers  = 4
		sendAmount = uint64(1_000_000)
	)

	e2ehelper.ExecuteBridging(
		t, ctx, apex, sequentialInstances,
		apex.Users[:parallelInstances],
		apex.Users[:receivers],
		[]string{cardanofw.ChainIDPrime},
		map[string][]string{
			cardanofw.ChainIDPrime: {cardanofw.ChainIDVector},
		}, new(big.Int).SetUint64(sendAmount))
}

func PrimeVectorBothDirectionsSequentialAndParallel(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem,
	receiverUser *cardanofw.TestApexUser, sequentialInstances, parallelInstances int,
) {
	t.Helper()

	const (
		sendAmount = uint64(1_000_000)
	)

	e2ehelper.ExecuteBridging(
		t, ctx, apex, sequentialInstances,
		apex.Users[:parallelInstances],
		[]*cardanofw.TestApexUser{receiverUser},
		[]string{cardanofw.ChainIDPrime, cardanofw.ChainIDVector},
		map[string][]string{
			cardanofw.ChainIDPrime:  {cardanofw.ChainIDVector},
			cardanofw.ChainIDVector: {cardanofw.ChainIDPrime},
		}, new(big.Int).SetUint64(sendAmount),
		e2ehelper.WithWaitForUnexpectedBridges(true))
}

func PrimeToVectorMismatchSubmittedAndReceiverAmounts(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	sendAmount := uint64(1_000_000)
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
	}

	bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
		user.GetAddress(cardanofw.ChainIDPrime), receivers,
		cardanofw.ChainIDVector, feeAmount)
	require.NoError(t, err)

	txHash, err := apex.SubmitTx(
		ctx, cardanofw.ChainIDPrime, user,
		apex.PrimeInfo.MultisigAddr, new(big.Int).SetUint64(sendAmount+feeAmount), bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apex.Config.APIKey)
}

func PrimeToVectorInvalidMetadataSlicedOff(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	txProviderPrime := apex.PrimeInfo.GetTxProvider()
	sendAmount := uint64(1_000_000)
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(cardanofw.ChainIDVector): sendAmount,
	}

	bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
		user.GetAddress(cardanofw.ChainIDPrime), receivers,
		cardanofw.ChainIDVector, feeAmount)
	require.NoError(t, err)

	// Send only half bytes of metadata making it invalid
	bridgingRequestMetadata = bridgingRequestMetadata[0 : len(bridgingRequestMetadata)/2]

	_, err = cardanofw.SendTx(
		ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
		apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
	require.Error(t, err)
}

func PrimeToVectorInvalidMetadataWrongType(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	txProviderPrime := apex.PrimeInfo.GetTxProvider()
	sendAmount := uint64(1_000_000)
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(cardanofw.ChainIDVector): sendAmount,
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
			"d":  cardanofw.ChainIDVector,
			"s":  cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
			"tx": transactions,
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := cardanofw.SendTx(
		ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
		apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
	require.NoError(t, err)

	_, err = cardanofw.WaitForRequestStates(ctx, apex, cardanofw.ChainIDPrime, txHash, apex.Config.APIKey, nil, 60)
	require.Error(t, err)
	require.ErrorContains(t, err, "timeout")
}

func PrimeToVectorInvalidMetadataInvalidDestination(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	txProviderPrime := apex.PrimeInfo.GetTxProvider()
	sendAmount := uint64(1_000_000)
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(cardanofw.ChainIDVector): sendAmount,
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

	txHash, err := cardanofw.SendTx(
		ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
		apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apex.Config.APIKey)
}

func PrimeToVectorInvalidMetadataInvalidSender(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	txProviderPrime := apex.PrimeInfo.GetTxProvider()
	sendAmount := uint64(1_000_000)
	feeAmount := uint64(1_100_000)

	receivers := map[string]uint64{
		user.GetAddress(cardanofw.ChainIDVector): sendAmount,
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
			"s":  "", // should be sender address (max len 40)
			"d":  cardanofw.ChainIDVector,
			"tx": transactions,
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := cardanofw.SendTx(
		ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeInfo.MultisigAddr,
		apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apex.Config.APIKey)
}

func PrimeToVectorInvalidMetadataInvalidTransactions(
	t *testing.T, ctx context.Context, apex *cardanofw.ApexSystem, user *cardanofw.TestApexUser,
) {
	t.Helper()

	txProviderPrime := apex.PrimeInfo.GetTxProvider()
	sendAmount := uint64(1_000_000)
	feeAmount := uint64(1_100_000)

	metadata := map[string]interface{}{
		"1": map[string]interface{}{
			"t":  "bridge",
			"d":  cardanofw.ChainIDVector,
			"s":  cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
			"tx": []cardanofw.BridgingRequestMetadataTransaction{}, // should not be empty
			"fa": feeAmount,
		},
	}

	bridgingRequestMetadata, err := json.Marshal(metadata)
	require.NoError(t, err)

	txHash, err := cardanofw.SendTx(
		ctx, txProviderPrime, user.PrimeWallet, sendAmount, apex.PrimeInfo.MultisigAddr,
		apex.Config.PrimeConfig.NetworkType, bridgingRequestMetadata)
	require.NoError(t, err)

	cardanofw.WaitForInvalidState(t, ctx, apex, cardanofw.ChainIDPrime, txHash, apex.Config.APIKey)
}
