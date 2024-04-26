package genesis

import (
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
)

func getApexContracts() []*contractInfo {
	return []*contractInfo{
		// Apex contracts
		{
			artifact: contractsapi.Bridge,
			address:  contracts.BridgeAddr,
		},
		{
			artifact: contractsapi.ClaimsHelper,
			address:  contracts.ClaimsHelperAddr,
		},
		{
			artifact: contractsapi.Claims,
			address:  contracts.ClaimsAddr,
		},
		{
			artifact: contractsapi.SignedBatches,
			address:  contracts.SignedBatchesAddr,
		},
		{
			artifact: contractsapi.Slots,
			address:  contracts.SlotsAddr,
		},
		{
			artifact: contractsapi.UTXOsc,
			address:  contracts.UTXOscAddr,
		},
		{
			artifact: contractsapi.Validators,
			address:  contracts.ValidatorsAddr,
		},
	}
}

func getApexProxyAddresses() (retVal []types.Address) {
	apexProxyToImplAddrMap := contracts.GetApexProxyImplementationMapping()
	for address := range apexProxyToImplAddrMap {
		retVal = append(retVal, address)
	}

	return
}
