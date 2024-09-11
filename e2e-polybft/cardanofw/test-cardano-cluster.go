package cardanofw

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/framework"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

//go:embed genesis-configuration/*
var cardanoFiles embed.FS

const hostIP = "127.0.0.1"

type TestCardanoNetworkConfig struct {
	NetworkMagic uint
	NetworkType  wallet.CardanoNetworkType
}

func (c *TestCardanoNetworkConfig) IsPrime() bool {
	if c.NetworkType == wallet.MainNetNetwork || c.NetworkType == wallet.TestNetNetwork {
		return true
	}

	return false
}

type TestCardanoClusterConfig struct {
	t *testing.T

	ID int
	TestCardanoNetworkConfig
	SecurityParam  int
	NodesCount     int
	StartNodeID    int
	Port           int
	OgmiosPort     int
	InitialSupply  *big.Int
	BlockTimeMilis int
	GenesisDir     string
	StartTimeDelay time.Duration

	WithLogs   bool
	WithStdout bool
	LogsDir    string
	TmpDir     string

	InitialFundsKeys   []string
	InitialFundsAmount uint64

	logsDirOnce sync.Once
}

func (c *TestCardanoClusterConfig) Dir(name string) string {
	return filepath.Join(c.TmpDir, name)
}

func (c *TestCardanoClusterConfig) GetStdout(name string, custom ...io.Writer) io.Writer {
	writers := []io.Writer{}

	if c.WithLogs {
		c.logsDirOnce.Do(func() {
			if err := c.initLogsDir(); err != nil {
				c.t.Fatal("GetStdout init logs dir", "err", err)
			}
		})

		f, err := os.OpenFile(filepath.Join(c.LogsDir, name+".log"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			c.t.Log("GetStdout open file error", "err", err)
		} else {
			writers = append(writers, f)

			c.t.Cleanup(func() {
				if err := f.Close(); err != nil {
					c.t.Log("GetStdout close file error", "err", err)
				}
			})
		}
	}

	if c.WithStdout {
		writers = append(writers, os.Stdout)
	}

	if len(custom) > 0 {
		writers = append(writers, custom...)
	}

	if len(writers) == 0 {
		return io.Discard
	}

	return io.MultiWriter(writers...)
}

func (c *TestCardanoClusterConfig) initLogsDir() error {
	if c.LogsDir == "" {
		logsDir := path.Join("../..", fmt.Sprintf("e2e-logs-cardano-%d", time.Now().UTC().Unix()), c.t.Name())
		if err := common.CreateDirSafe(logsDir, 0750); err != nil {
			return err
		}

		c.t.Logf("logs enabled for e2e test: %s", logsDir)
		c.LogsDir = logsDir
	}

	return nil
}

type TestCardanoCluster struct {
	Config       *TestCardanoClusterConfig
	Servers      []*TestCardanoServer
	OgmiosServer *TestOgmiosServer

	once         sync.Once
	failCh       chan struct{}
	executionErr error
}

type CardanoClusterOption func(*TestCardanoClusterConfig)

func WithNodesCount(num int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.NodesCount = num
	}
}

func WithBlockTime(blockTimeMilis int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.BlockTimeMilis = blockTimeMilis
	}
}

func WithStartTimeDelay(delay time.Duration) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.StartTimeDelay = delay
	}
}

func WithStartNodeID(startNodeID int) CardanoClusterOption { // COM: Should this be removed?
	return func(h *TestCardanoClusterConfig) {
		h.StartNodeID = startNodeID
	}
}

func WithPort(port int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.Port = port
	}
}

func WithOgmiosPort(ogmiosPort int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.OgmiosPort = ogmiosPort
	}
}

func WithLogsDir(logsDir string) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.LogsDir = logsDir
	}
}

func WithNetworkMagic(networkMagic uint) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.NetworkMagic = networkMagic
	}
}

func WithID(id int) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.ID = id
	}
}

func WithConfigGenesisDir(genesisDir string) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.GenesisDir = genesisDir
	}
}

func WithNetworkType(networkID wallet.CardanoNetworkType) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.NetworkType = networkID
	}
}

