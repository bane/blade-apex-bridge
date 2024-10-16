package cardanofw

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/types"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

type ApexSystem struct {
	PrimeCluster  *TestCardanoCluster
	VectorCluster *TestCardanoCluster
	Nexus         *TestEVMBridge
	BridgeCluster *framework.TestCluster
	Config        *ApexSystemConfig

	validators  []*TestApexValidator
	relayerNode *framework.Node

	PrimeMultisigAddr     string
	PrimeMultisigFeeAddr  string
	VectorMultisigAddr    string
	VectorMultisigFeeAddr string

	nexusRelayerWallet *crypto.ECDSAKey
	bladeAdmin         *crypto.ECDSAKey

	dataDirPath string

	Users []*TestApexUser
}

func NewApexSystem(
	dataDirPath string, opts ...ApexSystemOptions,
) (*ApexSystem, error) {
	config := getDefaultApexSystemConfig()
	for _, opt := range opts {
		opt(config)
	}

	apex := &ApexSystem{
		Config:      config,
		dataDirPath: dataDirPath,
	}

	if err := apex.createApexUsers(); err != nil {
		return nil, err
	}

	apex.Config.applyPremineFundingOptions(apex.Users)

	return apex, nil
}

func (a *ApexSystem) createApexUsers() (err error) {
	a.Users = make([]*TestApexUser, a.Config.UserCnt)

	for i := 0; i < int(a.Config.UserCnt); i++ {
		a.Users[i], err = NewTestApexUser(
			a.Config.PrimeClusterConfig.NetworkType,
			a.Config.VectorEnabled,
			a.Config.VectorClusterConfig.NetworkType,
			a.Config.NexusEnabled,
		)
		if err != nil {
			return fmt.Errorf("failed to create a new apex user: %w", err)
		}
	}

	return nil
}

func (a *ApexSystem) StopChains() {
	wg := sync.WaitGroup{}
	errorsContainer := [2]error{}

	fmt.Println("Stopping chains...")

	if a.PrimeCluster != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errorsContainer[0] = a.PrimeCluster.Stop()
		}()
	}

	if a.VectorCluster != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errorsContainer[1] = a.VectorCluster.Stop()
		}()
	}

	if a.Nexus != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			a.Nexus.Cluster.Stop()
		}()
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

	fmt.Printf("Chains has been stopped...%v\n", errors.Join(errorsContainer[:]...))
}

func (a *ApexSystem) StartChains(t *testing.T) {
	t.Helper()

	wg := &sync.WaitGroup{}
	errorsContainer := [3]error{}

	wg.Add(a.Config.ServiceCount())

	go func() {
		defer wg.Done()

		a.PrimeCluster, errorsContainer[0] = RunCardanoCluster(t, a.Config.PrimeClusterConfig)
	}()

	if a.Config.VectorEnabled {
		go func() {
			defer wg.Done()

			a.VectorCluster, errorsContainer[1] = RunCardanoCluster(t, a.Config.VectorClusterConfig)
		}()
	}

	if a.Config.NexusEnabled {
		go func() {
			defer wg.Done()

			a.Nexus, errorsContainer[2] = RunEVMChain(t, a.Config)
		}()
	}

	wg.Wait()

	t.Cleanup(a.StopChains) // cleanup everything

	require.NoError(t, errors.Join(errorsContainer[:]...))
}

func (a *ApexSystem) StartBridgeChain(t *testing.T) {
	t.Helper()

	bladeAdmin, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	a.bladeAdmin = bladeAdmin
	a.BridgeCluster = framework.NewTestCluster(t, a.Config.BladeValidatorCount,
		framework.WithBladeAdmin(bladeAdmin.Address().String()),
	)

	// create validators
	a.validators = make([]*TestApexValidator, a.Config.BladeValidatorCount)

	for idx := range a.validators {
		a.validators[idx] = NewTestApexValidator(
			a.dataDirPath, idx+1, a.BridgeCluster, a.BridgeCluster.Servers[idx])
	}

	a.BridgeCluster.WaitForReady(t)
}

