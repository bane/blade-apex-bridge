package cardanofw

import (
	"fmt"
	"io"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

type TestOgmiosServerConfig struct {
	ID         int
	ConfigFile string
	NetworkID  wallet.CardanoNetworkType
	Port       int
	SocketPath string
	StdOut     io.Writer
}

type TestOgmiosServer struct {
	config *TestOgmiosServerConfig
	node   *framework.Node
}

func NewOgmiosTestServer(config *TestOgmiosServerConfig) (*TestOgmiosServer, error) {
	srv := &TestOgmiosServer{
		config: config,
	}

	return srv, srv.Start()
}

func (t *TestOgmiosServer) IsRunning() bool {
	return t.node != nil
}

func (t *TestOgmiosServer) Stop() error {
	if err := t.node.Stop(); err != nil {
		return err
	}

	t.node = nil

	return nil
}

func (t *TestOgmiosServer) Start() error {
	// Build arguments
	args := []string{
		"--port", fmt.Sprint(t.config.Port),
		"--node-socket", t.config.SocketPath,
		"--node-config", t.config.ConfigFile,
	}
	binary := ResolveOgmiosBinary(t.config.NetworkID)

	node, err := framework.NewNode(binary, args, t.config.StdOut)
	if err != nil {
		return err
	}

	t.node = node
	t.node.SetShouldForceStop(true)

	return nil
}

func (t TestOgmiosServer) SocketPath() string {
	return t.config.SocketPath
}

func (t TestOgmiosServer) Port() int {
	return t.config.Port
}

func (t TestOgmiosServer) URL() string {
	return fmt.Sprintf("localhost:%d", t.config.Port)
}
