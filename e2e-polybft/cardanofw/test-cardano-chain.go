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
	"strings"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/crypto"
	cardWallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

const (
	defaultFundTokenAmount = uint64(100_000_000_000)
	defaultPremineAmount   = uint64(20_000_000_000)
)

type TestCardanoChainConfig struct {
	IsEnabled              bool
	ID                     int
	NetworkType            cardWallet.CardanoNetworkType
	NodesCount             int
	InitialHotWalletAmount *big.Int
	FundAmount             uint64
	PreminesAddresses      []string
	PremineAmount          uint64
	SlotRoundingThreshold  uint64
	TTLInc                 uint64
}

func NewPrimeChainConfig() *TestCardanoChainConfig {
	return &TestCardanoChainConfig{
		IsEnabled:              true,
		ID:                     0,
		NetworkType:            cardWallet.TestNetNetwork,
		NodesCount:             4,
		InitialHotWalletAmount: new(big.Int).SetUint64(defaultFundTokenAmount), // big.NewInt(0),
		PremineAmount:          defaultPremineAmount,
		FundAmount:             defaultFundTokenAmount,
	}
}

func NewVectorChainConfig(isEnabled bool) *TestCardanoChainConfig {
	return &TestCardanoChainConfig{
		IsEnabled:              isEnabled,
		ID:                     1,
		NetworkType:            cardWallet.VectorTestNetNetwork,
		NodesCount:             4,
		InitialHotWalletAmount: new(big.Int).SetUint64(defaultFundTokenAmount), // big.NewInt(0),
		PremineAmount:          defaultPremineAmount,
		FundAmount:             defaultFundTokenAmount,
	}
}

type TestCardanoChain struct {
	config          *TestCardanoChainConfig
	cluster         *TestCardanoCluster
	multisigAddr    string
	multisigFeeAddr string
}

var _ ITestApexChain = (*TestCardanoChain)(nil)

func NewTestCardanoChain(config *TestCardanoChainConfig) ITestApexChain {
	if !config.IsEnabled {
		getFlag := func(suffix string) string {
			return fmt.Sprintf("--%s-%s", GetNetworkName(config.NetworkType), suffix)
		}

		return NewTestApexChainDummy([]string{
			getFlag("network-address"), "localhost:1000",
			getFlag("network-magic"), fmt.Sprint(GetNetworkMagic(config.NetworkType)),
			getFlag("network-id"), fmt.Sprint(config.NetworkType),
			getFlag("ogmios-url"), "http://localhost:5500",
		})
	}

	return &TestCardanoChain{
		config: config,
	}
}

func (ec *TestCardanoChain) RunChain(t *testing.T) error {
	t.Helper()

	networkName := GetNetworkName(ec.config.NetworkType)
	ogmiosLogsFilePath := filepath.Join("..", "..", "e2e-logs-cardano",
		fmt.Sprintf("ogmios-%s-%s.log", networkName, strings.ReplaceAll(t.Name(), "/", "_")))

	cluster, err := NewCardanoTestCluster(
		WithID(ec.config.ID+1),
		WithNodesCount(ec.config.NodesCount),
		WithStartTimeDelay(time.Second*5),
		WithPort(5100+ec.config.ID*100),
		WithOgmiosPort(1337+ec.config.ID),
		WithNetworkType(ec.config.NetworkType),
		WithConfigGenesisDir(networkName),
		WithInitialFunds(ec.config.PreminesAddresses, ec.config.PremineAmount),
	)
	if err != nil {
		return err
	}

	fmt.Printf("Waiting for sockets to be ready\n")

	if err := cluster.WaitForReady(time.Minute * 2); err != nil {
		return err
	}

	if err := cluster.StartOgmios(ec.config.ID, GetLogsFile(t, ogmiosLogsFilePath, false)); err != nil {
		return err
	}

	if err := cluster.WaitForBlockWithState(10, time.Second*120); err != nil {
		return err
	}

	fmt.Printf("Cluster %d is ready\n", ec.config.ID)

	ec.cluster = cluster

	return nil
}

