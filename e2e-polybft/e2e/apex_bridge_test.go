package e2e

import (
	"context"
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
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Download Cardano executables from https://github.com/IntersectMBO/cardano-node/releases/tag/8.7.3 and unpack tar.gz file
// Add directory where unpacked files are located to the $PATH (in example bellow `~/Apps/cardano`)
// eq add line `export PATH=$PATH:~/Apps/cardano` to  `~/.bashrc`
// cd e2e-polybft/e2e
// ONLY_RUN_APEX_BRIDGE=true go test -v -timeout 0 -run ^Test_OnlyRunApexBridge_WithNexusAndVector$ github.com/0xPolygon/polygon-edge/e2e-polybft/e2e
func Test_OnlyRunApexBridge_WithNexusAndVector(t *testing.T) {
	if shouldRun := os.Getenv("ONLY_RUN_APEX_BRIDGE"); shouldRun != "true" {
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
		cardanofw.WithUserCardanoFund(500_000_000),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	oracleAPI, err := apex.GetBridgingAPI()
	require.NoError(t, err)

	fmt.Printf("oracle API: %s\n", oracleAPI)
	fmt.Printf("oracle API key: %s\n", apiKey)

	fmt.Printf("prime network url: %s\n", apex.PrimeCluster.NetworkAddress())
	fmt.Printf("prime ogmios url: %s\n", apex.PrimeCluster.OgmiosURL())
	fmt.Printf("prime bridging addr: %s\n", apex.PrimeMultisigAddr)
	fmt.Printf("prime fee addr: %s\n", apex.PrimeMultisigFeeAddr)

	fmt.Printf("vector network url: %s\n", apex.VectorCluster.NetworkAddress())
	fmt.Printf("vector ogmios url: %s\n", apex.VectorCluster.OgmiosURL())
	fmt.Printf("vector bridging addr: %s\n", apex.VectorMultisigAddr)
	fmt.Printf("vector fee addr: %s\n", apex.VectorMultisigFeeAddr)

	user := apex.Users[0]
	userPrimeSK, err := user.GetPrivateKey(cardanofw.ChainIDPrime)
	require.NoError(t, err)
	userVectorSK, err := user.GetPrivateKey(cardanofw.ChainIDVector)
	require.NoError(t, err)
	userNexusPK, err := user.GetPrivateKey(cardanofw.ChainIDNexus)
	require.NoError(t, err)

	fmt.Printf("user prime addr: %s\n", user.GetAddress(cardanofw.ChainIDPrime))
	fmt.Printf("user prime signing key hex: %s\n", userPrimeSK)
	fmt.Printf("user vector addr: %s\n", user.GetAddress(cardanofw.ChainIDVector))
	fmt.Printf("user vector signing key hex: %s\n", userVectorSK)

	chainID, err := apex.Nexus.Cluster.Servers[0].JSONRPC().ChainID()
	require.NoError(t, err)

	fmt.Printf("nexus user addr: %s\n", user.GetAddress(cardanofw.ChainIDNexus))
	fmt.Printf("nexus user signing key: %s\n", userNexusPK)
	fmt.Printf("nexus url: %s\n", apex.GetNexusDefaultJSONRPCAddr())
	fmt.Printf("nexus gateway sc addr: %s\n", apex.Nexus.GetGatewayAddress().String())
	fmt.Printf("nexus chainID: %v\n", chainID)

	fmt.Printf("bridge url: %s\n", apex.GetBridgeDefaultJSONRPCAddr())

	signalChannel := make(chan os.Signal, 1)
	// Notify the signalChannel when the interrupt signal is received (Ctrl+C)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	<-signalChannel
}

func TestE2E_ApexBridge_CardanoOracleState(t *testing.T) {
	if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
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
				sumMultisig, sumFee := uint64(0), uint64(0)

				switch chainID {
				case cardanofw.ChainIDPrime:
					multisigAddr, feeAddr = apex.PrimeMultisigAddr, apex.PrimeMultisigFeeAddr
				case cardanofw.ChainIDVector:
					multisigAddr, feeAddr = apex.VectorMultisigAddr, apex.VectorMultisigFeeAddr
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

				if sumMultisig != apex.Config.FundTokenAmount || sumFee != apex.Config.FundTokenAmount ||
					currentState.BlockSlot == 0 {
					break outerLoop
				} else {
					goodOraclesCount++
				}
			}
		}
	}
}

