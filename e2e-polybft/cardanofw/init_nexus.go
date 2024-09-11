package cardanofw

import (
	"bytes"
	"fmt"
	"math/big"
	"reflect"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/mitchellh/mapstructure"
	"github.com/umbracle/ethgo/abi"
)

var (
	// Proxy contracts
	ERC1967Proxy *contracts.Artifact

	// Nexus smart contracts
	NativeTokenPredicate *contracts.Artifact
	Gateway              *contracts.Artifact
	NativeTokenWallet    *contracts.Artifact
	Validators           *contracts.Artifact
)

func InitNexusContracts() (err error) {
	ERC1967Proxy, err = contracts.DecodeArtifact([]byte(contractsapi.ERC1967ProxyArtifact))
	if err != nil {
		return
	}

	NativeTokenPredicate, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_NativeTokenPredicateArtifact))
	if err != nil {
		return
	}

	Gateway, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_GatewayArtifact))
	if err != nil {
		return
	}

	NativeTokenWallet, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_NativeTokenWalletArtifact))
	if err != nil {
		return
	}

	Validators, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_ValidatorsArtifact))
	if err != nil {
		return
	}

	return
}

type GatewaySetDependenciesFn struct {
	NativeTokenPredicate_ types.Address `abi:"_nativeTokenPredicate"`
	Validators_           types.Address `abi:"_validators"`
}

func (g *GatewaySetDependenciesFn) Sig() []byte {
	return Gateway.Abi.Methods["setDependencies"].ID()
}

func (g *GatewaySetDependenciesFn) EncodeAbi() ([]byte, error) {
	return Gateway.Abi.Methods["setDependencies"].Encode(g)
}

func (g *GatewaySetDependenciesFn) DecodeAbi(buf []byte) error {
	return decodeMethod(Gateway.Abi.Methods["setDependencies"], buf, g)
}

type NativeTokenPredicateSetDependenciesFn struct {
	Gateway_           types.Address `abi:"_gateway"`
	NativeTokenWallet_ types.Address `abi:"_nativeTokenWallet"`
}

func (g *NativeTokenPredicateSetDependenciesFn) Sig() []byte {
	return NativeTokenPredicate.Abi.Methods["setDependencies"].ID()
}

func (g *NativeTokenPredicateSetDependenciesFn) EncodeAbi() ([]byte, error) {
	return NativeTokenPredicate.Abi.Methods["setDependencies"].Encode(g)
}

func (g *NativeTokenPredicateSetDependenciesFn) DecodeAbi(buf []byte) error {
	return decodeMethod(NativeTokenPredicate.Abi.Methods["setDependencies"], buf, g)
}

type NativeTokenWalletSetDependenciesFn struct {
	Predicate_ types.Address `abi:"_predicate"`
	Supply_    *big.Int      `abi:"_tokenSupply"`
}

func (g *NativeTokenWalletSetDependenciesFn) Sig() []byte {
	return NativeTokenWallet.Abi.Methods["setDependencies"].ID()
}

func (g *NativeTokenWalletSetDependenciesFn) EncodeAbi() ([]byte, error) {
	return NativeTokenWallet.Abi.Methods["setDependencies"].Encode(g)
}

func (g *NativeTokenWalletSetDependenciesFn) DecodeAbi(buf []byte) error {
	return decodeMethod(NativeTokenWallet.Abi.Methods["setDependencies"], buf, g)
}

type ValidatorChainData struct {
	Key_ [4]*big.Int `abi:"key"`
}

type ValidatorsSetValidatorsChainDataFn struct {
	ChainData_ []*ValidatorChainData `abi:"_validatorsChainData"`
}

func (g *ValidatorsSetValidatorsChainDataFn) Sig() []byte {
	return Validators.Abi.Methods["setValidatorsChainData"].ID()
}

func (g *ValidatorsSetValidatorsChainDataFn) EncodeAbi() ([]byte, error) {
	return Validators.Abi.Methods["setValidatorsChainData"].Encode(g)
}

func (g *ValidatorsSetValidatorsChainDataFn) DecodeAbi(buf []byte) error {
	return decodeMethod(Validators.Abi.Methods["setValidatorsChainData"], buf, g)
}

const abiMethodIDLength = 4

func decodeMethod(method *abi.Method, input []byte, out interface{}) error {
	if len(input) < abiMethodIDLength {
		return fmt.Errorf("invalid method data, len = %d", len(input))
	}

	sig := method.ID()
	if !bytes.HasPrefix(input, sig) {
		return fmt.Errorf("prefix is not correct")
	}

	val, err := abi.Decode(method.Inputs, input[abiMethodIDLength:])
	if err != nil {
		return err
	}

	return decodeImpl(val, out)
}

func decodeImpl(input interface{}, out interface{}) error {
	metadata := &mapstructure.Metadata{}
	dc := &mapstructure.DecoderConfig{
		Result:     out,
		TagName:    "abi",
		Metadata:   metadata,
		DecodeHook: customHook,
	}

	ms, err := mapstructure.NewDecoder(dc)
	if err != nil {
		return err
	}

	if err = ms.Decode(input); err != nil {
		return err
	}

	if len(metadata.Unused) != 0 {
		return fmt.Errorf("some keys not used: %v", metadata.Unused)
	}

	return nil
}

var bigTyp = reflect.TypeOf(new(big.Int))

func customHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f == bigTyp && t.Kind() == reflect.Uint64 {
		// convert big.Int to uint64 (if possible)
		b, ok := data.(*big.Int)
		if !ok {
			return nil, fmt.Errorf("data not a big.Int")
		}

		if !b.IsUint64() {
			return nil, fmt.Errorf("cannot format big.Int to uint64")
		}

		return b.Uint64(), nil
	}

	return data, nil
}