func WithInitialFunds(initialFundsKeys []string, initialFundsAmount uint64) CardanoClusterOption {
	return func(h *TestCardanoClusterConfig) {
		h.InitialFundsKeys = initialFundsKeys
		h.InitialFundsAmount = initialFundsAmount
	}
}

func NewCardanoTestCluster(t *testing.T, opts ...CardanoClusterOption) (*TestCardanoCluster, error) {
	t.Helper()

	var err error

	config := &TestCardanoClusterConfig{
		t:          t,
		WithLogs:   true,  // strings.ToLower(os.Getenv(e)) == "true"
		WithStdout: false, // strings.ToLower(os.Getenv(envStdoutEnabled)) == "true"
		TestCardanoNetworkConfig: TestCardanoNetworkConfig{
			NetworkType:  wallet.TestNetNetwork,
			NetworkMagic: 42,
		},
		SecurityParam:  10,
		NodesCount:     3,
		InitialSupply:  new(big.Int).SetUint64(11_111_111_112_000_000),
		StartTimeDelay: time.Second * 30,
		BlockTimeMilis: 2000,
		Port:           3000,
		OgmiosPort:     1337,
	}

	for _, opt := range opts {
		opt(config)
	}

	config.TmpDir, err = os.MkdirTemp("", "cardano-")
	if err != nil {
		return nil, err
	}

	cluster := &TestCardanoCluster{
		Servers: []*TestCardanoServer{},
		Config:  config,
		failCh:  make(chan struct{}),
		once:    sync.Once{},
	}

	startTime := time.Now().UTC().Add(config.StartTimeDelay)

	// init genesis
	if err := cluster.InitGenesis(startTime.Unix(), config.GenesisDir); err != nil {
		return nil, err
	}

	// copy config files
	if err := cluster.CopyConfigFilesStep1(config.GenesisDir); err != nil {
		return nil, err
	}

	// genesis create staked - babbage
	if err := cluster.GenesisCreateStaked(startTime); err != nil {
		return nil, err
	}

	// final step before starting nodes
	if err := cluster.CopyConfigFilesAndInitDirectoriesStep2(config.NetworkType); err != nil {
		return nil, err
	}

	for i := 0; i < cluster.Config.NodesCount; i++ {
		// time.Sleep(time.Second * 5)
		err = cluster.NewTestServer(t, i+1, config.Port+i)
		if err != nil {
			return nil, err
		}
	}

	return cluster, nil
}

func (c *TestCardanoCluster) NewTestServer(t *testing.T, id int, port int) error {
	t.Helper()

	srv, err := NewCardanoTestServer(t, &TestCardanoServerConfig{
		ID:   id,
		Port: port,
		//StdOut:       c.Config.GetStdout(fmt.Sprintf("node-%d", id)),
		ConfigFile:   c.Config.Dir("configuration.yaml"),
		NodeDir:      c.Config.Dir(fmt.Sprintf("node-spo%d", id)),
		NetworkMagic: c.Config.NetworkMagic,
		NetworkID:    c.Config.NetworkType,
	})
	if err != nil {
		return err
	}

	// watch the server for stop signals. It is important to fix the specific
	// 'node' reference since 'TestServer' creates a new one if restarted.
	go func(node *framework.Node) {
		<-node.Wait()

		if !node.ExitResult().Signaled {
			c.Fail(fmt.Errorf("server id = %d, port = %d has stopped unexpectedly", id, port))
		}
	}(srv.node)

	c.Servers = append(c.Servers, srv)

	return err
}

func (c *TestCardanoCluster) Fail(err error) {
	c.once.Do(func() {
		c.executionErr = err
		close(c.failCh)
	})
}

func (c *TestCardanoCluster) Stop() error {
	if c.OgmiosServer != nil && c.OgmiosServer.IsRunning() {
		if err := c.OgmiosServer.Stop(); err != nil {
			return err
		}
	}

	wg := sync.WaitGroup{}
	errs := []error(nil)

	for _, srv := range c.Servers {
		if srv.IsRunning() {
			wg.Add(1)

			go func(s *TestCardanoServer) {
				defer wg.Done()

				fmt.Printf("terminating cardano node: cluster=%d, node port=%d\n", c.Config.ID, s.Port())

				errs = append(errs, s.Stop())

				fmt.Printf("cardano node has been terminated: cluster=%d, node port=%d\n", c.Config.ID, s.Port())
			}(srv)
		}
	}

	wg.Wait()

	return errors.Join(errs...)
}