func TestE2E_ApexBridge(t *testing.T) {
	if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
		t.Skip()
	}

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithUserCnt(1),
		cardanofw.WithUserCardanoFund(500_000_000),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[0]

	sendAmount := uint64(1_000_000)
	prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
	require.NoError(t, err)

	expectedAmount := new(big.Int).SetUint64(sendAmount)
	expectedAmount.Add(expectedAmount, prevAmount)

	// Initiate bridging PRIME -> VECTOR
	apex.SubmitBridgingRequest(t, ctx,
		cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
		user, new(big.Int).SetUint64(sendAmount), user,
	)

	err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 15, time.Second*10)
	require.NoError(t, err)
}

func TestE2E_ApexBridge_BatchRecreated(t *testing.T) {
	if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
		t.Skip()
	}

	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithPrimeTTL(20, 1),
		cardanofw.WithVectorTTL(30, 1),
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(1),
		cardanofw.WithUserCardanoFund(500_000_000),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[0]

	sendAmount := uint64(1_000_000)

	// Initiate bridging PRIME -> VECTOR
	txHash := apex.SubmitBridgingRequest(t, ctx,
		cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
		user, new(big.Int).SetUint64(sendAmount), user,
	)

	timeoutTimer := time.NewTimer(time.Second * 300)
	defer timeoutTimer.Stop()

	var (
		wentFromFailedOnDestinationToIncludedInBatch bool
		prevStatus                                   string
		currentStatus                                string
	)

	apiURL, err := apex.GetBridgingAPI()
	require.NoError(t, err)

	requestURL := fmt.Sprintf(
		"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, cardanofw.ChainIDPrime, txHash)

	fmt.Printf("Bridging request txHash = %s\n", txHash)

for_loop:
	for {
		select {
		case <-timeoutTimer.C:
			fmt.Printf("Timeout\n")

			break for_loop
		case <-ctx.Done():
			fmt.Printf("Done\n")

			break for_loop
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
		}

		if prevStatus == "FailedToExecuteOnDestination" &&
			(currentStatus == "IncludedInBatch" || currentStatus == "SubmittedToDestination") {
			wentFromFailedOnDestinationToIncludedInBatch = true

			break for_loop
		}
	}

	require.True(t, wentFromFailedOnDestinationToIncludedInBatch)
}

