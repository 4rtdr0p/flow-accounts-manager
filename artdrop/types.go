package artdrop

import "github.com/flow-hydraulics/flow-wallet-api/transactions"

// Transaction types used by the artdrop plugin.
const (
	TxTypeSetup        transactions.Type = "ArtdropSetup"
	TxTypeTransfer     transactions.Type = "ArtdropTransfer"
	TxTypeCreateEscrow transactions.Type = "ArtdropCreateEscrow"
	TxTypeActivateChip transactions.Type = "ArtdropActivateChip"
	TxTypeRelease      transactions.Type = "ArtdropRelease"
	TxTypeCancel       transactions.Type = "ArtdropCancel"
	TxTypeRefund       transactions.Type = "ArtdropRefund"
)

// TransferRequest contains the parameters needed to transfer a certificate.
type TransferRequest struct {
	CertificateID *uint64 `json:"certificateId"`
	To            string  `json:"to"`
}

// CreateEscrowRequest contains the parameters needed to create a new escrow.
type CreateEscrowRequest struct {
	LogicOwner      string  `json:"logic_owner"`
	Buyer           string  `json:"buyer"`
	Seller          string  `json:"seller"`
	EditionId       uint64  `json:"edition_id"`
	ChipId          string  `json:"chip_id"`
	ChipPubKey      []byte  `json:"chip_pub_key"`
	UnlockAt        float64 `json:"unlock_at"`
	Nonce           uint64  `json:"nonce"`
	Amount          float64 `json:"amount"`
	VaultIdentifier string  `json:"vault_identifier"`
}

// ActivateChipRequest contains the parameters needed to activate a chip and settle an escrow.
type ActivateChipRequest struct {
	LogicOwner       string `json:"logic_owner"`
	EscrowId         uint64 `json:"escrow_id"`
	Challenge        string `json:"challenge"`
	Signature        []byte `json:"signature"`
	CertificateId    uint64 `json:"certificate_id"`
	CertificateOwner string `json:"certificate_owner"`
}

// EscrowActionRequest is a reusable payload for release, cancel and refund actions.
type EscrowActionRequest struct {
	LogicOwner string `json:"logic_owner"`
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
