package cardanofw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

const (
	InvalidState = "InvalidRequest"

	retryWait       = time.Millisecond * 1000
	retriesMaxCount = 10
)

func ResolveCardanoCliBinary(networkID wallet.CardanoNetworkType) string {
	var env, name string

	switch networkID {
	case wallet.VectorMainNetNetwork, wallet.VectorTestNetNetwork:
		env = "CARDANO_CLI_BINARY_VECTOR"
		name = "vector-cli"
	default:
		env = "CARDANO_CLI_BINARY"
		name = "cardano-cli"
	}

	return tryResolveFromEnv(env, name)
}

func ResolveOgmiosBinary(networkID wallet.CardanoNetworkType) string {
	var env, name string

	switch networkID {
	case wallet.VectorMainNetNetwork, wallet.VectorTestNetNetwork:
		env = "OGMIOS_BINARY_VECTOR"
		name = "vector-ogmios"
	default:
		env = "OGMIOS"
		name = "ogmios"
	}

	return tryResolveFromEnv(env, name)
}

func ResolveCardanoNodeBinary(networkID wallet.CardanoNetworkType) string {
	var env, name string

	switch networkID {
	case wallet.VectorMainNetNetwork, wallet.VectorTestNetNetwork:
		env = "CARDANO_NODE_BINARY_VECTOR"
		name = "vector-node"
	default:
		env = "CARDANO_NODE_BINARY_VECTOR"
		name = "cardano-node"
	}

	return tryResolveFromEnv(env, name)
}

func ResolveApexBridgeBinary() string {
	return tryResolveFromEnv("APEX_BRIDGE_BINARY", "apex-bridge")
}

func ResolveBladeBinary() string {
	return tryResolveFromEnv("BLADE_BINARY", "blade")
}

func RunCommandContext(
	ctx context.Context, binary string, args []string, stdout io.Writer, envVariables ...string,
) error {
	cmd := exec.CommandContext(ctx, binary, args...)

	return runCommand(cmd, stdout, envVariables...)
}

// runCommand executes command with given arguments
func RunCommand(binary string, args []string, stdout io.Writer, envVariables ...string) error {
	cmd := exec.Command(binary, args...)

	return runCommand(cmd, stdout, envVariables...)
}

func runCommand(cmd *exec.Cmd, stdout io.Writer, envVariables ...string) error {
	var stdErr bytes.Buffer

	cmd.Stderr = &stdErr
	cmd.Stdout = stdout

	cmd.Env = append(os.Environ(), envVariables...)

	if err := cmd.Run(); err != nil {
		if stdErr.Len() > 0 {
			return fmt.Errorf("failed to execute command: %s", stdErr.String())
		}

		return fmt.Errorf("failed to execute command: %w", err)
	}

	if stdErr.Len() > 0 {
		return fmt.Errorf("error during command execution: %s", stdErr.String())
	}

	return nil
}

func LoadJSON[TReturn any](path string) (*TReturn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %v. error: %w", path, err)
	}

	defer f.Close()

	var value TReturn

	if err = json.NewDecoder(f).Decode(&value); err != nil {
		return nil, fmt.Errorf("failed to decode %v. error: %w", path, err)
	}

	return &value, nil
}

func WaitUntil(
	t *testing.T,
	ctx context.Context, provider wallet.ITxProvider,
	timeoutDuration time.Duration,
	handler func(wallet.QueryTipData) bool,
) error {
	t.Helper()

	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()

	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout")
		case <-ticker.C:
		}

		tip, err := provider.GetTip(ctx)
		if err != nil {
			t.Log("error while retrieving tip", "err", err)
		} else if handler(tip) {
			return nil
		}
	}
}

func WaitUntilBlock(
	t *testing.T,
	ctx context.Context, provider wallet.ITxProvider,
	blockNum uint64, timeoutDuration time.Duration,
) error {
	t.Helper()

	return WaitUntil(t, ctx, provider, timeoutDuration, func(qtd wallet.QueryTipData) bool {
		return qtd.Block >= blockNum
	})
}

// SplitString splits large string into slice of substrings
func SplitString(s string, mxlen int) (res []string) {
	for i := 0; i < len(s); i += mxlen {
		end := i + mxlen
		if end > len(s) {
			end = len(s)
		}

		res = append(res, s[i:end])
	}

	return res
}

