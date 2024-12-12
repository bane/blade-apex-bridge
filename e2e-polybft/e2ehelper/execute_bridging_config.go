package e2ehelper

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/stretchr/testify/require"
)

type RestartValidatorsConfig struct {
	WaitTime   time.Duration
	StartIndxs []int
	StopIndxs  []int
}

type SendTxStrategyFn func(
	t *testing.T, ctx context.Context, apex IApexSystem, chains []srcDstChainPair,
	senders, receivers []*cardanofw.TestApexUser, sendAmountDfm *big.Int, txCountPerSender int)

type RestartValidatorStrategyFn func(
	t *testing.T, ctx context.Context, apex IApexSystem, configs []RestartValidatorsConfig)

type executeBridgingConfig struct {
	waitForUnexpectedBridges bool
	restartValidatorsConfigs []RestartValidatorsConfig
	sendTxStrategy           SendTxStrategyFn
	restartValidatorStrategy RestartValidatorStrategyFn
}

func newExecuteBridgingConfig(opts ...ExecuteBridgingOption) *executeBridgingConfig {
	config := &executeBridgingConfig{
		sendTxStrategy:           defaultSendTxStrategy,
		restartValidatorStrategy: defaultRestartValidatorStrategy,
	}

	for _, x := range opts {
		x(config)
	}

	return config
}

type ExecuteBridgingOption func(config *executeBridgingConfig)

func WithWaitForUnexpectedBridges(waitForUnexpectedBridges bool) ExecuteBridgingOption {
	return func(config *executeBridgingConfig) {
		config.waitForUnexpectedBridges = waitForUnexpectedBridges
	}
}

func WithRestartValidatorsConfig(restartValidatorsConfigs []RestartValidatorsConfig) ExecuteBridgingOption {
	return func(config *executeBridgingConfig) {
		config.restartValidatorsConfigs = restartValidatorsConfigs
	}
}

func WithSendTxStrategy(strategy SendTxStrategyFn) ExecuteBridgingOption {
	return func(config *executeBridgingConfig) {
		config.sendTxStrategy = strategy
	}
}

var (
	defaultSendTxStrategy SendTxStrategyFn = func(
		t *testing.T, ctx context.Context, apex IApexSystem, chains []srcDstChainPair,
		senders, receivers []*cardanofw.TestApexUser, sendAmountDfm *big.Int, txCountPerSender int) {
		t.Helper()

		var wg sync.WaitGroup

		for i, sender := range senders {
			for _, chainPair := range chains {
				wg.Add(1)

				go func(idx int, senderUser *cardanofw.TestApexUser, chainPair srcDstChainPair) {
					defer wg.Done()

					for j := 0; j < txCountPerSender; j++ {
						txHash := apex.SubmitBridgingRequest(
							t, ctx, chainPair.srcChain, chainPair.dstChain, senderUser, sendAmountDfm, receivers...)

						fmt.Printf("Sender: %d. run: %d. %s->%s tx sent: %s\n",
							idx+1, j+1, chainPair.srcChain, chainPair.dstChain, txHash)
					}
				}(i, sender, chainPair)
			}
		}

		wg.Wait()
	}
	defaultRestartValidatorStrategy RestartValidatorStrategyFn = func(
		t *testing.T, ctx context.Context, apex IApexSystem, configs []RestartValidatorsConfig) {
		t.Helper()

		for _, cfg := range configs {
			go func() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(cfg.WaitTime):
					for _, idx := range cfg.StopIndxs {
						require.NoError(t, apex.GetValidator(t, idx).Stop())
					}

					for _, idx := range cfg.StartIndxs {
						require.NoError(t, apex.GetValidator(t, idx).Start(ctx, false))
					}
				}
			}()
		}
	}
)
