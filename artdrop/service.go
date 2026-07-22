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

//go:embed cdc/get_certificate_base_tier.cdc
var getCertificateBaseTierCDC string

//go:embed cdc/get_certificate_chip_pubkey.cdc
var getCertificateChipPubKeyCDC string

//go:embed cdc/get_certificate_is_revealed.cdc
var getCertificateIsRevealedCDC string

//go:embed cdc/get_certificate_final_multiplier.cdc
var getCertificateFinalMultiplierCDC string

//go:embed cdc/get_certificate_display_name.cdc
var getCertificateDisplayNameCDC string

//go:embed cdc/get_certificates.cdc
var getCertificatesCDC string

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

//go:embed cdc/get_original_extended_summary.cdc
var getOriginalExtendedSummaryCDC string

//go:embed cdc/get_edition_summary.cdc
var getEditionSummaryCDC string

//go:embed cdc/get_platform_fee.cdc
var getPlatformFeeCDC string

//go:embed cdc/get_market_mode_name.cdc
var getMarketModeNameCDC string

//go:embed cdc/is_artist.cdc
var isArtistCDC string

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

// ListCertificates returns the certificates owned by the given address,
// enriched with editionId, serial, and isRevealed metadata.
//
// Reads from the artdrop/cdc/get_certificates.cdc script (added in
// testnet-api-verification.md), which returns one dictionary per cert
// with keys: id, editionId, serial, isRevealed. Falls back to the older
// get_certificate_ids.cdc shape (bare [UInt64]) if the script returns a
// plain UInt64 array — that path leaves the rich fields at their
// zero values for backwards compatibility with older deploys.
func (s *Service) ListCertificates(ctx context.Context, address string) ([]CertificateInfo, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, err
	}

	args := []transactions.Argument{cadence.NewAddress(flow.HexToAddress(address))}

	val, err := s.deps.Transactions.ExecuteScript(ctx, getCertificatesCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificates script: %w", err)
	}

	arr, ok := val.(cadence.Array)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Array", val)
	}

	certs := make([]CertificateInfo, 0, len(arr.Values))
	for i, v := range arr.Values {
		dict, ok := v.(cadence.Dictionary)
		if !ok {
			return nil, fmt.Errorf("unexpected element type %T at index %d, expected cadence.Dictionary", v, i)
		}
		fields := map[string]cadence.Value{}
		for _, kv := range dict.Pairs {
			if key, ok := kv.Key.(cadence.String); ok {
				fields[string(key)] = kv.Value
			}
		}

		info := CertificateInfo{}

		if id, ok := fields["id"].(cadence.UInt64); ok {
			info.Id = uint64(id)
		} else {
			return nil, fmt.Errorf("missing or wrong-typed 'id' at index %d (got %T)", i, fields["id"])
		}
		if editionId, ok := fields["editionId"].(cadence.UInt64); ok {
			info.EditionId = uint64(editionId)
		}
		if serial, ok := fields["serial"].(cadence.UInt64); ok {
			info.Serial = uint64(serial)
		}
		if revealed, ok := fields["isRevealed"].(cadence.Bool); ok {
			info.IsRevealed = bool(revealed)
		}

		certs = append(certs, info)
	}

	return certs, nil
}

// GetCollectionLength returns the number of certificates owned by the given address.
func (s *Service) GetCollectionLength(ctx context.Context, address string) (*CollectionLengthResponse, error) {
	certs, err := s.ListCertificates(ctx, address)
	if err != nil {
		return nil, err
	}

	return &CollectionLengthResponse{Length: len(certs)}, nil
}

// GetEscrow returns a summary of the requested escrow.
func (s *Service) GetEscrow(ctx context.Context, logicOwner string, escrowId uint64) (*EscrowSummary, error) {
	_, err := flow_helpers.ValidateAddress(logicOwner, s.deps.Config.ChainID)
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

// GetCertificateDetail returns consolidated metadata for a single certificate.
func (s *Service) GetCertificateDetail(ctx context.Context, address string, certificateId uint64) (*CertificateDetail, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, err
	}

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(address)),
		cadence.NewUInt64(certificateId),
	}
	detail := &CertificateDetail{Id: certificateId}

	baseTier, err := s.deps.Transactions.ExecuteScript(ctx, getCertificateBaseTierCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_base_tier script: %w", err)
	}
	if detail.BaseTier, err = optionalUFix64String(baseTier); err != nil {
		return nil, fmt.Errorf("decode certificate base tier: %w", err)
	}

	chipPubKey, err := s.deps.Transactions.ExecuteScript(ctx, getCertificateChipPubKeyCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_chip_pubkey script: %w", err)
	}
	detail.ChipPubKey, err = uint8ArrayBytes(chipPubKey)
	if err != nil {
		return nil, fmt.Errorf("decode certificate chip public key: %w", err)
	}

	isRevealed, err := s.deps.Transactions.ExecuteScript(ctx, getCertificateIsRevealedCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_is_revealed script: %w", err)
	}
	revealed, ok := isRevealed.(cadence.Bool)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Bool", isRevealed)
	}
	detail.IsRevealed = bool(revealed)

	finalMultiplier, err := s.deps.Transactions.ExecuteScript(ctx, getCertificateFinalMultiplierCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_final_multiplier script: %w", err)
	}
	if detail.FinalMultiplier, err = optionalUFix64String(finalMultiplier); err != nil {
		return nil, fmt.Errorf("decode certificate final multiplier: %w", err)
	}

	displayName, err := s.deps.Transactions.ExecuteScript(ctx, getCertificateDisplayNameCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_display_name script: %w", err)
	}
	name, ok := displayName.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.String", displayName)
	}
	value := string(name)
	detail.DisplayName = &value

	return detail, nil
}

