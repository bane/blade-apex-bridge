package cardanofw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/types"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

type RunCardanoClusterConfig struct {
	ID                 int
	NodesCount         int
	NetworkType        cardanowallet.CardanoNetworkType
	InitialFundsKeys   []string
	InitialFundsAmount uint64
}

type ApexSystem struct {
	PrimeCluster  *TestCardanoCluster
	VectorCluster *TestCardanoCluster
	Nexus         *TestEVMBridge
	Bridge        *TestCardanoBridge

	Config *ApexSystemConfig
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

func newApexSystemConfig(opts ...ApexSystemOptions) *ApexSystemConfig {
	config := &ApexSystemConfig{
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
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

func RunApexBridge(
	t *testing.T, ctx context.Context,
	apexOptions ...ApexSystemOptions,
) *ApexSystem {
	t.Helper()

	apexConfig := newApexSystemConfig(apexOptions...)
	apexSystem := &ApexSystem{Config: apexConfig}
	wg := &sync.WaitGroup{}
	serviceCount := apexConfig.ServiceCount()
	errorsContainer := [3]error{}

	wg.Add(serviceCount)

	go func() {
		defer wg.Done()

		apexSystem.PrimeCluster, errorsContainer[0] = RunCardanoCluster(t, ctx, apexConfig.PrimeClusterConfig)
	}()

	if apexConfig.VectorEnabled {
		go func() {
			defer wg.Done()

			apexSystem.VectorCluster, errorsContainer[1] = RunCardanoCluster(t, ctx, apexConfig.VectorClusterConfig)
		}()
	}

	if apexConfig.NexusEnabled {
		go func() {
			defer wg.Done()

			apexSystem.Nexus, errorsContainer[2] = RunEVMChain(t, apexConfig)
		}()
	}

	t.Cleanup(func() {
		fmt.Println("Stopping chains...")

		if apexSystem.PrimeCluster != nil {
			wg.Add(1)

			go func() {
				defer wg.Done()

				errorsContainer[0] = apexSystem.PrimeCluster.Stop()
			}()
		}

		if apexSystem.VectorCluster != nil {
			wg.Add(1)

			go func() {
				defer wg.Done()

				errorsContainer[1] = apexSystem.VectorCluster.Stop()
			}()
		}

		if apexSystem.Nexus != nil {
			wg.Add(1)

			go func() {
				defer wg.Done()

				apexSystem.Nexus.Cluster.Stop()
			}()
		}

		if apexSystem.Bridge != nil {
			wg.Add(1)

			go func() {
				defer wg.Done()

				fmt.Printf("Cleaning up apex bridge\n")
				apexSystem.Bridge.StopValidators()
				fmt.Printf("Done cleaning up apex bridge\n")
			}()
		}

		wg.Wait()

		fmt.Printf("Chains has been stopped...%v\n", errors.Join(errorsContainer[:]...))
	})

	fmt.Println("Starting chains...")

	wg.Wait()

	require.NoError(t, errors.Join(errorsContainer[:]...))

	fmt.Println("Chains have been started...")

	apexSystem.Bridge = SetupAndRunApexBridge(
		t, ctx,
		// path.Join(path.Dir(primeCluster.Config.TmpDir), "bridge"),
		"../../e2e-bridge-data-tmp-"+t.Name(),
		apexSystem,
	)

	if apexConfig.NexusEnabled {
		SetupAndRunNexusBridge(
			t, ctx,
			apexSystem,
		)
	}

	apexSystem.SetupAndRunValidatorsAndRelayer(t, ctx)

	fmt.Printf("Apex bridge setup done\n")

	return apexSystem
}
