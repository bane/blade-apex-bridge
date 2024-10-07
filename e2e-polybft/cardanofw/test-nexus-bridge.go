package cardanofw

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/0xPolygon/polygon-edge/command/genesis"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
)

type NexusBridgeOption func(*TestEVMBridge)

type TestEVMBridge struct {
	Admin   *wallet.Account
	Config  *ApexSystemConfig
	Cluster *framework.TestCluster

	NativeTokenWallet types.Address
	Gateway           types.Address
}

func (ec *TestEVMBridge) SetupChain(
	t *testing.T, bridgeURL string, bridgeAdmin *crypto.ECDSAKey, relayerAddr types.Address,
) {
	t.Helper()

	require.NoError(t, ec.InitSmartContracts(bridgeURL, bridgeAdmin))

	txn := ec.Cluster.Transfer(t,
		ec.Admin.Ecdsa,
		ec.GetHotWalletAddress(),
		ethgo.Ether(ec.Config.FundEthTokenAmount),
	)
	require.NotNil(t, txn)
	require.True(t, txn.Succeed())

	txn = ec.Cluster.Transfer(t,
		ec.Admin.Ecdsa,
		relayerAddr,
		ethgo.Ether(1),
	)
	require.NotNil(t, txn)
	require.True(t, txn.Succeed())
}

func (ec *TestEVMBridge) NodeURL() string {
	return fmt.Sprintf("http://localhost:%d", ec.Config.NexusStartingPort)
}

func (ec *TestEVMBridge) GetDefaultJSONRPCAddr() string {
	return ec.Cluster.Servers[0].JSONRPCAddr()
}

func (ec *TestEVMBridge) GetGatewayAddress() types.Address {
	return ec.Gateway
}

func (ec *TestEVMBridge) GetHotWalletAddress() types.Address {
	return ec.NativeTokenWallet
}

func (ec *TestEVMBridge) InitSmartContracts(
	bridgeURL string, bridgeAdmin *crypto.ECDSAKey,
) error {
	workingDirectory := filepath.Join(os.TempDir(), "deploy-apex-bridge-evm-gateway")
	// do not remove directory, try to reuse it next time if still exists
	if err := common.CreateDirSafe(workingDirectory, 0750); err != nil {
		return err
	}

	pk, err := ec.Admin.Ecdsa.MarshallPrivateKey()
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
			"--url", ec.GetDefaultJSONRPCAddr(),
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
	reNativeTokenWallet := regexp.MustCompile(`NativeTokenWallet Proxy Address\s*=\s*0x([a-fA-F0-9]+)`)

	if match := reGateway.FindStringSubmatch(output); len(match) > 0 {
		ec.Gateway = types.StringToAddress(match[1])
	}

	if match := reNativeTokenWallet.FindStringSubmatch(output); len(match) > 0 {
		ec.NativeTokenWallet = types.StringToAddress(match[1])
	}

	return nil
}

func (ec *TestEVMBridge) GetEthAmount(ctx context.Context, wallet *wallet.Account) (*big.Int, error) {
	return ec.GetAddressEthAmount(ctx, wallet.Address())
}

func (ec *TestEVMBridge) GetAddressEthAmount(ctx context.Context, addr types.Address) (*big.Int, error) {
	ethAmount, err := ec.Cluster.Servers[0].JSONRPC().GetBalance(addr, jsonrpc.LatestBlockNumberOrHash)
	if err != nil {
		return nil, err
	}

	return ethAmount, err
}

func RunEVMChain(
	t *testing.T,
	config *ApexSystemConfig,
) (*TestEVMBridge, error) {
	t.Helper()

	admin, err := wallet.GenerateAccount()
	if err != nil {
		return nil, err
	}

	cluster := framework.NewTestCluster(t, config.NexusValidatorCount,
		framework.WithPremine(admin.Address()),
		framework.WithPremine(config.NexusInitialFundsKeys...),
		framework.WithInitialPort(config.NexusStartingPort),
		framework.WithLogsDirSuffix(ChainIDNexus),
		framework.WithBladeAdmin(admin.Address().String()),
		framework.WithApexConfig(genesis.ApexConfigNexus),
		framework.WithBurnContract(config.NexusBurnContractInfo),
	)

	cluster.WaitForReady(t)

	fmt.Printf("EVM chain %d setup done\n", config.NexusStartingPort)

	return &TestEVMBridge{
		Admin:   admin,
		Cluster: cluster,

		Config: config,
	}, nil
}
