package artdrop

import (
	"context"
	"errors"

	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
)

// Service implements the artdrop plugin business logic.
type Service struct {
	deps plugins.PluginDeps
}

// NewService creates a new artdrop service using the shared plugin dependencies.
func NewService(deps plugins.PluginDeps) *Service {
	return &Service{deps: deps}
}

// Setup prepares an account to use the artdrop contract suite.
func (s *Service) Setup(ctx context.Context, sync bool, address string) (*jobs.Job, *transactions.Transaction, error) {
	return nil, nil, errors.New("artdrop: not implemented")
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
