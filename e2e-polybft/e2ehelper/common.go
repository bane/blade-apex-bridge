package e2ehelper

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/Ethernal-Tech/cardano-infrastructure/sendtx"
)

type IApexSystem interface {
	SubmitBridgingRequest(
		t *testing.T, ctx context.Context,
		sourceChain cardanofw.ChainID, destinationChain cardanofw.ChainID,
		sender *cardanofw.TestApexUser, dfmAmount *big.Int, receivers ...*cardanofw.TestApexUser,
	) string
	SubmitBridgingRequestSkyline(
		t *testing.T, ctx context.Context,
		sourceChain cardanofw.ChainID, destinationChain cardanofw.ChainID,
		sender *cardanofw.TestApexUser, dfmAmount *big.Int, bridgingType sendtx.BridgingType,
		receivers ...*cardanofw.TestApexUser,
	) string
	WaitForGreaterAmount(
		ctx context.Context, user *cardanofw.TestApexUser, chain cardanofw.ChainID,
		expectedAmountDfm *big.Int, numRetries int, waitTime time.Duration,
	) error
	WaitForExactAmount(
		ctx context.Context, user *cardanofw.TestApexUser, chain cardanofw.ChainID,
		expectedAmountDfm *big.Int, numRetries int, waitTime time.Duration,
	) error
	SubmitTx(
		ctx context.Context, sourceChain cardanofw.ChainID, sender *cardanofw.TestApexUser,
		receiver string, dfmAmount *big.Int, data []byte,
	) (string, error)
	GetBalance(
		ctx context.Context, user *cardanofw.TestApexUser, chainID cardanofw.ChainID,
	) (*big.Int, error)
	GetValidator(t *testing.T, idx int) *cardanofw.TestApexValidator
}

func getAllDestionationChains(chains []string, chainsDst map[string][]string) (res []string) {
	mp := map[string]bool{}

	for _, srcChain := range chains {
		for _, dstChain := range chainsDst[srcChain] {
			if !mp[dstChain] {
				mp[dstChain] = true

				res = append(res, dstChain)
			}
		}
	}

	return res
}

type srcDstChainPair struct {
	srcChain string
	dstChain string
}

func getAllChainPairs(chains []string, chainsDst map[string][]string) (res []srcDstChainPair) {
	for _, srcChain := range chains {
		for _, dstChain := range chainsDst[srcChain] {
			res = append(res, srcDstChainPair{
				srcChain: srcChain,
				dstChain: dstChain,
			})
		}
	}

	return res
}
