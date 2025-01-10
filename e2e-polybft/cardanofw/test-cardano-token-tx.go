package cardanofw

import (
	"context"
	"errors"
	"fmt"

	"github.com/Ethernal-Tech/cardano-infrastructure/common"
	"github.com/Ethernal-Tech/cardano-infrastructure/sendtx"
	cardanowallet "github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

const (
	defaultTokenName       = "test1"
	defaultTokenMintAmount = uint64(1_000_000_000)
)

func FundUserWithToken(ctx context.Context, chain ChainID,
	networkType cardanowallet.CardanoNetworkType, txProvider cardanowallet.ITxProvider,
	minterUser *TestApexUser, userToFund *TestApexUser, lovelaceFundAmount uint64, tokenFundAmount uint64,
) (*cardanowallet.TokenAmount, error) {
	minterWallet, _ := minterUser.GetCardanoWallet(chain)

	keyHash, err := cardanowallet.GetKeyHash(minterWallet.VerificationKey)
	if err != nil {
		return nil, err
	}

	policy := cardanowallet.PolicyScript{
		Type:    cardanowallet.PolicyScriptSigType,
		KeyHash: keyHash,
	}

	cardanoCliBinary := cardanowallet.ResolveCardanoCliBinary(networkType)

	pid, _ := cardanowallet.NewCliUtils(cardanoCliBinary).GetPolicyID(policy)
	mintToken := cardanowallet.NewTokenAmount(pid, defaultTokenName, defaultTokenMintAmount)

	txHash, err := MintTokens(
		ctx, networkType, txProvider, minterWallet, lovelaceFundAmount,
		[]cardanowallet.TokenAmount{mintToken}, []cardanowallet.IPolicyScript{policy},
	)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Done minting tokens. txHash: %s\n", txHash)

	userToFundAddr := userToFund.GetAddress(chain)

	fundToken := cardanowallet.NewTokenAmount(pid, defaultTokenName, tokenFundAmount)

	txHash, err = SendTxWithTokens(
		ctx, networkType, txProvider, minterWallet, userToFundAddr, lovelaceFundAmount,
		[]cardanowallet.TokenAmount{fundToken}, nil,
	)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Funded user %s with lovelace + native tokens. txHash: %s\n", userToFundAddr, txHash)

	return &fundToken, nil
}

func SendTxWithTokens(
	ctx context.Context,
	networkType cardanowallet.CardanoNetworkType,
	txProvider cardanowallet.ITxProvider,
	senderWallet *cardanowallet.Wallet,
	receiverAddr string,
	lovelaceAmount uint64,
	tokens []cardanowallet.TokenAmount,
	metadata []byte,
) (string, error) {
	if len(tokens) == 0 {
		return "", errors.New("no tokens")
	}

	txRaw, txHash, err := createNativeTokenTx(
		ctx, networkType, txProvider, senderWallet, receiverAddr, lovelaceAmount, tokens, metadata)
	if err != nil {
		return "", err
	}

	err = submitTokenTx(ctx, txProvider, txRaw, txHash, receiverAddr, tokens, lovelaceAmount)
	if err != nil {
		return "", err
	}

	return txHash, nil
}

func MintTokens(
	ctx context.Context,
	networkType cardanowallet.CardanoNetworkType,
	txProvider cardanowallet.ITxProvider,
	wallet *cardanowallet.Wallet,
	lovelaceAmount uint64,
	tokens []cardanowallet.TokenAmount,
	tokenPolicyScripts []cardanowallet.IPolicyScript,
) (string, error) {
	if len(tokens) == 0 || len(tokenPolicyScripts) == 0 {
		return "", errors.New("no tokens or policy scripts")
	}

	walletAddr, err := cardanowallet.NewEnterpriseAddress(networkType, wallet.VerificationKey)
	if err != nil {
		return "", err
	}

	txRaw, txHash, err := createMintTx(
		ctx, networkType, txProvider, wallet, lovelaceAmount,
		tokens, tokenPolicyScripts,
	)
	if err != nil {
		return "", err
	}

	err = submitTokenTx(ctx, txProvider, txRaw, txHash, walletAddr.String(), tokens, 0)
	if err != nil {
		return "", err
	}

	return txHash, nil
}

func createNativeTokenTx(
	ctx context.Context,
	networkType cardanowallet.CardanoNetworkType,
	txProvider cardanowallet.ITxProvider,
	senderWallet *cardanowallet.Wallet,
	receiverAddr string,
	lovelaceAmount uint64,
	tokens []cardanowallet.TokenAmount,
	metadata []byte,
) ([]byte, string, error) {
	senderWalletAddr, err := cardanowallet.NewEnterpriseAddress(networkType, senderWallet.VerificationKey)
	if err != nil {
		return nil, "", err
	}

	cardanoCliBinary := ResolveCardanoCliBinary(networkType)
	networkTestMagic := GetNetworkMagic(networkType)

	builder, err := cardanowallet.NewTxBuilder(cardanoCliBinary)
	if err != nil {
		return nil, "", err
	}

	defer builder.Dispose()

	builder.SetTestNetMagic(networkTestMagic)

	if err := builder.SetProtocolParametersAndTTL(ctx, txProvider, 0); err != nil {
		return nil, "", err
	}

	if len(metadata) != 0 {
		builder.SetMetaData(metadata)
	}

	desiredAmount := potentialFee + lovelaceAmount + MinUTxODefaultValue

	inputs, err := sendtx.GetUTXOsForAmounts(
		[]cardanowallet.Utxo{}, // TODO: FIX
		map[string]uint64{cardanowallet.AdaTokenName: desiredAmount},
		10,
		1,
	)
	if err != nil {
		return nil, "", err
	}

	lovelaceInputAmount := inputs.Sum[cardanowallet.AdaTokenName]

	remainingTokens, err := cardanowallet.GetTokensFromSumMap(inputs.Sum)
	if err != nil {
		return nil, "", err
	}

	for _, token := range tokens {
		for i, inputToken := range remainingTokens {
			if token.Name == inputToken.Name {
				remainingTokens[i].Amount -= token.Amount

				break
			}
		}
	}

	builder.AddInputs(inputs.Inputs...)
	builder.AddOutputs(cardanowallet.TxOutput{
		Addr:   receiverAddr,
		Amount: lovelaceAmount,
		Tokens: tokens,
	}).AddOutputs(cardanowallet.TxOutput{
		Addr:   senderWalletAddr.String(),
		Tokens: remainingTokens,
	})

	fee, err := builder.CalculateFee(0)
	if err != nil {
		return nil, "", err
	}

	change := lovelaceInputAmount - lovelaceAmount - fee
	// handle overflow or insufficient amount
	if change > lovelaceInputAmount || change < MinUTxODefaultValue {
		return []byte{}, "", fmt.Errorf("insufficient amount: %d", change)
	}

	builder.UpdateOutputAmount(-1, change)

	builder.SetFee(fee)

	txRaw, txHash, err := builder.Build()
	if err != nil {
		return nil, "", err
	}

	witness, err := cardanowallet.CreateTxWitness(txHash, senderWallet)
	if err != nil {
		return nil, "", err
	}

	txSigned, err := builder.AssembleTxWitnesses(txRaw, [][]byte{witness})
	if err != nil {
		return nil, "", err
	}

	return txSigned, txHash, nil
}

func createMintTx(
	ctx context.Context,
	networkType cardanowallet.CardanoNetworkType,
	txProvider cardanowallet.ITxProvider,
	wallet *cardanowallet.Wallet,
	lovelaceAmount uint64,
	tokens []cardanowallet.TokenAmount,
	tokenPolicyScripts []cardanowallet.IPolicyScript,
) ([]byte, string, error) {
	walletAddr, err := cardanowallet.NewEnterpriseAddress(networkType, wallet.VerificationKey)
	if err != nil {
		return nil, "", err
	}

	cardanoCliBinary := ResolveCardanoCliBinary(networkType)
	networkTestMagic := GetNetworkMagic(networkType)

	builder, err := cardanowallet.NewTxBuilder(cardanoCliBinary)
	if err != nil {
		return nil, "", err
	}

	defer builder.Dispose()

	builder.SetTestNetMagic(networkTestMagic)

	if err := builder.SetProtocolParametersAndTTL(ctx, txProvider, 0); err != nil {
		return nil, "", err
	}

	desiredAmount := potentialFee + lovelaceAmount + MinUTxODefaultValue

	inputs, err := sendtx.GetUTXOsForAmounts(
		[]cardanowallet.Utxo{}, // TODO: FIX
		map[string]uint64{cardanowallet.AdaTokenName: desiredAmount},
		10,
		1,
	)
	if err != nil {
		return nil, "", err
	}

	lovelaceInputAmount := inputs.Sum[cardanowallet.AdaTokenName]

	remainingTokens, err := cardanowallet.GetTokensFromSumMap(inputs.Sum)
	if err != nil {
		return nil, "", err
	}

	builder.AddInputs(inputs.Inputs...).AddTokenMints(tokenPolicyScripts, tokens)
	builder.AddOutputs(cardanowallet.TxOutput{
		Addr:   walletAddr.String(),
		Amount: lovelaceAmount,
		Tokens: append(remainingTokens, tokens...),
	}).AddOutputs(cardanowallet.TxOutput{
		Addr: walletAddr.String(),
	})

	fee, err := builder.CalculateFee(0)
	if err != nil {
		return nil, "", err
	}

	change := lovelaceInputAmount - lovelaceAmount - fee
	// handle overflow or insufficient amount
	if change > lovelaceInputAmount || change < MinUTxODefaultValue {
		return []byte{}, "", fmt.Errorf("insufficient amount: %d", change)
	}

	builder.UpdateOutputAmount(-1, change)

	builder.SetFee(fee)

	txRaw, txHash, err := builder.Build()
	if err != nil {
		return nil, "", err
	}

	witness, err := cardanowallet.CreateTxWitness(txHash, wallet)
	if err != nil {
		return nil, "", err
	}

	txSigned, err := builder.AssembleTxWitnesses(txRaw, [][]byte{witness})
	if err != nil {
		return nil, "", err
	}

	return txSigned, txHash, nil
}

func submitTokenTx(
	ctx context.Context,
	txProvider cardanowallet.ITxProvider,
	txRaw []byte,
	txHash string,
	addr string,
	tokenAmounts []cardanowallet.TokenAmount,
	lovelaceAmount uint64,
) error {
	utxo, err := txProvider.GetUtxos(ctx, addr)
	if err != nil {
		return err
	}

	amountsSum := cardanowallet.GetUtxosSum(utxo)
	expectedAtLeast := make(map[string]uint64)

	for _, tokAmount := range tokenAmounts {
		tokenName := tokAmount.TokenName()
		expectedAtLeast[tokenName] = amountsSum[tokenName] + tokAmount.Amount
	}

	if lovelaceAmount != 0 {
		expectedAtLeast[cardanowallet.AdaTokenName] = amountsSum[cardanowallet.AdaTokenName] + lovelaceAmount
	}

	if err := txProvider.SubmitTx(ctx, txRaw); err != nil {
		return err
	}

	fmt.Println("transaction has been submitted. hash =", txHash)

	newAmounts, err := common.ExecuteWithRetry(ctx, func(ctx context.Context) (map[string]uint64, error) {
		utxos, err := txProvider.GetUtxos(ctx, addr)
		if err != nil {
			return nil, err
		}

		amountsSum := cardanowallet.GetUtxosSum(utxos)

		for _, tokAmount := range tokenAmounts {
			tokenName := tokAmount.TokenName()
			if amountsSum[tokenName] < expectedAtLeast[tokenName] {
				return nil, common.ErrRetryTryAgain
			}
		}

		if lovelaceAmount != 0 {
			if amountsSum[cardanowallet.AdaTokenName] < expectedAtLeast[cardanowallet.AdaTokenName] {
				return nil, common.ErrRetryTryAgain
			}
		}

		return amountsSum, nil
	}, common.WithRetryCount(60))
	if err != nil {
		return err
	}

	fmt.Printf("transaction has been included in block. hash = %s, balance = %v\n", txHash, newAmounts)

	return nil
}