func (c *TestCardanoCluster) OgmiosURL() string {
	return fmt.Sprintf("http://localhost:%d", c.Config.OgmiosPort)
}

func (c *TestCardanoCluster) NetworkAddress() string {
	return fmt.Sprintf("localhost:%d", c.Config.Port)
}

func (c *TestCardanoCluster) NetworkConfig() TestCardanoNetworkConfig {
	return c.Config.TestCardanoNetworkConfig
}

func (c *TestCardanoCluster) Stats() ([]*wallet.QueryTipData, bool, error) {
	blocks := make([]*wallet.QueryTipData, len(c.Servers))
	ready := make([]bool, len(c.Servers))
	errors := make([]error, len(c.Servers))
	wg := sync.WaitGroup{}

	for i := range c.Servers {
		id, srv := i, c.Servers[i]
		if !srv.IsRunning() {
			ready[id] = true

			continue
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			ready[id], blocks[id], errors[id] = srv.Stat()
		}()
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			return nil, true, err
		} else if !ready[i] {
			return nil, false, nil
		}
	}

	return blocks, true, nil
}

func (c *TestCardanoCluster) WaitUntil(timeout, frequency time.Duration, handler func() (bool, error)) error {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout")
		case <-c.failCh:
			return c.executionErr
		case <-ticker.C:
		}

		finish, err := handler()
		if err != nil {
			return err
		} else if finish {
			return nil
		}
	}
}

func (c *TestCardanoCluster) WaitForReady(timeout time.Duration) error {
	return c.WaitUntil(timeout, time.Second*2, func() (bool, error) {
		_, ready, err := c.Stats()

		return ready, err
	})
}

func (c *TestCardanoCluster) WaitForBlock(
	n uint64, timeout time.Duration, frequency time.Duration,
) error {
	return c.WaitUntil(timeout, frequency, func() (bool, error) {
		tips, ready, err := c.Stats()
		if err != nil {
			return false, err
		} else if !ready {
			return false, nil
		}

		c.Config.t.Log("WaitForBlock", "tips", tips)

		for _, tip := range tips {
			if tip.Block < n {
				return false, nil
			}
		}

		return true, nil
	})
}

func (c *TestCardanoCluster) WaitForBlockWithState(
	n uint64, timeout time.Duration,
) error {
	servers := c.Servers
	blockState := make(map[uint64]map[int]string, len(c.Servers))

	return c.WaitUntil(timeout, time.Millisecond*200, func() (bool, error) {
		tips, ready, err := c.Stats()
		if err != nil {
			return false, err
		} else if !ready {
			return false, nil
		}

		for i, bn := range tips {
			serverID := servers[i].ID()
			// bn == nil -> server is stopped + dont remember smaller than n blocks
			if bn.Block < n {
				continue
			}

			if mp, exists := blockState[bn.Block]; exists {
				mp[serverID] = bn.Hash
			} else {
				blockState[bn.Block] = map[int]string{
					serverID: bn.Hash,
				}
			}
		}

		// for all running servers there must be at least one block >= n
		// that all servers have with same hash
		for _, mp := range blockState {
			if len(mp) != len(c.Servers) {
				continue
			}

			hash, ok := "", true

			for _, h := range mp {
				if hash == "" {
					hash = h
				} else if h != hash {
					ok = false

					break
				}
			}

			if ok {
				return true, nil
			}
		}

		return false, nil
	})
}

