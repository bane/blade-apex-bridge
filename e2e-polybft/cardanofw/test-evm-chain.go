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
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/command/genesis"
	"github.com/0xPolygon/polygon-edge/consensus/polybft"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
)

const (
	defaultFundEthTokenAmount        = uint64(100_000)
	defaultPremineEthTokenAmount     = uint64(100_000)
	defaultFundRelayerEthTokenAmount = uint64(5)
)

type TestEVMChainConfig struct {
	ChainID   string
	IsEnabled bool

	ValidatorCount         int
	InitialHotWalletAmount *big.Int
	FundAmount             *big.Int
	FundRelayerAmount      *big.Int
	PreminesAddresses      []types.Address
	PremineAmount          *big.Int
	StartingPort           int64
	ApexConfig             uint8
	BurnContractInfo       *polybft.BurnContractInfo
}

func NewNexusChainConfig(isEnabled bool) *TestEVMChainConfig {
	return &TestEVMChainConfig{
		ChainID:        ChainIDNexus,
		IsEnabled:      isEnabled,
		ValidatorCount: 4,
		StartingPort:   int64(30400),
		BurnContractInfo: &polybft.BurnContractInfo{
			BlockNumber: 0,
			Address:     types.ZeroAddress,
		},
		ApexConfig:             genesis.ApexConfigNexus,
		InitialHotWalletAmount: ethgo.Ether(defaultFundEthTokenAmount), // big.NewInt(0),
		PremineAmount:          ethgo.Ether(defaultPremineEthTokenAmount),
		FundAmount:             ethgo.Ether(defaultFundEthTokenAmount),
		FundRelayerAmount:      ethgo.Ether(defaultFundRelayerEthTokenAmount),
	}
}

type TestEVMChain struct {
	config        *TestEVMChainConfig
	admin         *crypto.ECDSAKey
	cluster       *framework.TestCluster
	gatewayAddr   types.Address
	relayerWallet *crypto.ECDSAKey
}

var _ ITestApexChain = (*TestEVMChain)(nil)

func NewTestEVMChain(config *TestEVMChainConfig) (ITestApexChain, error) {
	if !config.IsEnabled {
		getFlag := func(suffix string) string {
			return fmt.Sprintf("--%s-%s", config.ChainID, suffix)
		}

		return NewTestApexChainDummy([]string{
			getFlag("node-url"), "http://localhost:5500",
		}), nil
	}

	admin, err := crypto.GenerateECDSAKey()
	if err != nil {
		return nil, err
	}

	return &TestEVMChain{
		config: config,
		admin:  admin,
	}, nil
}

func (ec *TestEVMChain) RunChain(t *testing.T) error {
	t.Helper()

	cluster := framework.NewTestCluster(t, ec.config.ValidatorCount,
		framework.WithPremine(ec.admin.Address()),
		framework.WithPremine(ec.config.PreminesAddresses...),
		framework.WithInitialPort(ec.config.StartingPort),
		framework.WithLogsDirSuffix(ec.config.ChainID),
		framework.WithBladeAdmin(ec.admin.Address().String()),
		framework.WithApexConfig(ec.config.ApexConfig),
		framework.WithBurnContract(ec.config.BurnContractInfo),
	)

	if err := cluster.WaitForBlock(1, time.Minute); err != nil {
		return err
	}

	fmt.Printf("%s chain setup done: port = %d\n", ec.config.ChainID, ec.config.StartingPort)

	ec.cluster = cluster

	return nil
}

func (ec *TestEVMChain) Stop() error {
	if ec.cluster != nil {
		ec.cluster.Stop()
	}

	return nil
}

func (ec *TestEVMChain) CreateWallets(validator *TestApexValidator) error {
	_, err := validator.getEvmBatcherWallet()
	if err != nil {
		return err
	}

	if validator.ID == RunRelayerOnValidatorID {
		if err = validator.createEvmSpecificWallet("relayer-evm"); err != nil {
			return err
		}

		ec.relayerWallet, err = validator.getEvmRelayerWallet()
		if err != nil {
			return err
		}
	}

	return nil
}

func (ec *TestEVMChain) CreateAddresses(
	bladeAdmin *crypto.ECDSAKey, bridgeURL string,
) error {
	return nil
}

func (ec *TestEVMChain) FundWallets(ctx context.Context) error {
	key, err := ec.admin.MarshallPrivateKey()
	if err != nil {
		return err
	}

	_, err = ec.SendTx(ctx, hex.EncodeToString(key), ec.gatewayAddr.String(), ec.config.FundAmount, nil)
	if err != nil {
		return err
	}

	_, err = ec.SendTx(
		ctx, hex.EncodeToString(key), ec.relayerWallet.Address().String(), ec.config.FundRelayerAmount, nil)

	return err
}

