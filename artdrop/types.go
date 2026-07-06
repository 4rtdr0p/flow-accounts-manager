package artdrop

import "github.com/flow-hydraulics/flow-wallet-api/transactions"

// Transaction types used by the artdrop plugin.
const (
	TxTypeSetup        transactions.Type = "ArtdropSetup"
	TxTypeCreateEscrow transactions.Type = "ArtdropCreateEscrow"
	TxTypeActivateChip transactions.Type = "ArtdropActivateChip"
	TxTypeRelease      transactions.Type = "ArtdropRelease"
	TxTypeCancel       transactions.Type = "ArtdropCancel"
	TxTypeRefund       transactions.Type = "ArtdropRefund"
)

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
