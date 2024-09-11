package precompiled

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo/abi"
)

const (
	apexBLSSingleTypeByte = 0
	apexBLSMultiTypeByte  = 1
)

var (
	errApexBLSSignatureVerificationInvalidInput     = errors.New("invalid input")
	errApexBLSSignatureVerificationQuorumNotReached = errors.New("quorum not reached")
	// apexBLSInputDataMultiABIType is the ABI signature of the precompiled contract input data
	// (hash, signature, blsPublicKeys, bitmap)
	apexBLSInputDataMultiABIType = abi.MustNewType("tuple(bytes32, bytes, uint256[4][], uint256)")
	// (hash, signature, blsPublicKey)
	apexBLSInputDataSingleABIType = abi.MustNewType("tuple(bytes32, bytes, uint256[4])")
)

// apexBLSSignatureVerification verifies the given aggregated signatures using the default BLS utils functions.
// apexBLSSignatureVerification returns ABI encoded boolean value depends on validness of the given signatures.
type apexBLSSignatureVerification struct {
	domain []byte
}

// gas returns the gas required to execute the pre-compiled contract
func (c *apexBLSSignatureVerification) gas(input []byte, _ *chain.ForksInTime) uint64 {
	return 50000
}

// Run runs the precompiled contract with the given input.
// Input must be ABI encoded:
// - if first byte is 0 then other bytes are decoded as tuple(bytes32, bytes, uint256[4])
// - otherwise other input bytes are decoded as tuple(bytes32, bytes, uint256[4][], bytes)
// Output could be an error or ABI encoded "bool" value
func (c *apexBLSSignatureVerification) run(input []byte, caller types.Address, host runtime.Host) ([]byte, error) {
	if len(input) == 0 {
		return nil, errApexBLSSignatureVerificationInvalidInput
	}

	isSingle := input[0] == apexBLSSingleTypeByte

	inputType := apexBLSInputDataMultiABIType
	if isSingle {
		inputType = apexBLSInputDataSingleABIType
	}

	rawData, err := abi.Decode(inputType, input[1:])
	if err != nil {
		return nil, fmt.Errorf("%w: single = %v - %w",
			errApexBLSSignatureVerificationInvalidInput, isSingle, err)
	}

	var (
		data                 = rawData.(map[string]interface{})   //nolint:forcetypeassert
		msg                  = data["0"].([types.HashLength]byte) //nolint:forcetypeassert
		signatureBytes       = data["1"].([]byte)                 //nolint:forcetypeassert
		publicKeysSerialized [][4]*big.Int
	)

	if isSingle {
		publicKey := data["2"].([4]*big.Int) //nolint:forcetypeassert
		publicKeysSerialized = [][4]*big.Int{publicKey}
	} else {
		allPublicKeys := data["2"].([][4]*big.Int)         //nolint:forcetypeassert
		bmp := bitmap.Bitmap(data["3"].(*big.Int).Bytes()) //nolint:forcetypeassert

		for i, x := range allPublicKeys {
			//nolint:gosec
			if bmp.IsSet(uint64(i)) {
				publicKeysSerialized = append(publicKeysSerialized, x)
			}
		}

		quorumCnt := (len(allPublicKeys)*2)/3 + 1
		// ensure that the number of serialized public keys meets the required quorum count
		if len(publicKeysSerialized) < quorumCnt {
			return nil, errApexBLSSignatureVerificationQuorumNotReached
		}
	}

	signature, err := bls.UnmarshalSignature(signatureBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: signature - %w", errApexBLSSignatureVerificationInvalidInput, err)
	}

	blsPubKeys := make([]*bls.PublicKey, len(publicKeysSerialized))

	for i, pk := range publicKeysSerialized {
		blsPubKey, err := bls.UnmarshalPublicKeyFromBigInt(pk)
		if err != nil {
			return nil, fmt.Errorf("%w: public key - %w", errApexBLSSignatureVerificationInvalidInput, err)
		}

		blsPubKeys[i] = blsPubKey
	}

	if signature.VerifyAggregated(blsPubKeys, msg[:], c.domain) {
		return abiBoolTrue, nil
	}

	return abiBoolFalse, nil
}
