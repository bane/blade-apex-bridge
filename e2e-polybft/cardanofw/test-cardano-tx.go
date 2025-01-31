package cardanofw

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	infracommon "github.com/Ethernal-Tech/cardano-infrastructure/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

const (
	PotentialFee     = 250_000
	ttlSlotNumberInc = 500
)

func SendTx(ctx context.Context,
	txProvider wallet.ITxProvider,
	cardanoWallet *wallet.Wallet,
	amount uint64,
	receiver string,
	networkType wallet.CardanoNetworkType,
	metadata []byte,
) (txHash string, err error) {
	return infracommon.ExecuteWithRetry(ctx, func(ctx context.Context) (string, error) {
		return sendTx(ctx, txProvider, cardanoWallet, amount, receiver, networkType, metadata)
	})
}

func sendTx(ctx context.Context,
	txProvider wallet.ITxProvider,
	cardanoWallet *wallet.Wallet,
	amount uint64,
	receiver string,
	networkType wallet.CardanoNetworkType,
	metadata []byte,
) (string, error) {
	caddr, err := GetAddress(networkType, cardanoWallet)
	if err != nil {
		return "", err
	}

	cardanoWalletAddr := caddr.String()
	networkTestMagic := GetNetworkMagic(networkType)
	cardanoCliBinary := ResolveCardanoCliBinary(networkType)

	protocolParams, err := txProvider.GetProtocolParameters(ctx)
	if err != nil {
		return "", err
	}

	qtd, err := txProvider.GetTip(ctx)
	if err != nil {
		return "", err
	}

	outputs := []wallet.TxOutput{
		{
			Addr:   receiver,
			Amount: amount,
		},
	}
	desiredSum := amount + PotentialFee + MinUTxODefaultValue

	inputs, err := wallet.GetUTXOsForAmount(
		ctx, txProvider, cardanoWalletAddr,
		[]string{wallet.AdaTokenName},
		map[string]uint64{wallet.AdaTokenName: desiredSum},
		map[string]uint64{wallet.AdaTokenName: desiredSum},
	)
	if err != nil {
		return "", err
	}

	rawTx, txHash, err := CreateTx(
		cardanoCliBinary,
		networkTestMagic, protocolParams,
		qtd.Slot+ttlSlotNumberInc, metadata,
		outputs, inputs, cardanoWalletAddr, MinUTxODefaultValue)
	if err != nil {
		return "", err
	}

	txBilder, err := wallet.NewTxBuilder(cardanoCliBinary)
	if err != nil {
		return "", err
	}

	defer txBilder.Dispose()

	signedTx, err := txBilder.SignTx(rawTx, []wallet.ITxSigner{cardanoWallet})
	if err != nil {
		return "", err
	}

	return txHash, txProvider.SubmitTx(ctx, signedTx)
}

func GetGenesisWalletFromCluster(
	dirPath string,
	keyID uint,
) (*wallet.Wallet, error) {
	keyFileName := strings.Join([]string{"utxo", fmt.Sprint(keyID)}, "")

	sKey, err := wallet.NewKey(filepath.Join(dirPath, "utxo-keys", fmt.Sprintf("%s.skey", keyFileName)))
	if err != nil {
		return nil, err
	}

	sKeyBytes, err := sKey.GetKeyBytes()
	if err != nil {
		return nil, err
	}

	return wallet.NewWallet(sKeyBytes, nil), nil
}

// CreateTx creates tx and returns cbor of raw transaction data, tx hash and error
func CreateTx(
	cardanoCliBinary string,
	testNetMagic uint,
	protocolParams []byte,
	timeToLive uint64,
	metadataBytes []byte,
	outputs []wallet.TxOutput,
	inputs wallet.TxInputs,
	changeAddress string,
	minUTxODefaultValue uint64,
) ([]byte, string, error) {
	outputsSum := wallet.GetOutputsSum(outputs)[wallet.AdaTokenName]

	builder, err := wallet.NewTxBuilder(cardanoCliBinary)
	if err != nil {
		return nil, "", err
	}

	defer builder.Dispose()

	if len(metadataBytes) != 0 {
		builder.SetMetaData(metadataBytes)
	}

	tokens, err := wallet.GetTokensFromSumMap(inputs.Sum)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create tokens from sum map. err: %w", err)
	}

	if len(tokens) > 0 {
		fmt.Printf("CreateTx - found tokens in inputs, rerouting to change output: %v\n", tokens)
	}

	builder.SetProtocolParameters(protocolParams).SetTimeToLive(timeToLive).
		SetTestNetMagic(testNetMagic).
		AddInputs(inputs.Inputs...).
		AddOutputs(outputs...).AddOutputs(wallet.TxOutput{Addr: changeAddress, Tokens: tokens})

	fee, err := builder.CalculateFee(0)
	if err != nil {
		return nil, "", err
	}

	inputsAdaSum := inputs.Sum[wallet.AdaTokenName]
	change := inputsAdaSum - outputsSum - fee
	// handle overflow or insufficient amount
	if change > inputsAdaSum || change < minUTxODefaultValue {
		return []byte{}, "", fmt.Errorf("insufficient amount %d for %d or min utxo not satisfied",
			inputsAdaSum, outputsSum+fee)
	}

	builder.UpdateOutputAmount(-1, change)

	builder.SetFee(fee)

	return builder.Build()
}
