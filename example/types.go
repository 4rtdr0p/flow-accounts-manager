package example

import "github.com/flow-hydraulics/flow-wallet-api/transactions"

// TransactionType identifies the example account setup transaction.
const TransactionType = transactions.Type("ExampleSetup")

const (
	setupTokenNameFlowToken  = "FlowToken"
	setupTokenNameFUSD       = "FUSD"
	setupTokenNameExampleNFT = "ExampleNFT"
)

var setupTokenNames = []string{setupTokenNameFlowToken, setupTokenNameFUSD, setupTokenNameExampleNFT}
