package cardanofw

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/types"
	infracommon "github.com/Ethernal-Tech/cardano-infrastructure/common"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

type CardanoChainInfo struct {
	NetworkAddress string
	OgmiosURL      string
	MultisigAddr   string
	FeeAddr        string
	SocketPath     string
	FundBlockHash  string
	FundBlockSlot  uint64
}

func (ci *CardanoChainInfo) GetTxProvider() cardanowallet.ITxProvider {
	return cardanowallet.NewTxProviderOgmios(ci.OgmiosURL)
}

type EVMChainInfo struct {
	GatewayAddress types.Address
	RelayerAddress types.Address
	JSONRPCAddr    string
	AdminKey       *crypto.ECDSAKey
	FundBlockNum   uint64
}

type ApexSystem struct {
	BridgeCluster   *framework.TestCluster
	Config          *ApexSystemConfig
	bladeAdmin      *crypto.ECDSAKey
	bladeProxyAdmin *crypto.ECDSAKey

	validators  []*TestApexValidator
	relayerNode *framework.Node

	chains []ITestApexChain

	PrimeInfo  CardanoChainInfo
	VectorInfo CardanoChainInfo
	NexusInfo  EVMChainInfo

	dataDirPath string

	bridgingAPIs []string

	FunderUser *TestApexUser
	Users      []*TestApexUser
}

func NewApexSystem(
	dataDirPath string, opts ...ApexSystemOptions,
) (*ApexSystem, error) {
	config := getDefaultApexSystemConfig()
	for _, opt := range opts {
		opt(config)
	}

	nexus, err := NewTestEVMChain(config.NexusConfig)
	if err != nil {
		return nil, err
	}

	users := make([]*TestApexUser, config.UserCnt)
	for i := range users {
		users[i], err = NewTestApexUser(
			config.PrimeConfig.NetworkType,
			config.VectorConfig.IsEnabled,
			config.VectorConfig.NetworkType,
			config.NexusConfig.IsEnabled,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create a new apex user: %w", err)
		}
	}

	apex := &ApexSystem{
		Config:      config,
		Users:       users,
		dataDirPath: dataDirPath,
		chains: []ITestApexChain{
			NewTestCardanoChain(config.PrimeConfig),
			NewTestCardanoChain(config.VectorConfig),
			nexus,
		},
	}

	apex.Config.applyPremineFundingOptions(apex.Users)

	return apex, nil
}

func (a *ApexSystem) StopAll() error {
	fmt.Println("Stopping chains...")

	errs := make([]error, len(a.chains))
	wg := sync.WaitGroup{}

	wg.Add(len(a.chains))

	for i, chain := range a.chains {
		go func(idx int, chain ITestApexChain) {
			defer wg.Done()

			errs[idx] = chain.Stop()
		}(i, chain)
	}

	if a.BridgeCluster != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			fmt.Printf("Cleaning up apex bridge\n")
			a.BridgeCluster.Stop()
			fmt.Printf("Done cleaning up apex bridge\n")
		}()
	}

	wg.Wait()

	err := errors.Join(errs...)

	fmt.Printf("Chains has been stopped...%v\n", err)

	return err
}

func (a *ApexSystem) StartChains(t *testing.T) error {
	t.Helper()

	return a.execForEachChain(func(chain ITestApexChain) error {
		return chain.RunChain(t)
	})
}

func (a *ApexSystem) StartBridgeChain(t *testing.T) {
	t.Helper()

	bladeAdmin, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	bladeProxyAdmin, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	a.bladeAdmin = bladeAdmin
	a.bladeProxyAdmin = bladeProxyAdmin
	a.BridgeCluster = framework.NewTestCluster(t, a.Config.BladeValidatorCount,
		framework.WithBladeAdmin(bladeAdmin.Address().String()),
		framework.WithProxyContractsAdmin(bladeProxyAdmin.Address().String()),
	)

	// create validators
	a.validators = make([]*TestApexValidator, a.Config.BladeValidatorCount)

	for idx := range a.validators {
		a.validators[idx] = NewTestApexValidator(
			a.dataDirPath, idx+1, a.BridgeCluster, a.BridgeCluster.Servers[idx])
	}

	a.BridgeCluster.WaitForReady(t)
}

func (a *ApexSystem) CreateWallets() (err error) {
	return a.execForEachValidator(func(i int, validator *TestApexValidator) error {
		for _, chain := range a.chains {
			if err := chain.CreateWallets(validator); err != nil {
				return fmt.Errorf("operation failed for validator = %d and chain = %s: %w",
					i, chain.ChainID(), err)
			}
		}

		return nil
	})
}

