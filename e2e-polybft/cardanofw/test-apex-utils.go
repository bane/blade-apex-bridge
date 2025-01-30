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
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

const (
	ChainTypeCardano = iota
	ChainTypeEVM

	BatchStateFailedToExecute           = "FailedToExecuteOnDestination"
	BatchStateIncludedInBatch           = "IncludedInBatch"
	BatchStateExecuted                  = "ExecutedOnDestination"
	BridgingRequestStatusInvalidRequest = "InvalidRequest"

	minUTxODefaultValue         = uint64(1_043_020)
	ttlSlotNumberInc            = 500
	potentialFee                = 500_000
	bridgingFeeAmount           = uint64(1_100_000)
	defaultMinBridgingFeeAmount = uint64(1_043_020)
)

func ResolveCardanoCliBinary(networkID wallet.CardanoNetworkType) string {
	var env, name string

	switch networkID {
	case wallet.VectorMainNetNetwork, wallet.VectorTestNetNetwork, wallet.CardanoTestNetwork:
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
	case wallet.VectorMainNetNetwork, wallet.VectorTestNetNetwork, wallet.CardanoTestNetwork:
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
	case wallet.VectorMainNetNetwork, wallet.VectorTestNetNetwork, wallet.CardanoTestNetwork:
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

func GetBridgingRequestState(ctx context.Context, requestURL string, apiKey string) (
	*BridgingRequestStateResponse, error,
) {
	return GetAPIRequestGeneric[*BridgingRequestStateResponse](ctx, requestURL, apiKey)
}

func GetOracleState(ctx context.Context, requestURL string, apiKey string) (
	*OracleStateResponse, error,
) {
	return GetAPIRequestGeneric[*OracleStateResponse](ctx, requestURL, apiKey)
}

func GetAPIRequestGeneric[T any](ctx context.Context, requestURL string, apiKey string) (t T, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return t, err
	}

	req.Header.Set("X-API-KEY", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return t, err
	} else if resp.StatusCode != http.StatusOK {
		return t, fmt.Errorf("http status for %s code is %d", requestURL, resp.StatusCode)
	}

	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return t, err
	}

	var responseModel T

	err = json.Unmarshal(resBody, &responseModel)
	if err != nil {
		return t, err
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

type CardanoChainConfigUtxo struct {
	Hash    [32]byte `json:"id"`
	Index   uint32   `json:"index"`
	Address string   `json:"address"`
	Amount  uint64   `json:"amount"`
	Slot    uint64   `json:"slot"`
}

type OracleStateResponse struct {
	ChainID   string                   `json:"chainID"`
	Utxos     []CardanoChainConfigUtxo `json:"utxos"`
	BlockSlot uint64                   `json:"slot"`
	BlockHash string                   `json:"hash"`
}

func GetNetworkMagic(networkType wallet.CardanoNetworkType) uint {
	switch networkType {
	case wallet.VectorTestNetNetwork, wallet.CardanoTestNetwork:
		return wallet.VectorTestNetProtocolMagic
	case wallet.VectorMainNetNetwork:
		return wallet.VectorMainNetProtocolMagic
	case wallet.MainNetNetwork:
		return wallet.PrimeMainNetProtocolMagic
	case wallet.TestNetNetwork:
		return wallet.PrimeTestNetProtocolMagic
	default:
		return 0
	}
}

func GetNetworkName(networkType wallet.CardanoNetworkType) string {
	switch networkType {
	case wallet.VectorTestNetNetwork:
		return ChainIDCardano
		// return ChainIDVector
	case wallet.VectorMainNetNetwork:
		return ChainIDVector
	case wallet.MainNetNetwork:
		return ChainIDPrime
	case wallet.TestNetNetwork:
		return ChainIDPrime
	case wallet.CardanoTestNetwork:
		return ChainIDCardano
	default:
		return ""
	}
}

func GetAddress(networkType wallet.CardanoNetworkType, cardanoWallet *wallet.Wallet) (*wallet.CardanoAddress, error) {
	if len(cardanoWallet.StakeVerificationKey) > 0 {
		return wallet.NewBaseAddress(networkType,
			cardanoWallet.VerificationKey, cardanoWallet.StakeVerificationKey)
	}

	return wallet.NewEnterpriseAddress(networkType, cardanoWallet.VerificationKey)
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

func GetLogsFile(t *testing.T, filePath string, withStdout bool) io.Writer {
	t.Helper()

	var writers []io.Writer

	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		t.Log("failed to create log file", "err", err, "file", filePath)
	} else {
		writers = append(writers, f)

		t.Cleanup(func() {
			if err := f.Close(); err != nil {
				t.Log("GetStdout close file error", "err", err)
			}
		})
	}

	if withStdout {
		writers = append(writers, os.Stdout)
	}

	if len(writers) == 0 {
		return io.Discard
	}

	return io.MultiWriter(writers...)
}

func IsEnvVarTrue(name string) bool {
	return os.Getenv(name) == "true"
}

func ShouldSkipE2RRedundantTests() bool {
	return IsEnvVarTrue("SKIP_E2E_REDUNDANT_TESTS")
}

func WaitForRequestStateGeneric(
	ctx context.Context, apex *ApexSystem, chainID string, txHash string,
	apiKey string, timeout time.Duration, handler func(status string) bool,
) error {
	apiURL, err := apex.GetBridgingAPI()
	if err != nil {
		return err
	}

	var (
		requestURL = fmt.Sprintf(
			"%s/api/BridgingRequestState/Get?chainId=%s&txHash=%s", apiURL, chainID, txHash)
		currentStatus string
	)

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for {
		select {
		case <-timeoutTimer.C:
			fmt.Printf("Timeout\n")

			return errors.New("timeout")
		case <-ctx.Done():
			return errors.New("context done")
		case <-time.After(time.Millisecond * 500):
		}

		currentState, err := GetBridgingRequestState(ctx, requestURL, apiKey)
		if err != nil {
			continue
		}

		if currentStatus != currentState.Status {
			currentStatus = currentState.Status
			fmt.Printf("currentStatus = %s\n", currentStatus)

			if finished := handler(currentStatus); finished {
				return nil
			}
		}
	}
}

func WaitForBatchState(
	ctx context.Context, apex *ApexSystem, chainID string, txHash string,
	apiKey string, breakIfFailed bool, failAtLeastOnce bool, batchState string,
) (int, bool) {
	failedToExecuteCount := 0
	err := WaitForRequestStateGeneric(ctx, apex, chainID, txHash, apiKey, time.Second*300, func(status string) bool {
		if status == BatchStateFailedToExecute {
			failedToExecuteCount++

			if breakIfFailed {
				return true
			}
		}

		return status == batchState && (!failAtLeastOnce || failedToExecuteCount > 0)
	})

	return failedToExecuteCount, err != nil
}

func WaitForRequestStates(
	ctx context.Context, apex *ApexSystem, chainID string, txHash string,
	apiKey string, expectedStates []string, timeoutSec uint,
) (string, error) {
	selectedState := ""
	timeoutTime := time.Duration(timeoutSec) * time.Second
	err := WaitForRequestStateGeneric(ctx, apex, chainID, txHash, apiKey, timeoutTime, func(status string) bool {
		if len(expectedStates) == 0 {
			selectedState = status

			return true
		}

		for _, expectedState := range expectedStates {
			if strings.Compare(status, expectedState) == 0 {
				selectedState = expectedState

				return true
			}
		}

		return false
	})

	return selectedState, err
}

func WaitForInvalidState(
	t *testing.T, ctx context.Context, apex *ApexSystem, chainID string, txHash string, apiKey string) {
	t.Helper()

	state, err := WaitForRequestStates(
		ctx, apex, chainID, txHash, apiKey, []string{BridgingRequestStatusInvalidRequest}, 300)
	require.NoError(t, err)
	require.Equal(t, BridgingRequestStatusInvalidRequest, state)
}

func GetGenesisWalletFromCluster(
	dirPath string,
	keyID uint,
) (*wallet.Wallet, error) {
	keyFileName := strings.Join([]string{"utxo", fmt.Sprint(keyID)}, "")

	sKey, err := wallet.NewKey(filepath.Join(dirPath, "utxo-keys", fmt.Sprintf("%s.skey", keyFileName)))
	if err != nil {
		return nil, err
	}

	sKeyBytes, err := sKey.GetKeyBytes()
	if err != nil {
		return nil, err
	}

	vKey, err := wallet.NewKey(filepath.Join(dirPath, "utxo-keys", fmt.Sprintf("%s.vkey", keyFileName)))
	if err != nil {
		return nil, err
	}

	vKeyBytes, err := vKey.GetKeyBytes()
	if err != nil {
		return nil, err
	}

	return wallet.NewWallet(vKeyBytes, sKeyBytes), nil
}
