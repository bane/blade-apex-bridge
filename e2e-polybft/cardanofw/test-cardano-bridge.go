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
	"strings"
	"sync"
	"testing"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

const (
	ChainIDPrime  = "prime"
	ChainIDVector = "vector"

	BridgeSCAddr = "0xABEF000000000000000000000000000000000000"

	RunAPIOnValidatorID     = 1
	RunRelayerOnValidatorID = 1
)

type CardanoBridgeOption func(*TestCardanoBridge)

type TestCardanoBridge struct {
	validatorCount int
	dataDirPath    string

	validators  []*TestCardanoValidator
	relayerNode *framework.Node

	primeMultisigKeys     []string
	primeMultisigFeeKeys  []string
	vectorMultisigKeys    []string
	vectorMultisigFeeKeys []string

	PrimeMultisigAddr     string
	PrimeMultisigFeeAddr  string
	VectorMultisigAddr    string
	VectorMultisigFeeAddr string

	cluster *framework.TestCluster

	apiPortStart    int
	apiKey          string
	telemetryConfig string

	vectorTTLInc uint64
	primeTTLInc  uint64
}

func WithAPIPortStart(apiPortStart int) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.apiPortStart = apiPortStart
	}
}

func WithAPIKey(apiKey string) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.apiKey = apiKey
	}
}

func WithVectorTTLInc(ttlInc uint64) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.vectorTTLInc = ttlInc
	}
}

func WithPrimeTTLInc(ttlInc uint64) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.primeTTLInc = ttlInc
	}
}

func WithTelemetryConfig(tc string) CardanoBridgeOption {
	return func(h *TestCardanoBridge) {
		h.telemetryConfig = tc // something like "0.0.0.0:5001,localhost:8126"
	}
}

func NewTestCardanoBridge(
	dataDirPath string, validatorCount int, opts ...CardanoBridgeOption,
) *TestCardanoBridge {
	validators := make([]*TestCardanoValidator, validatorCount)

	for i := 0; i < validatorCount; i++ {
		validators[i] = NewTestCardanoValidator(dataDirPath, i+1)
	}

	bridge := &TestCardanoBridge{
		dataDirPath:    dataDirPath,
		validatorCount: validatorCount,
		validators:     validators,
	}

	for _, opt := range opts {
		opt(bridge)
	}

	return bridge
}

func (cb *TestCardanoBridge) CardanoCreateWalletsAndAddresses(
	primeNetworkConfig TestCardanoNetworkConfig, vectorNetworkConfig TestCardanoNetworkConfig,
) error {
	if err := cb.cardanoCreateWallets(); err != nil {
		return err
	}

	if err := cb.cardanoPrepareKeys(); err != nil {
		return err
	}

	return cb.cardanoCreateAddresses(primeNetworkConfig, vectorNetworkConfig)
}