func (ec *TestEVMChain) InitContracts(bridgeAdmin *crypto.ECDSAKey, bridgeURL string) error {
	workingDirectory := filepath.Join(os.TempDir(), "deploy-apex-bridge-evm-gateway")
	// do not remove directory, try to reuse it next time if still exists
	if err := common.CreateDirSafe(workingDirectory, 0750); err != nil {
		return err
	}

	pk, err := ec.admin.MarshallPrivateKey()
	if err != nil {
		return err
	}

	bridgeAdminPk, err := bridgeAdmin.MarshallPrivateKey()
	if err != nil {
		return err
	}

	var (
		b      bytes.Buffer
		params = []string{
			"deploy-evm",
			"--url", ec.cluster.Servers[0].JSONRPCAddr(),
			"--key", hex.EncodeToString(pk),
			"--bridge-url", bridgeURL,
			"--bridge-addr", contracts.Bridge.String(),
			"--bridge-key", hex.EncodeToString(bridgeAdminPk),
			"--dir", workingDirectory,
			"--clone",
		}
	)

	err = RunCommand(ResolveApexBridgeBinary(), params, io.MultiWriter(os.Stdout, &b))
	if err != nil {
		return err
	}

	output := b.String()
	reGateway := regexp.MustCompile(`Gateway Proxy Address\s*=\s*0x([a-fA-F0-9]+)`)

	if match := reGateway.FindStringSubmatch(output); len(match) > 0 {
		ec.gatewayAddr = types.StringToAddress(match[1])

		return nil
	}

	return errors.New("cannot find gateway address")
}

func (ec *TestEVMChain) RegisterChain(validator *TestApexValidator) error {
	return validator.RegisterChain(ec.config.ChainID, ec.config.InitialHotWalletAmount, ChainTypeEVM)
}

func (ec *TestEVMChain) GetGenerateConfigsParams(indx int) (result []string) {
	chainID := ec.ChainID()
	getFlag := func(suffix string) string {
		return fmt.Sprintf("--%s-%s", chainID, suffix)
	}

	server := ec.cluster.Servers[indx%len(ec.cluster.Servers)]

	return []string{
		getFlag("node-url"), server.JSONRPCAddr(),
	}
}

func (ec *TestEVMChain) PopulateApexSystem(apexSystem *ApexSystem) {
	if ec.config.ChainID == ChainIDNexus {
		apexSystem.NexusInfo = EVMChainInfo{
			GatewayAddress: ec.gatewayAddr,
			Node:           ec.cluster.Servers[0],
			RelayerAddress: ec.relayerWallet.Address(),
			AdminKey:       ec.admin,
		}
	}
}

func (ec *TestEVMChain) ChainID() string {
	return ec.config.ChainID
}

func (ec *TestEVMChain) GetAddressBalance(ctx context.Context, addr string) (*big.Int, error) {
	amount, err := ec.cluster.Servers[0].JSONRPC().GetBalance(types.StringToAddress(addr), jsonrpc.LatestBlockNumberOrHash)
	if err != nil {
		return nil, err
	}

	return amount, err
}

func (ec *TestEVMChain) BridgingRequest(
	ctx context.Context, destChainID ChainID, privateKey string, receivers map[string]*big.Int,
) (string, error) {
	const (
		feeAmount = 1_100_000
	)

	feeAmountWei := new(big.Int).SetUint64(feeAmount)
	feeAmountWei.Mul(feeAmountWei, new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil))

	params := []string{
		"sendtx",
		"--tx-type", "evm",
		"--gateway-addr", ec.gatewayAddr.String(),
		fmt.Sprintf("--%s-url", ec.config.ChainID), ec.cluster.Servers[0].JSONRPCAddr(),
		"--key", privateKey,
		"--chain-dst", destChainID,
		"--fee", feeAmountWei.String(),
	}

	for addr, amount := range receivers {
		params = append(params,
			"--receiver", fmt.Sprintf("%s:%s", addr, amount),
		)
	}

	var outb bytes.Buffer

	if err := RunCommand(ResolveApexBridgeBinary(), params, io.MultiWriter(os.Stdout, &outb)); err != nil {
		return "", err
	}

	output := outb.String()
	reTxHash := regexp.MustCompile(`Tx Hash\s*=\s*([^\s]+)`)

	if match := reTxHash.FindStringSubmatch(output); len(match) > 0 {
		return match[1], nil
	}

	return "", errors.New("tx hash not found in command output")
}

func (ec *TestEVMChain) SendTx(
	ctx context.Context, privateKey string, receiver string, amount *big.Int, data []byte,
) (string, error) {
	privateKeyECDSA, err := crypto.HexToECDSA(privateKey)
	if err != nil {
		return "", err
	}

	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithIPAddress(ec.cluster.Servers[0].JSONRPCAddr()),
		txrelayer.WithReceiptsTimeout(1*time.Minute),
		txrelayer.WithEstimateGasFallback(),
	)
	if err != nil {
		return "", err
	}

	key := crypto.NewECDSAKey(privateKeyECDSA)
	receiverAddr := types.StringToAddress(receiver)

	receipt, err := txRelayer.SendTransaction(types.NewTx(types.NewLegacyTx(
		types.WithFrom(key.Address()),
		types.WithValue(amount),
		types.WithInput(data),
		types.WithTo(&receiverAddr),
	)), key)
	if err != nil {
		return "", err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return "", fmt.Errorf("fund relayer failed: %d", receipt.Status)
	}

	return receipt.TransactionHash.String(), nil
}
