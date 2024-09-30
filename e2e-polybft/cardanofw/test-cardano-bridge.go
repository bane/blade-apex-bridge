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

const (
	ChainIDPrime  = "prime"
	ChainIDVector = "vector"
	ChainIDNexus  = "nexus"

	RunRelayerOnValidatorID = 1
)

type CardanoBridgeOption func(*TestCardanoBridge)

type TestCardanoBridge struct {
	dataDirPath string

	validators  []*TestCardanoValidator
	relayerNode *framework.Node

	PrimeMultisigAddr     string
	PrimeMultisigFeeAddr  string
	VectorMultisigAddr    string
	VectorMultisigFeeAddr string

	relayerWallet *crypto.ECDSAKey

	cluster             *framework.TestCluster
	proxyContractsAdmin *crypto.ECDSAKey
	bladeAdmin          *crypto.ECDSAKey

	config *ApexSystemConfig
}

func NewTestCardanoBridge(
	dataDirPath string, apexSystemConfig *ApexSystemConfig,
) *TestCardanoBridge {
	validators := make([]*TestCardanoValidator, apexSystemConfig.BladeValidatorCount)

	for i := 0; i < apexSystemConfig.BladeValidatorCount; i++ {
		validators[i] = NewTestCardanoValidator(dataDirPath, i+1)
	}

	bridge := &TestCardanoBridge{
		dataDirPath: dataDirPath,
		validators:  validators,
		config:      apexSystemConfig,
	}

	return bridge
}

func (cb *TestCardanoBridge) StartValidators(t *testing.T, epochSize int) {
	t.Helper()

	proxyContractsAdmin, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	bladeAdmin, err := crypto.GenerateECDSAKey()
	require.NoError(t, err)

	cb.proxyContractsAdmin = proxyContractsAdmin
	cb.bladeAdmin = bladeAdmin
	cb.cluster = framework.NewTestCluster(t, cb.config.BladeValidatorCount,
		framework.WithEpochSize(epochSize),
		framework.WithProxyContractsAdmin(proxyContractsAdmin.Address().String()),
		framework.WithBladeAdmin(bladeAdmin.Address().String()),
	)

	for idx, validator := range cb.validators {
		require.NoError(t, validator.SetClusterAndServer(cb.cluster, cb.cluster.Servers[idx]))
	}
}

func (cb *TestCardanoBridge) GetProxyContractsAdmin() *crypto.ECDSAKey {
	return cb.proxyContractsAdmin
}

func (cb *TestCardanoBridge) GetBladeAdmin() *crypto.ECDSAKey {
	return cb.bladeAdmin
}

func (cb *TestCardanoBridge) NexusCreateWalletsAndAddresses(createBLSKeys bool) (err error) {
	if !cb.config.NexusEnabled || len(cb.validators) == 0 {
		return nil
	}

	// relayer is on the first validator only
	relayerValidator := cb.validators[0]

	if err = relayerValidator.createSpecificWallet("relayer-evm"); err != nil {
		return err
	}

	cb.relayerWallet, err = relayerValidator.getRelayerWallet()
	if err != nil {
		return err
	}

	for _, validator := range cb.validators {
		if createBLSKeys {
			if err = validator.createSpecificWallet("batcher-evm"); err != nil {
				return err
			}
		}

		validator.BatcherBN256PrivateKey, err = validator.getBatcherWallet(!createBLSKeys)
		if err != nil {
			return err
		}
	}

	return err
}

func (cb *TestCardanoBridge) GetRelayerWalletAddr() types.Address {
	return cb.relayerWallet.Address()
}

func (cb *TestCardanoBridge) GetValidator(t *testing.T, idx int) *TestCardanoValidator {
	t.Helper()

	require.True(t, idx >= 0 && idx < len(cb.validators))

	return cb.validators[idx]
}

func (cb *TestCardanoBridge) WaitForValidatorsReady(t *testing.T) {
	t.Helper()

	cb.cluster.WaitForReady(t)
}

func (cb *TestCardanoBridge) StopValidators() {
	if cb.cluster != nil {
		cb.cluster.Stop()
	}
}

func (cb *TestCardanoBridge) GetValidatorsCount() int {
	return len(cb.validators)
}

func (cb *TestCardanoBridge) GetFirstServer() *framework.TestServer {
	return cb.cluster.Servers[0]
}

