package artdrop

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

// originalCreatedResult / editionCreatedResult are the JSON shapes exposed
// on the async job's Result field (see jobs.Job.Result / JSONResponse.Result)
// so the front end can read the newly created on-chain id straight from
// pollJob instead of falling back to base64-decoding events off the public
// Flow REST API with 10-minute retries.
//
// This is the artdrop plugin's implementation of
// transactions.ResultExtractorFunc, registered per Type in NewService. The
// core transactions package (transactions/jobs.go executeTransactionJob)
// calls it generically after a transaction completes — it has no knowledge
// of OriginalCreated/EditionCreated or any other ArtDrop event shape.
type originalCreatedResult struct {
	OriginalId uint64 `json:"originalId"`
}

type editionCreatedResult struct {
	EditionId  uint64 `json:"editionId"`
	OriginalId uint64 `json:"originalId"`
}

// escrowCreatedResult mirrors originalCreatedResult/editionCreatedResult for
// TxTypeCreateEscrow (issue #98): it lets the purchase flow (and any other
// caller of Service.CreateEscrow) resolve the on-chain escrowId from the
// async job's Result instead of a separate query, once the job completes.
type escrowCreatedResult struct {
	EscrowId      uint64 `json:"escrowId"`
	CertificateId uint64 `json:"certificateId"`
}

// certificateReEscrowedResult is the re-escrow (TxTypeReEscrow) counterpart of
// escrowCreatedResult. Its JSON shape is deliberately identical
// ({escrowId, certificateId}) so the front end reads the new escrowId from the
// re-escrow job's Result with the exact same code path it uses for
// create-escrow (front #494/#495) — a re-escrow emits CertificateReEscrowed,
// not EscrowCreated, so without a dedicated extractor the job Result came back
// empty and the re-escrow read-after-write was broken.
type certificateReEscrowedResult struct {
	EscrowId      uint64 `json:"escrowId"`
	CertificateId uint64 `json:"certificateId"`
}

// findArtDropEvent returns the Value of the first event among events whose
// qualified type ends in ".<name>" (e.g. "A.ec581a0282d99a1a.ArtDropCore.
// OriginalCreated"), regardless of which address ArtDropCore is currently
// deployed at.
func findArtDropEvent(events []flow.Event, name string) (cadence.Event, bool) {
	suffix := "." + name
	for _, e := range events {
		if strings.HasSuffix(e.Type, suffix) {
			return e.Value, true
		}
	}
	return cadence.Event{}, false
}

// extractOriginalCreatedResult implements transactions.ResultExtractorFunc
// for TxTypeCreateOriginal. See ArtDropCore.OriginalCreated (id: UInt64)
// in artdrop-protocol/contracts/core/ArtDropCore.cdc.
func extractOriginalCreatedResult(events []flow.Event) (string, error) {
	evt, ok := findArtDropEvent(events, "OriginalCreated")
	if !ok {
		return "", fmt.Errorf("artdrop: OriginalCreated event not found among %d event(s)", len(events))
	}

	fields := evt.FieldsMappedByName()
	id, ok := fields["id"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: OriginalCreated.id missing or wrong type (got %T)", fields["id"])
	}

	b, err := json.Marshal(originalCreatedResult{OriginalId: uint64(id)})
	if err != nil {
		return "", fmt.Errorf("artdrop: marshal OriginalCreated result: %w", err)
	}
	return string(b), nil
}

// extractEditionCreatedResult implements transactions.ResultExtractorFunc
// for TxTypeCreateEdition. See ArtDropCore.EditionCreated (id, originalId:
// UInt64) in artdrop-protocol/contracts/core/ArtDropCore.cdc.
func extractEditionCreatedResult(events []flow.Event) (string, error) {
	evt, ok := findArtDropEvent(events, "EditionCreated")
	if !ok {
		return "", fmt.Errorf("artdrop: EditionCreated event not found among %d event(s)", len(events))
	}

	fields := evt.FieldsMappedByName()
	id, ok := fields["id"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: EditionCreated.id missing or wrong type (got %T)", fields["id"])
	}
	originalId, ok := fields["originalId"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: EditionCreated.originalId missing or wrong type (got %T)", fields["originalId"])
	}

	b, err := json.Marshal(editionCreatedResult{
		EditionId:  uint64(id),
		OriginalId: uint64(originalId),
	})
	if err != nil {
		return "", fmt.Errorf("artdrop: marshal EditionCreated result: %w", err)
	}
	return string(b), nil
}

// extractEscrowCreatedResult implements transactions.ResultExtractorFunc for
// TxTypeCreateEscrow. See ArtDropCore.EscrowCreated (escrowId, certificateId:
// UInt64, among other fields) in artdrop-protocol/contracts/core/
// ArtDropCore.cdc — note the event field is "escrowId", not "id" (unlike
// OriginalCreated/EditionCreated above).
func extractEscrowCreatedResult(events []flow.Event) (string, error) {
	evt, ok := findArtDropEvent(events, "EscrowCreated")
	if !ok {
		return "", fmt.Errorf("artdrop: EscrowCreated event not found among %d event(s)", len(events))
	}

	fields := evt.FieldsMappedByName()
	escrowId, ok := fields["escrowId"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: EscrowCreated.escrowId missing or wrong type (got %T)", fields["escrowId"])
	}
	certificateId, ok := fields["certificateId"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: EscrowCreated.certificateId missing or wrong type (got %T)", fields["certificateId"])
	}

	b, err := json.Marshal(escrowCreatedResult{
		EscrowId:      uint64(escrowId),
		CertificateId: uint64(certificateId),
	})
	if err != nil {
		return "", fmt.Errorf("artdrop: marshal EscrowCreated result: %w", err)
	}
	return string(b), nil
}

// extractCertificateReEscrowedResult implements transactions.ResultExtractorFunc
// for TxTypeReEscrow. See ArtDropCore.CertificateReEscrowed (escrowId,
// certificateId: UInt64, among other fields) in artdrop-protocol/contracts/
// core/ArtDropCore.cdc. A re-escrow reuses an existing certificate and emits
// CertificateReEscrowed (NOT EscrowCreated), so the create-escrow extractor
// never matched and the job Result came back empty (broken re-escrow
// read-after-write). Field names match EscrowCreated: "escrowId" and
// "certificateId".
func extractCertificateReEscrowedResult(events []flow.Event) (string, error) {
	evt, ok := findArtDropEvent(events, "CertificateReEscrowed")
	if !ok {
		return "", fmt.Errorf("artdrop: CertificateReEscrowed event not found among %d event(s)", len(events))
	}

	fields := evt.FieldsMappedByName()
	escrowId, ok := fields["escrowId"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: CertificateReEscrowed.escrowId missing or wrong type (got %T)", fields["escrowId"])
	}
	certificateId, ok := fields["certificateId"].(cadence.UInt64)
	if !ok {
		return "", fmt.Errorf("artdrop: CertificateReEscrowed.certificateId missing or wrong type (got %T)", fields["certificateId"])
	}

	b, err := json.Marshal(certificateReEscrowedResult{
		EscrowId:      uint64(escrowId),
		CertificateId: uint64(certificateId),
	})
	if err != nil {
		return "", fmt.Errorf("artdrop: marshal CertificateReEscrowed result: %w", err)
	}
	return string(b), nil
}