func TestE2E_ApexBridge_InvalidScenarios(t *testing.T) {
	const (
		apiKey = "test_api_key"
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(10),
		cardanofw.WithUserCardanoFund(500_000_000),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[0]

	txProviderPrime := apex.GetPrimeTxProvider()

	t.Run("Submitter not enough funds", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)

		receivers := map[string]uint64{
			user.GetAddress(cardanofw.ChainIDVector): sendAmount * 10, // 10Ada
		}

		bridgingRequestMetadata, err := cardanofw.CreateCardanoBridgingMetaData(
			user.GetAddress(cardanofw.ChainIDPrime), receivers,
			cardanofw.ChainIDVector, feeAmount)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHash)
	})

	t.Run("Multiple submitters don't have enough funds", func(t *testing.T) {
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
				ctx, txProviderPrime, apex.Users[i].PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
				apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
			require.NoError(t, err)

			apiURL, err := apex.GetBridgingAPI()
			require.NoError(t, err)
			cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHash)
		}
	})

	t.Run("Multiple submitters don't have enough funds parallel", func(t *testing.T) {
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
					sendAmount+feeAmount, apex.PrimeMultisigAddr,
					apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
				require.NoError(t, err)
			}()
		}

		wg.Wait()

		for i := 0; i < instances; i++ {
			apiURL, err := apex.GetBridgingAPI()
			require.NoError(t, err)
			cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHashes[i])
		}
	})

	t.Run("Submitted invalid metadata", func(t *testing.T) {
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
			ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.Error(t, err)
	})

	t.Run("Submitted invalid metadata - wrong type", func(t *testing.T) {
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
			ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
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
			ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHash)
	})

	t.Run("Submitted invalid metadata - invalid sender", func(t *testing.T) {
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
			ctx, txProviderPrime, user.PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHash)
	})

	t.Run("Submitted invalid metadata - empty tx", func(t *testing.T) {
		sendAmount := uint64(1_000_000)
		feeAmount := uint64(1_100_000)
		var transactions = make([]cardanofw.BridgingRequestMetadataTransaction, 0)

		metadata := map[string]interface{}{
			"1": map[string]interface{}{
				"t":  "bridge",
				"d":  cardanofw.ChainIDVector,
				"s":  cardanofw.SplitString(user.GetAddress(cardanofw.ChainIDPrime), 40),
				"tx": transactions, // should not be empty
				"fa": feeAmount,
			},
		}

		bridgingRequestMetadata, err := json.Marshal(metadata)
		require.NoError(t, err)

		txHash, err := cardanofw.SendTx(
			ctx, txProviderPrime, user.PrimeWallet, sendAmount, apex.PrimeMultisigAddr,
			apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
		require.NoError(t, err)

		apiURL, err := apex.GetBridgingAPI()
		require.NoError(t, err)
		cardanofw.WaitForInvalidState(t, ctx, apiURL, apiKey, cardanofw.ChainIDPrime, txHash)
	})
}