func (c *TestCardanoCluster) StartOgmios(t *testing.T, id int) error {
	t.Helper()

	srv, err := NewOgmiosTestServer(t, &TestOgmiosServerConfig{
		ID:         id,
		ConfigFile: c.Servers[0].config.ConfigFile,
		NetworkID:  c.Config.NetworkType,
		Port:       c.Config.OgmiosPort,
		SocketPath: c.Servers[0].SocketPath(),
		StdOut:     c.Config.GetStdout(fmt.Sprintf("ogmios-%d", c.Config.ID)),
	})
	if err != nil {
		return err
	}

	// watch the server for stop signals. It is important to fix the specific
	// 'node' reference since 'TestServer' creates a new one if restarted.
	go func(node *framework.Node, id int, port int) {
		<-node.Wait()

		if !node.ExitResult().Signaled {
			c.Fail(fmt.Errorf("ogmios id = %d, port = %d has stopped unexpectedly", id, port))
		}
	}(srv.node, c.Config.ID, c.Config.OgmiosPort)

	c.OgmiosServer = srv

	return err
}

func (c *TestCardanoCluster) InitGenesis(startTime int64, genesisDir string) error {
	var b bytes.Buffer

	fnContent, err := cardanoFiles.ReadFile(filepath.Join("genesis-configuration", genesisDir, "byron-genesis-spec.json"))
	if err != nil {
		return err
	}

	protParamsFile := c.Config.Dir("byron-genesis-spec.json")
	if err := os.WriteFile(protParamsFile, fnContent, 0600); err != nil {
		return err
	}

	args := []string{
		"byron", "genesis", "genesis",
		"--protocol-magic", strconv.FormatUint(uint64(c.Config.NetworkMagic), 10),
		"--start-time", strconv.FormatInt(startTime, 10),
		"--k", strconv.Itoa(c.Config.SecurityParam),
		"--n-poor-addresses", "0",
		"--n-delegate-addresses", strconv.Itoa(c.Config.NodesCount),
		"--total-balance", c.Config.InitialSupply.String(),
		"--delegate-share", "1",
		"--avvm-entry-count", "0",
		"--avvm-entry-balance", "0",
		"--protocol-parameters-file", protParamsFile,
		"--genesis-output-dir", c.Config.Dir("byron-gen-command"),
	}
	stdOut := c.Config.GetStdout("cardano-genesis", &b)

	return RunCommand(ResolveCardanoCliBinary(c.Config.NetworkType), args, stdOut)
}

func (c *TestCardanoCluster) CopyConfigFilesStep1(genesisDir string) error {
	items := [][2]string{
		{"alonzo-babbage-test-genesis.json", "genesis.alonzo.spec.json"},
		{"conway-babbage-test-genesis.json", "genesis.conway.spec.json"},
		{"configuration.yaml", "configuration.yaml"},
	}
	for _, it := range items {
		fnContent, err := cardanoFiles.ReadFile(path.Join("genesis-configuration", genesisDir, it[0]))
		if err != nil {
			return err
		}

		protParamsFile := c.Config.Dir(it[1])
		if err := os.WriteFile(protParamsFile, fnContent, 0600); err != nil {
			return err
		}
	}

	return nil
}

