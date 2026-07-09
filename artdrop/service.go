package artdrop

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"

	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

//go:embed cdc/setup_collection.cdc
var setupCollectionCDC string

//go:embed cdc/register_provider.cdc
var registerProviderCDC string

// Service implements the artdrop plugin business logic.
type Service struct {
	deps plugins.PluginDeps
}

// NewService creates a new artdrop service using the shared plugin dependencies.
func NewService(deps plugins.PluginDeps) *Service {
	return &Service{deps: deps}
}

// Transfer executes an ArtDrop protocol transfer of a certificate NFT.
func (s *Service) Transfer(ctx context.Context, sync bool, address string, req TransferRequest) (*jobs.Job, *transactions.Transaction, error) {
	if req.CertificateID == nil {
		return nil, nil, fmt.Errorf("field 'certificateId' is required")
	}

	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}

	to, err := flow_helpers.ValidateAddress(req.To, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}

	scriptPath := s.deps.Config.ScriptPathProtocolTransfer
	if scriptPath == "" {
		return nil, nil, fmt.Errorf("protocol transfer script path is empty")
	}

	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read protocol transfer script: %w", err)
	}

	args := []transactions.Argument{
		cadence.NewUInt64(*req.CertificateID),
		cadence.NewAddress(flow.HexToAddress(address)),
		cadence.NewAddress(flow.HexToAddress(to)),
	}

	return s.deps.Transactions.Create(ctx, sync, address, string(script), args, TxTypeTransfer)
}

// Setup prepares an account to use the artdrop contract suite.
func (s *Service) Setup(ctx context.Context, sync bool, address string) (*jobs.Job, *transactions.Transaction, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}

	if _, _, err := s.deps.Transactions.Create(ctx, true, address, setupCollectionCDC, nil, TxTypeSetup); err != nil {
		return nil, nil, fmt.Errorf("setup artdrop collection: %w", err)
	}

	job, tx, err := s.deps.Transactions.Create(ctx, sync, address, registerProviderCDC, nil, TxTypeSetup)
	if err != nil {
		return nil, nil, fmt.Errorf("register artdrop provider: %w", err)
	}

	return job, tx, nil
}

// CreateEscrow starts a new escrow between a buyer and a seller.
func (s *Service) CreateEscrow(ctx context.Context, sync bool, address string, req CreateEscrowRequest) (*jobs.Job, *transactions.Transaction, error) {
	return nil, nil, errors.New("artdrop: not implemented")
}

// ActivateChip validates a chip signature and settles the escrow.
func (s *Service) ActivateChip(ctx context.Context, sync bool, address string, escrowId uint64, req ActivateChipRequest) (*jobs.Job, *transactions.Transaction, error) {
	return nil, nil, errors.New("artdrop: not implemented")
}

// Release releases the escrowed funds to the seller.
func (s *Service) Release(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error) {
	return nil, nil, errors.New("artdrop: not implemented")
}

// Cancel cancels the escrow and returns the funds to the buyer.
func (s *Service) Cancel(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error) {
	return nil, nil, errors.New("artdrop: not implemented")
}

// Refund refunds the escrowed funds.
func (s *Service) Refund(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error) {
	return nil, nil, errors.New("artdrop: not implemented")
}

// ListCertificates returns the certificates owned by the given address.
func (s *Service) ListCertificates(ctx context.Context, address string) ([]CertificateInfo, error) {
	return nil, errors.New("artdrop: not implemented")
}

// GetEscrow returns a summary of the requested escrow.
func (s *Service) GetEscrow(ctx context.Context, escrowId uint64) (*EscrowSummary, error) {
	return nil, errors.New("artdrop: not implemented")
}
