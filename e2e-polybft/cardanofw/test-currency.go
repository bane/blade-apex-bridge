package cardanofw

import "math/big"

const (
	DfmDecimals = 6
	WeiDecimals = 18
)

func DfmToChainNativeTokenAmount(chainID string, dfmAmount *big.Int) *big.Int {
	if chainID == ChainIDNexus {
		return DfmToWei(dfmAmount)
	}

	return dfmAmount
}

func ChainNativeTokenAmountToDfm(chainID string, nativeTokenAmount *big.Int) *big.Int {
	if chainID == ChainIDNexus {
		return WeiToDfm(nativeTokenAmount)
	}

	return nativeTokenAmount
}

func ApexToDfm(apex *big.Int) *big.Int {
	dfm := new(big.Int).Set(apex)
	base := big.NewInt(10)

	return dfm.Mul(dfm, base.Exp(base, big.NewInt(DfmDecimals), nil))
}

func DfmToApex(dfm *big.Int) *big.Int {
	apex := new(big.Int).Set(dfm)
	base := big.NewInt(10)

	return apex.Div(apex, base.Exp(base, big.NewInt(DfmDecimals), nil))
}

func ApexToWei(apex *big.Int) *big.Int {
	wei := new(big.Int).Set(apex)
	base := big.NewInt(10)

	return wei.Mul(wei, base.Exp(base, big.NewInt(WeiDecimals), nil))
}

func DfmToWei(dfm *big.Int) *big.Int {
	wei := new(big.Int).Set(dfm)
	base := big.NewInt(10)

	return wei.Mul(wei, base.Exp(base, big.NewInt(WeiDecimals-DfmDecimals), nil))
}

func WeiToDfm(wei *big.Int) *big.Int {
	dfm := new(big.Int).Set(wei)
	base := big.NewInt(10)
	dfm.Div(dfm, base.Exp(base, big.NewInt(WeiDecimals-DfmDecimals), nil))

	return dfm
}

func WeiToDfmCeil(wei *big.Int) *big.Int {
	dfm := new(big.Int).Set(wei)
	base := big.NewInt(10)
	mod := new(big.Int)
	dfm.DivMod(dfm, base.Exp(base, big.NewInt(WeiDecimals-DfmDecimals), nil), mod)

	if mod.BitLen() > 0 { // for zero big.Int BitLen() == 0
		dfm.Add(dfm, big.NewInt(1))
	}

	return dfm
}
