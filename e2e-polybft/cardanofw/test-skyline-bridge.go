package cardanofw

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func SetupAndRunSkylineBridge(
	t *testing.T,
	ctx context.Context,
	opts ...ApexSystemOptions,
) *ApexSystem {
	t.Helper()

	bridgeDataDir := filepath.Join("..", "..", "e2e-bridge-data-tmp-"+t.Name())

	os.RemoveAll(bridgeDataDir)

	skylineSystem, err := NewSkylineSystem(bridgeDataDir, opts...)
	require.NoError(t, err)

	fmt.Printf("Starting chains...\n")

	// stop all chains and the bridge
	t.Cleanup(func() {
		assert.NoError(t, skylineSystem.StopAll())
	})

	require.NoError(t, skylineSystem.StartChains(t))

	fmt.Printf("Chains have been started. Starting bridge chain...\n")

	skylineSystem.StartBridgeChain(t)

	fmt.Printf("Bridge chain has been started. Validators are ready\n")

	require.NoError(t, skylineSystem.CreateWallets())

	fmt.Printf("Wallets have been created.\n")

	require.NoError(t, skylineSystem.RegisterChains("skyline"))

	fmt.Printf("Chains have been registered\n")

	require.NoError(t, skylineSystem.CreateAddresses())

	fmt.Printf("Multisig addresses have been created\n")

	require.NoError(t, skylineSystem.InitContracts(ctx))
	require.NoError(t, skylineSystem.FinishConfiguringSkyline())

	fmt.Printf("Contracts have been set up\n")

	require.NoError(t, skylineSystem.FundWallets(ctx))

	fmt.Printf("Wallets have been funded\n") // <-ok

	require.NoError(t, skylineSystem.GenerateSkylineConfigs())
	require.NoError(t, skylineSystem.FinishConfiguringSkyline())

	fmt.Printf("Configs have been generated\n")

	require.NoError(t, skylineSystem.StartValidatorComponents(ctx))

	fmt.Printf("Validator components started\n")

	require.NoError(t, skylineSystem.StartRelayer(ctx))

	fmt.Printf("Relayer started. Apex bridge setup done\n")

	return skylineSystem
}