func (a *ApexSystem) GetPrimeGenesisWallet(t *testing.T) cardanowallet.IWallet {
	t.Helper()

	result, err := GetGenesisWalletFromCluster(a.PrimeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	return result
}

func (a *ApexSystem) GetVectorGenesisWallet(t *testing.T) cardanowallet.IWallet {
	t.Helper()

	result, err := GetGenesisWalletFromCluster(a.VectorCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	return result
}

func (a *ApexSystem) GetPrimeTxProvider() cardanowallet.ITxProvider {
	return cardanowallet.NewTxProviderOgmios(a.PrimeCluster.OgmiosURL())
}

func (a *ApexSystem) GetVectorTxProvider() cardanowallet.ITxProvider {
	return cardanowallet.NewTxProviderOgmios(a.VectorCluster.OgmiosURL())
}

func (a *ApexSystem) PrimeTransfer(
	t *testing.T, ctx context.Context,
	sender cardanowallet.IWallet, sendAmount uint64, receiver string,
) {
	t.Helper()

	a.transferCardano(
		t, ctx, a.GetPrimeTxProvider(),
		sender, sendAmount, receiver, a.PrimeCluster.NetworkConfig())
}

func (a *ApexSystem) VectorTransfer(
	t *testing.T, ctx context.Context,
	sender cardanowallet.IWallet, sendAmount uint64, receiver string,
) {
	t.Helper()

	a.transferCardano(
		t, ctx, a.GetVectorTxProvider(),
		sender, sendAmount, receiver, a.VectorCluster.NetworkConfig())
}

func (a *ApexSystem) transferCardano(
	t *testing.T,
	ctx context.Context, txProvider cardanowallet.ITxProvider,
	sender cardanowallet.IWallet, sendAmount uint64,
	receiver string, networkConfig TestCardanoNetworkConfig,
) {
	t.Helper()

	prevAmount, err := GetTokenAmount(ctx, txProvider, receiver)
	require.NoError(t, err)

	_, err = SendTx(ctx, txProvider, sender,
		sendAmount, receiver, networkConfig, []byte{})
	require.NoError(t, err)

	err = cardanowallet.WaitForAmount(
		context.Background(), txProvider, receiver, func(val uint64) bool {
			return val == prevAmount+sendAmount
		}, 60, time.Second*2, IsRecoverableError)
	require.NoError(t, err)
}

func (a *ApexSystem) GetVectorNetworkType() cardanowallet.CardanoNetworkType {
	if a.Config.VectorEnabled && a.VectorCluster != nil {
		return a.VectorCluster.Config.NetworkType
	}

	return cardanowallet.TestNetNetwork // does not matter really
}

func (a *ApexSystem) GetBridgeDefaultJSONRPCAddr() string {
	return a.BridgeCluster.Servers[0].JSONRPCAddr()
}

func (a *ApexSystem) GetNexusDefaultJSONRPCAddr() string {
	return a.Nexus.GetDefaultJSONRPCAddr()
}

func (a *ApexSystem) GetBridgeAdmin() *crypto.ECDSAKey {
	return a.bladeAdmin
}

func (a *ApexSystem) CreateWallets() (err error) {
	for _, validator := range a.validators {
		if err = validator.CardanoWalletCreate(ChainIDPrime); err != nil {
			return err
		}

		if a.Config.VectorEnabled {
			if err = validator.CardanoWalletCreate(ChainIDVector); err != nil {
				return err
			}
		}

		if a.Config.NexusEnabled {
			validator.BatcherBN256PrivateKey, err = validator.getEvmBatcherWallet()
			if err != nil {
				return err
			}

			if validator.ID == RunRelayerOnValidatorID {
				if err = validator.createEvmSpecificWallet("relayer-evm"); err != nil {
					return err
				}

				a.nexusRelayerWallet, err = validator.getEvmRelayerWallet()
				if err != nil {
					return err
				}
			}
		}
	}

	return err
}

func (a *ApexSystem) GetNexusRelayerWalletAddr() types.Address {
	return a.nexusRelayerWallet.Address()
}

func (a *ApexSystem) GetValidatorsCount() int {
	return len(a.validators)
}

func (a *ApexSystem) GetValidator(t *testing.T, idx int) *TestApexValidator {
	t.Helper()

	require.True(t, idx >= 0 && idx < len(a.validators))

	return a.validators[idx]
}

func (a *ApexSystem) RegisterChains() error {
	tokenSupply := new(big.Int).SetUint64(a.Config.FundTokenAmount)

	errs := make([]error, len(a.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(a.validators))

	for i, validator := range a.validators {
		go func(validator *TestApexValidator, indx int) {
			defer wg.Done()

			errs[indx] = validator.RegisterChain(ChainIDPrime, tokenSupply, ChainTypeCardano)
			if errs[indx] != nil {
				return
			}

			if a.Config.VectorEnabled {
				errs[indx] = validator.RegisterChain(ChainIDVector, tokenSupply, ChainTypeCardano)
				if errs[indx] != nil {
					return
				}
			}

			if a.Config.NexusEnabled {
				errs[indx] = validator.RegisterChain(ChainIDNexus, tokenSupply, ChainTypeEVM)
				if errs[indx] != nil {
					return
				}
			}
		}(validator, i)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (a *ApexSystem) GenerateConfigs() error {
	errs := make([]error, len(a.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(a.validators))

	for i, validator := range a.validators {
		go func(validator *TestApexValidator, indx int) {
			defer wg.Done()

			telemetryConfig := ""
			if indx == 0 {
				telemetryConfig = a.Config.TelemetryConfig
			}

			var (
				primeNetworkAddr   string
				primeNetworkMagic  uint
				primeNetworkID     uint
				vectorOgmiosURL    = "http://localhost:1000"
				vectorNetworkAddr  = "localhost:5499"
				vectorNetworkMagic = uint(0)
				vectorNetworkID    = uint(0)
				nexusNodeURL       = "http://localhost:5500"
			)

			if a.Config.TargetOneCardanoClusterServer {
				primeNetworkAddr = a.PrimeCluster.NetworkAddress()
				primeNetworkMagic = a.PrimeCluster.Config.NetworkMagic
				primeNetworkID = uint(a.PrimeCluster.Config.NetworkType)
			} else {
				primeServer := a.PrimeCluster.Servers[indx%len(a.PrimeCluster.Servers)]
				primeNetworkAddr = primeServer.NetworkAddress()
				primeNetworkMagic = primeServer.config.NetworkMagic
				primeNetworkID = uint(primeServer.config.NetworkID)
			}

			if a.Config.VectorEnabled {
				vectorOgmiosURL = a.VectorCluster.OgmiosURL()

				if a.Config.TargetOneCardanoClusterServer {
					vectorNetworkAddr = a.VectorCluster.NetworkAddress()
					vectorNetworkMagic = a.VectorCluster.Config.NetworkMagic
					vectorNetworkID = uint(a.VectorCluster.Config.NetworkType)
				} else {
					vectorServer := a.VectorCluster.Servers[indx%len(a.VectorCluster.Servers)]
					vectorNetworkAddr = vectorServer.NetworkAddress()
					vectorNetworkMagic = vectorServer.config.NetworkMagic
					vectorNetworkID = uint(vectorServer.config.NetworkID)
				}
			}

			if a.Config.NexusEnabled {
				nexusNodeURLIndx := 0
				if a.Config.TargetOneCardanoClusterServer {
					nexusNodeURLIndx = indx % len(a.Nexus.Cluster.Servers)
				}

				nexusNodeURL = a.Nexus.Cluster.Servers[nexusNodeURLIndx].JSONRPCAddr()
			}

			errs[indx] = validator.GenerateConfigs(
				primeNetworkAddr,
				primeNetworkMagic,
				primeNetworkID,
				a.PrimeCluster.OgmiosURL(),
				a.Config.PrimeSlotRoundingThreshold,
				a.Config.PrimeTTLInc,
				vectorNetworkAddr,
				vectorNetworkMagic,
				vectorNetworkID,
				vectorOgmiosURL,
				a.Config.VectorSlotRoundingThreshold,
				a.Config.VectorTTLInc,
				a.Config.APIPortStart+indx,
				a.Config.APIKey,
				telemetryConfig,
				nexusNodeURL,
			)
		}(validator, i)
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return err
	}

	if handler := a.Config.CustomOracleHandler; handler != nil {
		for _, val := range a.validators {
			err := UpdateJSONFile(
				val.GetValidatorComponentsConfig(), val.GetValidatorComponentsConfig(), handler, false)
			if err != nil {
				return err
			}
		}
	}

	if handler := a.Config.CustomRelayerHandler; handler != nil {
		for _, val := range a.validators {
			if RunRelayerOnValidatorID != val.ID {
				continue
			}

			if err := UpdateJSONFile(val.GetRelayerConfig(), val.GetRelayerConfig(), handler, false); err != nil {
				return err
			}
		}
	}

	return nil
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

func (a *ApexSystem) GetBridgingAPI() (string, error) {
	apis, err := a.GetBridgingAPIs()
	if err != nil {
		return "", err
	}

	return apis[0], nil
}

func (a *ApexSystem) GetBridgingAPIs() (res []string, err error) {
	for _, validator := range a.validators {
		hasAPI := a.Config.APIValidatorID == -1 || validator.ID == a.Config.APIValidatorID

		if hasAPI {
			if validator.APIPort == 0 {
				return nil, fmt.Errorf("api port not defined")
			}

			res = append(res, fmt.Sprintf("http://localhost:%d", validator.APIPort))
		}
	}

	if len(res) == 0 {
		return nil, fmt.Errorf("not running API")
	}

	return res, nil
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

func (a *ApexSystem) FundCardanoMultisigAddresses(
	ctx context.Context,
) error {
	const (
		numOfRetries = 90
		waitTime     = time.Second * 2
	)

	var fundTokenAmount = a.Config.FundTokenAmount

	fund := func(cluster *TestCardanoCluster, fundTokenAmount uint64, addr string) (string, error) {
		txProvider := cardanowallet.NewTxProviderOgmios(cluster.OgmiosURL())

		defer txProvider.Dispose()

		genesisWallet, err := GetGenesisWalletFromCluster(cluster.Config.TmpDir, 1)
		if err != nil {
			return "", err
		}

		txHash, err := SendTx(ctx, txProvider, genesisWallet, fundTokenAmount,
			addr, cluster.NetworkConfig(), []byte{})
		if err != nil {
			return "", err
		}

		err = cardanowallet.WaitForTxHashInUtxos(
			ctx, txProvider, addr, txHash, numOfRetries, waitTime, IsRecoverableError)
		if err != nil {
			return "", err
		}

		return txHash, nil
	}

	txHash, err := fund(a.PrimeCluster, fundTokenAmount, a.PrimeMultisigAddr)
	if err != nil {
		return err
	}

	fmt.Printf("Prime multisig addr funded: %s\n", txHash)

	txHash, err = fund(a.PrimeCluster, fundTokenAmount, a.PrimeMultisigFeeAddr)
	if err != nil {
		return err
	}

	fmt.Printf("Prime fee addr funded: %s\n", txHash)

	if a.Config.VectorEnabled {
		txHash, err := fund(a.VectorCluster, fundTokenAmount, a.VectorMultisigAddr)
		if err != nil {
			return err
		}

		fmt.Printf("Vector multisig addr funded: %s\n", txHash)

		txHash, err = fund(a.VectorCluster, fundTokenAmount, a.VectorMultisigFeeAddr)
		if err != nil {
			return err
		}

		fmt.Printf("Vector fee addr funded: %s\n", txHash)
	}

	return nil
}

func (a *ApexSystem) CreateCardanoMultisigAddresses() (err error) {
	a.PrimeMultisigAddr, a.PrimeMultisigFeeAddr, err = a.cardanoCreateAddress(a.PrimeCluster.Config.NetworkType, nil)
	if err != nil {
		return fmt.Errorf("failed to create addresses for prime: %w", err)
	}

	if a.Config.VectorEnabled {
		a.VectorMultisigAddr, a.VectorMultisigFeeAddr, err = a.cardanoCreateAddress(a.VectorCluster.Config.NetworkType, nil)
		if err != nil {
			return fmt.Errorf("failed to create addresses for vector: %w", err)
		}
	}

	return nil
}

func (a *ApexSystem) cardanoCreateAddress(
	network cardanowallet.CardanoNetworkType, keys []string,
) (string, string, error) {
	bridgeAdminPk, err := a.bladeAdmin.MarshallPrivateKey()
	if err != nil {
		return "", "", err
	}

	bothAddresses := false

	args := []string{
		"create-address",
		"--network-id", fmt.Sprint(network),
	}

	if len(keys) == 0 {
		args = append(args,
			"--bridge-url", a.GetBridgeDefaultJSONRPCAddr(),
			"--bridge-addr", contracts.Bridge.String(),
			"--chain", GetNetworkName(network),
			"--bridge-key", hex.EncodeToString(bridgeAdminPk))

		bothAddresses = true
	}

	for _, key := range keys {
		args = append(args, "--key", key)
	}

	var outb bytes.Buffer

	err = RunCommand(ResolveApexBridgeBinary(), args, io.MultiWriter(os.Stdout, &outb))
	if err != nil {
		return "", "", err
	}

	if !bothAddresses {
		return strings.TrimSpace(strings.ReplaceAll(outb.String(), "Address = ", "")), "", nil
	}

	multisig, fee, output := "", "", outb.String()
	reGateway := regexp.MustCompile(`Multisig Address\s*=\s*([^\s]+)`)
	reNativeTokenWallet := regexp.MustCompile(`Fee Payer Address\s*=\s*([^\s]+)`)

	if match := reGateway.FindStringSubmatch(output); len(match) > 0 {
		multisig = match[1]
	}

	if match := reNativeTokenWallet.FindStringSubmatch(output); len(match) > 0 {
		fee = match[1]
	}

	return multisig, fee, nil
}
func (a *ApexSystem) GetBalance(
	ctx context.Context, user *TestApexUser, chain ChainID,
) (*big.Int, error) {
	switch chain {
	case ChainIDPrime:
		balance, err := GetTokenAmount(ctx, a.GetPrimeTxProvider(), user.GetAddress(ChainIDPrime))

		return new(big.Int).SetUint64(balance), err
	case ChainIDVector:
		balance, err := GetTokenAmount(ctx, a.GetVectorTxProvider(), user.GetAddress(ChainIDVector))

		return new(big.Int).SetUint64(balance), err
	case ChainIDNexus:
		return a.Nexus.GetAddressEthAmount(ctx, user.NexusAddress)
	}

	return nil, fmt.Errorf("unsupported chain")
}

func (a *ApexSystem) WaitForGreaterAmount(
	ctx context.Context, user *TestApexUser, chain ChainID,
	expectedAmount *big.Int, numRetries int, waitTime time.Duration,
	isRecoverableError ...cardanowallet.IsRecoverableErrorFn,
) error {
	return a.WaitForAmount(ctx, user, chain, func(val *big.Int) bool {
		return val.Cmp(expectedAmount) == 1
	}, numRetries, waitTime)
}

func (a *ApexSystem) WaitForExactAmount(
	ctx context.Context, user *TestApexUser, chain ChainID,
	expectedAmount *big.Int, numRetries int, waitTime time.Duration,
	isRecoverableError ...cardanowallet.IsRecoverableErrorFn,
) error {
	return a.WaitForAmount(ctx, user, chain, func(val *big.Int) bool {
		return val.Cmp(expectedAmount) == 0
	}, numRetries, waitTime)
}

func (a *ApexSystem) WaitForAmount(
	ctx context.Context, user *TestApexUser, chain ChainID,
	cmpHandler func(*big.Int) bool, numRetries int, waitTime time.Duration,
	isRecoverableError ...cardanowallet.IsRecoverableErrorFn,
) error {
	if chain == ChainIDPrime || chain == ChainIDVector {
		isRecoverableError = append(isRecoverableError, IsRecoverableError)
	}

	return cardanowallet.ExecuteWithRetry(ctx, numRetries, waitTime, func() (bool, error) {
		newBalance, err := a.GetBalance(ctx, user, chain)

		return err == nil && cmpHandler(newBalance), err
	}, isRecoverableError...)
}

func (a *ApexSystem) SubmitBridgingRequest(
	t *testing.T, ctx context.Context,
	sourceChain ChainID, destinationChain ChainID,
	sender *TestApexUser, sendAmount *big.Int, receivers ...*TestApexUser,
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
		!a.Config.VectorEnabled && (sourceChain == ChainIDVector || destinationChain == ChainIDVector))
	require.False(t,
		!a.Config.NexusEnabled && (sourceChain == ChainIDNexus || destinationChain == ChainIDNexus))
	require.True(t,
		sourceChain == ChainIDPrime ||
			(sourceChain == ChainIDVector && destinationChain == ChainIDPrime) ||
			(sourceChain == ChainIDNexus && destinationChain == ChainIDPrime),
	)

	// check if number of receivers is valid
	require.Greater(t, len(receivers), 0)
	require.Less(t, len(receivers), 5)

	// check if users are valid for the bridging - do they have necessary wallets
	require.True(t, sourceChain != ChainIDVector || sender.HasVectorWallet)
	require.True(t, sourceChain != ChainIDNexus || sender.HasNexusWallet)

	for _, receiver := range receivers {
		require.True(t, destinationChain != ChainIDVector || receiver.HasVectorWallet)
		require.True(t, destinationChain != ChainIDNexus || receiver.HasNexusWallet)
	}

	if sourceChain == ChainIDPrime || sourceChain == ChainIDVector {
		return a.submitCardanoBridgingRequest(t, ctx,
			sourceChain, destinationChain, sender, sendAmount.Uint64(), receivers...)
	} else {
		return a.submitEvmBridgingRequest(t, ctx,
			sourceChain, destinationChain, sender, sendAmount, receivers...)
	}
}
func (a *ApexSystem) submitEvmBridgingRequest(
	t *testing.T, _ context.Context,
	sourceChain ChainID, destinationChain ChainID,
	sender *TestApexUser, sendAmount *big.Int, receivers ...*TestApexUser,
) string {
	t.Helper()

	const (
		feeAmount = 1_100_000
	)

	feeAmountWei := new(big.Int).SetUint64(feeAmount)
	feeAmountWei.Mul(feeAmountWei, new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil))

	senderPrivateKey, err := sender.GetPrivateKey(sourceChain)
	require.NoError(t, err)

	params := []string{
		"sendtx",
		"--tx-type", "evm",
		"--gateway-addr", a.Nexus.Gateway.String(),
		"--nexus-url", a.GetNexusDefaultJSONRPCAddr(),
		"--key", senderPrivateKey,
		"--chain-dst", destinationChain,
		"--fee", feeAmountWei.String(),
	}

	receiversParam := make([]string, 0, len(receivers))
	for i := 0; i < len(receivers); i++ {
		receiversParam = append(
			receiversParam,
			"--receiver", fmt.Sprintf("%s:%s", receivers[i].GetAddress(destinationChain), sendAmount),
		)
	}

	params = append(params, receiversParam...)

	var outb bytes.Buffer

	err = RunCommand(ResolveApexBridgeBinary(), params, io.MultiWriter(os.Stdout, &outb))
	require.NoError(t, err)

	txHash, output := "", outb.String()
	reTxHash := regexp.MustCompile(`Tx Hash\s*=\s*([^\s]+)`)

	if match := reTxHash.FindStringSubmatch(output); len(match) > 0 {
		txHash = match[1]
	}

	return txHash
}

func (a *ApexSystem) submitCardanoBridgingRequest(
	t *testing.T, ctx context.Context,
	sourceChain ChainID, destinationChain ChainID,
	sender *TestApexUser, sendAmount uint64, receivers ...*TestApexUser,
) string {
	t.Helper()

	const (
		feeAmount              = 1_100_000
		waitForTxNumRetries    = 60
		waitForTxRetryWaitTime = time.Second * 2
	)

	var (
		txProvider    cardanowallet.ITxProvider
		networkConfig TestCardanoNetworkConfig
		bridgingAddr  string
	)

	if sourceChain == ChainIDPrime {
		txProvider = a.GetPrimeTxProvider()
		networkConfig = a.PrimeCluster.NetworkConfig()
		bridgingAddr = a.PrimeMultisigAddr
	} else if sourceChain == ChainIDVector {
		txProvider = a.GetVectorTxProvider()
		networkConfig = a.VectorCluster.NetworkConfig()
		bridgingAddr = a.VectorMultisigAddr
	}

	receiverAddrsMap := make(map[string]uint64, len(receivers))

	for _, receiver := range receivers {
		addr := receiver.GetAddress(destinationChain)
		receiverAddrsMap[addr] += sendAmount
	}

	require.NotNil(t, txProvider)

	senderWallet, senderAddr := sender.GetCardanoWallet(sourceChain)

	bridgingRequestMetadata, err := CreateCardanoBridgingMetaData(
		senderAddr.String(), receiverAddrsMap, destinationChain, feeAmount)
	require.NoError(t, err)

	txHash, err := SendTx(ctx, txProvider, senderWallet,
		uint64(len(receivers))*sendAmount+feeAmount, bridgingAddr, networkConfig, bridgingRequestMetadata)
	require.NoError(t, err)

	err = cardanowallet.WaitForTxHashInUtxos(
		ctx, txProvider, bridgingAddr, txHash, waitForTxNumRetries, waitForTxRetryWaitTime, IsRecoverableError)
	require.NoError(t, err)

	return txHash
}
