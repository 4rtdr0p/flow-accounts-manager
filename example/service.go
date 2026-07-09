package example

import (
	"context"
	_ "embed"

	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	log "github.com/sirupsen/logrus"
)

//go:embed setup_example.cdc
var setupExampleCDC string

// Service implements the example account setup logic.
type Service struct {
	deps plugins.PluginDeps
}

// NewService creates a new example service using the shared plugin dependencies.
func NewService(deps plugins.PluginDeps) *Service {
	return &Service{deps: deps}
}

// SetupExampleAccount runs the bundled example setup transaction for the given address.
// It registers FlowToken, FUSD and ExampleNFT as enabled account tokens when the
// transaction succeeds (or is idempotent).
func (s *Service) SetupExampleAccount(ctx context.Context, sync bool, address string) (*jobs.Job, *transactions.Transaction, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}

	if _, err := s.deps.Accounts.Details(address); err != nil {
		return nil, nil, err
	}

	job, tx, err := s.deps.Transactions.Create(ctx, sync, address, setupExampleCDC, nil, TransactionType)
	if err == nil || flow_helpers.IsVaultExistsError(err) {
		for _, tokenName := range setupTokenNames {
			if addErr := s.deps.Tokens.AddAccountToken(tokenName, address); addErr != nil {
				log.
					WithFields(log.Fields{"error": addErr, "tokenName": tokenName}).
					Warn("Error adding account token during example setup")
			}
		}
	}

	return job, tx, err
}
