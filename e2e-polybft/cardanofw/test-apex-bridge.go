package cardanofw

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/stretchr/testify/require"
)

const FundTokenAmount = uint64(100_000_000_000)

type ApexSystem struct {
	PrimeCluster  *TestCardanoCluster
	VectorCluster *TestCardanoCluster
	Bridge        *TestCardanoBridge
}

type CardanoChainConfig struct {
	NetworkType      wallet.CardanoNetworkType
	GenesisConfigDir string
}

func SetupAndRunApexCardanoChains(
	t *testing.T,
	ctx context.Context,
	cardanoNodesNum int,
	cardanoConfigs []CardanoChainConfig,
) []*TestCardanoCluster {
	t.Helper()

	clusterCount := len(cardanoConfigs)

	var (
		clErrors    = make([]error, clusterCount)
		clusters    = make([]*TestCardanoCluster, clusterCount)
		wg          sync.WaitGroup
		baseLogsDir = path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), t.Name())
	)

	cleanupFunc := func() {
		fmt.Printf("Cleaning up cardano chains")

		wg := sync.WaitGroup{}
		stopErrs := []error(nil)

		for i := 0; i < clusterCount; i++ {
			if clusters[i] != nil {
				wg.Add(1)

				go func(cl *TestCardanoCluster) {
					defer wg.Done()

					stopErrs = append(stopErrs, cl.Stop())
				}(clusters[i])
			}
		}

		wg.Wait()

		fmt.Printf("Done cleaning up cardano chains: %v\n", errors.Join(stopErrs...))
	}

	t.Cleanup(cleanupFunc)

	for i := 0; i < clusterCount; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			clusters[id], clErrors[id] = RunCardanoCluster(t, ctx, id, cardanoNodesNum,
				cardanoConfigs[id].NetworkType, cardanoConfigs[id].GenesisConfigDir,
				baseLogsDir)
		}(i)
	}

	wg.Wait()

	for i := 0; i < clusterCount; i++ {
		require.NoError(t, clErrors[i])
	}

	return clusters
}

func RunCardanoCluster(
	t *testing.T,
	ctx context.Context,
	id int,
	cardanoNodesNum int,
	networkType wallet.CardanoNetworkType,
	genesisConfigDir string,
	baseLogsDir string,
) (*TestCardanoCluster, error) {
	t.Helper()

	networkMagic := GetNetworkMagic(networkType)
	logsDir := fmt.Sprintf("%s/%d", baseLogsDir, id)

	if err := common.CreateDirSafe(logsDir, 0750); err != nil {
		return nil, err
	}

	cluster, err := NewCardanoTestCluster(t,
		WithID(id+1),
		WithNodesCount(cardanoNodesNum),
		WithStartTimeDelay(time.Second*5),
		WithPort(5100+id*100),
		WithOgmiosPort(1337+id),
		WithLogsDir(logsDir),
		WithNetworkMagic(networkMagic),
		WithNetworkType(networkType),
		WithConfigGenesisDir(genesisConfigDir),
	)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Waiting for sockets to be ready\n")

	if err := cluster.WaitForReady(time.Minute * 2); err != nil {
		return nil, err
	}

	if err := cluster.StartOgmios(t, id); err != nil {
		return nil, err
	}

	if err := cluster.WaitForBlockWithState(10, time.Second*120); err != nil {
		return nil, err
	}

	fmt.Printf("Cluster %d is ready\n", id)

	return cluster, nil
}