func (a *ApexSystem) CreateAddresses() error {
	// must not be parallelized because each request use same admin wallet
	for _, chain := range a.chains {
		if err := chain.CreateAddresses(a.bladeAdmin, a.GetBridgeDefaultJSONRPCAddr()); err != nil {
			return err
		}
	}

	return nil
}

func (a *ApexSystem) InitContracts(ctx context.Context) error {
	// must not be parallelized because each request use same admin wallet
	for _, chain := range a.chains {
		if err := chain.InitContracts(a.bladeAdmin, a.GetBridgeDefaultJSONRPCAddr()); err != nil {
			return err
		}
	}

	// after contracts have been initialized populate all the needed things into apex object
	for _, chain := range a.chains {
		chain.PopulateApexSystem(a)
	}

	return nil
}

func (a *ApexSystem) FundWallets(ctx context.Context) error {
	return a.execForEachChain(func(chain ITestApexChain) error {
		return chain.FundWallets(ctx)
	})
}

func (a *ApexSystem) FundChainHotWallet(ctx context.Context, chainID string, dfmAmount *big.Int) error {
	chain, err := a.getChain(chainID)
	if err != nil {
		return err
	}

	pk, err := chain.GetAdminPrivateKey()
	if err != nil {
		return err
	}

	_, err = chain.SendTx(
		ctx, pk, chain.GetHotWalletAddress(), DfmToChainNativeTokenAmount(chainID, dfmAmount), nil)

	return err
}

func (a *ApexSystem) RegisterChains() error {
	return a.execForEachValidator(func(i int, validator *TestApexValidator) error {
		for _, chain := range a.chains {
			if err := chain.RegisterChain(validator); err != nil {
				return fmt.Errorf("operation failed for validator = %d and chain = %s: %w",
					i, chain.ChainID(), err)
			}
		}

		return nil
	})
}

