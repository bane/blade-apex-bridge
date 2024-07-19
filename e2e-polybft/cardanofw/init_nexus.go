package cardanofw

import (
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
)

var (
	// Proxy contracts
	ERC1967Proxy *contracts.Artifact

	// Nexus smart contracts
	ClaimsTest *contracts.Artifact
)

func InitNexusContracts() (
	err error) {
	ERC1967Proxy, err = contracts.DecodeArtifact([]byte(contractsapi.ERC1967ProxyArtifact))
	if err != nil {
		return
	}

	ClaimsTest, err = contracts.DecodeArtifact([]byte(contractsapi.ClaimsTestArtifact))
	if err != nil {
		return
	}

	return
}
