package genesis

import (
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	apexConfigFlag            = "apex-config"
	apexConfigDescriptionFlag = "change behaviour of blade (0 - apex bridge (default) | 1 - blade | 2 - nexus chain)"

	ApexConfigDefault     = 0
	ApexConfigNormalBlade = 1
	ApexConfigNexus       = 2
)

func getApexContracts() []*contractInfo {
	return []*contractInfo{
		// Apex contracts
		{
			artifact: contractsapi.ApexBridgeContracts.Bridge,
			address:  contracts.BridgeAddr,
		},
		{
			artifact: contractsapi.ApexBridgeContracts.ClaimsHelper,
			address:  contracts.ClaimsHelperAddr,
		},
		{
			artifact: contractsapi.ApexBridgeContracts.Claims,
			address:  contracts.ClaimsAddr,
		},
		{
			artifact: contractsapi.ApexBridgeContracts.SignedBatches,
			address:  contracts.SignedBatchesAddr,
		},
		{
			artifact: contractsapi.ApexBridgeContracts.Slots,
			address:  contracts.SlotsAddr,
		},
		{
			artifact: contractsapi.ApexBridgeContracts.Validators,
			address:  contracts.ValidatorsAddr,
		},
		{
			artifact: contractsapi.ApexBridgeContracts.Admin,
			address:  contracts.ApexBridgeAdminAddr,
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

func (p *genesisParams) processConfigApex(chainConfig *chain.Chain) {
	switch p.apexConfig {
	case ApexConfigDefault:
		chainConfig.Params.Forks.RemoveFork(chain.Governance).RemoveFork(chain.London)
		chainConfig.Params.BurnContract = nil
	case ApexConfigNexus:
		chainConfig.Genesis.GasLimit = 0x500000
		chainConfig.Params.BurnContract = map[uint64]types.Address{
			0: types.ZeroAddress,
		}
		chainConfig.Params.Forks.
			RemoveFork(chain.Governance).
			RemoveFork(chain.EIP3855).
			RemoveFork(chain.Berlin).
			RemoveFork(chain.EIP3607)
	}
}
