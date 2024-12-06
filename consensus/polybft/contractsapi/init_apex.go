package contractsapi

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/contracts"
)

type ApexBridgeContractsInfo struct {
	Bridge        *contracts.Artifact
	ClaimsHelper  *contracts.Artifact
	Claims        *contracts.Artifact
	SignedBatches *contracts.Artifact
	Slots         *contracts.Artifact
	Validators    *contracts.Artifact
	Admin         *contracts.Artifact
}

var ApexBridgeContracts *ApexBridgeContractsInfo

func initApexContracts() error {
	bridge, err := contracts.DecodeArtifact([]byte(BridgeArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex bridge sc: %w", err)
	}

	claimsHelper, err := contracts.DecodeArtifact([]byte(ClaimsHelperArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex claims helper sc: %w", err)
	}

	claims, err := contracts.DecodeArtifact([]byte(ClaimsArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex claims sc: %w", err)
	}

	signedBatches, err := contracts.DecodeArtifact([]byte(SignedBatchesArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex signed batches sc: %w", err)
	}

	slots, err := contracts.DecodeArtifact([]byte(SlotsArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex slots sc: %w", err)
	}

	validators, err := contracts.DecodeArtifact([]byte(ValidatorsArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex validators sc: %w", err)
	}

	admin, err := contracts.DecodeArtifact([]byte(ApexBridgeAdminArtifact))
	if err != nil {
		return fmt.Errorf("failed to decode apex admin sc: %w", err)
	}

	ApexBridgeContracts = &ApexBridgeContractsInfo{
		Bridge:        bridge,
		ClaimsHelper:  claimsHelper,
		Claims:        claims,
		SignedBatches: signedBatches,
		Slots:         slots,
		Validators:    validators,
		Admin:         admin,
	}

	return nil
}