// GetOriginalSummary returns the complete W12 summary of an Original.
func (s *Service) GetOriginalSummary(ctx context.Context, originalId uint64) (*OriginalSummary, error) {
	args := []transactions.Argument{cadence.NewUInt64(originalId)}

	val, err := s.deps.Transactions.ExecuteScript(ctx, getOriginalExtendedSummaryCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_original_extended_summary script: %w", err)
	}

	fields, ok, err := optionalDictionaryFields(val)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	var summary OriginalSummary
	if id, ok := fields["id"].(cadence.UInt64); ok {
		summary.Id = uint64(id)
	}
	if name, ok := fields["name"].(cadence.String); ok {
		summary.Name = string(name)
	}
	if artist, ok := fields["artist"].(cadence.Address); ok {
		summary.Artist = flow_helpers.FormatAddress(flow.BytesToAddress(artist.Bytes()))
	}
	if prices, ok := fields["prices"].(cadence.Dictionary); ok {
		summary.Prices = ufix64Dictionary(prices)
	}
	if createdAt, ok := fields["createdAtBlock"].(cadence.UInt64); ok {
		summary.CreatedAtBlock = uint64(createdAt)
	}
	if schemaVersion, ok := fields["schemaVersion"].(cadence.UInt8); ok {
		summary.SchemaVersion = uint8(schemaVersion)
	}
	if editionCount, ok := fields["editionCount"].(cadence.UInt64); ok {
		summary.EditionCount = uint64(editionCount)
	}
	if totalMinted, ok := fields["totalMintedAcrossEditions"].(cadence.UInt64); ok {
		summary.TotalMintedAcrossEditions = uint64(totalMinted)
	}
	if displayName, ok := fields["displayName"].(cadence.Optional); ok && displayName.Value != nil {
		name, ok := displayName.Value.(cadence.String)
		if !ok {
			return nil, fmt.Errorf("unexpected displayName optional inner type %T, expected cadence.String", displayName.Value)
		}
		value := string(name)
		summary.DisplayName = &value
	}

	return &summary, nil
}

