package artdrop

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/chips"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
)

// chipPublicKeyLength is the size, in bytes, of the raw (uncompressed,
// no 0x04 prefix) ECDSA P-256 public key EscrowModule.verifyChipSignature
// requires — see docs/CHIP-SIGNING-DESIGN.md §1.
const chipPublicKeyLength = 64

// ProvisionChip creates a custodial Flow account whose ECDSA P-256 key
// becomes chipId's on-chain identity, registers that key via the delegated
// ChipAdmin capability, and persists the chipId -> account mapping (design
// doc §2).
//
// Idempotent: if chipId is already provisioned, its existing mapping is
// returned as-is — no new account is created and nothing is re-submitted
// on-chain. There is deliberately no re-key path here yet; that would be a
// second, explicit operation (replaceChipPublicKey via isReplace=true), not
// something ProvisionChip does implicitly.
func (s *Service) ProvisionChip(ctx context.Context, chipId string) (*ChipInfo, error) {
	if chipId == "" {
		return nil, fmt.Errorf("field 'chip_id' is required")
	}
	if s.chipStore == nil {
		return nil, fmt.Errorf("artdrop: chip provisioning requires a database, none is configured")
	}

	if existing, err := s.chipStore.GetChip(ctx, chipId); err != nil {
		return nil, fmt.Errorf("check existing chip mapping: %w", err)
	} else if existing != nil {
		return chipInfoFromRow(existing), nil
	}

	if s.deps.Accounts == nil {
		return nil, fmt.Errorf("artdrop: chip provisioning requires the accounts service, none is configured")
	}

	// Create the chip's custodial account through the ordinary account-
	// creation path (same one every other custodial account in this
	// service goes through). This is deliberately NOT a special "chip
	// account" code path — see the guard below instead, which rejects
	// provisioning outright if the resulting key isn't what chip signing
	// requires, rather than trying to force a specific key type through a
	// keys.Manager that has no per-call algorithm override (it is driven
	// entirely by the deployment's DEFAULT_KEY_TYPE/DEFAULT_SIGN_ALGO).
	_, account, err := s.deps.Accounts.Create(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("create chip custodial account: %w", err)
	}
	if len(account.Keys) == 0 {
		return nil, fmt.Errorf("artdrop: chip account %s was created with no stored key", account.Address)
	}

	key := account.Keys[0]

	// CRITICAL guard (design doc §4b): chip-challenge signing
	// (transactions.SignChipChallenge) only works on a LOCAL, in-memory
	// ECDSA_P256 key — it rejects KMS-backed signers and any non-P256
	// curve outright. A deployment that has changed DEFAULT_KEY_TYPE or
	// DEFAULT_SIGN_ALGO away from "local"/"ECDSA_P256" would otherwise
	// silently provision a chip that can never be activated. Reject loudly
	// here instead.
	if key.Type != keys.AccountKeyTypeLocal {
		return nil, fmt.Errorf(
			"artdrop: chip signing requires a local key, but chip account %s was provisioned with key type %q — "+
				"chip accounts cannot be KMS-backed (see docs/CHIP-SIGNING-DESIGN.md §4b); check DEFAULT_KEY_TYPE",
			account.Address, key.Type,
		)
	}
	if key.SignAlgo != crypto.ECDSA_P256.String() {
		return nil, fmt.Errorf(
			"artdrop: chip signing requires ECDSA_P256, but chip account %s was provisioned with sign algo %q — "+
				"check DEFAULT_SIGN_ALGO (see docs/CHIP-SIGNING-DESIGN.md §4b)",
			account.Address, key.SignAlgo,
		)
	}

	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(key.PublicKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("decode chip account public key: %w", err)
	}
	if len(pubKeyBytes) != chipPublicKeyLength {
		return nil, fmt.Errorf(
			"artdrop: chip account %s public key is %d bytes, expected %d (raw P-256 x||y)",
			account.Address, len(pubKeyBytes), chipPublicKeyLength,
		)
	}

	adminAddress, err := flow_helpers.ValidateAddress(s.deps.Config.AdminAddress, s.deps.Config.ChainID)
	if err != nil {
		return nil, fmt.Errorf("validate admin address: %w", err)
	}

	// logicOwner here is the Core account (A3) that issued the ChipAdmin
	// capability this admin account claimed at deploy time — see
	// cdc/register_chip_via_delegated_cap.cdc's doc comment. This is
	// ArtDropCoreAddress, not Config.LogicOwner (the EscrowModule account
	// CreateEscrow/ActivateChip use).
	args := []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(s.cfg.ArtDropCoreAddress)),
		cadence.String(chipId),
		newUInt8Array(pubKeyBytes),
		cadence.NewBool(false), // isReplace: initial registration only
	}

	if _, _, err := s.deps.Transactions.Create(ctx, true, adminAddress, s.registerChipViaDelegatedCapCDC, args, TxTypeProvisionChip); err != nil {
		return nil, fmt.Errorf("register chip public key on-chain: %w", err)
	}

	row := chips.Chip{
		ChipID:         chipId,
		AccountAddress: account.Address,
		PublicKey:      hex.EncodeToString(pubKeyBytes),
		SigningMode:    chipSigningModeCustodial,
	}
	if err := s.chipStore.UpsertChip(ctx, row); err != nil {
		return nil, fmt.Errorf("persist chip mapping: %w", err)
	}

	return chipInfoFromRow(&row), nil
}

// GetChip returns chipId's provisioning mapping, or (nil, nil) if it has not
// been provisioned.
func (s *Service) GetChip(ctx context.Context, chipId string) (*ChipInfo, error) {
	if chipId == "" {
		return nil, fmt.Errorf("field 'chip_id' is required")
	}
	if s.chipStore == nil {
		return nil, fmt.Errorf("artdrop: chip lookups require a database, none is configured")
	}

	row, err := s.chipStore.GetChip(ctx, chipId)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return chipInfoFromRow(row), nil
}

func chipInfoFromRow(row *chips.Chip) *ChipInfo {
	return &ChipInfo{
		ChipId:            row.ChipID,
		AccountAddress:    row.AccountAddress,
		PublicKey:         row.PublicKey,
		SigningMode:       row.SigningMode,
		RegisteredAtBlock: row.RegisteredAtBlock,
	}
}
