package cardanofw

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/types"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

const (
	ChainIDPrime  = "prime"
	ChainIDVector = "vector"
	ChainIDNexus  = "nexus"

	RunRelayerOnValidatorID = 1

	defaultFundTokenAmount    = uint64(100_000_000_000)
	defaultFundEthTokenAmount = uint64(100_000)
)

type RunCardanoClusterConfig struct {
	ID                 int
	NodesCount         int
	NetworkType        cardanowallet.CardanoNetworkType
	InitialFundsKeys   []string
	InitialFundsAmount uint64
}

type ApexSystemConfig struct {
	// Apex
	VectorEnabled bool

	APIValidatorID int // -1 all validators
	APIPortStart   int
	APIKey         string

	VectorTTLInc                uint64
	VectorSlotRoundingThreshold uint64
	PrimeTTLInc                 uint64
	PrimeSlotRoundingThreshold  uint64

	TelemetryConfig               string
	TargetOneCardanoClusterServer bool

	BladeValidatorCount int

	PrimeClusterConfig  *RunCardanoClusterConfig
	VectorClusterConfig *RunCardanoClusterConfig

	// Nexus EVM
	NexusEnabled bool

	NexusValidatorCount   int
	NexusStartingPort     int64
	NexusBurnContractInfo *polybft.BurnContractInfo

	CustomOracleHandler  func(mp map[string]interface{})
	CustomRelayerHandler func(mp map[string]interface{})

	FundTokenAmount    uint64
	FundEthTokenAmount uint64
}

type ApexSystemOptions func(*ApexSystemConfig)

func WithAPIValidatorID(apiValidatorID int) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.APIValidatorID = apiValidatorID
	}
}

func WithAPIPortStart(apiPortStart int) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.APIPortStart = apiPortStart
	}
}

func WithAPIKey(apiKey string) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.APIKey = apiKey
	}
}

func WithVectorTTL(threshold, ttlInc uint64) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.VectorSlotRoundingThreshold = threshold
		h.VectorTTLInc = ttlInc
	}
}

func WithPrimeTTL(threshold, ttlInc uint64) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.PrimeSlotRoundingThreshold = threshold
		h.PrimeTTLInc = ttlInc
	}
}

func WithTelemetryConfig(tc string) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.TelemetryConfig = tc // something like "0.0.0.0:5001,localhost:8126"
	}
}

func WithTargetOneCardanoClusterServer(targetOneCardanoClusterServer bool) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.TargetOneCardanoClusterServer = targetOneCardanoClusterServer
	}
}

func WithVectorEnabled(enabled bool) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.VectorEnabled = enabled
	}
}

func WithNexusEnabled(enabled bool) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.NexusEnabled = enabled
	}
}

func WithNexusStartintPort(port int64) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.NexusStartingPort = port
	}
}

func WithNexusBurningContract(contractInfo *polybft.BurnContractInfo) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.NexusBurnContractInfo = contractInfo
	}
}

func WithPrimeClusterConfig(config *RunCardanoClusterConfig) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.PrimeClusterConfig = config
	}
}

func WithVectorClusterConfig(config *RunCardanoClusterConfig) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.VectorClusterConfig = config
	}
}

func WithCustomConfigHandlers(callbackOracle, callbackRelayer func(mp map[string]interface{})) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.CustomOracleHandler = callbackOracle
		h.CustomRelayerHandler = callbackRelayer
	}
}

func getDefaultApexSystemConfig() *ApexSystemConfig {
	return &ApexSystemConfig{
		APIValidatorID: 1,
		APIPortStart:   40000,
		APIKey:         "test_api_key",

		BladeValidatorCount: 4,

		PrimeClusterConfig: &RunCardanoClusterConfig{
			ID:          0,
			NodesCount:  4,
			NetworkType: cardanowallet.TestNetNetwork,
		},
		VectorClusterConfig: &RunCardanoClusterConfig{
			ID:          1,
			NodesCount:  4,
			NetworkType: cardanowallet.VectorTestNetNetwork,
		},

		NexusValidatorCount: 4,
		NexusStartingPort:   int64(30400),

		VectorEnabled: true,
		NexusEnabled:  false,
		NexusBurnContractInfo: &polybft.BurnContractInfo{
			BlockNumber: 0,
			Address:     types.ZeroAddress,
		},

		CustomOracleHandler:  nil,
		CustomRelayerHandler: nil,

		FundTokenAmount:    defaultFundTokenAmount,
		FundEthTokenAmount: defaultFundEthTokenAmount,
	}
}

func (as *ApexSystemConfig) ServiceCount() int {
	// Prime
	count := 1

	if as.VectorEnabled {
		count++
	}

	if as.NexusEnabled {
		count++
	}

	return count
}

func RunCardanoCluster(
	t *testing.T,
	config *RunCardanoClusterConfig,
) (*TestCardanoCluster, error) {
	t.Helper()

	networkMagic := GetNetworkMagic(config.NetworkType)
	networkName := GetNetworkName(config.NetworkType)
	ogmiosLogsFilePath := filepath.Join("..", "..", "e2e-logs-cardano",
		fmt.Sprintf("ogmios-%s-%s.log", networkName, t.Name()))

	cluster, err := NewCardanoTestCluster(
		WithID(config.ID+1),
		WithNodesCount(config.NodesCount),
		WithStartTimeDelay(time.Second*5),
		WithPort(5100+config.ID*100),
		WithOgmiosPort(1337+config.ID),
		WithNetworkMagic(networkMagic),
		WithNetworkType(config.NetworkType),
		WithConfigGenesisDir(networkName),
		WithInitialFunds(config.InitialFundsKeys, config.InitialFundsAmount),
	)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Waiting for sockets to be ready\n")

	if err := cluster.WaitForReady(time.Minute * 2); err != nil {
		return nil, err
	}

	if err := cluster.StartOgmios(config.ID, GetLogsFile(t, ogmiosLogsFilePath, false)); err != nil {
		return nil, err
	}

	if err := cluster.WaitForBlockWithState(10, time.Second*120); err != nil {
		return nil, err
	}

	fmt.Printf("Cluster %d is ready\n", config.ID)

	return cluster, nil
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
