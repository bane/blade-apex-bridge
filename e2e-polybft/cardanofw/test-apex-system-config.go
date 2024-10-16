package cardanofw

import (
	"encoding/hex"

	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/types"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

type ChainID = string

const (
	ChainIDPrime  ChainID = "prime"
	ChainIDVector ChainID = "vector"
	ChainIDNexus  ChainID = "nexus"

	RunRelayerOnValidatorID = 1

	defaultFundTokenAmount    = uint64(100_000_000_000)
	defaultFundEthTokenAmount = uint64(100_000)
)

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
	NexusInitialFundsKeys []types.Address

	CustomOracleHandler  func(mp map[string]interface{})
	CustomRelayerHandler func(mp map[string]interface{})

	FundTokenAmount    uint64
	FundEthTokenAmount uint64

	UserCnt         uint
	UserCardanoFund uint64
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

func WithUserCnt(userCnt uint) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.UserCnt = userCnt
	}
}

func WithUserCardanoFund(userCardanoFund uint64) ApexSystemOptions {
	return func(h *ApexSystemConfig) {
		h.UserCardanoFund = userCardanoFund
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

		UserCnt:         10,
		UserCardanoFund: 5_000_000,
	}
}

func (asc *ApexSystemConfig) ServiceCount() int {
	// Prime
	count := 1

	if asc.VectorEnabled {
		count++
	}

	if asc.NexusEnabled {
		count++
	}

	return count
}

func (asc *ApexSystemConfig) applyPremineFundingOptions(users []*TestApexUser) {
	if len(asc.PrimeClusterConfig.InitialFundsKeys) == 0 {
		asc.PrimeClusterConfig.InitialFundsKeys = make([]string, 0, len(users))
	}

	if len(asc.VectorClusterConfig.InitialFundsKeys) == 0 {
		asc.VectorClusterConfig.InitialFundsKeys = make([]string, 0, len(users))
	}

	if len(asc.NexusInitialFundsKeys) == 0 {
		asc.NexusInitialFundsKeys = make([]types.Address, 0, len(users))
	}

	if asc.PrimeClusterConfig.InitialFundsAmount == 0 {
		asc.PrimeClusterConfig.InitialFundsAmount = asc.UserCardanoFund
	}

	if asc.VectorClusterConfig.InitialFundsAmount == 0 {
		asc.VectorClusterConfig.InitialFundsAmount = asc.UserCardanoFund
	}

	for _, user := range users {
		asc.PrimeClusterConfig.InitialFundsKeys = append(asc.PrimeClusterConfig.InitialFundsKeys,
			hex.EncodeToString(user.PrimeAddress.Bytes()))

		if user.HasVectorWallet {
			asc.VectorClusterConfig.InitialFundsKeys = append(asc.VectorClusterConfig.InitialFundsKeys,
				hex.EncodeToString(user.VectorAddress.Bytes()))
		}

		if user.HasNexusWallet {
			asc.NexusInitialFundsKeys = append(asc.NexusInitialFundsKeys, user.NexusAddress)
		}
	}
}