func TestE2E_ApexBridge_ValidScenarios(t *testing.T) {
	const (
		apiKey  = "test_api_key"
		userCnt = 15
	)

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(userCnt),
		cardanofw.WithUserCardanoFund(20_000_000_000),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("vector user addr: ", user.VectorAddress)
	fmt.Println("prime multisig addr: ", apex.PrimeMultisigAddr)
	fmt.Println("prime fee addr: ", apex.PrimeMultisigFeeAddr)
	fmt.Println("vector multisig addr: ", apex.VectorMultisigAddr)
	fmt.Println("vector fee addr: ", apex.VectorMultisigFeeAddr)

	t.Run("From prime to vector one by one - wait for other side", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		for i := 0; i < instances; i++ {
			prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
			require.NoError(t, err)

			fmt.Printf("%v - prevAmount %v\n", i+1, prevAmount)

			expectedAmount := new(big.Int).SetUint64(sendAmount)
			expectedAmount.Add(expectedAmount, prevAmount)

			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
				user, new(big.Int).SetUint64(sendAmount), user,
			)

			fmt.Printf("%v - Tx sent. hash: %s. %v - expectedAmount\n", i+1, txHash, expectedAmount)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 20, time.Second*10)
			require.NoError(t, err)
		}
	})

	t.Run("From prime to vector one by one", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
				user, new(big.Int).SetUint64(sendAmount), user,
			)

			fmt.Printf("Tx %v sent. hash: %s\n", i+1, txHash)
		}

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(instances))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
	})

	//nolint:dupl
	t.Run("From prime to vector parallel", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash := apex.SubmitBridgingRequest(t, ctx,
					cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
					apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
				)
				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(instances))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", instances)
	})

	t.Run("From vector to prime one by one", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			sendAmount = uint64(1_000_000)
			instances  = 5
		)

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			txHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
				user, new(big.Int).SetUint64(sendAmount), user,
			)

			fmt.Printf("Tx %v sent. hash: %s\n", i+1, txHash)
		}

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(instances))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
	})

	//nolint:dupl
	t.Run("From vector to prime parallel", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			instances  = 5
			sendAmount = uint64(1_000_000)
		)

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < instances; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				txHash := apex.SubmitBridgingRequest(t, ctx,
					cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
					apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
				)

				fmt.Printf("Tx %v sent. hash: %s\n", idx+1, txHash)
			}(i)
		}

		wg.Wait()

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(instances))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", instances)
	})

	t.Run("From prime to vector sequential and parallel", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
			sendAmount          = uint64(1_000_000)
		)

		var wg sync.WaitGroup

		prevAmount, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)

		for j := 0; j < sequentialInstances; j++ {
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)

					fmt.Printf("run: %v. Tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs\n", sequentialInstances*parallelInstances)

		expectedAmount := new(big.Int).SetUint64(sendAmount)
		expectedAmount.Mul(expectedAmount, big.NewInt(parallelInstances))
		expectedAmount.Mul(expectedAmount, big.NewInt(sequentialInstances))
		expectedAmount.Add(expectedAmount, prevAmount)

		err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmount, 100, time.Second*10)
		require.NoError(t, err)
		fmt.Printf("%v TXs confirmed\n", sequentialInstances*parallelInstances)
	})

	t.Run("From prime to vector sequential and parallel with max receivers", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			sequentialInstances = 5
			parallelInstances   = 10
			receivers           = 4
			sendAmount          = uint64(1_000_000)
		)

		var (
			wg                          sync.WaitGroup
			destinationUsers            = make([]*cardanofw.TestApexUser, receivers)
			destinationUsersPrevAmounts = make([]*big.Int, receivers)
			err                         error
		)

		for i := 0; i < receivers; i++ {
			destinationUsers[i] = apex.Users[i]

			destinationUsersPrevAmounts[i], err = apex.GetBalance(ctx, destinationUsers[i], cardanofw.ChainIDVector)
			require.NoError(t, err)
		}

		for j := 0; j < sequentialInstances; j++ {
			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), destinationUsers...,
					)
					fmt.Printf("run: %v. Tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		fmt.Printf("Waiting for %v TXs\n", sequentialInstances*parallelInstances)

		var wgResult sync.WaitGroup

		for i := 0; i < receivers; i++ {
			wgResult.Add(1)

			go func(receiverIdx int) {
				defer wgResult.Done()

				expectedAmount := new(big.Int).SetUint64(sendAmount)
				expectedAmount.Mul(expectedAmount, big.NewInt(parallelInstances))
				expectedAmount.Mul(expectedAmount, big.NewInt(sequentialInstances))
				expectedAmount.Add(expectedAmount, destinationUsersPrevAmounts[receiverIdx])

				err = apex.WaitForExactAmount(ctx, destinationUsers[receiverIdx], cardanofw.ChainIDVector, expectedAmount, 100, time.Second*10)
				require.NoError(t, err)
				fmt.Printf("%v receiver, %v TXs confirmed\n", receiverIdx, sequentialInstances*parallelInstances)
			}(i)
		}

		wgResult.Wait()
	})

	t.Run("Both directions sequential", func(t *testing.T) {
		if shouldSkip := os.Getenv("SKIP_E2E_REDUNDANT_TESTS"); shouldSkip == "true" {
			t.Skip()
		}

		const (
			instances  = 5
			sendAmount = uint64(1_000_000)
		)

		prevAmountOnVector, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)
		prevAmountOnPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		for i := 0; i < instances; i++ {
			primeTxHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
				apex.Users[0], new(big.Int).SetUint64(sendAmount), user,
			)

			fmt.Printf("prime tx %v sent. hash: %s\n", i+1, primeTxHash)

			vectorTxHash := apex.SubmitBridgingRequest(t, ctx,
				cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
				apex.Users[0], new(big.Int).SetUint64(sendAmount), user,
			)

			fmt.Printf("vector tx %v sent. hash: %s\n", i+1, vectorTxHash)
		}

		var wg sync.WaitGroup

		wg.Add(2)

		errs := make([]error, 2)

		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on vector\n", instances)

			expectedAmountOnVector := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(instances))
			expectedAmountOnVector.Add(expectedAmountOnVector, prevAmountOnVector)

			errs[0] = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 100, time.Second*10)
		}()

		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on prime\n", instances)

			expectedAmountOnPrime := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(instances))
			expectedAmountOnPrime.Add(expectedAmountOnPrime, prevAmountOnPrime)

			errs[1] = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 100, time.Second*10)
		}()

		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		fmt.Printf("%v TXs on vector confirmed\n", instances)
		fmt.Printf("%v TXs on prime confirmed\n", instances)
	})

	t.Run("Both directions sequential and parallel - one node goes off in the middle", func(t *testing.T) {
		const (
			sequentialInstances  = 5
			parallelInstances    = 6
			stopAfter            = time.Second * 60
			validatorStoppingIdx = 1
			sendAmount           = uint64(1_000_000)
		)

		prevAmountOnVector, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)
		prevAmountOnPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
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
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)

					fmt.Printf("run: %d. Prime tx %d sent. hash: %s\n", idx+1, j+1, txHash)
				}
			}(i)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)

					fmt.Printf("run: %d. Vector tx %d sent. hash: %s\n", idx+1, j+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		wg.Add(2)

		//nolint:dupl
		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on vector:\n", sequentialInstances*parallelInstances)

			expectedAmountOnVector := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(parallelInstances))
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(sequentialInstances))
			expectedAmountOnVector.Add(expectedAmountOnVector, prevAmountOnVector)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 100, time.Second*10)
			assert.NoError(t, err)

			fmt.Printf("TXs on vector expected amount received: %v\n", err)

			// nothing else should be bridged for 2 minutes
			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on vector")

			fmt.Printf("TXs on vector finished with success: %v\n", err != nil)
		}()

		//nolint:dupl
		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on prime\n", sequentialInstances*parallelInstances)

			expectedAmountOnPrime := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(parallelInstances))
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(sequentialInstances))
			expectedAmountOnPrime.Add(expectedAmountOnPrime, prevAmountOnPrime)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 100, time.Second*10)
			assert.NoError(t, err)

			fmt.Printf("TXs on prime expected amount received: %v\n", err)

			// nothing else should be bridged for 2 minutes
			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on prime")

			fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
		}()

		wg.Wait()
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

		prevAmountOnVector, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)
		prevAmountOnPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
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
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)
					fmt.Printf("run: %d. Prime tx %d sent. hash: %s\n", idx+1, j+1, txHash)
				}
			}(i)

			go func(idx int) {
				defer wg.Done()

				for j := 0; j < sequentialInstances; j++ {
					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)
					fmt.Printf("run: %d. Vector tx %d sent. hash: %s\n", idx+1, j+1, txHash)
				}
			}(i)
		}

		wg.Wait()

		wg.Add(2)

		//nolint:dupl
		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on vector:\n", sequentialInstances*parallelInstances)

			expectedAmountOnVector := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(parallelInstances))
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(sequentialInstances))
			expectedAmountOnVector.Add(expectedAmountOnVector, prevAmountOnVector)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 100, time.Second*10)
			assert.NoError(t, err)

			fmt.Printf("TXs on vector expected amount received: %v\n", err)

			if err != nil {
				return
			}

			// nothing else should be bridged for 2 minutes
			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on vector")

			fmt.Printf("TXs on vector finished with success: %v\n", err != nil)
		}()

		//nolint:dupl
		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on prime\n", sequentialInstances*parallelInstances)

			expectedAmountOnPrime := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(parallelInstances))
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(sequentialInstances))
			expectedAmountOnPrime.Add(expectedAmountOnPrime, prevAmountOnPrime)

			err = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 100, time.Second*10)
			assert.NoError(t, err)

			fmt.Printf("TXs on prime expected amount received: %v\n", err)

			if err != nil {
				return
			}

			// nothing else should be bridged for 2 minutes
			err = apex.WaitForGreaterAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 12, time.Second*10)
			assert.ErrorIs(t, err, wallet.ErrWaitForTransactionTimeout, "more tokens than expected are on prime")

			fmt.Printf("TXs on prime finished with success: %v\n", err != nil)
		}()

		wg.Wait()
	})

	t.Run("Both directions sequential and parallel", func(t *testing.T) {
		const (
			sequentialInstances = 5
			parallelInstances   = 6
		)

		prevAmountOnVector, err := apex.GetBalance(ctx, user, cardanofw.ChainIDVector)
		require.NoError(t, err)
		prevAmountOnPrime, err := apex.GetBalance(ctx, user, cardanofw.ChainIDPrime)
		require.NoError(t, err)

		sendAmount := uint64(1_000_000)

		for j := 0; j < sequentialInstances; j++ {
			var wg sync.WaitGroup

			for i := 0; i < parallelInstances; i++ {
				wg.Add(1)

				go func(run, idx int) {
					defer wg.Done()

					txHash := apex.SubmitBridgingRequest(t, ctx,
						cardanofw.ChainIDPrime, cardanofw.ChainIDVector,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
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
						cardanofw.ChainIDVector, cardanofw.ChainIDPrime,
						apex.Users[idx], new(big.Int).SetUint64(sendAmount), user,
					)

					fmt.Printf("run: %v. Vector tx %v sent. hash: %s\n", run+1, idx+1, txHash)
				}(j, i)
			}

			wg.Wait()
		}

		var wg sync.WaitGroup

		wg.Add(2)

		errs := make([]error, 2)

		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on vector\n", sequentialInstances*parallelInstances)

			expectedAmountOnVector := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(parallelInstances))
			expectedAmountOnVector.Mul(expectedAmountOnVector, big.NewInt(sequentialInstances))
			expectedAmountOnVector.Add(expectedAmountOnVector, prevAmountOnVector)

			errs[0] = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDVector, expectedAmountOnVector, 100, time.Second*10)
		}()

		go func() {
			defer wg.Done()

			fmt.Printf("Waiting for %v TXs on prime\n", sequentialInstances*parallelInstances)

			expectedAmountOnPrime := new(big.Int).SetUint64(sendAmount)
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(parallelInstances))
			expectedAmountOnPrime.Mul(expectedAmountOnPrime, big.NewInt(sequentialInstances))
			expectedAmountOnPrime.Add(expectedAmountOnPrime, prevAmountOnPrime)

			errs[1] = apex.WaitForExactAmount(ctx, user, cardanofw.ChainIDPrime, expectedAmountOnPrime, 100, time.Second*10)
		}()

		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		fmt.Printf("%v TXs on vector confirmed\n", sequentialInstances*parallelInstances)
		fmt.Printf("%v TXs on prime confirmed\n", sequentialInstances*parallelInstances)
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

	apex := cardanofw.SetupAndRunApexBridge(
		t, ctx,
		cardanofw.WithAPIKey(apiKey),
		cardanofw.WithUserCnt(userCnt),
		cardanofw.WithUserCardanoFund(100_000_000),
	)

	defer require.True(t, apex.ApexBridgeProcessesRunning())

	user := apex.Users[userCnt-1]

	txProviderPrime := apex.GetPrimeTxProvider()
	txProviderVector := apex.GetVectorTxProvider()

	fmt.Println("prime user addr: ", user.PrimeAddress)
	fmt.Println("vector user addr: ", user.VectorAddress)
	fmt.Println("prime multisig addr: ", apex.PrimeMultisigAddr)
	fmt.Println("prime fee addr: ", apex.PrimeMultisigFeeAddr)
	fmt.Println("vector multisig addr: ", apex.VectorMultisigAddr)
	fmt.Println("vector fee addr: ", apex.VectorMultisigFeeAddr)

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
						ctx, txProviderPrime, apex.Users[idx].PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
						apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
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
						ctx, txProviderPrime, apex.Users[idx].PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
						apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
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
						ctx, txProviderPrime, apex.Users[idx].PrimeWallet, sendAmount+feeAmount, apex.PrimeMultisigAddr,
						apex.PrimeCluster.NetworkConfig(), bridgingRequestMetadata)
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
						ctx, txProviderVector, apex.Users[idx].VectorWallet, sendAmount+feeAmount, apex.VectorMultisigAddr,
						apex.VectorCluster.NetworkConfig(), bridgingRequestMetadata)
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
			require.NoError(t, err)
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
