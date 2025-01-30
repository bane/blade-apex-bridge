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
	infracommon "github.com/Ethernal-Tech/cardano-infrastructure/common"
	infrawallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

const (
	defaultFundTokenAmount = uint64(100_000_000_000)
	defaultPremineAmount   = uint64(20_000_000_000)
)

type TestCardanoChainConfig struct {
	IsEnabled              bool
	ID                     int
	NetworkType            infrawallet.CardanoNetworkType
	NodesCount             int
	InitialHotWalletAmount *big.Int
	FundAmount             uint64
	FundFeeAmount          uint64
	PreminesAddresses      []string
	PremineAmount          uint64
	SlotRoundingThreshold  uint64
	TTLInc                 uint64
}

func NewPrimeChainConfig() *TestCardanoChainConfig {
	return &TestCardanoChainConfig{
		IsEnabled:              true,
		ID:                     0,
		NetworkType:            infrawallet.TestNetNetwork,
		NodesCount:             4,
		InitialHotWalletAmount: big.NewInt(0),
		PremineAmount:          defaultPremineAmount,
		FundAmount:             defaultFundTokenAmount,
		FundFeeAmount:          defaultFundTokenAmount,
	}
}

func NewVectorChainConfig(isEnabled bool) *TestCardanoChainConfig {
	return &TestCardanoChainConfig{
		IsEnabled:              isEnabled,
		ID:                     1,
		NetworkType:            infrawallet.VectorTestNetNetwork,
		NodesCount:             4,
		InitialHotWalletAmount: big.NewInt(0),
		PremineAmount:          defaultPremineAmount,
		FundAmount:             defaultFundTokenAmount,
		FundFeeAmount:          defaultFundTokenAmount,
	}
}

func NewRemotePrimeChainConfig() *TestCardanoChainConfig {
	return &TestCardanoChainConfig{
		IsEnabled:   true,
		ID:          0,
		NetworkType: infrawallet.TestNetNetwork,
	}
}

func NewRemoteVectorChainConfig(isEnabled bool) *TestCardanoChainConfig {
	return &TestCardanoChainConfig{
		IsEnabled:   isEnabled,
		ID:          1,
		NetworkType: infrawallet.VectorTestNetNetwork,
	}
}

type TestCardanoChain struct {
	config          *TestCardanoChainConfig
	cluster         *TestCardanoCluster
	ogmiosURL       string
	multisigAddr    string
	multisigFeeAddr string
	fundBlockSlot   uint64
	fundBlockHash   string
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

	fmt.Printf("Waiting for sockets to be ready %s (%d)\n", networkName, ec.config.ID)

	ec.cluster = cluster // at this point in time cluster has already been created

	if err := cluster.WaitForReady(time.Minute * 2); err != nil {
		return err
	}

	if err := cluster.StartOgmios(ec.config.ID, GetLogsFile(t, ogmiosLogsFilePath, false)); err != nil {
		return err
	}

	if err := cluster.WaitForBlockWithState(10, time.Second*120); err != nil {
		return err
	}

	ec.ogmiosURL = ec.cluster.OgmiosURL()

	fmt.Printf("Cluster %s (%d) is ready\n", networkName, ec.config.ID)

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
	privateKey, err := ec.GetAdminPrivateKey()
	if err != nil {
		return err
	}

	if ec.config.FundFeeAmount != 0 {
		txHash, err := ec.SendTx(
			ctx, privateKey, ec.multisigFeeAddr, new(big.Int).SetUint64(ec.config.FundFeeAmount), nil)
		if err != nil {
			return err
		}

		fmt.Printf("%s fee addr funded: %s\n", GetNetworkName(ec.config.NetworkType), txHash)
	}

	if ec.config.FundAmount != 0 {
		txHash, err := ec.SendTx(
			ctx, privateKey, ec.multisigAddr, new(big.Int).SetUint64(ec.config.FundAmount), nil)
		if err != nil {
			return err
		}

		fmt.Printf("%s multisig addr funded: %s\n", GetNetworkName(ec.config.NetworkType), txHash)
	}

	// retrieve latest tip
	tip, err := infrawallet.NewTxProviderOgmios(ec.ogmiosURL).GetTip(ctx)
	if err != nil {
		return err
	}

	ec.fundBlockHash = tip.Hash
	ec.fundBlockSlot = tip.Slot

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
		getFlag("ogmios-url"), ec.ogmiosURL,
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
	chainInfo := CardanoChainInfo{
		NetworkAddress: ec.cluster.Servers[0].NetworkAddress(),
		OgmiosURL:      ec.ogmiosURL,
		MultisigAddr:   ec.multisigAddr,
		FeeAddr:        ec.multisigFeeAddr,
		SocketPath:     ec.cluster.OgmiosServer.SocketPath(),
		FundBlockHash:  ec.fundBlockHash,
		FundBlockSlot:  ec.fundBlockSlot,
	}

	switch ec.ChainID() {
	case ChainIDPrime:
		apexSystem.PrimeInfo = chainInfo
	case ChainIDVector:
		apexSystem.VectorInfo = chainInfo
	}
}

