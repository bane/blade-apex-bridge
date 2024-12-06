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

func SetupAndRunApexBridge(
	t *testing.T,
	ctx context.Context,
	opts ...ApexSystemOptions,
) *ApexSystem {
	t.Helper()

	bridgeDataDir := filepath.Join("..", "..", "e2e-bridge-data-tmp-"+t.Name())

	os.RemoveAll(bridgeDataDir)

	apexSystem, err := NewApexSystem(bridgeDataDir, opts...)
	require.NoError(t, err)

	fmt.Printf("Starting chains...\n")

	// stop all chains and the bridge
	t.Cleanup(func() {
		assert.NoError(t, apexSystem.StopAll())
	})

	require.NoError(t, apexSystem.StartChains(t))

	fmt.Printf("Chains have been started. Starting bridge chain...\n")

	apexSystem.StartBridgeChain(t)

	fmt.Printf("Bridge chain has been started. Validators are ready\n")

	require.NoError(t, apexSystem.CreateWallets())

	fmt.Printf("Wallets have been created.\n")

	require.NoError(t, apexSystem.RegisterChains())

	fmt.Printf("Chains have been registered\n")

	require.NoError(t, apexSystem.CreateAddresses())

	fmt.Printf("Multisig addresses have been created\n")

	require.NoError(t, apexSystem.InitContracts(ctx))

	fmt.Printf("Contracts have been set up\n")

	require.NoError(t, apexSystem.FundWallets(ctx))

	fmt.Printf("Wallets have been funded\n")

	require.NoError(t, apexSystem.GenerateConfigs())

	fmt.Printf("Configs have been generated\n")

	require.NoError(t, apexSystem.StartValidatorComponents(ctx))

	fmt.Printf("Validator components started\n")

	require.NoError(t, apexSystem.StartRelayer(ctx))

	fmt.Printf("Relayer started. Apex bridge setup done\n")

	return apexSystem
}