func (ec *TestCardanoChain) Stop() error {
	if ec.cluster != nil {
		return ec.cluster.Stop()
	}

	return nil
}

func (ec *TestCardanoChain) CreateWallets(validator *TestApexValidator) error {
	return validator.CardanoWalletCreate(GetNetworkName(ec.config.NetworkType))
}

func (ec *TestCardanoChain) CreateAddresses(
	bladeAdmin *crypto.ECDSAKey, bridgeURL string,
) error {
	bridgeAdminPk, err := bladeAdmin.MarshallPrivateKey()
	if err != nil {
		return err
	}

	args := []string{
		"create-address",
		"--network-id", fmt.Sprint(ec.config.NetworkType),
		"--bridge-url", bridgeURL,
		"--bridge-addr", contracts.Bridge.String(),
		"--bridge-key", hex.EncodeToString(bridgeAdminPk),
		"--chain", GetNetworkName(ec.config.NetworkType),
	}

	var outb bytes.Buffer

	err = RunCommand(ResolveApexBridgeBinary(), args, io.MultiWriter(os.Stdout, &outb))
	if err != nil {
		return err
	}

	output := outb.String()
	reMultisig := regexp.MustCompile(`Multisig Address\s*=\s*([^\s]+)`)
	reFee := regexp.MustCompile(`Fee Payer Address\s*=\s*([^\s]+)`)

	if match := reMultisig.FindStringSubmatch(output); len(match) > 0 {
		ec.multisigAddr = match[1]
	}

	if match := reFee.FindStringSubmatch(output); len(match) > 0 {
		ec.multisigFeeAddr = match[1]
	}

	return nil
}

func (ec *TestCardanoChain) FundWallets(ctx context.Context) error {
	genesisWallet, err := GetGenesisWalletFromCluster(ec.cluster.Config.TmpDir, 1)
	if err != nil {
		return err
	}

	privateKey := hex.EncodeToString(genesisWallet.GetSigningKey())
	fundAmount := new(big.Int).SetUint64(ec.config.FundAmount)

	txHash, err := ec.SendTx(ctx, privateKey, ec.multisigAddr, fundAmount, nil)
	if err != nil {
		return err
	}

	fmt.Printf("%s multisig addr funded: %s\n", GetNetworkName(ec.config.NetworkType), txHash)

	txHash, err = ec.SendTx(ctx, privateKey, ec.multisigFeeAddr, fundAmount, nil)
	if err != nil {
		return err
	}

	fmt.Printf("%s fee addr funded: %s\n", GetNetworkName(ec.config.NetworkType), txHash)

	return nil
}

func (ec *TestCardanoChain) InitContracts(bridgeAdmin *crypto.ECDSAKey, bridgeURL string) error {
	return nil
}

func (ec *TestCardanoChain) RegisterChain(validator *TestApexValidator) error {
	return validator.RegisterChain(ec.ChainID(), ec.config.InitialHotWalletAmount, ChainTypeCardano)
}

func (ec *TestCardanoChain) GetGenerateConfigsParams(indx int) (result []string) {
	getFlag := func(suffix string) string {
		return fmt.Sprintf("--%s-%s", ec.ChainID(), suffix)
	}

	server := ec.cluster.Servers[indx%len(ec.cluster.Servers)]
	result = []string{
		getFlag("network-address"), server.NetworkAddress(),
		getFlag("network-magic"), fmt.Sprint(GetNetworkMagic(ec.config.NetworkType)),
		getFlag("network-id"), fmt.Sprint(ec.config.NetworkType),
		getFlag("ogmios-url"), ec.cluster.OgmiosURL(),
	}

	if ec.config.TTLInc > 0 {
		result = append(result, getFlag("ttl-slot-inc"), fmt.Sprint(ec.config.TTLInc))
	}

	if ec.config.SlotRoundingThreshold > 0 {
		result = append(result, getFlag("slot-rounding-threshold"), fmt.Sprint(ec.config.SlotRoundingThreshold))
	}

	return result
}