func (a *ApexSystem) GenerateConfigs() error {
	err := a.execForEachValidator(func(i int, validator *TestApexValidator) error {
		telemetryConfig := ""
		if i == 0 {
			telemetryConfig = a.Config.TelemetryConfig
		}

		serverIndx := i
		if a.Config.TargetOneCardanoClusterServer {
			serverIndx = 0
		}

		var args []string

		for _, chain := range a.chains {
			args = append(args, chain.GetGenerateConfigsParams(serverIndx)...)
		}

		err := validator.GenerateConfigs(a.Config.APIPortStart+i, a.Config.APIKey, telemetryConfig, args...)
		if err != nil {
			return err
		}

		if handler := a.Config.CustomOracleHandler; handler != nil {
			fileName := validator.GetValidatorComponentsConfig()
			if err := UpdateJSONFile(fileName, fileName, handler, false); err != nil {
				return err
			}
		}

		if handler := a.Config.CustomRelayerHandler; handler != nil && RunRelayerOnValidatorID == validator.ID {
			fileName := validator.GetRelayerConfig()
			if err := UpdateJSONFile(fileName, fileName, handler, false); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	return a.setBridgingAPIs()
}

func (a *ApexSystem) GetBridgeDefaultJSONRPCAddr() string {
	return a.BridgeCluster.Servers[0].JSONRPCAddr()
}

func (a *ApexSystem) GetBridgeAdmin() *crypto.ECDSAKey {
	return a.bladeAdmin
}

func (a *ApexSystem) GetBridgeProxyAdmin() *crypto.ECDSAKey {
	return a.bladeProxyAdmin
}

func (a *ApexSystem) GetValidatorsCount() int {
	return len(a.validators)
}

func (a *ApexSystem) GetValidator(t *testing.T, idx int) *TestApexValidator {
	t.Helper()

	require.True(t, idx >= 0 && idx < len(a.validators))

	return a.validators[idx]
}

func (a *ApexSystem) StartValidatorComponents(ctx context.Context) (err error) {
	for _, validator := range a.validators {
		hasAPI := a.Config.APIValidatorID == -1 || validator.ID == a.Config.APIValidatorID

		if err = validator.Start(ctx, hasAPI); err != nil {
			return err
		}
	}

	return err
}

func (a *ApexSystem) StartRelayer(ctx context.Context) (err error) {
	for _, validator := range a.validators {
		if RunRelayerOnValidatorID != validator.ID {
			continue
		}

		a.relayerNode, err = framework.NewNodeWithContext(ctx, ResolveApexBridgeBinary(), []string{
			"run-relayer",
			"--config", validator.GetRelayerConfig(),
		}, os.Stdout)
		if err != nil {
			return err
		}
	}

	return nil
}

func (a ApexSystem) StopRelayer() error {
	if a.relayerNode == nil {
		return errors.New("relayer not started")
	}

	return a.relayerNode.Stop()
}

func (a *ApexSystem) setBridgingAPIs() error {
	var bridgingAPIs []string

	for _, validator := range a.validators {
		hasAPI := a.Config.APIValidatorID == -1 || validator.ID == a.Config.APIValidatorID

		if hasAPI {
			if validator.APIPort == 0 {
				return fmt.Errorf("api port not defined")
			}

			bridgingAPIs = append(bridgingAPIs, fmt.Sprintf("http://localhost:%d", validator.APIPort))
		}
	}

	a.bridgingAPIs = bridgingAPIs

	return nil
}

func (a *ApexSystem) GetBridgingAPIs() ([]string, error) {
	if len(a.bridgingAPIs) == 0 {
		return nil, fmt.Errorf("not running API")
	}

	return a.bridgingAPIs, nil
}

func (a *ApexSystem) GetBridgingAPI() (string, error) {
	apis, err := a.GetBridgingAPIs()
	if err != nil {
		return "", err
	}

	return apis[0], nil
}

func (a *ApexSystem) ApexBridgeProcessesRunning() bool {
	if a.relayerNode == nil || a.relayerNode.ExitResult() != nil {
		return false
	}

	for _, validator := range a.validators {
		if validator.node == nil || validator.node.ExitResult() != nil {
			return false
		}
	}

	return true
}

func (a *ApexSystem) GetBalance(
	ctx context.Context, user *TestApexUser, chainID ChainID,
) (*big.Int, error) {
	chain, err := a.getChain(chainID)
	if err != nil {
		return nil, err
	}

	amount, err := chain.GetAddressBalance(ctx, user.GetAddress(chainID))
	if err != nil {
		return nil, err
	}

	return ChainNativeTokenAmountToDfm(chainID, amount), nil
}

func (a *ApexSystem) WaitForGreaterAmount(
	ctx context.Context, user *TestApexUser, chain ChainID,
	expectedAmountDfm *big.Int, numRetries int, waitTime time.Duration,
) error {
	lastAmount, err := a.WaitForAmount(ctx, user, chain, func(val *big.Int) bool {
		return val.Cmp(expectedAmountDfm) == 1
	}, numRetries, waitTime)
	if err != nil {
		return fmt.Errorf("amount mismatch: expected %s, but received %s: %w",
			expectedAmountDfm, lastAmount, err)
	}

	return nil
}

func (a *ApexSystem) WaitForExactAmount(
	ctx context.Context, user *TestApexUser, chain ChainID,
	expectedAmountDfm *big.Int, numRetries int, waitTime time.Duration,
) error {
	lastAmount, err := a.WaitForAmount(ctx, user, chain, func(val *big.Int) bool {
		return val.Cmp(expectedAmountDfm) >= 0
	}, numRetries, waitTime)
	if err != nil {
		return fmt.Errorf("amount mismatch: expected %s, but received %s: %w",
			expectedAmountDfm, lastAmount, err)
	} else if lastAmount.Cmp(expectedAmountDfm) > 0 {
		return fmt.Errorf("amount mismatch: received amount %s is greater than expected %s",
			lastAmount, expectedAmountDfm)
	}

	return nil
}

func (a *ApexSystem) WaitForAmount(
	ctx context.Context, user *TestApexUser, chain ChainID,
	cmpHandler func(*big.Int) bool, numRetries int, waitTime time.Duration,
) (*big.Int, error) {
	return infracommon.ExecuteWithRetry(ctx, func(ctx context.Context) (*big.Int, error) {
		newBalance, err := a.GetBalance(ctx, user, chain)
		if err != nil {
			return nil, err
		}

		if !cmpHandler(newBalance) {
			return newBalance, infracommon.ErrRetryTryAgain
		}

		return newBalance, nil
	}, infracommon.WithRetryCount(numRetries), infracommon.WithRetryWaitTime(waitTime))
}

func (a *ApexSystem) DefundHotWallet(
	chain ChainID, defundReceiverAddress string, defundDfm *big.Int,
) error {
	pkBytes, err := a.GetBridgeAdmin().MarshallPrivateKey()
	if err != nil {
		return err
	}

	pk := hex.EncodeToString(pkBytes)

	return RunCommand(ResolveApexBridgeBinary(), []string{
		"bridge-admin", "defund",
		"--bridge-url", a.GetBridgeDefaultJSONRPCAddr(),
		"--chain", chain,
		"--amount", defundDfm.String(),
		"--key", pk,
		"--addr", defundReceiverAddress,
	}, os.Stdout)
}

func (a *ApexSystem) SubmitTx(
	ctx context.Context, sourceChain ChainID, sender *TestApexUser,
	receiverAddr string, dfmAmount *big.Int, data []byte,
) (string, error) {
	privateKey, err := sender.GetPrivateKey(sourceChain)
	if err != nil {
		return "", err
	}

	chain, err := a.getChain(sourceChain)
	if err != nil {
		return "", err
	}

	return chain.SendTx(
		ctx, privateKey, receiverAddr,
		DfmToChainNativeTokenAmount(sourceChain, dfmAmount), data)
}

func (a *ApexSystem) SubmitBridgingRequest(
	t *testing.T, ctx context.Context,
	sourceChain ChainID, destinationChain ChainID,
	sender *TestApexUser, dfmAmount *big.Int, receivers ...*TestApexUser,
) string {
	t.Helper()

	require.True(t, sourceChain != destinationChain)

	// check if sourceChain is supported
	require.True(t,
		sourceChain == ChainIDPrime ||
			sourceChain == ChainIDVector ||
			sourceChain == ChainIDNexus,
	)

	// check if destinationChain is supported
	require.True(t,
		destinationChain == ChainIDPrime ||
			destinationChain == ChainIDVector ||
			destinationChain == ChainIDNexus,
	)

	// check if bridging direction is supported
	require.False(t,
		!a.Config.VectorConfig.IsEnabled && (sourceChain == ChainIDVector || destinationChain == ChainIDVector))
	require.False(t,
		!a.Config.NexusConfig.IsEnabled && (sourceChain == ChainIDNexus || destinationChain == ChainIDNexus))
	require.True(t,
		sourceChain == ChainIDPrime ||
			(sourceChain == ChainIDVector && destinationChain == ChainIDPrime) ||
			(sourceChain == ChainIDNexus && destinationChain == ChainIDPrime),
	)

	// check if number of receivers is valid
	require.Greater(t, len(receivers), 0)
	require.Less(t, len(receivers), 5)

	const feeAmountDfm = uint64(1_100_000)

	feeAmount := DfmToChainNativeTokenAmount(sourceChain, new(big.Int).SetUint64(feeAmountDfm))

	receiversMap := make(map[string]*big.Int, len(receivers))

	for _, receiver := range receivers {
		require.True(t, destinationChain != ChainIDVector || receiver.HasVectorWallet)
		require.True(t, destinationChain != ChainIDNexus || receiver.HasNexusWallet)

		receiversMap[receiver.GetAddress(destinationChain)] = DfmToChainNativeTokenAmount(sourceChain, dfmAmount)
	}

	// check if users are valid for the bridging - do they have necessary wallets
	require.True(t, sourceChain != ChainIDVector || sender.HasVectorWallet)
	require.True(t, sourceChain != ChainIDNexus || sender.HasNexusWallet)

	privateKey, err := sender.GetPrivateKey(sourceChain)
	require.NoError(t, err)

	txHash, err := a.GetChainMust(t, sourceChain).BridgingRequest(
		ctx, destinationChain, privateKey, receiversMap, feeAmount)
	require.NoError(t, err)

	return txHash
}

func (a *ApexSystem) GetChainMust(t *testing.T, chainID string) ITestApexChain {
	t.Helper()

	chain, err := a.getChain(chainID)
	require.NoError(t, err)

	return chain
}

func (a *ApexSystem) execForEachChain(handler func(chain ITestApexChain) error) error {
	errs := make([]error, len(a.chains))
	wg := &sync.WaitGroup{}

	wg.Add(len(a.chains))

	for i, ch := range a.chains {
		go func(idx int, chain ITestApexChain) {
			defer wg.Done()

			if err := handler(chain); err != nil {
				errs[idx] = fmt.Errorf("operation failed for chain %s: %w", chain.ChainID(), err)
			}
		}(i, ch)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (a *ApexSystem) execForEachValidator(handler func(i int, validator *TestApexValidator) error) error {
	errs := make([]error, len(a.validators))
	wg := &sync.WaitGroup{}

	wg.Add(len(a.validators))

	for i, valid := range a.validators {
		go func(idx int, validator *TestApexValidator) {
			defer wg.Done()

			if err := handler(idx, validator); err != nil {
				errs[idx] = fmt.Errorf("operation failed for validator = %d: %w", idx, err)
			}
		}(i, valid)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (a *ApexSystem) getChain(chainID string) (ITestApexChain, error) {
	for _, chain := range a.chains {
		if chain.ChainID() == chainID {
			return chain, nil
		}
	}

	return nil, fmt.Errorf("unknown chain: %s", chainID)
}
