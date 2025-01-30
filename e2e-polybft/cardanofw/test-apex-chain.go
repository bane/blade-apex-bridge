package cardanofw

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/Ethernal-Tech/cardano-infrastructure/sendtx"
)

type ITestApexChain interface {
	RunChain(t *testing.T) error
	Stop() error
	CreateWallets(validator *TestApexValidator) error
	CreateAddresses(bladeAdmin *crypto.ECDSAKey, bridgeURL string) error
	FundWallets(ctx context.Context) error
	RegisterChain(validator *TestApexValidator, system string) error
	InitContracts(bridgeAdmin *crypto.ECDSAKey, bridgeURL string) error
	GetGenerateConfigsParams(indx int) []string
	PopulateApexSystem(apexSystem *ApexSystem)
	UpdateTxSendChainConfiguration(configs map[string]sendtx.ChainConfig)
	ChainID() string
	GetAddressBalance(ctx context.Context, addr string) (*big.Int, error)
	BridgingRequest(
		ctx context.Context,
		destChainID ChainID,
		privateKey string,
		receivers map[string]*big.Int,
		feeAmount *big.Int,
		bridgingTypes ...sendtx.BridgingType,
	) (string, error)
	SendTx(
		ctx context.Context, privateKey string, receiver string, amount *big.Int, data []byte,
	) (string, error)
	GetHotWalletAddress() string
	GetAdminPrivateKey() (string, error)
	CreateMetadata(
		senderAddr string,
		dstChainID string,
		receivers []sendtx.BridgingTxReceiver,
		bridgingFee uint64,
		exchangeRate sendtx.ExchangeRate,
	) ([]byte, error)

	SetNativeTokenName(string)
}

type TestApexChainDummy struct {
	configParams []string
}

func NewTestApexChainDummy(configParams []string) *TestApexChainDummy {
	return &TestApexChainDummy{
		configParams: configParams,
	}
}

func (t *TestApexChainDummy) BridgingRequest(
	ctx context.Context,
	destChainID string,
	privateKey string,
	receivers map[string]*big.Int,
	feeAmount *big.Int,
	bridgingTypes ...sendtx.BridgingType,
) (string, error) {
	return "", nil
}

func (t *TestApexChainDummy) ChainID() string {
	return ""
}

func (t *TestApexChainDummy) CreateAddresses(bladeAdmin *crypto.ECDSAKey, bridgeURL string) error {
	return nil
}

func (t *TestApexChainDummy) CreateWallets(validator *TestApexValidator) error {
	return nil
}

func (t *TestApexChainDummy) FundWallets(ctx context.Context) error {
	return nil
}

func (t *TestApexChainDummy) GetAddressBalance(ctx context.Context, addr string) (*big.Int, error) {
	return nil, nil
}

func (t *TestApexChainDummy) GetGenerateConfigsParams(indx int) []string {
	return t.configParams
}

func (t *TestApexChainDummy) InitContracts(bridgeAdmin *crypto.ECDSAKey, bridgeURL string) error {
	return nil
}

func (t *TestApexChainDummy) PopulateApexSystem(apexSystem *ApexSystem) {
}

func (t *TestApexChainDummy) UpdateTxSendChainConfiguration(_ map[string]sendtx.ChainConfig) {
}

func (t *TestApexChainDummy) RegisterChain(validator *TestApexValidator, system string) error {
	return nil
}

func (*TestApexChainDummy) RunChain(t *testing.T) error {
	t.Helper()

	return nil
}

func (t *TestApexChainDummy) SendTx(
	ctx context.Context, privateKey string, receiver string, amount *big.Int, data []byte,
) (string, error) {
	return "", nil
}

func (t *TestApexChainDummy) Stop() error {
	return nil
}

func (t *TestApexChainDummy) GetHotWalletAddress() string {
	return ""
}

func (t *TestApexChainDummy) GetAdminPrivateKey() (string, error) {
	return "", nil
}

func (t *TestApexChainDummy) CreateMetadata(
	senderAddr string,
	dstChainID string,
	receivers []sendtx.BridgingTxReceiver,
	bridgingFee uint64,
	exchangeRate sendtx.ExchangeRate,
) ([]byte, error) {
	return nil, nil
}

func (t *TestApexChainDummy) SetNativeTokenName(string) {

}

var _ ITestApexChain = (*TestApexChainDummy)(nil)
