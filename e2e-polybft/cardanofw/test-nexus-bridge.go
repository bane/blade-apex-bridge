package cardanofw

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/command/genesis"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/wallet"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/jsonrpc"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/0xPolygon/polygon-edge/types"
	ci "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
)

const (
	FundEthTokenAmount = uint64(100_000)
)

type NexusBridgeOption func(*TestEVMBridge)

type TestEVMBridge struct {
	Admin   *wallet.Account
	Cluster *framework.TestCluster

	contracts *ContractsAddrs

	Config *ApexSystemConfig
}

func (ec *TestEVMBridge) GetGatewayAddress() types.Address {
	return ec.contracts.gateway
}

type ContractsAddrs struct {
	nativeTokenPredicate types.Address
	nativeTokenWallet    types.Address
	validators           types.Address
	gateway              types.Address
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

func SetupAndRunNexusBridge(
	t *testing.T,
	ctx context.Context,
	apexSystem *ApexSystem,
) {
	t.Helper()

	err := apexSystem.Nexus.deployContracts(apexSystem)
	require.NoError(t, err)

	apexSystem.Nexus.Cluster.Transfer(t,
		apexSystem.Nexus.Admin.Ecdsa,
		apexSystem.Nexus.contracts.nativeTokenWallet,
		ethgo.Ether(FundEthTokenAmount),
	)

	apexSystem.Nexus.Cluster.Transfer(t,
		apexSystem.Nexus.Admin.Ecdsa,
		apexSystem.Bridge.GetRelayerWalletAddr(),
		ethgo.Ether(1),
	)
}

func (ec *TestEVMBridge) SendTxEvm(privateKey string, receiver string, amount *big.Int) error {
	return ec.SendTxEvmMultipleReceivers(privateKey, []string{receiver}, amount)
}

func (ec *TestEVMBridge) SendTxEvmMultipleReceivers(privateKey string, receivers []string, amount *big.Int) error {
	params := []string{
		"sendtx",
		"--tx-type", "evm",
		"--gateway-addr", ec.contracts.gateway.String(),
		"--nexus-url", ec.Cluster.Servers[0].JSONRPCAddr(),
		"--key", privateKey,
		"--chain-dst", "prime",
		"--fee", "1000010000000000000",
	}

	receiversParam := make([]string, 0)
	for i := 0; i < len(receivers); i++ {
		receiversParam = append(receiversParam, "--receiver")
		receiversParam = append(receiversParam, fmt.Sprintf("%s:%s", receivers[i], amount))
	}

	params = append(params, receiversParam...)

	return RunCommand(ResolveApexBridgeBinary(), params, os.Stdout)
}

func GetEthAmount(ctx context.Context, evmChain *TestEVMBridge, wallet *wallet.Account) (*big.Int, error) {
	ethAmount, err := evmChain.Cluster.Servers[0].JSONRPC().GetBalance(wallet.Address(), jsonrpc.LatestBlockNumberOrHash)
	if err != nil {
		return nil, err
	}

	return ethAmount, err
}

func WaitForEthAmount(
	ctx context.Context,
	evmChain *TestEVMBridge,
	wallet *wallet.Account,
	cmpHandler func(*big.Int) bool,
	numRetries int,
	waitTime time.Duration,
	isRecoverableError ...ci.IsRecoverableErrorFn,
) error {
	return ci.ExecuteWithRetry(ctx, numRetries, waitTime, func() (bool, error) {
		ethers, err := GetEthAmount(ctx, evmChain, wallet)

		return err == nil && cmpHandler(ethers), err
	}, isRecoverableError...)
}

func (ec *TestEVMBridge) NodeURL() string {
	return fmt.Sprintf("http://localhost:%d", ec.Config.NexusStartingPort)
}

func (ec *TestEVMBridge) deployContracts(apexSystem *ApexSystem) error {
	err := InitNexusContracts()
	if err != nil {
		return err
	}

	ec.contracts = &ContractsAddrs{}

	txRelayer, err := txrelayer.NewTxRelayer(txrelayer.WithClient(ec.Cluster.Servers[0].JSONRPC()))
	if err != nil {
		return err
	}

	// Deploy contracts with proxy & call "initialize"
	nativeTokenPredicateInit, _ := NativeTokenPredicate.Abi.Methods["initialize"].Encode(map[string]interface{}{})

	ec.contracts.nativeTokenPredicate, err =
		deployContractWithProxy(txRelayer, ec.Admin, NativeTokenPredicate, nativeTokenPredicateInit)
	if err != nil {
		return err
	}

	nativeTokenWalletInit, _ := NativeTokenWallet.Abi.Methods["initialize"].Encode(map[string]interface{}{})

	ec.contracts.nativeTokenWallet, err =
		deployContractWithProxy(txRelayer, ec.Admin, NativeTokenWallet, nativeTokenWalletInit)
	if err != nil {
		return err
	}

	validatorAddresses := make([]types.Address, len(apexSystem.Bridge.validators))
	for idx, validator := range apexSystem.Bridge.validators {
		validatorAddresses[idx], err = validator.getValidatorEthAddress()
		if err != nil {
			return err
		}
	}

	validatorsInit, _ := Validators.Abi.Methods["initialize"].Encode(map[string]interface{}{
		"_validators": validatorAddresses,
	})

	ec.contracts.validators, err = deployContractWithProxy(txRelayer, ec.Admin, Validators, validatorsInit)
	if err != nil {
		return err
	}

	gatewayInit, _ := Gateway.Abi.Methods["initialize"].Encode(map[string]interface{}{})

	ec.contracts.gateway, err = deployContractWithProxy(txRelayer, ec.Admin, Gateway, gatewayInit)
	if err != nil {
		return err
	}

	// Call "setDependencies"
	err = ec.contracts.gatewaySetDependencies(txRelayer, ec.Admin)
	if err != nil {
		return err
	}

	err = ec.contracts.nativeTokenPredicateSetDependencies(txRelayer, ec.Admin)
	if err != nil {
		return err
	}

	err = ec.contracts.nativeTokenWalletSetDependencies(txRelayer, ec.Admin, big.NewInt(0))
	if err != nil {
		return err
	}

	err = ec.contracts.validatorsSetDependencies(txRelayer, ec.Admin, apexSystem.Bridge.validators)
	if err != nil {
		return err
	}

	return nil
}

func deployContractWithProxy(
	txRelayer txrelayer.TxRelayer,
	admin *wallet.Account,
	contract *contracts.Artifact,
	initParams []byte,
) (addr types.Address, err error) {
	// deploy contract
	receipt, err := txRelayer.SendTransaction(
		types.NewTx(types.NewLegacyTx(
			types.WithFrom(admin.Ecdsa.Address()),
			types.WithInput(contract.Bytecode),
		)),
		admin.Ecdsa)
	if err != nil {
		return addr, err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return addr, fmt.Errorf("deploying smart contract failed: %d", receipt.Status)
	}

	input, _ := ERC1967Proxy.Abi.Constructor.Inputs.Encode(map[string]interface{}{
		"implementation": types.Address(receipt.ContractAddress),
		"_data":          initParams,
	})
	input = append(ERC1967Proxy.Bytecode, input...)

	// deploy proxy contract and call initialize
	receipt, err = txRelayer.SendTransaction(
		types.NewTx(types.NewLegacyTx(
			types.WithFrom(admin.Ecdsa.Address()),
			types.WithInput(input),
		)),
		admin.Ecdsa)
	if err != nil {
		return addr, err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return addr, fmt.Errorf("deploying proxy smart contract failed: %d", receipt.Status)
	}

	return types.Address(receipt.ContractAddress), nil
}

func (ca *ContractsAddrs) gatewaySetDependencies(
	txRelayer txrelayer.TxRelayer,
	admin *wallet.Account,
) error {
	gateway := GatewaySetDependenciesFn{
		NativeTokenPredicate_: ca.nativeTokenPredicate,
		Validators_:           ca.validators,
	}

	encoded, err := gateway.EncodeAbi()
	if err != nil {
		return err
	}

	receipt, err := txRelayer.SendTransaction(
		types.NewTx(types.NewLegacyTx(
			types.WithFrom(admin.Address()),
			types.WithTo(&ca.gateway),
			types.WithInput(encoded),
		)), admin.Ecdsa)
	if err != nil {
		return err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("calling setDependencies failed: %d", receipt.Status)
	}

	return nil
}

func (ca *ContractsAddrs) nativeTokenPredicateSetDependencies(
	txRelayer txrelayer.TxRelayer,
	admin *wallet.Account,
) error {
	nativeTokenPredicate := NativeTokenPredicateSetDependenciesFn{
		Gateway_:           ca.gateway,
		NativeTokenWallet_: ca.nativeTokenWallet,
	}

	encoded, err := nativeTokenPredicate.EncodeAbi()
	if err != nil {
		return err
	}

	receipt, err := txRelayer.SendTransaction(
		types.NewTx(types.NewLegacyTx(
			types.WithFrom(admin.Address()),
			types.WithTo(&ca.nativeTokenPredicate),
			types.WithInput(encoded),
		)), admin.Ecdsa)
	if err != nil {
		return err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("calling setDependencies failed: %d", receipt.Status)
	}

	return nil
}

func (ca *ContractsAddrs) nativeTokenWalletSetDependencies(
	txRelayer txrelayer.TxRelayer,
	admin *wallet.Account,
	tokenSupply *big.Int,
) error {
	nativeTokenWallet := NativeTokenWalletSetDependenciesFn{
		Predicate_: ca.nativeTokenPredicate,
		Supply_:    tokenSupply,
	}

	encoded, err := nativeTokenWallet.EncodeAbi()
	if err != nil {
		return err
	}

	receipt, err := txRelayer.SendTransaction(
		types.NewTx(types.NewLegacyTx(
			types.WithFrom(admin.Address()),
			types.WithTo(&ca.nativeTokenWallet),
			types.WithInput(encoded),
		)), admin.Ecdsa)
	if err != nil {
		return err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("calling setDependencies failed: %d", receipt.Status)
	}

	return nil
}

func (ca *ContractsAddrs) validatorsSetDependencies(
	txRelayer txrelayer.TxRelayer,
	admin *wallet.Account,
	validators []*TestCardanoValidator,
) error {
	validatorsData := ValidatorsSetDependenciesFn{
		Gateway_:   ca.gateway,
		ChainData_: makeValidatorChainData(validators),
	}

	encoded, err := validatorsData.EncodeAbi()
	if err != nil {
		return err
	}

	receipt, err := txRelayer.SendTransaction(
		types.NewTx(types.NewLegacyTx(
			types.WithFrom(admin.Address()),
			types.WithTo(&ca.validators),
			types.WithInput(encoded),
		)), admin.Ecdsa)
	if err != nil {
		return err
	} else if receipt.Status != uint64(types.ReceiptSuccess) {
		return fmt.Errorf("calling setDependencies failed: %d", receipt.Status)
	}

	return nil
}

func makeValidatorChainData(validators []*TestCardanoValidator) []*ValidatorAddressChainData {
	validatorAddrChainData := make([]*ValidatorAddressChainData, len(validators))

	for idx, validator := range validators {
		validatorAddr, _ := validator.getValidatorEthAddress()
		validatorAddrChainData[idx] = &ValidatorAddressChainData{
			Address_: validatorAddr,
			Data_: &ValidatorChainData{
				Key_: validator.BatcherBN256PrivateKey.PublicKey().ToBigInt(),
			},
		}
	}

	return validatorAddrChainData
}
