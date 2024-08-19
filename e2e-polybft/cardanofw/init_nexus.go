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
	ERC20TokenPredicate *contracts.Artifact
	Gateway             *contracts.Artifact
	NativeERC20Mintable *contracts.Artifact
	Validators          *contracts.Artifact
)

func InitNexusContracts() (
	err error) {
	ERC1967Proxy, err = contracts.DecodeArtifact([]byte(contractsapi.ERC1967ProxyArtifact))
	if err != nil {
		return
	}

	ERC20TokenPredicate, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_ERC20TokenPredicateArtifact))
	if err != nil {
		return
	}

	Gateway, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_GatewayArtifact))
	if err != nil {
		return
	}

	NativeERC20Mintable, err = contracts.DecodeArtifact([]byte(contractsapi.Nexus_NativeERC20MintableArtifact))
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
	Erc20_      types.Address `abi:"_eRC20TokenPredicate"`
	Validators_ types.Address `abi:"_validators"`
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

type ERC20PredicateSetDependenciesFn struct {
	Gateway_     types.Address `abi:"_gateway"`
	NativeToken_ types.Address `abi:"_nativeToken"`
}

func (g *ERC20PredicateSetDependenciesFn) Sig() []byte {
	return ERC20TokenPredicate.Abi.Methods["setDependencies"].ID()
}

func (g *ERC20PredicateSetDependenciesFn) EncodeAbi() ([]byte, error) {
	return ERC20TokenPredicate.Abi.Methods["setDependencies"].Encode(g)
}

func (g *ERC20PredicateSetDependenciesFn) DecodeAbi(buf []byte) error {
	return decodeMethod(ERC20TokenPredicate.Abi.Methods["setDependencies"], buf, g)
}

type NativeERC20SetDependenciesFn struct {
	Predicate_ types.Address `abi:"predicate_"`
	Name_      string        `abi:"name_"`
	Symbol_    string        `abi:"symbol_"`
	Decimals_  uint8         `abi:"decimals_"`
	Supply_    *big.Int      `abi:"tokenSupply_"`
}

func (g *NativeERC20SetDependenciesFn) Sig() []byte {
	return NativeERC20Mintable.Abi.Methods["setDependencies"].ID()
}

func (g *NativeERC20SetDependenciesFn) EncodeAbi() ([]byte, error) {
	return NativeERC20Mintable.Abi.Methods["setDependencies"].Encode(g)
}

func (g *NativeERC20SetDependenciesFn) DecodeAbi(buf []byte) error {
	return decodeMethod(NativeERC20Mintable.Abi.Methods["setDependencies"], buf, g)
}

type ValidatorChainData struct {
	Key_ [4]*big.Int `abi:"key"`
}

type ValidatorAddressChainData struct {
	Address_ types.Address       `abi:"addr"`
	Data_    *ValidatorChainData `abi:"data"`
}

type ValidatorsSetDependenciesFn struct {
	Gateway_   types.Address                `abi:"_gatewayAddress"`
	ChainData_ []*ValidatorAddressChainData `abi:"_chainDatas"`
}

func (g *ValidatorsSetDependenciesFn) Sig() []byte {
	return Validators.Abi.Methods["setDependencies"].ID()
}

func (g *ValidatorsSetDependenciesFn) EncodeAbi() ([]byte, error) {
	return Validators.Abi.Methods["setDependencies"].Encode(g)
}

func (g *ValidatorsSetDependenciesFn) DecodeAbi(buf []byte) error {
	return decodeMethod(Validators.Abi.Methods["setDependencies"], buf, g)
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