func (ec *TestCardanoChain) PopulateApexSystem(apexSystem *ApexSystem) {
	switch ec.ChainID() {
	case ChainIDPrime:
		apexSystem.PrimeInfo = CardanoChainInfo{
			NetworkAddress: ec.cluster.Servers[0].NetworkAddress(),
			OgmiosURL:      ec.cluster.OgmiosURL(),
			MultisigAddr:   ec.multisigAddr,
			FeeAddr:        ec.multisigFeeAddr,
		}
	case ChainIDVector:
		apexSystem.VectorInfo = CardanoChainInfo{
			NetworkAddress: ec.cluster.Servers[0].NetworkAddress(),
			OgmiosURL:      ec.cluster.OgmiosURL(),
			MultisigAddr:   ec.multisigAddr,
			FeeAddr:        ec.multisigFeeAddr,
		}
	}
}

func (ec *TestCardanoChain) ChainID() string {
	return GetNetworkName(ec.config.NetworkType)
}

func (ec *TestCardanoChain) GetAddressBalance(ctx context.Context, addr string) (*big.Int, error) {
	utxos, err := cardWallet.NewTxProviderOgmios(ec.cluster.OgmiosURL()).GetUtxos(ctx, addr)
	if err != nil {
		return nil, err
	}

	return new(big.Int).SetUint64(cardWallet.GetUtxosSum(utxos)), nil
}

func (ec *TestCardanoChain) BridgingRequest(
	ctx context.Context, destChainID ChainID, privateKey string, receivers map[string]*big.Int,
) (string, error) {
	const (
		feeAmount              = uint64(1_100_000)
		waitForTxNumRetries    = 60
		waitForTxRetryWaitTime = time.Second * 2
	)

	privateKeyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return "", err
	}

	verificationKeys := cardWallet.GetVerificationKeyFromSigningKey(privateKeyBytes)
	totalAmount := new(big.Int).SetUint64(feeAmount)
	receiversMap := make(map[string]uint64, len(receivers))

	for addr, amount := range receivers {
		totalAmount.Add(totalAmount, amount)
		receiversMap[addr] = amount.Uint64()
	}

	senderAddr, err := cardWallet.NewEnterpriseAddress(ec.config.NetworkType, verificationKeys)
	if err != nil {
		return "", err
	}

	bridgingRequestMetadata, err := CreateCardanoBridgingMetaData(
		senderAddr.String(), receiversMap, destChainID, feeAmount)
	if err != nil {
		return "", err
	}

	return ec.SendTx(ctx, privateKey, ec.multisigAddr, totalAmount, bridgingRequestMetadata)
}

func (ec *TestCardanoChain) SendTx(
	ctx context.Context, privateKey string, receiverAddr string, amount *big.Int, data []byte,
) (string, error) {
	const (
		waitForTxNumRetries    = 75
		waitForTxRetryWaitTime = time.Second * 2
	)

	privateKeyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return "", err
	}

	wallet := cardWallet.NewWallet(cardWallet.GetVerificationKeyFromSigningKey(privateKeyBytes), privateKeyBytes)
	txProvider := cardWallet.NewTxProviderOgmios(ec.cluster.OgmiosURL())

	txHash, err := SendTx(ctx, txProvider, wallet,
		amount.Uint64(), receiverAddr, ec.config.NetworkType, data)
	if err != nil {
		return "", err
	}

	err = cardWallet.WaitForTxHashInUtxos(
		ctx, txProvider, receiverAddr, txHash, waitForTxNumRetries, waitForTxRetryWaitTime, IsRecoverableError)
	if err != nil {
		return "", err
	}

	return txHash, nil
}
