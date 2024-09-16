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

	polybftWallet "github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/types"
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

type TestCardanoValidator struct {
	ID                     int
	APIPort                int
	dataDirPath            string
	cluster                *framework.TestCluster
	server                 *framework.TestServer
	node                   *framework.Node
	BatcherBN256PrivateKey *bn256.PrivateKey
}

func NewTestCardanoValidator(
	dataDirPath string,
	id int,
) *TestCardanoValidator {
	return &TestCardanoValidator{
		dataDirPath: filepath.Join(dataDirPath, fmt.Sprintf("validator_%d", id)),
		ID:          id,
	}
}

func (cv *TestCardanoValidator) SetClusterAndServer(
	cluster *framework.TestCluster, server *framework.TestServer,
) error {
	cv.cluster = cluster
	cv.server = server
	// move wallets files
	srcPath := filepath.Join(cv.dataDirPath, secretsCardano.CardanoFolderLocal)
	dstPath := filepath.Join(cv.server.DataDir(), secretsCardano.CardanoFolderLocal)

	if err := common.CreateDirSafe(dstPath, 0750); err != nil {
		return fmt.Errorf("failed to create dst directory: %w", err)
	}

	files, err := os.ReadDir(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, file := range files {
		sourcePath := filepath.Join(srcPath, file.Name())
		destPath := filepath.Join(dstPath, file.Name())
		// Move the file
		if err := os.Rename(sourcePath, destPath); err != nil {
			return fmt.Errorf("failed to move file %s: %w", file.Name(), err)
		}
	}

	return nil
}

func (cv *TestCardanoValidator) GetBridgingConfigsDir() string {
	return filepath.Join(cv.dataDirPath, BridgingConfigsDir)
}

func (cv *TestCardanoValidator) GetValidatorComponentsConfig() string {
	return filepath.Join(cv.GetBridgingConfigsDir(), ValidatorComponentsConfigFileName)
}

func (cv *TestCardanoValidator) GetRelayerConfig() string {
	return filepath.Join(cv.GetBridgingConfigsDir(), RelayerConfigFileName)
}

func (cv *TestCardanoValidator) GetNexusTestDir() string {
	return filepath.Join(cv.dataDirPath, NexusDir)
}

func (cv *TestCardanoValidator) CardanoWalletCreate(chainID string) error {
	return RunCommand(ResolveApexBridgeBinary(), []string{
		"wallet-create",
		"--chain", chainID,
		"--validator-data-dir", cv.dataDirPath,
	}, os.Stdout)
}

func (cv *TestCardanoValidator) GetCardanoWallet(chainID string) (*CardanoWallet, error) {
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

func (cv *TestCardanoValidator) RegisterChain(
	chainID string,
	multisigAddr string,
	multisigFeeAddr string,
	tokenSupply *big.Int,
	chainType uint8,
) error {
	return RunCommand(ResolveApexBridgeBinary(), []string{
		"register-chain",
		"--chain", chainID,
		"--type", fmt.Sprint(chainType),
		"--validator-data-dir", cv.server.DataDir(),
		"--addr", multisigAddr,
		"--addr-fee", multisigFeeAddr,
		"--token-supply", fmt.Sprint(tokenSupply),
		"--bridge-url", cv.server.JSONRPCAddr(),
		"--bridge-addr", BridgeSCAddr,
	}, os.Stdout)
}

func (cv *TestCardanoValidator) GenerateConfigs(
	primeNetworkAddress string,
	primeNetworkMagic uint,
	primeNetworkID uint,
	primeOgmiosURL string,
	primeSlotRoundingThreshold uint64,
	primeTTLInc uint64,
	vectorNetworkAddress string,
	vectorNetworkMagic uint,
	vectorNetworkID uint,
	vectorOgmiosURL string,
	vectorSlotRoundingThreshold uint64,
	vectorTTLInc uint64,
	apiPort int,
	apiKey string,
	telemetryConfig string,
	nexusGatewayAddr string,
	nexusRelayerAddr string,
	nexusNodeURL string,
) error {
	cv.APIPort = apiPort

	args := []string{
		"generate-configs",
		"--validator-data-dir", cv.server.DataDir(),
		"--output-dir", cv.GetBridgingConfigsDir(),
		"--output-validator-components-file-name", ValidatorComponentsConfigFileName,
		"--output-relayer-file-name", RelayerConfigFileName,
		"--prime-network-address", primeNetworkAddress,
		"--prime-network-magic", fmt.Sprint(primeNetworkMagic),
		"--prime-network-id", fmt.Sprint(primeNetworkID),
		"--prime-ogmios-url", primeOgmiosURL,
		"--vector-network-address", vectorNetworkAddress,
		"--vector-network-magic", fmt.Sprint(vectorNetworkMagic),
		"--vector-network-id", fmt.Sprint(vectorNetworkID),
		"--vector-ogmios-url", vectorOgmiosURL,
		"--bridge-node-url", cv.server.JSONRPCAddr(),
		"--bridge-sc-address", BridgeSCAddr,
		"--nexus-sc-address", nexusGatewayAddr,
		"--nexus-relayer-addr", nexusRelayerAddr,
		"--nexus-node-url", nexusNodeURL,
		"--relayer-data-dir", cv.GetNexusTestDir(),
		"--logs-path", filepath.Join(cv.dataDirPath, BridgingLogsDir),
		"--dbs-path", filepath.Join(cv.dataDirPath, BridgingDBsDir),
		"--api-port", fmt.Sprint(apiPort),
		"--api-keys", apiKey,
		"--telemetry", telemetryConfig,
		"--relayer-data-dir", cv.server.DataDir(),
	}

	if primeTTLInc > 0 {
		args = append(args,
			"--prime-ttl-slot-inc", fmt.Sprint(primeTTLInc),
		)
	}

	if vectorTTLInc > 0 {
		args = append(args,
			"--vector-ttl-slot-inc", fmt.Sprint(vectorTTLInc),
		)
	}

	if primeSlotRoundingThreshold > 0 {
		args = append(args, "--prime-slot-rounding-threshold", fmt.Sprint(primeSlotRoundingThreshold))
	}

	if vectorSlotRoundingThreshold > 0 {
		args = append(args, "--vector-slot-rounding-threshold", fmt.Sprint(vectorSlotRoundingThreshold))
	}

	return RunCommand(ResolveApexBridgeBinary(), args, os.Stdout)
}

func (cv *TestCardanoValidator) Start(ctx context.Context, runAPI bool) (err error) {
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

func (cv *TestCardanoValidator) Stop() error {
	if cv.node == nil {
		return errors.New("validator not started")
	}

	return cv.node.Stop()
}

func (cv *TestCardanoValidator) getValidatorEthAddress() (types.Address, error) {
	secretsMngr, err := cv.getSecretsManager(cv.server.DataDir())
	if err != nil {
		return types.Address{}, fmt.Errorf("failed to create secrets manager: %w", err)
	}

	key, err := polybftWallet.GetEcdsaFromSecret(secretsMngr)
	if err != nil {
		return types.Address{}, fmt.Errorf("failed to load wallet: %w", err)
	}

	return key.Address(), nil
}

func (cv *TestCardanoValidator) createSpecificWallet(walletType string) error {
	return RunCommand(ResolveApexBridgeBinary(), []string{
		"wallet-create",
		"--chain", ChainIDNexus,
		"--validator-data-dir", cv.server.DataDir(),
		"--type", walletType,
	}, os.Stdout)
}

func (cv *TestCardanoValidator) getBatcherWallet(loadFromBlade bool) (*bn256.PrivateKey, error) {
	secretsMngr, err := cv.getSecretsManager(cv.server.DataDir())
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	var keyName string

	if !loadFromBlade {
		keyName = fmt.Sprintf("%s%s_%s", secretsCardano.OtherKeyLocalPrefix, ChainIDNexus, "batcher_evm_key")
	} else {
		keyName = secretsCardano.ValidatorBLSKey
	}

	bytes, err := secretsMngr.GetSecret(keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}

	bn256, err := bn256.UnmarshalPrivateKey(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal wallet: %w", err)
	}

	return bn256, nil
}

func (cv *TestCardanoValidator) getRelayerWallet() (*crypto.ECDSAKey, error) {
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

func (cv *TestCardanoValidator) getSecretsManager(path string) (secretsCardano.SecretsManager, error) {
	return secretsHelper.CreateSecretsManager(&secretsCardano.SecretsManagerConfig{
		Path: path,
		Type: secretsCardano.Local,
	})
}
