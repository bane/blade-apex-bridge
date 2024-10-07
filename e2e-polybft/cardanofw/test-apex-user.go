package cardanofw

import (
	"encoding/hex"
	"fmt"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

type TestApexUser struct {
	PrimeWallet  cardanowallet.IWallet
	PrimeAddress cardanowallet.CardanoAddress

	HasVectorWallet bool
	VectorWallet    cardanowallet.IWallet
	VectorAddress   cardanowallet.CardanoAddress

	HasNexusWallet bool
	NexusWallet    *crypto.ECDSAKey
	NexusAddress   types.Address
}

func NewTestApexUser(
	primeNetworkType cardanowallet.CardanoNetworkType,
	vectorEnabled bool,
	vectorNetworkType cardanowallet.CardanoNetworkType,
	nexusEnabled bool,
) (*TestApexUser, error) {
	var (
		vectorWallet      *cardanowallet.Wallet        = nil
		vectorUserAddress cardanowallet.CardanoAddress = nil
		nexusWallet       *crypto.ECDSAKey             = nil
		nexusUserAddress                               = types.Address{}
	)

	primeWallet, err := cardanowallet.GenerateWallet(false)
	if err != nil {
		return nil, err
	}

	primeUserAddress, err := GetAddress(primeNetworkType, primeWallet)
	if err != nil {
		return nil, err
	}

	if vectorEnabled {
		vectorWallet, err = cardanowallet.GenerateWallet(false)
		if err != nil {
			return nil, err
		}

		vectorUserAddress, err = GetAddress(vectorNetworkType, vectorWallet)
		if err != nil {
			return nil, err
		}
	}

	if nexusEnabled {
		nexusWallet, err = crypto.GenerateECDSAKey()
		if err != nil {
			return nil, err
		}

		nexusUserAddress = nexusWallet.Address()
	}

	return &TestApexUser{
		PrimeWallet:     primeWallet,
		PrimeAddress:    primeUserAddress,
		VectorWallet:    vectorWallet,
		VectorAddress:   vectorUserAddress,
		HasVectorWallet: vectorEnabled,
		NexusWallet:     nexusWallet,
		NexusAddress:    nexusUserAddress,
		HasNexusWallet:  nexusEnabled,
	}, nil
}

func NewExistingTestApexUser(
	primePrivateKey, vectorPrivateKey, nexusPrivateKey string,
	primeNetworkType cardanowallet.CardanoNetworkType,
	vectorNetworkType cardanowallet.CardanoNetworkType,
) (*TestApexUser, error) {
	var (
		vectorWallet      *cardanowallet.Wallet        = nil
		vectorUserAddress cardanowallet.CardanoAddress = nil
		nexusWallet       *crypto.ECDSAKey             = nil
		nexusUserAddress                               = types.Address{}
	)

	primePrivateKeyBytes, err := cardanowallet.GetKeyBytes(primePrivateKey)
	if err != nil {
		return nil, err
	}

	primeWallet := cardanowallet.NewWallet(
		cardanowallet.GetVerificationKeyFromSigningKey(primePrivateKeyBytes), primePrivateKeyBytes)

	primeUserAddress, err := GetAddress(primeNetworkType, primeWallet)
	if err != nil {
		return nil, err
	}

	if vectorPrivateKey != "" {
		vectorPrivateKeyBytes, err := cardanowallet.GetKeyBytes(vectorPrivateKey)
		if err != nil {
			return nil, err
		}

		vectorWallet = cardanowallet.NewWallet(
			cardanowallet.GetVerificationKeyFromSigningKey(vectorPrivateKeyBytes), vectorPrivateKeyBytes)

		vectorUserAddress, err = GetAddress(vectorNetworkType, vectorWallet)
		if err != nil {
			return nil, err
		}
	}

	if nexusPrivateKey != "" {
		pkBytes, err := hex.DecodeString(nexusPrivateKey)
		if err != nil {
			return nil, err
		}

		nexusWallet, err = crypto.NewECDSAKeyFromRawPrivECDSA(pkBytes)
		if err != nil {
			return nil, err
		}

		nexusUserAddress = nexusWallet.Address()
	}

	return &TestApexUser{
		PrimeWallet:     primeWallet,
		PrimeAddress:    primeUserAddress,
		VectorWallet:    vectorWallet,
		VectorAddress:   vectorUserAddress,
		HasVectorWallet: vectorPrivateKey != "",
		NexusWallet:     nexusWallet,
		NexusAddress:    nexusUserAddress,
		HasNexusWallet:  nexusPrivateKey != "",
	}, nil
}

func (u *TestApexUser) GetCardanoWallet(chain ChainID) (
	cardanowallet.IWallet, cardanowallet.CardanoAddress,
) {
	if chain == ChainIDPrime {
		return u.PrimeWallet, u.PrimeAddress
	} else if chain == ChainIDVector {
		return u.VectorWallet, u.VectorAddress
	}

	return nil, nil
}

func (u *TestApexUser) GetEvmWallet(chain ChainID) (
	*crypto.ECDSAKey, types.Address,
) {
	if chain == ChainIDNexus {
		return u.NexusWallet, u.NexusAddress
	}

	return nil, types.Address{}
}

func (u *TestApexUser) GetAddress(chain ChainID) string {
	switch chain {
	case ChainIDPrime:
		return u.PrimeAddress.String()
	case ChainIDVector:
		if u.HasVectorWallet {
			return u.VectorAddress.String()
		}

		return ""
	case ChainIDNexus:
		if u.HasNexusWallet {
			return u.NexusAddress.String()
		}

		return ""
	}

	return ""
}

func (u *TestApexUser) GetPrivateKey(chain ChainID) (string, error) {
	switch chain {
	case ChainIDPrime:
		return hex.EncodeToString(u.PrimeWallet.GetSigningKey()), nil
	case ChainIDVector:
		if u.HasVectorWallet {
			return hex.EncodeToString(u.VectorWallet.GetSigningKey()), nil
		}

		return "", fmt.Errorf("user doesn't have a vector wallet")
	case ChainIDNexus:
		if u.HasNexusWallet {
			pkBytes, err := u.NexusWallet.MarshallPrivateKey()
			if err != nil {
				return "", err
			}

			return hex.EncodeToString(pkBytes), nil
		}

		return "", fmt.Errorf("user doesn't have a nexus wallet")
	}

	return "", nil
}
