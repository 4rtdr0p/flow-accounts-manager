package tokens

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/flow-hydraulics/flow-wallet-api/accounts"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/templates"
	"github.com/flow-hydraulics/flow-wallet-api/templates/template_strings"
	"github.com/onflow/flow-go-sdk"
	flow_templates "github.com/onflow/flow-go-sdk/templates"
)

// DeployTokenContractForAccount is used for testing purposes.
func (s *ServiceImpl) DeployTokenContractForAccount(ctx context.Context, runSync bool, tokenName, address string) error {
	// Check if the input is a valid address
	address, err := flow_helpers.ValidateAddress(address, s.cfg.ChainID)
	if err != nil {
		return err
	}

	token, err := s.templates.GetTokenByName(tokenName)
	if err != nil {
		return err
	}

	n := token.Name

	flowAddress := flow.HexToAddress(address)
	account, err := s.fc.GetAccount(ctx, flowAddress)
	if err != nil {
		return err
	}
	if _, ok := account.Contracts[n]; ok {
		return nil
	}

	src, err := contractSourceForDeploy(n, s.cfg.ChainID)
	if err != nil {
		return err
	}

	c := flow_templates.Contract{Name: n, Source: src}

	err = accounts.AddContract(ctx, s.fc, s.km, address, c, s.cfg.TransactionTimeout)
	if err != nil && !strings.Contains(err.Error(), "cannot overwrite existing contract") {
		return err
	}

	return nil
}

func contractSourceForDeploy(name string, chainID flow.ChainID) (string, error) {
	contractPath := filepath.Join("flow", "cadence", "contracts", name+".cdc")
	if b, err := os.ReadFile(contractPath); err == nil {
		return templates.ResolveContractImports(chainID, string(b)), nil
	}

	tmplStr, err := template_strings.GetByName(name)
	if err != nil {
		return "", err
	}

	token := templates.Token{Name: name}
	return templates.TokenCode(chainID, &token, tmplStr)
}
