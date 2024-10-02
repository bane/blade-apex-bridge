package cardanofw

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

type TestCardanoServerConfig struct {
	ID           int
	NodeDir      string
	ConfigFile   string
	Port         int
	SocketPath   string
	NetworkMagic uint
	NetworkID    cardanowallet.CardanoNetworkType
	StdOut       io.Writer
}

type TestCardanoServer struct {
	config     *TestCardanoServerConfig
	txProvider cardanowallet.ITxProvider
	node       *framework.Node
}

func NewCardanoTestServer(config *TestCardanoServerConfig) (*TestCardanoServer, error) {
	srv := &TestCardanoServer{
		config: config,
	}

	return srv, srv.Start()
}

func (t *TestCardanoServer) IsRunning() bool {
	return t.node != nil
}

func (t *TestCardanoServer) Stop() error {
	if err := t.node.Stop(); err != nil {
		return err
	}

	t.txProvider.Dispose()

	t.node = nil
	t.txProvider = nil

	return nil
}

func (t *TestCardanoServer) Start() error {
	// Build arguments
	args := []string{
		"run",
		"--config", t.config.ConfigFile,
		"--topology", fmt.Sprintf("%s/topology.json", t.config.NodeDir),
		"--database-path", fmt.Sprintf("%s/db", t.config.NodeDir),
		"--socket-path", t.SocketPath(),
		"--shelley-kes-key", fmt.Sprintf("%s/kes.skey", t.config.NodeDir),
		"--shelley-vrf-key", fmt.Sprintf("%s/vrf.skey", t.config.NodeDir),
		"--byron-delegation-certificate", fmt.Sprintf("%s/byron-delegation.cert", t.config.NodeDir),
		"--byron-signing-key", fmt.Sprintf("%s/byron-delegate.key", t.config.NodeDir),
		"--shelley-operational-certificate", fmt.Sprintf("%s/opcert.cert", t.config.NodeDir),
		"--port", strconv.Itoa(t.config.Port),
	}
	binary := ResolveCardanoNodeBinary(t.config.NetworkID)

	node, err := framework.NewNode(binary, args, t.config.StdOut)
	if err != nil {
		return err
	}

	t.node = node
	t.node.SetShouldForceStop(true)

	return nil
}

func (t *TestCardanoServer) Stat() (bool, *cardanowallet.QueryTipData, error) {
	txProvider, err := t.getTxProvider()
	if err != nil {
		return false, nil, err
	}

	queryTipData, err := txProvider.GetTip(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), "Network.Socket.connect") &&
			strings.Contains(err.Error(), "does not exist") {
			return false, nil, nil
		}
	}

	return true, &queryTipData, err
}

func (t TestCardanoServer) ID() int {
	return t.config.ID
}

func (t TestCardanoServer) SocketPath() string {
	// socketPath handle for windows \\.\pipe\
	return fmt.Sprintf("%s/node.socket", t.config.NodeDir)
}

func (t TestCardanoServer) Port() int {
	return t.config.Port
}

func (c *TestCardanoServer) NetworkAddress() string {
	return fmt.Sprintf("localhost:%d", c.config.Port)
}

func (t *TestCardanoServer) getTxProvider() (cardanowallet.ITxProvider, error) {
	if t.txProvider == nil {
		txProvider, err := cardanowallet.NewTxProviderCli(
			t.config.NetworkMagic, t.SocketPath(), ResolveCardanoCliBinary(t.config.NetworkID))
		if err != nil {
			return nil, fmt.Errorf("failed to create tx provider: %w", err)
		}

		t.txProvider = txProvider
	}

	return t.txProvider, nil
}
