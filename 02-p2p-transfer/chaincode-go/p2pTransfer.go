/*
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"log"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
	"github.com/npci/drunix-samples/p2p-transfer/chaincode-go/chaincode"
)

func main() {
	p2pChaincode, err := contractapi.NewChaincode(&chaincode.SmartContract{})
	if err != nil {
		log.Panicf("Error creating p2p-transfer chaincode: %v", err)
	}

	if err := p2pChaincode.Start(); err != nil {
		log.Panicf("Error starting p2p-transfer chaincode: %v", err)
	}
}
