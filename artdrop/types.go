package artdrop

import "github.com/flow-hydraulics/flow-wallet-api/transactions"

// Transaction types used by the artdrop plugin.
//
// TxTypeRelease, TxTypeCancel and TxTypeRefund were removed along with
// Service.Release/Cancel/Refund: the escrow-lifecycle redesign (2026-08)
// deleted the underlying EscrowModule functions (releaseEscrow, cancel,
// refund) entirely — release now happens automatically inside
// activateChipAndSettle (or via the OperationalAdmin-only releaseOnTimeout,
// which the wallet-api has no path to call), and buyer-initiated
// cancel/refund no longer exist on-chain at all.
const (
	TxTypeSetup             transactions.Type = "ArtdropSetup"
	TxTypeTransfer          transactions.Type = "ArtdropTransfer"
	TxTypeCreateEscrow      transactions.Type = "ArtdropCreateEscrow"
	TxTypeActivateChip      transactions.Type = "ArtdropActivateChip"
	TxTypeSetupArtistDirect transactions.Type = "ArtdropSetupArtistDirect"
	TxTypeCreateOriginal    transactions.Type = "ArtdropCreateOriginal"
	TxTypeCreateEdition     transactions.Type = "ArtdropCreateEdition"
)

// TransferRequest contains the parameters needed to transfer a certificate.
type TransferRequest struct {
	CertificateID *uint64 `json:"certificateId"`
	To            string  `json:"to"`
}

// CreateOriginalRequest contains the parameters needed for an artist to
// create a new Original. The artist identity itself is not part of the
// body — it comes from the {artistAddress} path segment and is bound to
// the real transaction signer via UserAuthorizer, never from client input.
type CreateOriginalRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Prices      map[string]float64 `json:"prices"`
}

// CreateEditionRequest contains the parameters needed for an artist to
// create a new Edition for one of their existing Originals. originalId
// comes from the path, not the body.
type CreateEditionRequest struct {
	ReprintLimit      uint64             `json:"reprint_limit"`
	Prices            map[string]float64 `json:"prices"`
	ProfitSplit       map[string]float64 `json:"profit_split"`
	RarityCurve       []uint64           `json:"rarity_curve"`
	MultiplierWeights map[string]float64 `json:"multiplier_weights"`
	RarityProfile     uint8              `json:"rarity_profile"`
}

// CreateEscrowRequest contains the parameters needed to create a new escrow.
//
// LogicOwner (the EscrowModule capability owner) and VaultIdentifier (the
// storage path escrow creation withdraws from) used to be client-supplied
// here. Both are now server-controlled — LogicOwner comes from
// Config.LogicOwner and VaultIdentifier is fixed to defaultVaultIdentifier
// — and were removed outright rather than kept-but-ignored: this plugin has
// no callers that predate the protocol's address migration to preserve
// compatibility with.
//
// ChipPubKey was removed the same way (chip-registry redesign, 2026-08-19):
// EscrowModule.createEscrow no longer takes a chip public key at all — it
// looks the chip's registered key up from ArtDropRegistry.ChipPublicKeyIndex
// by chipId. Provisioning that key is a separate, one-time-per-chip,
// OperationalAdmin-gated operation the wallet API cannot perform (see
// artdrop/cdc/create_escrow.cdc).
type CreateEscrowRequest struct {
	Buyer     string  `json:"buyer"`
	Seller    string  `json:"seller"`
	EditionId uint64  `json:"edition_id"`
	ChipId    string  `json:"chip_id"`
	UnlockAt  float64 `json:"unlock_at"`
	Nonce     uint64  `json:"nonce"`
	Amount    float64 `json:"amount"`
}

// ActivateChipRequest contains the parameters needed to activate a chip and
// settle an escrow.
//
// LogicOwner was removed — see CreateEscrowRequest. CertificateId and
// CertificateOwner were removed for the same reason as LogicOwner/
// VaultIdentifier, but with a sharper edge: the escrow-lifecycle redesign
// (2026-08) changed EscrowModule.activateChipAndSettle to derive both
// values from the escrow's own on-chain state specifically because a
// caller could previously pass arbitrary values here to steal a
// certificate. Keeping the fields around — even ignored — would leave a
// client-controlled input sitting on exactly the path that vulnerability
// used, inviting it to get wired back in later.
type ActivateChipRequest struct {
	EscrowId  uint64 `json:"escrow_id"`
	Challenge string `json:"challenge"`
	Signature []byte `json:"signature"`
}

// CertificateInfo represents a single certificate returned by the list endpoint.
type CertificateInfo struct {
	Id              uint64  `json:"id"`
	EditionId       uint64  `json:"edition_id"`
	Serial          uint64  `json:"serial"`
	IsRevealed      bool    `json:"is_revealed"`
	FinalMultiplier *string `json:"final_multiplier,omitempty"`
}

// EscrowSummary is the minimal representation of an escrow returned by the get endpoint.
type EscrowSummary struct {
	Id     uint64 `json:"id"`
	Status uint8  `json:"status"`
}

// OriginalSummary contains the metadata of an ArtDrop Original.
type OriginalSummary struct {
	Id                        uint64            `json:"id"`
	Artist                    string            `json:"artist"`
	Name                      string            `json:"name"`
	Prices                    map[string]string `json:"prices,omitempty"`
	CreatedAtBlock            uint64            `json:"createdAtBlock"`
	SchemaVersion             uint8             `json:"schemaVersion"`
	EditionCount              uint64            `json:"editionCount"`
	TotalMintedAcrossEditions uint64            `json:"totalMintedAcrossEditions"`
	DisplayName               *string           `json:"displayName"`
}

// EditionSummary contains the metadata of an ArtDrop Edition.
type EditionSummary struct {
	Id                uint64            `json:"id"`
	OriginalId        uint64            `json:"originalId"`
	Artist            string            `json:"artist"`
	ShuffleSeedBlock  uint64            `json:"shuffleSeedBlock"`
	ReprintLimit      uint64            `json:"reprintLimit"`
	MaxSupply         uint64            `json:"maxSupply"`
	Prices            map[string]string `json:"prices,omitempty"`
	ProfitSplit       map[string]string `json:"profitSplit,omitempty"`
	RarityCurve       []uint64          `json:"rarityCurve,omitempty"`
	MultiplierWeights map[string]string `json:"multiplierWeights,omitempty"`
	CreatedAtBlock    uint64            `json:"createdAtBlock"`
	SchemaVersion     uint8             `json:"schemaVersion"`
	State             string            `json:"state"`
	TotalMinted       uint64            `json:"totalMinted"`
	RarityProfile     uint8             `json:"rarityProfile"`
}

// CertificateDetail holds consolidated read-only data for a single certificate.
type CertificateDetail struct {
	Id              uint64  `json:"id"`
	BaseTier        *string `json:"baseTier,omitempty"`
	ChipPubKey      []byte  `json:"chipPubKey,omitempty"`
	IsRevealed      bool    `json:"isRevealed"`
	FinalMultiplier *string `json:"finalMultiplier,omitempty"`
	DisplayName     *string `json:"displayName,omitempty"`
}

// PlatformFeeResponse is the current platform fee in basis points.
type PlatformFeeResponse struct {
	Fee string `json:"fee"`
}

// MarketModeResponse is the current market mode name.
type MarketModeResponse struct {
	Mode string `json:"mode"`
}

// CollectionLengthResponse is the number of certificates in a collection.
type CollectionLengthResponse struct {
	Length int `json:"length"`
}
