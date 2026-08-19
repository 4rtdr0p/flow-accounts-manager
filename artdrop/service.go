package artdrop

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"sort"
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

//go:embed cdc/get_certificate_detail.cdc
var getCertificateDetailCDC string

//go:embed cdc/get_certificates.cdc
var getCertificatesCDC string

//go:embed cdc/get_escrow_summary.cdc
var getEscrowSummaryCDC string

//go:embed cdc/create_escrow.cdc
var createEscrowCDC string

//go:embed cdc/activate_chip_and_settle.cdc
var activateChipAndSettleCDC string

//go:embed cdc/get_original_extended_summary.cdc
var getOriginalExtendedSummaryCDC string

//go:embed cdc/get_edition_summary.cdc
var getEditionSummaryCDC string

//go:embed cdc/get_edition_ids_by_original.cdc
var getEditionIDsByOriginalCDC string

//go:embed cdc/get_platform_fee.cdc
var getPlatformFeeCDC string

//go:embed cdc/get_market_mode_name.cdc
var getMarketModeNameCDC string

//go:embed cdc/is_artist.cdc
var isArtistCDC string

//go:embed cdc/onboard_artist.cdc
var onboardArtistCDC string

//go:embed cdc/setup_artist_direct_claim.cdc
var setupArtistDirectClaimCDC string

//go:embed cdc/create_original.cdc
var createOriginalCDC string

//go:embed cdc/create_edition.cdc
var createEditionCDC string

// defaultVaultIdentifier is the only storage path escrow creation is allowed
// to withdraw from: the standard FLOW vault, matching the /storage/
// flowTokenVault path release_escrow.cdc, cancel_escrow.cdc and
// refund_escrow.cdc already hardcode. CreateEscrowRequest.VaultIdentifier
// used to let a caller name an arbitrary storage path here; a legitimate
// caller never needs anything other than their FLOW vault, so the value is
// now fixed server-side instead of trusting client input.
const defaultVaultIdentifier = "flowTokenVault"

// Service implements the artdrop plugin business logic.
type Service struct {
	deps plugins.PluginDeps
	cfg  Config

	setupCollectionCDC            string
	registerProviderCDC           string
	getCertificateDetailCDC       string
	getCertificatesCDC            string
	getEscrowSummaryCDC           string
	createEscrowCDC               string
	activateChipAndSettleCDC      string
	getOriginalExtendedSummaryCDC string
	getEditionSummaryCDC          string
	getEditionIDsByOriginalCDC    string
	getPlatformFeeCDC             string
	getMarketModeNameCDC          string
	isArtistCDC                   string
	onboardArtistCDC              string
	setupArtistDirectClaimCDC     string
	createOriginalCDC             string
	createEditionCDC              string
}

// NewService creates a new artdrop service using the shared plugin
// dependencies and the artdrop contract-address config. It substitutes cfg's
// addresses into every embedded .cdc script's import lines once, up front,
// so per-request handling never has to think about it again. Returns an
// error rather than a partially-wired Service if cfg is missing or fails to
// validate — see Config.normalizeAndValidate.
func NewService(deps plugins.PluginDeps, cfg *Config) (*Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("artdrop: config is required")
	}

	validated := *cfg
	if err := validated.normalizeAndValidate(); err != nil {
		return nil, fmt.Errorf("artdrop: invalid config: %w", err)
	}

	sub := func(script string) string { return substituteAddresses(script, validated) }

	return &Service{
		deps: deps,
		cfg:  validated,

		setupCollectionCDC:            sub(setupCollectionCDC),
		registerProviderCDC:           sub(registerProviderCDC),
		getCertificateDetailCDC:       sub(getCertificateDetailCDC),
		getCertificatesCDC:            sub(getCertificatesCDC),
		getEscrowSummaryCDC:           sub(getEscrowSummaryCDC),
		createEscrowCDC:               sub(createEscrowCDC),
		activateChipAndSettleCDC:      sub(activateChipAndSettleCDC),
		getOriginalExtendedSummaryCDC: sub(getOriginalExtendedSummaryCDC),
		getEditionSummaryCDC:          sub(getEditionSummaryCDC),
		getEditionIDsByOriginalCDC:    sub(getEditionIDsByOriginalCDC),
		getPlatformFeeCDC:             sub(getPlatformFeeCDC),
		getMarketModeNameCDC:          sub(getMarketModeNameCDC),
		isArtistCDC:                   sub(isArtistCDC),
		onboardArtistCDC:              sub(onboardArtistCDC),
		setupArtistDirectClaimCDC:     sub(setupArtistDirectClaimCDC),
		createOriginalCDC:             sub(createOriginalCDC),
		createEditionCDC:              sub(createEditionCDC),
	}, nil
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

	return s.deps.Transactions.Create(ctx, sync, s.deps.Config.AdminAddress, substituteAddresses(string(script), s.cfg), args, TxTypeTransfer)
}