func WaitForRequestState(expectedState string, ctx context.Context, requestURL string, apiKey string,
	timeout uint) (string, error) {
	var (
		currentState *BridgingRequestStateResponse
		err          error
	)

	timeoutTimer := time.NewTimer(time.Second * time.Duration(timeout))
	defer timeoutTimer.Stop()

	for {
		select {
		case <-timeoutTimer.C:
			fmt.Printf("Timeout\n")

			return "", errors.New("Timeout")
		case <-ctx.Done():
			fmt.Printf("Done\n")

			return "", errors.New("Done")
		case <-time.After(time.Millisecond * 500):
		}

		currentState, err = GetBridgingRequestState(ctx, requestURL, apiKey)
		if err != nil {
			fmt.Println("error requesting bridging state", err)

			continue
		} else if currentState == nil {
			fmt.Println("empty currentState")

			continue
		}

		fmt.Println(currentState.Status)

		if strings.Compare(currentState.Status, expectedState) == 0 {
			return currentState.Status, nil
		}
	}
}

func WaitForInvalidState(t *testing.T, ctx context.Context, apiURL string, apiKey string, txHash string) {
	t.Helper()

	requestURL := fmt.Sprintf(
		"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, "prime", txHash)

	state, err := WaitForRequestState(InvalidState, ctx, requestURL, apiKey, 300)
	require.NoError(t, err)
	require.Equal(t, InvalidState, state)
}

func GetBridgingRequestState(ctx context.Context, requestURL string, apiKey string) (
	*BridgingRequestStateResponse, error,
) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-KEY", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status for %s code is %d", requestURL, resp.StatusCode)
	}

	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var responseModel *BridgingRequestStateResponse

	err = json.Unmarshal(resBody, &responseModel)
	if err != nil {
		return nil, err
	}

	return responseModel, nil
}

type BridgingRequestStateResponse struct {
	SourceChainID      string `json:"sourceChainId"`
	SourceTxHash       string `json:"sourceTxHash"`
	DestinationChainID string `json:"destinationChainId"`
	Status             string `json:"status"`
	DestinationTxHash  string `json:"destinationTxHash"`
}

// GetTokenAmount returns token amount for address
func GetTokenAmount(ctx context.Context, txProvider wallet.ITxProvider, addr string) (uint64, error) {
	var utxos []wallet.Utxo

	err := ExecuteWithRetryIfNeeded(ctx, func() (err error) {
		utxos, err = txProvider.GetUtxos(ctx, addr)

		return err
	})
	if err != nil {
		return 0, err
	}

	return wallet.GetUtxosSum(utxos), nil
}

// WaitForAmount waits for address to have amount specified by cmpHandler
func WaitForAmount(ctx context.Context, txRetriever wallet.IUTxORetriever,
	addr string, cmpHandler func(uint64) bool, numRetries int, waitTime time.Duration,
) error {
	return wallet.WaitForAmount(ctx, txRetriever, addr, cmpHandler, numRetries, waitTime, IsRecoverableError)
}

func ExecuteWithRetryIfNeeded(ctx context.Context, handler func() error) error {
	for i := 1; ; i++ {
		err := handler()
		if err == nil || !IsRecoverableError(err) {
			return err
		} else if i == retriesMaxCount {
			return fmt.Errorf("execution failed after %d retries: %w", retriesMaxCount, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryWait):
		}
	}
}

func IsRecoverableError(err error) bool {
	return strings.Contains(err.Error(), "status code 500")
}

func GetDestinationChainID(isSourcePrime bool) string {
	if isSourcePrime {
		return "vector"
	}

	return "prime"
}

func GetNetworkID(isSourcePrime bool) wallet.CardanoNetworkType {
	// if isSourcePrime { return wallet.TestNetNetwork } return wallet.VectorTestNetNetwork with apex cardano-node
	return wallet.TestNetNetwork
}

func GetNetworkMagic(isSourcePrime bool) uint {
	if isSourcePrime {
		return 3311
	}

	return 1127
}

func GetAddress(isSourcePrime bool, cardanoWallet wallet.IWallet) (wallet.CardanoAddress, error) {
	if len(cardanoWallet.GetStakeVerificationKey()) > 0 {
		return wallet.NewBaseAddress(GetNetworkID(isSourcePrime),
			cardanoWallet.GetVerificationKey(), cardanoWallet.GetStakeVerificationKey())
	}

	return wallet.NewEnterpriseAddress(GetNetworkID(isSourcePrime), cardanoWallet.GetVerificationKey())
}

func GetTestNetMagicArgs(testnetMagic uint) []string {
	if testnetMagic == 0 || testnetMagic == wallet.MainNetProtocolMagic {
		return []string{"--mainnet"}
	}

	return []string{"--testnet-magic", strconv.FormatUint(uint64(testnetMagic), 10)}
}

func tryResolveFromEnv(env, name string) string {
	if bin := os.Getenv(env); bin != "" {
		return bin
	}
	// fallback
	return name
}
