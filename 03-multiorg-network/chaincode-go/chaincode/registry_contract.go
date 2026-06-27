package chaincode

import (
	"encoding/json"
	"fmt"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// =============================================================================
// Drunix Two-Org Shared Registry — demo chaincode
//
// Purpose: prove a GENUINE multi-org network. A trade asset is created by one
// organisation and transferred to the other. The chaincode is deployed with an
// endorsement policy that REQUIRES BOTH orgs to sign, so every state change is
// validated independently by Org1 and Org2 before it commits to the shared
// ledger. Querying the same asset from either org returns identical data —
// that identical-state-across-orgs is the whole point of the demo.
//
// The chaincode itself is intentionally simple. The multi-org proof lives in
// the DEPLOYMENT (two peers, two MSPs, AND-endorsement) — see README.
//
// Each asset records the MSP id of the org that created it and the org that
// currently owns it, so you can see ownership cross the org boundary.
// =============================================================================

type SmartContract struct {
	contractapi.Contract
}

// Asset is a tradeable item on the shared ledger.
// Fields alphabetical for cross-language determinism.
type Asset struct {
	CreatedByOrg string `json:"CreatedByOrg"` // MSP id that issued the asset
	DocType      string `json:"DocType"`      // "asset"
	ID           string `json:"ID"`
	OwnerOrg     string `json:"OwnerOrg"` // MSP id that currently owns it
	Value        int    `json:"Value"`
}

const docTypeAsset = "asset"

func callerMSPID(ctx contractapi.TransactionContextInterface) (string, error) {
	id, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return "", fmt.Errorf("failed to read caller MSP id: %v", err)
	}
	return id, nil
}

func assetKey(id string) string { return "ASSET_" + id }

// CreateAsset issues an asset. The creating org becomes the initial owner.
func (s *SmartContract) CreateAsset(ctx contractapi.TransactionContextInterface, id string, value int) error {
	exists, err := s.AssetExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("asset %s already exists", id)
	}

	org, err := callerMSPID(ctx)
	if err != nil {
		return err
	}

	asset := Asset{
		ID:           id,
		DocType:      docTypeAsset,
		Value:        value,
		CreatedByOrg: org,
		OwnerOrg:     org,
	}
	assetJSON, err := json.Marshal(asset)
	if err != nil {
		return err
	}
	if err := ctx.GetStub().PutState(assetKey(id), assetJSON); err != nil {
		return err
	}
	_ = ctx.GetStub().SetEvent("AssetCreated", assetJSON)
	return nil
}

// TransferAsset moves ownership to a different org (e.g. Org1MSP -> Org2MSP).
// Only the current owning org may transfer it. Because the endorsement policy
// requires BOTH orgs to sign, the receiving org also validates this move.
func (s *SmartContract) TransferAsset(ctx contractapi.TransactionContextInterface, id string, newOwnerOrg string) (string, error) {
	asset, err := s.ReadAsset(ctx, id)
	if err != nil {
		return "", err
	}

	caller, err := callerMSPID(ctx)
	if err != nil {
		return "", err
	}
	if caller != asset.OwnerOrg {
		return "", fmt.Errorf("transfer denied: caller org %s is not the owner org %s", caller, asset.OwnerOrg)
	}
	if newOwnerOrg == asset.OwnerOrg {
		return "", fmt.Errorf("asset %s already owned by %s", id, newOwnerOrg)
	}

	oldOwner := asset.OwnerOrg
	asset.OwnerOrg = newOwnerOrg

	assetJSON, err := json.Marshal(asset)
	if err != nil {
		return "", err
	}
	if err := ctx.GetStub().PutState(assetKey(id), assetJSON); err != nil {
		return "", err
	}
	_ = ctx.GetStub().SetEvent("AssetTransferred", assetJSON)

	return fmt.Sprintf("asset %s transferred from %s to %s", id, oldOwner, newOwnerOrg), nil
}

func (s *SmartContract) ReadAsset(ctx contractapi.TransactionContextInterface, id string) (*Asset, error) {
	assetJSON, err := ctx.GetStub().GetState(assetKey(id))
	if err != nil {
		return nil, fmt.Errorf("failed to read asset: %v", err)
	}
	if assetJSON == nil {
		return nil, fmt.Errorf("asset %s does not exist", id)
	}
	var asset Asset
	if err := json.Unmarshal(assetJSON, &asset); err != nil {
		return nil, err
	}
	return &asset, nil
}

func (s *SmartContract) AssetExists(ctx contractapi.TransactionContextInterface, id string) (bool, error) {
	assetJSON, err := ctx.GetStub().GetState(assetKey(id))
	if err != nil {
		return false, fmt.Errorf("failed to read asset: %v", err)
	}
	return assetJSON != nil, nil
}

func (s *SmartContract) GetAllAssets(ctx contractapi.TransactionContextInterface) ([]*Asset, error) {
	it, err := ctx.GetStub().GetStateByRange("ASSET_", "ASSET_~")
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var assets []*Asset
	for it.HasNext() {
		kv, err := it.Next()
		if err != nil {
			return nil, err
		}
		var a Asset
		if err := json.Unmarshal(kv.Value, &a); err != nil {
			continue
		}
		if a.DocType == docTypeAsset {
			assets = append(assets, &a)
		}
	}
	return assets, nil
}

// WhoAmI returns the caller's MSP id — handy to confirm which org you are
// acting as during the demo.
func (s *SmartContract) WhoAmI(ctx contractapi.TransactionContextInterface) (string, error) {
	return callerMSPID(ctx)
}
