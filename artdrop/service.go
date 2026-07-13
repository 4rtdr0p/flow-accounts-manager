package artdrop

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

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

//go:embed cdc/get_certificate_ids.cdc
var getCertificateIDsCDC string

//go:embed cdc/get_escrow_summary.cdc
var getEscrowSummaryCDC string

//go:embed cdc/create_escrow.cdc
var createEscrowCDC string

//go:embed cdc/activate_chip_and_settle.cdc
var activateChipAndSettleCDC string

//go:embed cdc/release_escrow.cdc
var releaseEscrowCDC string

//go:embed cdc/cancel_escrow.cdc
var cancelEscrowCDC string

//go:embed cdc/refund_escrow.cdc
var refundEscrowCDC string

//go:embed cdc/get_original_summary.cdc
var getOriginalSummaryCDC string

//go:embed cdc/get_edition_summary.cdc
var getEditionSummaryCDC string

//go:embed cdc/get_platform_fee.cdc
var getPlatformFeeCDC string

//go:embed cdc/get_market_mode_name.cdc
var getMarketModeNameCDC string

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

	return s.deps.Transactions.Create(ctx, sync, s.deps.Config.AdminAddress, string(script), args, TxTypeTransfer)
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
	if _, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID); err != nil {
		return nil, nil, err
	}

	proposerAddress, err := flow_helpers.ValidateAddress(s.deps.Config.AdminAddress, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, fmt.Errorf("validate admin address: %w", err)
	}

	logicOwner, err := flow_helpers.ValidateAddress(req.LogicOwner, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	buyer, err := flow_helpers.ValidateAddress(req.Buyer, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	seller, err := flow_helpers.ValidateAddress(req.Seller, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	unlockAt, err := newUFix64(req.UnlockAt)
	if err != nil {
		return nil, nil, fmt.Errorf("field 'unlock_at': %w", err)
	}
	amount, err := newUFix64(req.Amount)
	if err != nil {
		return nil, nil, fmt.Errorf("field 'amount': %w", err)
	}
	if req.ChipId == "" {
		return nil, nil, fmt.Errorf("field 'chip_id' is required")
	}
	if req.VaultIdentifier == "" {
		return nil, nil, fmt.Errorf("field 'vault_identifier' is required")
	}

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(logicOwner)),
		cadence.NewAddress(flow.HexToAddress(buyer)),
		cadence.NewAddress(flow.HexToAddress(seller)),
		cadence.NewUInt64(req.EditionId),
		cadence.String(req.ChipId),
		newUInt8Array(req.ChipPubKey),
		unlockAt,
		cadence.NewUInt64(req.Nonce),
		amount,
		cadence.String(req.VaultIdentifier),
	}

	return s.deps.Transactions.Create(ctx, sync, proposerAddress, createEscrowCDC, args, TxTypeCreateEscrow)
}

// ActivateChip validates a chip signature and settles the escrow.
func (s *Service) ActivateChip(ctx context.Context, sync bool, address string, escrowId uint64, req ActivateChipRequest) (*jobs.Job, *transactions.Transaction, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	logicOwner, err := flow_helpers.ValidateAddress(req.LogicOwner, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	certificateOwner, err := flow_helpers.ValidateAddress(req.CertificateOwner, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	if req.Challenge == "" {
		return nil, nil, fmt.Errorf("field 'challenge' is required")
	}

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(logicOwner)),
		cadence.NewUInt64(escrowId),
		cadence.String(req.Challenge),
		newUInt8Array(req.Signature),
		cadence.NewUInt64(req.CertificateId),
		cadence.NewAddress(flow.HexToAddress(certificateOwner)),
	}

	return s.deps.Transactions.Create(ctx, sync, address, activateChipAndSettleCDC, args, TxTypeActivateChip)
}

// Release releases the escrowed funds to the seller.
func (s *Service) Release(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error) {
	return s.escrowAction(ctx, sync, address, escrowId, req, releaseEscrowCDC, TxTypeRelease)
}

// Cancel cancels the escrow and returns the funds to the buyer.
func (s *Service) Cancel(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error) {
	return s.escrowAction(ctx, sync, address, escrowId, req, cancelEscrowCDC, TxTypeCancel)
}

// Refund refunds the escrowed funds.
func (s *Service) Refund(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error) {
	return s.escrowAction(ctx, sync, address, escrowId, req, refundEscrowCDC, TxTypeRefund)
}

// ListCertificates returns the certificates owned by the given address.
func (s *Service) ListCertificates(ctx context.Context, address string) ([]CertificateInfo, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, err
	}

	args := []transactions.Argument{cadence.NewAddress(flow.HexToAddress(address))}

	val, err := s.deps.Transactions.ExecuteScript(ctx, getCertificateIDsCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_ids script: %w", err)
	}

	ids, ok := val.(cadence.Array)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Array", val)
	}

	certs := make([]CertificateInfo, len(ids.Values))
	for i, v := range ids.Values {
		id, ok := v.(cadence.UInt64)
		if !ok {
			return nil, fmt.Errorf("unexpected element type %T at index %d, expected cadence.UInt64", v, i)
		}
		certs[i] = CertificateInfo{Id: uint64(id)}
	}

	return certs, nil
}