func (cb *TestCardanoBridge) StartValidators(t *testing.T, epochSize int) {
	t.Helper()

	cb.cluster = framework.NewTestCluster(t, cb.validatorCount,
		framework.WithEpochSize(epochSize),
	)

	for idx, validator := range cb.validators {
		require.NoError(t, validator.SetClusterAndServer(cb.cluster, cb.cluster.Servers[idx]))
	}
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

func (cb *TestCardanoBridge) RegisterChains(
	primeTokenSupply *big.Int,
	vectorTokenSupply *big.Int,
) error {
	errs := make([]error, len(cb.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(cb.validators))

	for i, validator := range cb.validators {
		go func(validator *TestCardanoValidator, indx int) {
			defer wg.Done()

			errs[indx] = validator.RegisterChain(
				ChainIDPrime, cb.PrimeMultisigAddr, cb.PrimeMultisigFeeAddr, primeTokenSupply, ChainTypeCardano)
			if errs[indx] != nil {
				return
			}

			errs[indx] = validator.RegisterChain(
				ChainIDVector, cb.VectorMultisigAddr, cb.VectorMultisigFeeAddr, vectorTokenSupply, ChainTypeCardano)
			if errs[indx] != nil {
				return
			}
		}(validator, i)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (cb *TestCardanoBridge) GenerateConfigs(
	primeCluster *TestCardanoCluster,
	vectorCluster *TestCardanoCluster,
) error {
	errs := make([]error, len(cb.validators))
	wg := sync.WaitGroup{}

	wg.Add(len(cb.validators))

	for i, validator := range cb.validators {
		go func(validator *TestCardanoValidator, indx int) {
			defer wg.Done()

			telemetryConfig := ""
			if indx == 0 {
				telemetryConfig = cb.telemetryConfig
			}

			errs[indx] = validator.GenerateConfigs(
				primeCluster.NetworkURL(),
				primeCluster.Config.NetworkMagic,
				uint(primeCluster.Config.NetworkType),
				primeCluster.OgmiosURL(),
				cb.primeTTLInc,
				vectorCluster.NetworkURL(),
				vectorCluster.Config.NetworkMagic,
				uint(vectorCluster.Config.NetworkType),
				vectorCluster.OgmiosURL(),
				cb.vectorTTLInc,
				cb.apiPortStart+indx,
				cb.apiKey,
				telemetryConfig,
			)
		}(validator, i)
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (cb *TestCardanoBridge) StartValidatorComponents(ctx context.Context) (err error) {
	for _, validator := range cb.validators {
		if err = validator.Start(ctx, RunAPIOnValidatorID == validator.ID); err != nil {
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
	for _, validator := range cb.validators {
		if validator.ID == RunAPIOnValidatorID {
			if validator.APIPort == 0 {
				return "", fmt.Errorf("api port not defined")
			}

			return fmt.Sprintf("http://localhost:%d", validator.APIPort), nil
		}
	}

	return "", fmt.Errorf("not running API")
}

func (cb *TestCardanoBridge) cardanoCreateWallets() (err error) {
	for _, validator := range cb.validators {
		err = validator.CardanoWalletCreate(ChainIDPrime)
		if err != nil {
			return err
		}

		err = validator.CardanoWalletCreate(ChainIDVector)
		if err != nil {
			return err
		}
	}

	return err
}

func (cb *TestCardanoBridge) cardanoPrepareKeys() (err error) {
	cb.primeMultisigKeys = make([]string, cb.validatorCount)
	cb.primeMultisigFeeKeys = make([]string, cb.validatorCount)
	cb.vectorMultisigKeys = make([]string, cb.validatorCount)
	cb.vectorMultisigFeeKeys = make([]string, cb.validatorCount)

	for idx, validator := range cb.validators {
		primeWallet, err := validator.GetCardanoWallet(ChainIDPrime)
		if err != nil {
			return err
		}

		cb.primeMultisigKeys[idx] = hex.EncodeToString(primeWallet.Multisig.GetVerificationKey())
		cb.primeMultisigFeeKeys[idx] = hex.EncodeToString(primeWallet.MultisigFee.GetVerificationKey())

		vectorWallet, err := validator.GetCardanoWallet(ChainIDVector)
		if err != nil {
			return err
		}

		cb.vectorMultisigKeys[idx] = hex.EncodeToString(vectorWallet.Multisig.GetVerificationKey())
		cb.vectorMultisigFeeKeys[idx] = hex.EncodeToString(vectorWallet.MultisigFee.GetVerificationKey())
	}

	return err
}

func (cb *TestCardanoBridge) cardanoCreateAddresses(
	primeNetworkConfig TestCardanoNetworkConfig, vectorNetworkConfig TestCardanoNetworkConfig,
) error {
	errs := make([]error, 4)
	wg := sync.WaitGroup{}

	wg.Add(4)

	go func() {
		defer wg.Done()

		cb.PrimeMultisigAddr, errs[0] = cb.cardanoCreateAddress(primeNetworkConfig.NetworkType, cb.primeMultisigKeys)
	}()

	go func() {
		defer wg.Done()

		cb.PrimeMultisigFeeAddr, errs[1] = cb.cardanoCreateAddress(primeNetworkConfig.NetworkType, cb.primeMultisigFeeKeys)
	}()

	go func() {
		defer wg.Done()

		cb.VectorMultisigAddr, errs[2] = cb.cardanoCreateAddress(vectorNetworkConfig.NetworkType, cb.vectorMultisigKeys)
	}()

	go func() {
		defer wg.Done()

		cb.VectorMultisigFeeAddr, errs[3] = cb.cardanoCreateAddress(vectorNetworkConfig.NetworkType, cb.vectorMultisigFeeKeys)
	}()

	wg.Wait()

	return errors.Join(errs...)
}

func (cb *TestCardanoBridge) cardanoCreateAddress(network wallet.CardanoNetworkType, keys []string) (string, error) {
	args := []string{
		"create-address",
		"--network-id", fmt.Sprint(network),
	}

	for _, key := range keys {
		args = append(args, "--key", key)
	}

	var outb bytes.Buffer

	err := RunCommand(ResolveApexBridgeBinary(), args, io.MultiWriter(os.Stdout, &outb))
	if err != nil {
		return "", err
	}

	result := strings.TrimSpace(strings.ReplaceAll(outb.String(), "Address = ", ""))

	return result, nil
}