func SetupAndRunApexBridge(
	t *testing.T,
	ctx context.Context,
	dataDir string,
	bladeValidatorsNum int,
	primeCluster *TestCardanoCluster,
	vectorCluster *TestCardanoCluster,
	opts ...CardanoBridgeOption,
) *TestCardanoBridge {
	t.Helper()

	const (
		bladeEpochSize = 5
		numOfRetries   = 90
		waitTime       = time.Second * 2
	)

	cleanupDataDir := func() {
		os.RemoveAll(dataDir)
	}

	cleanupDataDir()

	cb := NewTestCardanoBridge(dataDir, bladeValidatorsNum, opts...)

	cleanupFunc := func() {
		fmt.Printf("Cleaning up apex bridge\n")

		// cleanupDataDir()
		cb.StopValidators()

		fmt.Printf("Done cleaning up apex bridge\n")
	}

	t.Cleanup(cleanupFunc)

	require.NoError(t, cb.CardanoCreateWalletsAndAddresses(primeCluster.NetworkConfig(), vectorCluster.NetworkConfig()))

	fmt.Printf("Wallets and addresses created\n")

	txProviderPrime := wallet.NewTxProviderOgmios(primeCluster.OgmiosURL())
	txProviderVector := wallet.NewTxProviderOgmios(vectorCluster.OgmiosURL())

	primeGenesisWallet, err := GetGenesisWalletFromCluster(primeCluster.Config.TmpDir, 1)
	require.NoError(t, err)

	_, err = SendTx(ctx, txProviderPrime, primeGenesisWallet, FundTokenAmount,
		cb.PrimeMultisigAddr, primeCluster.NetworkConfig(), []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigAddr, func(val uint64) bool {
		return val == FundTokenAmount
	}, numOfRetries, waitTime, IsRecoverableError)
	require.NoError(t, err)

	fmt.Printf("Prime multisig addr funded\n")

	_, err = SendTx(ctx, txProviderPrime, primeGenesisWallet, FundTokenAmount,
		cb.PrimeMultisigFeeAddr, primeCluster.NetworkConfig(), []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderPrime, cb.PrimeMultisigFeeAddr, func(val uint64) bool {
		return val == FundTokenAmount
	}, numOfRetries, waitTime, IsRecoverableError)
	require.NoError(t, err)

	fmt.Printf("Prime multisig fee addr funded\n")

	vectorGenesisWallet, err := GetGenesisWalletFromCluster(vectorCluster.Config.TmpDir, 1)
	require.NoError(t, err)

	_, err = SendTx(ctx, txProviderVector, vectorGenesisWallet, FundTokenAmount,
		cb.VectorMultisigAddr, vectorCluster.NetworkConfig(), []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigAddr, func(val uint64) bool {
		return val == FundTokenAmount
	}, numOfRetries, waitTime, IsRecoverableError)
	require.NoError(t, err)

	fmt.Printf("Vector multisig addr funded\n")

	_, err = SendTx(ctx, txProviderVector, vectorGenesisWallet, FundTokenAmount,
		cb.VectorMultisigFeeAddr, vectorCluster.NetworkConfig(), []byte{})
	require.NoError(t, err)

	err = wallet.WaitForAmount(context.Background(), txProviderVector, cb.VectorMultisigFeeAddr, func(val uint64) bool {
		return val == FundTokenAmount
	}, numOfRetries, waitTime, IsRecoverableError)
	require.NoError(t, err)

	fmt.Printf("Vector multisig fee addr funded\n")

	cb.StartValidators(t, bladeEpochSize)

	fmt.Printf("Validators started\n")

	cb.WaitForValidatorsReady(t)

	fmt.Printf("Validators ready\n")

	// need params for it to work properly
	primeTokenSupply := new(big.Int).SetUint64(FundTokenAmount)
	vectorTokenSupply := new(big.Int).SetUint64(FundTokenAmount)
	require.NoError(t, cb.RegisterChains(primeTokenSupply, vectorTokenSupply))

	fmt.Printf("Chain registered\n")

	// need params for it to work properly
	require.NoError(t, cb.GenerateConfigs(
		primeCluster,
		vectorCluster,
	))

	fmt.Printf("Configs generated\n")

	require.NoError(t, cb.StartValidatorComponents(ctx))
	fmt.Printf("Validator components started\n")

	require.NoError(t, cb.StartRelayer(ctx))
	fmt.Printf("Relayer started\n")

	return cb
}