// GetEscrow returns a summary of the requested escrow.
func (s *Service) GetEscrow(ctx context.Context, logicOwner string, escrowId uint64) (*EscrowSummary, error) {
	logicOwner, err := flow_helpers.ValidateAddress(logicOwner, s.deps.Config.ChainID)
	if err != nil {
		return nil, err
	}

	args := []transactions.Argument{
		cadence.NewUInt64(escrowId),
	}

	val, err := s.deps.Transactions.ExecuteScript(ctx, getEscrowSummaryCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_escrow_summary script: %w", err)
	}

	status, ok := val.(cadence.UInt8)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.UInt8", val)
	}

	return &EscrowSummary{
		Id:     escrowId,
		Status: uint8(status),
	}, nil
}

// GetOriginalSummary returns a summary of an Original.
func (s *Service) GetOriginalSummary(ctx context.Context, originalId uint64) (*OriginalSummary, error) {
	args := []transactions.Argument{cadence.NewUInt64(originalId)}

	val, err := s.deps.Transactions.ExecuteScript(ctx, getOriginalSummaryCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_original_summary script: %w", err)
	}

	opt, ok := val.(cadence.Optional)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Optional", val)
	}
	if opt.Value == nil {
		return nil, nil
	}

	str, ok := opt.Value.(cadence.Struct)
	if !ok {
		return nil, fmt.Errorf("unexpected optional inner type %T, expected cadence.Struct", opt.Value)
	}

	fields := str.FieldsMappedByName()
	var summary OriginalSummary
	if id, ok := fields["id"].(cadence.UInt64); ok {
		summary.Id = uint64(id)
	}
	if name, ok := fields["name"].(cadence.String); ok {
		summary.Name = string(name)
	}
	if artist, ok := fields["artistName"].(cadence.String); ok {
		summary.ArtistName = string(artist)
	}
	if editionIDs, ok := fields["editionIDs"].(cadence.Array); ok {
		for _, v := range editionIDs.Values {
			if id, ok := v.(cadence.UInt64); ok {
				summary.EditionIds = append(summary.EditionIds, uint64(id))
			}
		}
	}

	return &summary, nil
}

// GetEditionSummary returns a summary of an Edition.
func (s *Service) GetEditionSummary(ctx context.Context, editionId uint64) (*EditionSummary, error) {
	args := []transactions.Argument{cadence.NewUInt64(editionId)}

	val, err := s.deps.Transactions.ExecuteScript(ctx, getEditionSummaryCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_edition_summary script: %w", err)
	}

	opt, ok := val.(cadence.Optional)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Optional", val)
	}
	if opt.Value == nil {
		return nil, nil
	}

	str, ok := opt.Value.(cadence.Struct)
	if !ok {
		return nil, fmt.Errorf("unexpected optional inner type %T, expected cadence.Struct", opt.Value)
	}

	fields := str.FieldsMappedByName()
	var summary EditionSummary
	if id, ok := fields["id"].(cadence.UInt64); ok {
		summary.Id = uint64(id)
	}
	if state, ok := fields["state"].(cadence.UInt8); ok {
		summary.State = uint8(state)
	}
	if tm, ok := fields["totalMinted"].(cadence.UInt64); ok {
		summary.TotalMinted = uint64(tm)
	}
	if ms, ok := fields["maxSupply"].(cadence.UInt64); ok {
		summary.MaxSupply = uint64(ms)
	}

	return &summary, nil
}

// GetPlatformFee returns the current platform fee.
func (s *Service) GetPlatformFee(ctx context.Context) (*PlatformFeeResponse, error) {
	val, err := s.deps.Transactions.ExecuteScript(ctx, getPlatformFeeCDC, nil)
	if err != nil {
		return nil, fmt.Errorf("execute get_platform_fee script: %w", err)
	}

	fee, ok := val.(cadence.UFix64)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.UFix64", val)
	}

	return &PlatformFeeResponse{Fee: fee.String()}, nil
}

// GetMarketMode returns the current market mode name.
func (s *Service) GetMarketMode(ctx context.Context) (*MarketModeResponse, error) {
	val, err := s.deps.Transactions.ExecuteScript(ctx, getMarketModeNameCDC, nil)
	if err != nil {
		return nil, fmt.Errorf("execute get_market_mode_name script: %w", err)
	}

	mode, ok := val.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.String", val)
	}

	return &MarketModeResponse{Mode: string(mode)}, nil
}

func (s *Service) escrowAction(ctx context.Context, sync bool, address string, escrowId uint64, req EscrowActionRequest, code string, txType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	logicOwner, err := flow_helpers.ValidateAddress(req.LogicOwner, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(logicOwner)),
		cadence.NewUInt64(escrowId),
	}

	return s.deps.Transactions.Create(ctx, sync, address, code, args, txType)
}

func newUInt8Array(bytes []byte) cadence.Array {
	values := make([]cadence.Value, 0, len(bytes))
	for _, b := range bytes {
		values = append(values, cadence.NewUInt8(b))
	}
	return cadence.NewArray(values)
}

func newUFix64(value float64) (cadence.UFix64, error) {
	if value < 0 {
		return 0, fmt.Errorf("must be non-negative")
	}
	formatted := strconv.FormatFloat(value, 'f', 8, 64)
	formatted = strings.TrimRight(formatted, "0")
	if strings.HasSuffix(formatted, ".") {
		formatted += "0"
	}
	return cadence.NewUFix64(formatted)
}
