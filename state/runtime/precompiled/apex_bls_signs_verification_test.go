package precompiled

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/bitmap"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo/abi"
)

func Test_apexBLSSignatureVerification(t *testing.T) {
	const (
		validatorsCount = 8
	)

	encodeSingle := func(hash []byte, signature []byte, publicKey [4]*big.Int) []byte {
		encoded, err := abi.Encode([]interface{}{hash, signature, publicKey},
			apexBLSInputDataSingleABIType)
		require.NoError(t, err)

		return append([]byte{apexBLSSingleTypeByte}, encoded...)
	}

	encodeMulti := func(hash []byte, signature []byte, publicKeys [][4]*big.Int, bmp bitmap.Bitmap) []byte {
		encoded, err := abi.Encode([]interface{}{hash, signature, publicKeys, bmp},
			apexBLSInputDataMultiABIType)
		require.NoError(t, err)

		return append([]byte{apexBLSMultiTypeByte}, encoded...)
	}

	aggregateSignatures := func(
		allSignatures []*bls.Signature, indexes ...int,
	) ([]byte, bitmap.Bitmap) {
		bmp := bitmap.Bitmap{}
		signatures := make(bls.Signatures, len(indexes))

		for i, indx := range indexes {
			signatures[i] = allSignatures[indx]
			bmp.Set(uint64(indx))
		}

		signature, err := signatures.Aggregate().Marshal()
		require.NoError(t, err)

		return signature, bmp
	}

	domain := crypto.Keccak256([]byte("sevap is in the house!"))
	message := crypto.Keccak256([]byte("test message to sign"))
	b := &apexBLSSignatureVerification{domain: domain}

	validators, err := bls.CreateRandomBlsKeys(validatorsCount)
	require.NoError(t, err)

	pubKeys := make([][4]*big.Int, len(validators))
	signatures := make([]*bls.Signature, len(validators))

	for i, validator := range validators {
		signatures[i], err = validator.Sign(message, domain)
		require.NoError(t, err)

		pubKeys[i] = validator.PublicKey().ToBigInt()
	}

	t.Run("correct single", func(t *testing.T) {
		for i := range validators {
			signature, err := signatures[i].Marshal()
			require.NoError(t, err)

			out, err := b.run(encodeSingle(message, signature, pubKeys[i]), types.ZeroAddress, nil)
			require.NoError(t, err)
			require.Equal(t, abiBoolTrue, out)
		}
	})

	t.Run("wrong single", func(t *testing.T) {
		for i := range validators {
			signature, err := signatures[i].Marshal()
			require.NoError(t, err)

			j := (i + 1) % len(validators)

			out, err := b.run(encodeSingle(message, signature, pubKeys[j]), types.ZeroAddress, nil)
			require.NoError(t, err)
			require.Equal(t, abiBoolFalse, out)
		}
	})

	t.Run("correct multi 1", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 0, 5, 4, 3, 6, 2)

		out, err := b.run(encodeMulti(message, signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.NoError(t, err)
		require.Equal(t, abiBoolTrue, out)
	})

	t.Run("correct multi 2", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 4, 5, 1, 7, 0, 2, 6)

		out, err := b.run(encodeMulti(message, signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.NoError(t, err)
		require.Equal(t, abiBoolTrue, out)
	})

	t.Run("correct multi 3", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 0, 1, 2, 3, 4, 7, 5, 6)

		out, err := b.run(encodeMulti(message, signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.NoError(t, err)
		require.Equal(t, abiBoolTrue, out)
	})

	t.Run("correct multi 4", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 7, 6, 5, 1, 3, 2)

		out, err := b.run(encodeMulti(message, signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.NoError(t, err)
		require.Equal(t, abiBoolTrue, out)
	})

	t.Run("wrong multi 1", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 7, 6, 5, 1, 3, 2)

		out, err := b.run(encodeMulti(append([]byte{1}, message...), signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.NoError(t, err)
		require.Equal(t, abiBoolTrue, out)
	})

	t.Run("wrong multi 2", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 7, 6, 5, 1, 3, 0)

		bmp.Set(uint64(2))

		out, err := b.run(encodeMulti(message, signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.NoError(t, err)
		require.Equal(t, abiBoolFalse, out)
	})

	t.Run("multi quorum not reached", func(t *testing.T) {
		signature, bmp := aggregateSignatures(signatures, 7, 6, 5, 1, 3)

		_, err := b.run(encodeMulti(message, signature, pubKeys, bmp), types.ZeroAddress, nil)
		require.ErrorIs(t, err, errApexBLSSignatureVerificationQuorumNotReached)
	})
}