// Setup prepares an account to use the artdrop contract suite.
func (s *Service) Setup(ctx context.Context, sync bool, address string) (*jobs.Job, *transactions.Transaction, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}

	if _, _, err := s.deps.Transactions.Create(ctx, true, address, s.setupCollectionCDC, nil, TxTypeSetup); err != nil {
		return nil, nil, fmt.Errorf("setup artdrop collection: %w", err)
	}

	job, tx, err := s.deps.Transactions.Create(ctx, sync, address, s.registerProviderCDC, nil, TxTypeSetup)
	if err != nil {
		return nil, nil, fmt.Errorf("register artdrop provider: %w", err)
	}

	return job, tx, nil
}

// SetupArtistDirect onboards an artist and claims ArtistDirect in the artist account.
func (s *Service) SetupArtistDirect(ctx context.Context, sync bool, artistAddress string) (*jobs.Job, *transactions.Transaction, error) {
	artistAddress, err := flow_helpers.ValidateAddress(artistAddress, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	adminAddress, err := flow_helpers.ValidateAddress(s.deps.Config.AdminAddress, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, fmt.Errorf("validate admin address: %w", err)
	}

	if _, _, err := s.deps.Transactions.Create(
		ctx,
		true,
		adminAddress,
		s.onboardArtistCDC,
		[]transactions.Argument{cadence.NewAddress(flow.HexToAddress(artistAddress))},
		TxTypeSetupArtistDirect,
	); err != nil {
		return nil, nil, fmt.Errorf("onboard artist: %w", err)
	}

	// The claim's `provider` must be the account the inbox entry was
	// published from — ArtDropCore.issueArtistDirectCapability publishes to
	// `self.account.inbox`, i.e. the ArtDropCore contract account itself,
	// not the wallet-api's admin account. Those two only happened to
	// coincide in a single-account deployment; they've been separate
	// accounts since, so this claim has been unable to find its inbox
	// entry the entire time they diverged. adminAddress is still the
	// correct proposer/signer above (the account holding the
	// ArtistOnboarding capability) — this is a different address entirely.
	job, tx, err := s.deps.Transactions.Create(
		ctx,
		sync,
		artistAddress,
		s.setupArtistDirectClaimCDC,
		[]transactions.Argument{
			cadence.NewAddress(flow.HexToAddress(s.cfg.ArtDropCoreAddress)),
			cadence.String("artist-direct-" + artistAddress),
		},
		TxTypeSetupArtistDirect,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("claim artist direct capability: %w", err)
	}

	return job, tx, nil
}

// CreateOriginal creates an Original signed by the artist account.
func (s *Service) CreateOriginal(ctx context.Context, sync bool, artistAddress string, req CreateOriginalRequest) (*jobs.Job, *transactions.Transaction, error) {
	artistAddress, err := flow_helpers.ValidateAddress(artistAddress, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	if req.Name == "" {
		return nil, nil, fmt.Errorf("field 'name' is required")
	}
	if req.Description == "" {
		return nil, nil, fmt.Errorf("field 'description' is required")
	}
	if len(req.Prices) == 0 {
		return nil, nil, fmt.Errorf("field 'prices' is required")
	}

	prices, err := cadenceUFix64Dictionary(req.Prices)
	if err != nil {
		return nil, nil, fmt.Errorf("field 'prices': %w", err)
	}

	return s.deps.Transactions.Create(
		ctx,
		sync,
		artistAddress,
		s.createOriginalCDC,
		[]transactions.Argument{
			cadence.String(req.Name),
			cadence.String(req.Description),
			prices,
		},
		TxTypeCreateOriginal,
	)
}

// CreateEdition creates an Edition signed by the artist account.
func (s *Service) CreateEdition(ctx context.Context, sync bool, artistAddress string, originalID uint64, req CreateEditionRequest) (*jobs.Job, *transactions.Transaction, error) {
	artistAddress, err := flow_helpers.ValidateAddress(artistAddress, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	if len(req.Prices) == 0 {
		return nil, nil, fmt.Errorf("field 'prices' is required")
	}
	if len(req.ProfitSplit) == 0 {
		return nil, nil, fmt.Errorf("field 'profit_split' is required")
	}
	if len(req.RarityCurve) == 0 {
		return nil, nil, fmt.Errorf("field 'rarity_curve' is required")
	}
	if len(req.MultiplierWeights) == 0 {
		return nil, nil, fmt.Errorf("field 'multiplier_weights' is required")
	}

	prices, err := cadenceUFix64Dictionary(req.Prices)
	if err != nil {
		return nil, nil, fmt.Errorf("field 'prices': %w", err)
	}
	profitSplit, err := cadenceUFix64Dictionary(req.ProfitSplit)
	if err != nil {
		return nil, nil, fmt.Errorf("field 'profit_split': %w", err)
	}
	multiplierWeights, err := cadenceUFix64Dictionary(req.MultiplierWeights)
	if err != nil {
		return nil, nil, fmt.Errorf("field 'multiplier_weights': %w", err)
	}

	return s.deps.Transactions.Create(
		ctx,
		sync,
		artistAddress,
		s.createEditionCDC,
		[]transactions.Argument{
			cadence.NewUInt64(originalID),
			cadence.NewUInt64(req.ReprintLimit),
			prices,
			profitSplit,
			cadenceUInt64Array(req.RarityCurve),
			multiplierWeights,
			cadence.NewUInt8(req.RarityProfile),
		},
		TxTypeCreateEdition,
	)
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

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(s.cfg.LogicOwner)),
		cadence.NewAddress(flow.HexToAddress(buyer)),
		cadence.NewAddress(flow.HexToAddress(seller)),
		cadence.NewUInt64(req.EditionId),
		cadence.String(req.ChipId),
		unlockAt,
		cadence.NewUInt64(req.Nonce),
		amount,
		cadence.String(defaultVaultIdentifier),
	}

	return s.deps.Transactions.Create(ctx, sync, proposerAddress, s.createEscrowCDC, args, TxTypeCreateEscrow)
}

// ActivateChip validates a chip signature and settles the escrow.
//
// certificateId/certificateOwner are no longer request inputs: the
// escrow-lifecycle redesign (2026-08) changed EscrowModule.
// activateChipAndSettle to derive both from the escrow's own on-chain
// state, closing a certificate-theft vulnerability where a caller could
// pass arbitrary values here. See ActivateChipRequest.
func (s *Service) ActivateChip(ctx context.Context, sync bool, address string, escrowId uint64, req ActivateChipRequest) (*jobs.Job, *transactions.Transaction, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, nil, err
	}
	if req.Challenge == "" {
		return nil, nil, fmt.Errorf("field 'challenge' is required")
	}

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(s.cfg.LogicOwner)),
		cadence.NewUInt64(escrowId),
		cadence.String(req.Challenge),
		newUInt8Array(req.Signature),
	}

	return s.deps.Transactions.Create(ctx, sync, address, s.activateChipAndSettleCDC, args, TxTypeActivateChip)
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

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getCertificatesCDC, args)
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
func (s *Service) GetEscrow(ctx context.Context, escrowId uint64) (*EscrowSummary, error) {
	args := []transactions.Argument{
		cadence.NewUInt64(escrowId),
	}

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getEscrowSummaryCDC, args)
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
//
// Single-script implementation — the previous version ran 5 sequential
// ExecuteScript calls (baseTier, chipPubKey, isRevealed, finalMultiplier,
// displayName), each of which panicked individually on a missing
// collection or missing NFT id. That meant a single failure discarded
// every other successfully-read field and the HTTP response itself, and
// there was no clean 404 path. See `get_certificate_detail.cdc` and
// issue #53 for the consolidation rationale.
//
// The script returns `{String: AnyStruct}?` (nil when the account has no
// certificate collection, when the capability is the wrong type, or when
// the certificate id does not exist in the collection); the handler
// translates that nil into HTTP 404 — matching the existing
// GetOriginalSummary / GetEditionSummary / GetOriginalExtendedSummary
// pattern. This is a deliberate behavior change from the previous 5-script
// implementation (which returned 400 + a raw panic message for those
// three cases — see issue #49). Flagged for repo-owner review in the
// commit message that introduced this function.
func (s *Service) GetCertificateDetail(ctx context.Context, address string, certificateId uint64) (*CertificateDetail, error) {
	address, err := flow_helpers.ValidateAddress(address, s.deps.Config.ChainID)
	if err != nil {
		return nil, err
	}

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(address)),
		cadence.NewUInt64(certificateId),
	}

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getCertificateDetailCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_certificate_detail script: %w", err)
	}

	fields, ok, err := optionalDictionaryFields(val)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	detail := &CertificateDetail{Id: certificateId}

	if id, ok := fields["id"].(cadence.UInt64); ok {
		detail.Id = uint64(id)
	}

	if detail.BaseTier, err = optionalUFix64String(fields["baseTier"]); err != nil {
		return nil, fmt.Errorf("decode certificate base tier: %w", err)
	}

	if detail.ChipPubKey, err = uint8ArrayBytes(fields["chipPubKey"]); err != nil {
		return nil, fmt.Errorf("decode certificate chip public key: %w", err)
	}

	if revealed, ok := fields["isRevealed"].(cadence.Bool); ok {
		detail.IsRevealed = bool(revealed)
	} else {
		return nil, fmt.Errorf("unexpected script result type %T for isRevealed, expected cadence.Bool", fields["isRevealed"])
	}

	if detail.FinalMultiplier, err = optionalUFix64String(fields["finalMultiplier"]); err != nil {
		return nil, fmt.Errorf("decode certificate final multiplier: %w", err)
	}

	if dn, ok := fields["displayName"].(cadence.Optional); ok && dn.Value != nil {
		name, ok := dn.Value.(cadence.String)
		if !ok {
			return nil, fmt.Errorf("unexpected displayName optional inner type %T, expected cadence.String", dn.Value)
		}
		value := string(name)
		detail.DisplayName = &value
	}

	return detail, nil
}

