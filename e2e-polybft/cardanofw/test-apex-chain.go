package cardanofw

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/crypto"
)

type ITestApexChain interface {
	RunChain(t *testing.T) error
	Stop() error
	CreateWallets(validator *TestApexValidator) error
	CreateAddresses(bladeAdmin *crypto.ECDSAKey, bridgeURL string) error
	FundWallets(ctx context.Context) error
	RegisterChain(validator *TestApexValidator) error
	InitContracts(bridgeAdmin *crypto.ECDSAKey, bridgeURL string) error
	GetGenerateConfigsParams(indx int) []string
	PopulateApexSystem(apexSystem *ApexSystem)
	ChainID() string
	GetAddressBalance(ctx context.Context, addr string) (*big.Int, error)
	BridgingRequest(
		ctx context.Context, destChainID ChainID, privateKey string, receivers map[string]*big.Int,
	) (string, error)
	SendTx(
		ctx context.Context, privateKey string, receiver string, amount *big.Int, data []byte,
	) (string, error)
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
	ctx context.Context, destChainID string, privateKey string, receivers map[string]*big.Int,
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

func (t *TestApexChainDummy) RegisterChain(validator *TestApexValidator) error {
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

var _ ITestApexChain = (*TestApexChainDummy)(nil)
