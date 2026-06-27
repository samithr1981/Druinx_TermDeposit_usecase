/*
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"log"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
	"github.com/npci/drunix-samples/multiorg-registry/chaincode-go/chaincode"
)

func main() {
	cc, err := contractapi.NewChaincode(&chaincode.SmartContract{})
	if err != nil {
		log.Panicf("Error creating multiorg-registry chaincode: %v", err)
	}
	if err := cc.Start(); err != nil {
		log.Panicf("Error starting multiorg-registry chaincode: %v", err)
	}
}
