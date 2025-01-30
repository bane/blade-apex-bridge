package cardanofw

import (
	"encoding/hex"
	"fmt"

	"github.com/0xPolygon/polygon-edge/crypto"
	wallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

type ApexPrivateKeys struct {
	PrimePaymentSigningKeyCborHex  string `json:"primePaymentSKCborHex"`
	PrimeStakeSigningKeyCborHex    string `json:"primeStakeSKCborHex"`
	VectorPaymentSigningKeyCborHex string `json:"vectorPaymentSKCborHex"`
	NexusPrivateKey                string `json:"nexusPK"`
}

func (keys *ApexPrivateKeys) Wallets() (
	prime *wallet.Wallet, vector *wallet.Wallet,
	nexus *crypto.ECDSAKey, err error,
) {
	prime, err = newCardanoWalletFromCborHex(
		keys.PrimePaymentSigningKeyCborHex, keys.PrimeStakeSigningKeyCborHex)
	if err != nil {
		return nil, nil, nil, err
	}

	if len(keys.VectorPaymentSigningKeyCborHex) > 0 {
		vector, err = newCardanoWalletFromCborHex(keys.VectorPaymentSigningKeyCborHex, "")
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if len(keys.NexusPrivateKey) > 0 {
		nexus, err = newNexusWallet(keys.NexusPrivateKey)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return prime, vector, nexus, nil
}

func (keys *ApexPrivateKeys) User(
	primeNetworkType wallet.CardanoNetworkType,
	vectorNetworkType wallet.CardanoNetworkType,
) (*TestApexUser, error) {
	primeWallet, vectorWallet, nexusWallet, err := keys.Wallets()
	if err != nil {
		return nil, err
	}

	return NewExistingTestApexUser(
		primeWallet, vectorWallet, nexusWallet,
		primeNetworkType,
		vectorNetworkType,
	)
}

func newNexusWallet(privateKey string) (*crypto.ECDSAKey, error) {
	if len(privateKey) == 0 {
		return nil, fmt.Errorf("empty private key")
	}

	pkBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return nil, err
	}

	return crypto.NewECDSAKeyFromRawPrivECDSA(pkBytes)
}

func newCardanoWalletFromCborHex(paymentKey, stakeKey string) (*wallet.Wallet, error) {
	paymentBytes, err := wallet.GetKeyBytes(paymentKey)
	if err != nil {
		return nil, err
	}

	var stakeBytes []byte
	if len(stakeKey) > 0 {
		stakeBytes, err = wallet.GetKeyBytes(stakeKey)
		if err != nil {
			return nil, err
		}
	}

	return wallet.NewWallet(paymentBytes, stakeBytes), nil
}