// GetEditionSummary returns a summary of an Edition.
//
// Uses the flat dictionary script instead of the contract's
// `ArtDropCore.EditionSummary` struct — the contract's `state` field is
// an enum (not a bare UInt8), so the previous handler's
// `fields["state"].(cadence.UInt8)` assertion silently failed and `state`
// was always returned as 0; also, the contract has no `maxSupply` field
// (the field is named `reprintLimit`). See `get_edition_summary.cdc`.
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

	dict, ok := opt.Value.(cadence.Dictionary)
	if !ok {
		return nil, fmt.Errorf("unexpected optional inner type %T, expected cadence.Dictionary", opt.Value)
	}

	fields := map[string]cadence.Value{}
	for _, kv := range dict.Pairs {
		if k, ok := kv.Key.(cadence.String); ok {
			fields[string(k)] = kv.Value
		}
	}

	var summary EditionSummary
	if id, ok := fields["id"].(cadence.UInt64); ok {
		summary.Id = uint64(id)
	}
	if originalId, ok := fields["originalId"].(cadence.UInt64); ok {
		summary.OriginalId = uint64(originalId)
	}
	if artist, ok := fields["artist"].(cadence.Address); ok {
		summary.Artist = flow_helpers.FormatAddress(flow.BytesToAddress(artist.Bytes()))
	}
	if seedBlock, ok := fields["shuffleSeedBlock"].(cadence.UInt64); ok {
		summary.ShuffleSeedBlock = uint64(seedBlock)
	}
	if reprintLimit, ok := fields["reprintLimit"].(cadence.UInt64); ok {
		summary.ReprintLimit = uint64(reprintLimit)
		summary.MaxSupply = summary.ReprintLimit
	}
	if prices, ok := fields["prices"].(cadence.Dictionary); ok {
		summary.Prices = ufix64Dictionary(prices)
	}
	if profitSplit, ok := fields["profitSplit"].(cadence.Dictionary); ok {
		summary.ProfitSplit = ufix64Dictionary(profitSplit)
	}
	if rarityCurve, ok := fields["rarityCurve"].(cadence.Array); ok {
		summary.RarityCurve = uint64Array(rarityCurve)
	}
	if multiplierWeights, ok := fields["multiplierWeights"].(cadence.Dictionary); ok {
		summary.MultiplierWeights = ufix64Dictionary(multiplierWeights)
	}
	if createdAt, ok := fields["createdAtBlock"].(cadence.UInt64); ok {
		summary.CreatedAtBlock = uint64(createdAt)
	}
	if schemaVersion, ok := fields["schemaVersion"].(cadence.UInt8); ok {
		summary.SchemaVersion = uint8(schemaVersion)
	}
	if state := fields["state"]; state != nil {
		summary.State = cadenceString(state)
	}
	if tm, ok := fields["totalMinted"].(cadence.UInt64); ok {
		summary.TotalMinted = uint64(tm)
	}
	if rarityProfile, ok := fields["rarityProfile"].(cadence.UInt8); ok {
		summary.RarityProfile = uint8(rarityProfile)
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

// IsArtist reports whether the given address has created at least one Original,
// as tracked by ArtDropRegistry.ArtistIndex.
func (s *Service) IsArtist(ctx context.Context, address string) (bool, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return false, err
	}

	args := []transactions.Argument{cadence.NewAddress(flow.HexToAddress(address))}

	val, err := s.deps.Transactions.ExecuteScript(ctx, isArtistCDC, args)
	if err != nil {
		return false, fmt.Errorf("execute is_artist script: %w", err)
	}

	result, ok := val.(cadence.Bool)
	if !ok {
		return false, fmt.Errorf("unexpected script result type %T, expected cadence.Bool", val)
	}

	return bool(result), nil
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

func optionalUFix64String(value cadence.Value) (*string, error) {
	opt, ok := value.(cadence.Optional)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Optional", value)
	}
	if opt.Value == nil {
		return nil, nil
	}
	ufix, ok := opt.Value.(cadence.UFix64)
	if !ok {
		return nil, fmt.Errorf("unexpected optional inner type %T, expected cadence.UFix64", opt.Value)
	}
	result := ufix.String()
	return &result, nil
}

func uint8ArrayBytes(value cadence.Value) ([]byte, error) {
	array, ok := value.(cadence.Array)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Array", value)
	}
	bytes := make([]byte, 0, len(array.Values))
	for i, v := range array.Values {
		b, ok := v.(cadence.UInt8)
		if !ok {
			return nil, fmt.Errorf("unexpected element type %T at index %d, expected cadence.UInt8", v, i)
		}
		bytes = append(bytes, byte(b))
	}
	return bytes, nil
}

func optionalDictionaryFields(value cadence.Value) (map[string]cadence.Value, bool, error) {
	opt, ok := value.(cadence.Optional)
	if !ok {
		return nil, false, fmt.Errorf("unexpected script result type %T, expected cadence.Optional", value)
	}
	if opt.Value == nil {
		return nil, false, nil
	}

	dict, ok := opt.Value.(cadence.Dictionary)
	if !ok {
		return nil, false, fmt.Errorf("unexpected optional inner type %T, expected cadence.Dictionary", opt.Value)
	}

	fields := map[string]cadence.Value{}
	for _, kv := range dict.Pairs {
		if k, ok := kv.Key.(cadence.String); ok {
			fields[string(k)] = kv.Value
		}
	}
	return fields, true, nil
}

func ufix64Dictionary(dict cadence.Dictionary) map[string]string {
	if len(dict.Pairs) == 0 {
		return nil
	}
	values := make(map[string]string, len(dict.Pairs))
	for _, pair := range dict.Pairs {
		key, ok := pair.Key.(cadence.String)
		if !ok {
			continue
		}
		value, ok := pair.Value.(cadence.UFix64)
		if !ok {
			continue
		}
		values[string(key)] = value.String()
	}
	return values
}

func uint64Array(array cadence.Array) []uint64 {
	if len(array.Values) == 0 {
		return nil
	}
	values := make([]uint64, 0, len(array.Values))
	for _, v := range array.Values {
		if value, ok := v.(cadence.UInt64); ok {
			values = append(values, uint64(value))
		}
	}
	return values
}

func cadenceString(value cadence.Value) string {
	if str, ok := value.(cadence.String); ok {
		return string(str)
	}
	return value.String()
}
