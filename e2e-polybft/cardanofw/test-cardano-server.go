package cardanofw

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

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
	t *testing.T

	config     *TestCardanoServerConfig
	txProvider cardanowallet.ITxProvider
	node       *framework.Node
}

func NewCardanoTestServer(t *testing.T, config *TestCardanoServerConfig) (*TestCardanoServer, error) {
	t.Helper()

	srv := &TestCardanoServer{
		t:      t,
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

	t.node = nil

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

func (t TestCardanoServer) Stat() (bool, *cardanowallet.QueryTipData, error) {
	queryTipData, err := t.getTxProvider().GetTip(context.Background())
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

func (t *TestCardanoServer) getTxProvider() cardanowallet.ITxProvider {
	if t.txProvider == nil {
		txProvider, err := cardanowallet.NewTxProviderCli(
			t.config.NetworkMagic, t.SocketPath(), ResolveCardanoCliBinary(t.config.NetworkID))
		if err != nil {
			t.t.Fatalf("failed to create tx provider: %s", err.Error())
		}

		t.txProvider = txProvider
	}

	return t.txProvider
}
