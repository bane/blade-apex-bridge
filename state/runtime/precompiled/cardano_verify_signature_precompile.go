package precompiled

import (
	"encoding/hex"
	"errors"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/types"
	cardano_indexer "github.com/Ethernal-Tech/cardano-infrastructure/indexer"
	cardano_wallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
	"github.com/Ethernal-Tech/ethgo/abi"
)

var cardanoVerifySignaturePrecompileInputABIType = abi.MustNewType("tuple(bytes, bytes, bytes32, bool)")

// cardanoVerifySignaturePrecompile is a concrete implementation of the contract interface.
type cardanoVerifySignaturePrecompile struct {
}

// gas returns the gas required to execute the pre-compiled contract
func (c *cardanoVerifySignaturePrecompile) gas(_ []byte, config *chain.ForksInTime) uint64 {
	return 50_000
}

// Run runs the precompiled contract with the given input.
// isValidSignature(string rawTxOrMessage, string signature, string verifyingKey, bool isTx):
// Output could be an error or ABI encoded "bool" value
func (c *cardanoVerifySignaturePrecompile) run(input []byte, caller types.Address, _ runtime.Host) ([]byte, error) {
	rawData, err := abi.Decode(cardanoVerifySignaturePrecompileInputABIType, input)
	if err != nil {
		return nil, errors.Join(runtime.ErrInvalidInputData, err)
	}

	data := rawData.(map[string]interface{}) //nolint: forcetypeassert
	rawTxOrMessage := data["0"].([]byte)     //nolint: forcetypeassert
	signature := data["1"].([]byte)          //nolint: forcetypeassert
	verifyingKey := data["2"].([32]byte)     //nolint: forcetypeassert
	isTx := data["3"].(bool)                 //nolint: forcetypeassert

	// second parameter can be witness
	signatureFromWitness, _, err := cardano_wallet.TxWitnessRaw(signature).GetSignatureAndVKey()
	if err == nil {
		signature = signatureFromWitness
	}

	// if first argument is raw transaction we need to get tx hash from it
	if isTx {
		txInfo, err := cardano_indexer.ParseTxInfo(rawTxOrMessage)
		if err != nil {
			return nil, err
		}

		rawTxOrMessage, err = hex.DecodeString(txInfo.Hash)
		if err != nil {
			return nil, err
		}
	}

	err = cardano_wallet.VerifyMessage(rawTxOrMessage, verifyingKey[:], signature)
	if err != nil {
		if errors.Is(err, cardano_wallet.ErrInvalidSignature) {
			return abiBoolFalse, nil
		}

		return nil, err
	}

	return abiBoolTrue, nil
}
