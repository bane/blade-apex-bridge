package cardanofw

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/common"
	bn256 "github.com/Ethernal-Tech/bn256"
	secretsCardano "github.com/Ethernal-Tech/cardano-infrastructure/secrets"
	secretsHelper "github.com/Ethernal-Tech/cardano-infrastructure/secrets/helper"
	cardanoWallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

const (
	BridgingConfigsDir = "bridging-configs"
	BridgingLogsDir    = "bridging-logs"
	BridgingDBsDir     = "bridging-dbs"

	NexusDir = "nexus-test-logs"

	ValidatorComponentsConfigFileName = "vc_config.json"
	RelayerConfigFileName             = "relayer_config.json"
)

type CardanoWallet struct {
	Multisig    *cardanoWallet.Wallet `json:"multisig"`
	MultisigFee *cardanoWallet.Wallet `json:"fee"`
}

type TestApexValidator struct {
	ID          int
	APIPort     int
	dataDirPath string
	cluster     *framework.TestCluster
	server      *framework.TestServer
	node        *framework.Node
}

func NewTestApexValidator(
	dataDirPath string, id int, cluster *framework.TestCluster, server *framework.TestServer,
) *TestApexValidator {
	return &TestApexValidator{
		dataDirPath: filepath.Join(dataDirPath, fmt.Sprintf("validator_%d", id)),
		ID:          id,
		cluster:     cluster,
		server:      server,
	}
}

func (cv *TestApexValidator) GetBridgingConfigsDir() string {
	return filepath.Join(cv.dataDirPath, BridgingConfigsDir)
}

func (cv *TestApexValidator) GetValidatorComponentsConfig() string {
	return filepath.Join(cv.GetBridgingConfigsDir(), ValidatorComponentsConfigFileName)
}

func (cv *TestApexValidator) GetRelayerConfig() string {
	return filepath.Join(cv.GetBridgingConfigsDir(), RelayerConfigFileName)
}

func (cv *TestApexValidator) GetNexusTestDir() string {
	return filepath.Join(cv.dataDirPath, NexusDir)
}

func (cv *TestApexValidator) CardanoWalletCreate(chain ChainID) error {
	return RunCommand(ResolveApexBridgeBinary(), []string{
		"wallet-create",
		"--chain", chain,
		"--validator-data-dir", cv.server.DataDir(),
	}, os.Stdout)
}

func (cv *TestApexValidator) GetCardanoWallet(chainID string) (*CardanoWallet, error) {
	secretsMngr, err := cv.getSecretsManager(cv.dataDirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	keyName := fmt.Sprintf("%s%s_key", secretsCardano.CardanoKeyLocalPrefix, chainID)

	bytes, err := secretsMngr.GetSecret(keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	var cardanoWallet *CardanoWallet

	if err := json.Unmarshal(bytes, &cardanoWallet); err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	return cardanoWallet, nil
}

func (cv *TestApexValidator) RegisterChain(
	chain ChainID,
	tokenSupply *big.Int,
	chainType uint8,
) error {
	return RunCommand(ResolveApexBridgeBinary(), []string{
		"register-chain",
		"--chain", chain,
		"--type", fmt.Sprint(chainType),
		"--validator-data-dir", cv.server.DataDir(),
		"--token-supply", fmt.Sprint(tokenSupply),
		"--bridge-url", cv.server.JSONRPCAddr(),
		"--bridge-addr", contracts.Bridge.String(),
	}, os.Stdout)
}

func (cv *TestApexValidator) GenerateConfigs(
	apiPort int,
	apiKey string,
	telemetryConfig string,
	args ...string,
) error {
	cv.APIPort = apiPort
	logsPath := filepath.Join(cv.dataDirPath, BridgingLogsDir)
	dbsPath := filepath.Join(cv.dataDirPath, BridgingDBsDir)

	args = append([]string{
		"generate-configs",
		"--validator-data-dir", cv.server.DataDir(),
		"--output-dir", cv.GetBridgingConfigsDir(),
		"--output-validator-components-file-name", ValidatorComponentsConfigFileName,
		"--output-relayer-file-name", RelayerConfigFileName,
		"--bridge-node-url", cv.server.JSONRPCAddr(),
		"--bridge-sc-address", contracts.Bridge.String(),
		"--relayer-data-dir", cv.GetNexusTestDir(),
		"--logs-path", logsPath,
		"--dbs-path", dbsPath,
		"--api-port", fmt.Sprint(apiPort),
		"--api-keys", apiKey,
		"--telemetry", telemetryConfig,
		"--relayer-data-dir", cv.server.DataDir(),
	}, args...)

	if err := RunCommand(ResolveApexBridgeBinary(), args, os.Stdout); err != nil {
		return err
	}

	if err := common.CreateDirSafe(logsPath, 0770); err != nil {
		return err
	}

	return common.CreateDirSafe(dbsPath, 0770)
}

func (cv *TestApexValidator) Start(ctx context.Context, runAPI bool) (err error) {
	args := []string{
		"run-validator-components",
		"--config", cv.GetValidatorComponentsConfig(),
	}

	if runAPI {
		args = append(args, "--run-api")
	}

	cv.node, err = framework.NewNodeWithContext(ctx, ResolveApexBridgeBinary(), args, os.Stdout)

	return err
}

func (cv *TestApexValidator) Stop() error {
	if cv.node == nil {
		return errors.New("validator not started")
	}

	return cv.node.Stop()
}

func (cv *TestApexValidator) createEvmSpecificWallet(walletType string) error {
	return RunCommand(ResolveApexBridgeBinary(), []string{
		"wallet-create",
		"--chain", ChainIDNexus,
		"--validator-data-dir", cv.server.DataDir(),
		"--type", walletType,
	}, os.Stdout)
}

func (cv *TestApexValidator) getEvmBatcherWallet() (*bn256.PrivateKey, error) {
	secretsMngr, err := cv.getSecretsManager(cv.server.DataDir())
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	bytes, err := secretsMngr.GetSecret(secretsCardano.ValidatorBLSKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	bn256, err := bn256.UnmarshalPrivateKey(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal wallet: %w", err)
	}

	return bn256, nil
}

func (cv *TestApexValidator) getEvmRelayerWallet() (*crypto.ECDSAKey, error) {
	secretsMngr, err := cv.getSecretsManager(cv.server.DataDir())
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	keyName := fmt.Sprintf("%s%s_%s", secretsCardano.OtherKeyLocalPrefix, ChainIDNexus, "relayer_evm_key")

	strBytes, err := secretsMngr.GetSecret(keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	ecdsaRaw, err := hex.DecodeString(string(strBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ecdsa key: %w", err)
	}

	pk, err := crypto.NewECDSAKeyFromRawPrivECDSA(ecdsaRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ecdsa key: %w", err)
	}

	return pk, nil
}

func (cv *TestApexValidator) getSecretsManager(path string) (secretsCardano.SecretsManager, error) {
	return secretsHelper.CreateSecretsManager(&secretsCardano.SecretsManagerConfig{
		Path: path,
		Type: secretsCardano.Local,
	})
}