func (cb *TestCardanoBridge) RegisterChains(fundTokenAmount uint64) error {
	primeTokenSupply := new(big.Int).SetUint64(fundTokenAmount)
	vectorTokenSupply := new(big.Int).SetUint64(fundTokenAmount)
	nexusTokenSupplyDfm := new(big.Int).SetUint64(fundTokenAmount)
	nexusTokenSupplyDfm.Mul(nexusTokenSupplyDfm, new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil))

	errs := make([]error, len(cb.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(cb.validators))

	for i, validator := range cb.validators {
		go func(validator *TestCardanoValidator, indx int) {
			defer wg.Done()

			errs[indx] = validator.RegisterChain(ChainIDPrime, primeTokenSupply, ChainTypeCardano)
			if errs[indx] != nil {
				return
			}

			if cb.config.VectorEnabled {
				errs[indx] = validator.RegisterChain(ChainIDVector, vectorTokenSupply, ChainTypeCardano)
				if errs[indx] != nil {
					return
				}
			}

			if cb.config.NexusEnabled {
				errs[indx] = validator.RegisterChain(ChainIDNexus, nexusTokenSupplyDfm, ChainTypeEVM)
				if errs[indx] != nil {
					return
				}
			}
		}(validator, i)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (cb *TestCardanoBridge) GenerateConfigs(
	primeCluster *TestCardanoCluster,
	vectorCluster *TestCardanoCluster,
	nexus *TestEVMBridge,
) error {
	errs := make([]error, len(cb.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(cb.validators))

	for i, validator := range cb.validators {
		go func(validator *TestCardanoValidator, indx int) {
			defer wg.Done()

			telemetryConfig := ""
			if indx == 0 {
				telemetryConfig = cb.config.TelemetryConfig
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

			if cb.config.TargetOneCardanoClusterServer {
				primeNetworkAddr = primeCluster.NetworkAddress()
				primeNetworkMagic = primeCluster.Config.NetworkMagic
				primeNetworkID = uint(primeCluster.Config.NetworkType)
			} else {
				primeServer := primeCluster.Servers[indx%len(primeCluster.Servers)]
				primeNetworkAddr = primeServer.NetworkAddress()
				primeNetworkMagic = primeServer.config.NetworkMagic
				primeNetworkID = uint(primeServer.config.NetworkID)
			}

			if cb.config.VectorEnabled {
				vectorOgmiosURL = vectorCluster.OgmiosURL()

				if cb.config.TargetOneCardanoClusterServer {
					vectorNetworkAddr = vectorCluster.NetworkAddress()
					vectorNetworkMagic = vectorCluster.Config.NetworkMagic
					vectorNetworkID = uint(vectorCluster.Config.NetworkType)
				} else {
					vectorServer := vectorCluster.Servers[indx%len(vectorCluster.Servers)]
					vectorNetworkAddr = vectorServer.NetworkAddress()
					vectorNetworkMagic = vectorServer.config.NetworkMagic
					vectorNetworkID = uint(vectorServer.config.NetworkID)
				}
			}

			if cb.config.NexusEnabled {
				nexusNodeURLIndx := 0
				if cb.config.TargetOneCardanoClusterServer {
					nexusNodeURLIndx = indx % len(nexus.Cluster.Servers)
				}

				nexusNodeURL = nexus.Cluster.Servers[nexusNodeURLIndx].JSONRPCAddr()
			}

			errs[indx] = validator.GenerateConfigs(
				primeNetworkAddr,
				primeNetworkMagic,
				primeNetworkID,
				primeCluster.OgmiosURL(),
				cb.config.PrimeSlotRoundingThreshold,
				cb.config.PrimeTTLInc,
				vectorNetworkAddr,
				vectorNetworkMagic,
				vectorNetworkID,
				vectorOgmiosURL,
				cb.config.VectorSlotRoundingThreshold,
				cb.config.VectorTTLInc,
				cb.config.APIPortStart+indx,
				cb.config.APIKey,
				telemetryConfig,
				nexusNodeURL,
			)
		}(validator, i)
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return err
	}

	if cb.config.CustomOracleHandler != nil {
		for _, val := range cb.validators {
			err := UpdateJSONFile(
				val.GetValidatorComponentsConfig(),
				val.GetValidatorComponentsConfig(),
				cb.config.CustomOracleHandler,
				false,
			)
			if err != nil {
				return err
			}
		}
	}

	if cb.config.CustomRelayerHandler != nil {
		err := UpdateJSONFile(
			cb.validators[0].GetRelayerConfig(),
			cb.validators[0].GetRelayerConfig(),
			cb.config.CustomRelayerHandler,
			false,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cb *TestCardanoBridge) StartValidatorComponents(ctx context.Context) (err error) {
	for _, validator := range cb.validators {
		hasAPI := cb.config.APIValidatorID == -1 || validator.ID == cb.config.APIValidatorID

		if err = validator.Start(ctx, hasAPI); err != nil {
			return err
		}
	}

	return err
}

func (cb *TestCardanoBridge) StartRelayer(ctx context.Context) (err error) {
	for _, validator := range cb.validators {
		if RunRelayerOnValidatorID != validator.ID {
			continue
		}

		cb.relayerNode, err = framework.NewNodeWithContext(ctx, ResolveApexBridgeBinary(), []string{
			"run-relayer",
			"--config", validator.GetRelayerConfig(),
		}, os.Stdout)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cb TestCardanoBridge) StopRelayer() error {
	if cb.relayerNode == nil {
		return errors.New("relayer not started")
	}

	return cb.relayerNode.Stop()
}

func (cb *TestCardanoBridge) GetBridgingAPI() (string, error) {
	apis, err := cb.GetBridgingAPIs()
	if err != nil {
		return "", err
	}

	return apis[0], nil
}

func (cb *TestCardanoBridge) GetBridgingAPIs() (res []string, err error) {
	for _, validator := range cb.validators {
		hasAPI := cb.config.APIValidatorID == -1 || validator.ID == cb.config.APIValidatorID

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

func (cb *TestCardanoBridge) ApexBridgeProcessesRunning() bool {
	if cb.relayerNode == nil || cb.relayerNode.ExitResult() != nil {
		return false
	}

	for _, validator := range cb.validators {
		if validator.node == nil || validator.node.ExitResult() != nil {
			return false
		}
	}

	return true
}

func (cb *TestCardanoBridge) CreateCardanoWallets() (err error) {
	for _, validator := range cb.validators {
		if err = validator.CardanoWalletCreate(ChainIDPrime); err != nil {
			return err
		}

		if cb.config.VectorEnabled {
			if err = validator.CardanoWalletCreate(ChainIDVector); err != nil {
				return err
			}
		}
	}

	return err
}

func (cb *TestCardanoBridge) FundCardanoMultisigAddresses(
	ctx context.Context, primeCluster, vectorCluster *TestCardanoCluster, fundTokenAmount uint64,
) error {
	const (
		numOfRetries = 90
		waitTime     = time.Second * 2
	)

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

	txHash, err := fund(primeCluster, fundTokenAmount, cb.PrimeMultisigAddr)
	if err != nil {
		return err
	}

	fmt.Printf("Prime multisig addr funded: %s\n", txHash)

	txHash, err = fund(primeCluster, fundTokenAmount, cb.PrimeMultisigFeeAddr)
	if err != nil {
		return err
	}

	fmt.Printf("Prime fee addr funded: %s\n", txHash)

	if cb.config.VectorEnabled {
		txHash, err := fund(vectorCluster, fundTokenAmount, cb.VectorMultisigAddr)
		if err != nil {
			return err
		}

		fmt.Printf("Vector multisig addr funded: %s\n", txHash)

		txHash, err = fund(vectorCluster, fundTokenAmount, cb.VectorMultisigFeeAddr)
		if err != nil {
			return err
		}

		fmt.Printf("Vector fee addr funded: %s\n", txHash)
	}

	return nil
}

func (cb *TestCardanoBridge) CreateCardanoMultisigAddresses(
	primeNetworkType, vectorNetworkType cardanowallet.CardanoNetworkType,
) (err error) {
	cb.PrimeMultisigAddr, cb.PrimeMultisigFeeAddr, err = cb.cardanoCreateAddress(primeNetworkType, nil)
	if err != nil {
		return fmt.Errorf("failed to create address for prime: %w", err)
	}

	cb.VectorMultisigAddr, cb.VectorMultisigFeeAddr, err = cb.cardanoCreateAddress(vectorNetworkType, nil)
	if err != nil {
		return fmt.Errorf("failed to create address for vector: %w", err)
	}

	return nil
}

func (cb *TestCardanoBridge) cardanoCreateAddress(
	network cardanowallet.CardanoNetworkType, keys []string,
) (string, string, error) {
	bridgeAdminPk, err := cb.bladeAdmin.MarshallPrivateKey()
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
			"--bridge-url", cb.cluster.Servers[0].JSONRPCAddr(),
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