func (ec *TestCardanoChain) ChainID() string {
	return GetNetworkName(ec.config.NetworkType)
}

func (ec *TestCardanoChain) GetAddressBalance(ctx context.Context, addr string) (*big.Int, error) {
	utxos, err := infrawallet.NewTxProviderOgmios(ec.ogmiosURL).GetUtxos(ctx, addr)
	if err != nil {
		return nil, err
	}

	sum := infrawallet.GetUtxosSum(utxos)

	return new(big.Int).SetUint64(sum[infrawallet.AdaTokenName]), nil
}

func (ec *TestCardanoChain) BridgingRequest(
	ctx context.Context, destChainID ChainID, privateKey string, receivers map[string]*big.Int, feeAmount *big.Int,
) (string, error) {
	paymentKey, stakeKey, err := FromCardanoPrivateKeyString(privateKey)
	if err != nil {
		return "", err
	}

	wallet := infrawallet.NewWallet(paymentKey, stakeKey)

	caddr, err := GetAddress(ec.config.NetworkType, wallet)
	if err != nil {
		return "", err
	}

	senderAddr := caddr.String()

	totalAmount := new(big.Int).Set(feeAmount)
	receiversMap := make(map[string]uint64, len(receivers))

	for addr, amount := range receivers {
		totalAmount.Add(totalAmount, amount)
		receiversMap[addr] = amount.Uint64()
	}

	bridgingRequestMetadata, err := CreateCardanoBridgingMetaData(
		senderAddr, receiversMap, destChainID, feeAmount.Uint64())
	if err != nil {
		return "", err
	}

	return ec.SendTx(ctx, privateKey, ec.multisigAddr, totalAmount, bridgingRequestMetadata)
}

func (ec *TestCardanoChain) SendTx(
	ctx context.Context, privateKey string, receiverAddr string, amount *big.Int, data []byte,
) (string, error) {
	const (
		retryCount    = 75
		retryWaitTime = time.Second * 2
	)

	paymentKey, stakeKey, err := FromCardanoPrivateKeyString(privateKey)
	if err != nil {
		return "", err
	}

	wallet := infrawallet.NewWallet(paymentKey, stakeKey)

	txProvider := infrawallet.NewTxProviderOgmios(ec.ogmiosURL)

	txHash, err := SendTx(ctx, txProvider, wallet,
		amount.Uint64(), receiverAddr, ec.config.NetworkType, data)
	if err != nil {
		return "", err
	}

	return infracommon.ExecuteWithRetry(ctx, func(ctx context.Context) (string, error) {
		contains, err := infrawallet.IsTxInUtxos(ctx, txProvider, receiverAddr, txHash)
		if err != nil {
			return "", err
		} else if !contains {
			return "", infracommon.ErrRetryTryAgain
		}

		return txHash, nil
	}, infracommon.WithRetryCount(retryCount), infracommon.WithRetryWaitTime(retryWaitTime))
}

func (ec *TestCardanoChain) GetHotWalletAddress() string {
	return ec.multisigAddr
}

func (ec *TestCardanoChain) GetAdminPrivateKey() (string, error) {
	genesisWallet, err := GetGenesisWalletFromCluster(ec.cluster.Config.TmpDir, 1)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(genesisWallet.SigningKey), nil
}
