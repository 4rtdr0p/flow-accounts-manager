package accounts

import (
	"fmt"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/onflow/flow-go-sdk"
)

// RequireCustodialForSigning rejects API signing for non-custodial accounts (watchlist or graduated).
func RequireCustodialForSigning(store Store, address string, chainID flow.ChainID) error {
	address, err := flow_helpers.ValidateAddress(address, chainID)
	if err != nil {
		return err
	}

	account, err := store.Account(address)
	if err != nil {
		return err
	}

	if account.Type == AccountTypeNonCustodial {
		return &errors.RequestError{
			StatusCode: http.StatusForbidden,
			Err:        fmt.Errorf("account is non-custodial"),
		}
	}

	return nil
}