// GetOriginalSummary returns the complete W12 summary of an Original.
func (s *Service) GetOriginalSummary(ctx context.Context, originalId uint64) (*OriginalSummary, error) {
	args := []transactions.Argument{cadence.NewUInt64(originalId)}

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getOriginalExtendedSummaryCDC, args)
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

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getEditionSummaryCDC, args)
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

// GetEditionIDsByOriginal returns the edition IDs belonging to an Original.
func (s *Service) GetEditionIDsByOriginal(ctx context.Context, originalId uint64) ([]uint64, error) {
	args := []transactions.Argument{cadence.NewUInt64(originalId)}

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getEditionIDsByOriginalCDC, args)
	if err != nil {
		return nil, fmt.Errorf("execute get_edition_ids_by_original script: %w", err)
	}

	arr, ok := val.(cadence.Array)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T, expected cadence.Array", val)
	}

	editionIDs := make([]uint64, 0, len(arr.Values))
	for _, v := range arr.Values {
		id, ok := v.(cadence.UInt64)
		if !ok {
			return nil, fmt.Errorf("unexpected edition id type %T, expected cadence.UInt64", v)
		}
		editionIDs = append(editionIDs, uint64(id))
	}

	return editionIDs, nil
}

// GetPlatformFee returns the current platform fee.
func (s *Service) GetPlatformFee(ctx context.Context) (*PlatformFeeResponse, error) {
	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getPlatformFeeCDC, nil)
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
	val, err := s.deps.Transactions.ExecuteScript(ctx, s.getMarketModeNameCDC, nil)
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

	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(address)),
		cadence.NewAddress(flow.HexToAddress(s.cfg.ArtDropRegistryAddress)),
	}

	val, err := s.deps.Transactions.ExecuteScript(ctx, s.isArtistCDC, args)
	if err != nil {
		return false, fmt.Errorf("execute is_artist script: %w", err)
	}

	result, ok := val.(cadence.Bool)
	if !ok {
		return false, fmt.Errorf("unexpected script result type %T, expected cadence.Bool", val)
	}

	return bool(result), nil
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

func cadenceUFix64Dictionary(values map[string]float64) (cadence.Dictionary, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]cadence.KeyValuePair, 0, len(keys))
	for _, key := range keys {
		value, err := newUFix64(values[key])
		if err != nil {
			return cadence.Dictionary{}, fmt.Errorf("%s: %w", key, err)
		}
		pairs = append(pairs, cadence.KeyValuePair{
			Key:   cadence.String(key),
			Value: value,
		})
	}
	return cadence.NewDictionary(pairs), nil
}

func cadenceUInt64Array(values []uint64) cadence.Array {
	cadenceValues := make([]cadence.Value, 0, len(values))
	for _, value := range values {
		cadenceValues = append(cadenceValues, cadence.NewUInt64(value))
	}
	return cadence.NewArray(cadenceValues)
}
