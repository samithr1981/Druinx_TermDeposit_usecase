/*
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"log"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
	"github.com/npci/drunix-samples/abcfd-tokenisation/chaincode-go/chaincode"
)

func main() {
	fdChaincode, err := contractapi.NewChaincode(&chaincode.SmartContract{})
	if err != nil {
		log.Panicf("Error creating abcfd-tokenisation chaincode: %v", err)
	}

	if err := fdChaincode.Start(); err != nil {
		log.Panicf("Error starting abcfd-tokenisation chaincode: %v", err)
	}
}