func RunApexBridge(
	t *testing.T, ctx context.Context,
	opts ...CardanoBridgeOption,
) *ApexSystem {
	t.Helper()

	const (
		cardanoNodesNum    = 4
		bladeValidatorsNum = 4
	)

	clusters := SetupAndRunApexCardanoChains(t, ctx, cardanoNodesNum, []CardanoChainConfig{
		{NetworkType: wallet.TestNetNetwork, GenesisConfigDir: "prime"},
		{NetworkType: wallet.VectorTestNetNetwork, GenesisConfigDir: "vector"},
	})

	primeCluster := clusters[0]
	require.NotNil(t, primeCluster)

	vectorCluster := clusters[1]
	require.NotNil(t, vectorCluster)

	cb := SetupAndRunApexBridge(t,
		ctx,
		// path.Join(path.Dir(primeCluster.Config.TmpDir), "bridge"),
		"../../e2e-bridge-data-tmp-"+t.Name(),
		bladeValidatorsNum,
		primeCluster,
		vectorCluster,
		opts...,
	)

	fmt.Printf("Apex bridge setup done\n")

	return &ApexSystem{
		PrimeCluster:  primeCluster,
		VectorCluster: vectorCluster,
		Bridge:        cb,
	}
}

func (a *ApexSystem) GetPrimeGenesisWallet(t *testing.T) wallet.IWallet {
	t.Helper()

	primeGenesisWallet, err := GetGenesisWalletFromCluster(a.PrimeCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	return primeGenesisWallet
}

func (a *ApexSystem) GetVectorGenesisWallet(t *testing.T) wallet.IWallet {
	t.Helper()

	vectorGenesisWallet, err := GetGenesisWalletFromCluster(a.VectorCluster.Config.TmpDir, 2)
	require.NoError(t, err)

	return vectorGenesisWallet
}

func (a *ApexSystem) GetPrimeTxProvider() wallet.ITxProvider {
	return wallet.NewTxProviderOgmios(a.PrimeCluster.OgmiosURL())
}

func (a *ApexSystem) GetVectorTxProvider() wallet.ITxProvider {
	return wallet.NewTxProviderOgmios(a.VectorCluster.OgmiosURL())
}

func (a *ApexSystem) CreateAndFundUser(t *testing.T, ctx context.Context, sendAmount uint64,
	primeNetworkConfig TestCardanoNetworkConfig, vectorNetworkConfig TestCardanoNetworkConfig,
) *TestApexUser {
	t.Helper()

	user := NewTestApexUser(t, primeNetworkConfig.NetworkType, vectorNetworkConfig.NetworkType)

	txProviderPrime := a.GetPrimeTxProvider()
	txProviderVector := a.GetVectorTxProvider()

	// Fund prime address
	primeGenesisWallet := a.GetPrimeGenesisWallet(t)

	user.SendToUser(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, primeNetworkConfig)

	fmt.Printf("Prime user address funded\n")

	// Fund vector address
	vectorGenesisWallet := a.GetVectorGenesisWallet(t)

	user.SendToUser(t, ctx, txProviderVector, vectorGenesisWallet, sendAmount, vectorNetworkConfig)

	fmt.Printf("Vector user address funded\n")

	return user
}

func (a *ApexSystem) CreateAndFundExistingUser(
	t *testing.T, ctx context.Context, primePrivateKey, vectorPrivateKey string, sendAmount uint64,
	primeNetworkConfig TestCardanoNetworkConfig, vectorNetworkConfig TestCardanoNetworkConfig,
) *TestApexUser {
	t.Helper()

	user := NewTestApexUserWithExistingWallets(t, primePrivateKey, vectorPrivateKey,
		primeNetworkConfig.NetworkType, vectorNetworkConfig.NetworkType)

	txProviderPrime := a.GetPrimeTxProvider()
	txProviderVector := a.GetVectorTxProvider()

	// Fund prime address
	primeGenesisWallet := a.GetPrimeGenesisWallet(t)

	user.SendToUser(t, ctx, txProviderPrime, primeGenesisWallet, sendAmount, primeNetworkConfig)

	fmt.Printf("Prime user address funded\n")

	// Fund vector address
	vectorGenesisWallet := a.GetVectorGenesisWallet(t)

	user.SendToUser(t, ctx, txProviderVector, vectorGenesisWallet, sendAmount, vectorNetworkConfig)

	fmt.Printf("Vector user address funded\n")

	return user
}