func (c *TestCardanoCluster) CopyConfigFilesAndInitDirectoriesStep2(networkType wallet.CardanoNetworkType) error {
	if err := common.CreateDirSafe(c.Config.Dir("genesis/byron"), 0750); err != nil {
		return err
	}

	if err := common.CreateDirSafe(c.Config.Dir("genesis/shelley"), 0750); err != nil {
		return err
	}

	err := updateJSONFile(
		c.Config.Dir("byron-gen-command/genesis.json"),
		c.Config.Dir("genesis/byron/genesis.json"),
		noChanges,
		true)
	if err != nil {
		return err
	}

	err = updateJSONFile(
		c.Config.Dir("genesis.json"),
		c.Config.Dir("genesis/shelley/genesis.json"),
		func(mp map[string]interface{}) {
			getShelleyGenesis(networkType)(mp)

			funds := getMapFromInterfaceKey(mp, "initialFunds")

			for _, addr := range c.Config.InitialFundsKeys {
				funds[addr] = c.Config.InitialFundsAmount
			}

			var prevMax uint64

			if v, exists := mp["maxLovelaceSupply"]; exists {
				if maxLovelaceSupply, ok := v.(float64); ok {
					prevMax = uint64(maxLovelaceSupply)
				} else {
					//nolint:gosec
					prevMax = uint64(v.(int)) //nolint:forcetypeassert
				}
			}

			mp["maxLovelaceSupply"] = prevMax + uint64(len(c.Config.InitialFundsKeys))*c.Config.InitialFundsAmount
		},
		true)
	if err != nil {
		return err
	}

	if err := os.Rename(
		c.Config.Dir("genesis.alonzo.json"),
		c.Config.Dir("genesis/shelley/genesis.alonzo.json"),
	); err != nil {
		return err
	}

	err = updateJSONFile(
		c.Config.Dir("genesis.conway.json"),
		c.Config.Dir("genesis/shelley/genesis.conway.json"),
		getConwayGenesis(networkType),
		true)
	if err != nil {
		return err
	}

	for i := 0; i < c.Config.NodesCount; i++ {
		nodeID := i + 1
		if err := common.CreateDirSafe(c.Config.Dir(fmt.Sprintf("node-spo%d", nodeID)), 0750); err != nil {
			return err
		}

		producers := make([]map[string]interface{}, 0, c.Config.NodesCount-1)

		for pid := 0; pid < c.Config.NodesCount; pid++ {
			if i != pid {
				producers = append(producers, map[string]interface{}{
					"addr":    hostIP,
					"valency": 1,
					"port":    c.Config.Port + pid,
				})
			}
		}

		topologyJSONContent, err := json.MarshalIndent(map[string]interface{}{
			"Producers": producers,
		}, "", "    ")
		if err != nil {
			return err
		}

		if err := os.WriteFile(
			c.Config.Dir(fmt.Sprintf("node-spo%d/topology.json", nodeID)),
			topologyJSONContent,
			0600,
		); err != nil {
			return err
		}

		// keys
		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("pools/vrf%d.skey", nodeID)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/vrf.skey", nodeID))); err != nil {
			return err
		}

		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("pools/opcert%d.cert", nodeID)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/opcert.cert", nodeID))); err != nil {
			return err
		}

		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("pools/kes%d.skey", nodeID)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/kes.skey", nodeID))); err != nil {
			return err
		}

		// byron related
		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("byron-gen-command/delegate-keys.%03d.key", i)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/byron-delegate.key", nodeID))); err != nil {
			return err
		}

		if err := os.Rename(
			c.Config.Dir(fmt.Sprintf("byron-gen-command/delegation-cert.%03d.json", i)),
			c.Config.Dir(fmt.Sprintf("node-spo%d/byron-delegation.cert", nodeID))); err != nil {
			return err
		}
	}

	return nil
}

// Because in Babbage the overlay schedule and decentralization parameter are deprecated,
// we must use the "create-staked" cli command to create SPOs in the ShelleyGenesis
func (c *TestCardanoCluster) GenesisCreateStaked(startTime time.Time) error {
	var b bytes.Buffer

	exprectedErr := fmt.Sprintf(
		"%d genesis keys, %d non-delegating UTxO keys, %d stake pools, %d delegating UTxO keys, %d delegation map entries",
		c.Config.NodesCount, c.Config.NodesCount, c.Config.NodesCount, c.Config.NodesCount, c.Config.NodesCount)
	stdOut := c.Config.GetStdout("cardano-genesis-create-staked", &b)

	args := append([]string{
		"genesis", "create-staked",
		"--genesis-dir", c.Config.Dir(""),
		"--start-time", startTime.Format("2006-01-02T15:04:05Z"),
		"--supply", "2000000000000",
		"--supply-delegated", "240000000002",
		"--gen-genesis-keys", strconv.Itoa(c.Config.NodesCount),
		"--gen-pools", strconv.Itoa(c.Config.NodesCount),
		"--gen-stake-delegs", strconv.Itoa(c.Config.NodesCount),
		"--gen-utxo-keys", strconv.Itoa(c.Config.NodesCount),
	}, GetTestNetMagicArgs(c.Config.NetworkMagic)...)

	err := RunCommand(ResolveCardanoCliBinary(c.Config.NetworkType), args, stdOut)
	if strings.Contains(err.Error(), exprectedErr) {
		return nil
	}

	return err
}

func (c *TestCardanoCluster) RunningServersCount() int {
	cnt := 0

	for _, srv := range c.Servers {
		if srv.IsRunning() {
			cnt++
		}
	}

	return cnt
}
