package transactions

import (
	"context"
	"fmt"

	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
)

// SignChipChallenge signs the raw UTF-8 bytes of payload with the custodial
// account at address, producing a signature in EXACTLY the form the on-chain
// EscrowModule.verifyChipSignature check accepts:
//
//	PublicKey(chipPubKey, ECDSA_P256).verify(
//	    signature:           <64-byte raw r||s>,
//	    signedData:          payload,          // raw UTF-8 bytes, NOT a hash
//	    domainSeparationTag: "",               // EMPTY
//	    hashAlgorithm:       SHA2_256,          // SHA-256, not SHA3
//	)
//
// This deliberately does NOT match either ready-made signing path:
//   - it is NOT flow.SignUserMessage (that prepends the "FLOW-V0.0-user" tag);
//   - it is NOT the account's default transaction signer (that hashes with
//     SHA3_256 and prepends the transaction domain tag).
//
// It loads the account's decrypted key via the same keys.Manager path as
// transaction signing (UserAuthorizer), then re-signs with a one-off SHA2_256
// signer over the same private key. The hash algorithm is a code-layer choice,
// not part of the keypair, so signing with SHA2_256 yields a signature that
// still verifies against the account's unchanged P-256 public key.
//
// Only LOCAL, in-memory ECDSA_P256 keys are supported. KMS-backed signers
// cannot be re-hashed with SHA2_256 (the hasher is fixed inside the HSM), and
// AWS-KMS keys are secp256k1 (wrong curve as well as wrong hash), so chip
// accounts must be provisioned as local P-256 keys; anything else is rejected
// with a clear error. Provisioning such accounts is a later phase.
//
// Signing custodial key material is a privileged operation, so it runs through
// the same CustodialSigningGuard that gates transaction building.
func (s *ServiceImpl) SignChipChallenge(ctx context.Context, address string, payload []byte) ([]byte, error) {
	if s.custodialSigningGuard != nil {
		if err := s.custodialSigningGuard(address); err != nil {
			return nil, err
		}
	}

	validAddr, err := flow_helpers.ValidateAddress(address, s.cfg.ChainID)
	if err != nil {
		return nil, err
	}

	// Load the account's decrypted key via the same path transaction signing
	// uses. We only need the Signer; the returned Key/Address are unused here.
	authorizer, err := s.km.UserAuthorizer(ctx, flow.HexToAddress(validAddr))
	if err != nil {
		return nil, fmt.Errorf("error while getting authorizer for chip account %s: %w", validAddr, err)
	}

	return signChipChallengeRaw(authorizer.Signer, payload)
}

// signChipChallengeRaw is the crypto core of SignChipChallenge, split out so it
// can be unit-tested against Go's own crypto/ecdsa without a full service.
//
// It requires a local in-memory ECDSA_P256 signer, re-signs the raw payload
// bytes with a SHA2_256 hasher and no domain-separation tag, and returns the
// 64-byte raw r||s signature. The onflow/crypto ECDSA signer already emits raw
// r||s (each of r and s left-padded to 32 bytes for P-256), NOT DER, which is
// exactly the shape the Cadence PublicKey.verify expects — so no DER->raw
// conversion is needed.
func signChipChallengeRaw(signer crypto.Signer, payload []byte) ([]byte, error) {
	inMem, ok := signer.(crypto.InMemorySigner)
	if !ok {
		return nil, fmt.Errorf(
			"chip signing requires a local in-memory key; got signer of type %T "+
				"(chip accounts must use local ECDSA_P256 keys, not KMS-backed keys)",
			signer,
		)
	}

	if inMem.PrivateKey.Algorithm() != crypto.ECDSA_P256 {
		return nil, fmt.Errorf(
			"chip signing requires ECDSA_P256, but chip account key is %s",
			inMem.PrivateKey.Algorithm(),
		)
	}

	// Re-sign with the SAME private key but pinned to SHA2_256, matching the
	// on-chain verify. This is the only deviation from the stored account
	// signer (which uses SHA3_256).
	sha2Signer, err := crypto.NewInMemorySigner(inMem.PrivateKey, crypto.SHA2_256)
	if err != nil {
		return nil, fmt.Errorf("error while building SHA2_256 chip signer: %w", err)
	}

	// Sign the raw UTF-8 payload directly. InMemorySigner.Sign performs no
	// domain-tag prepending of its own (the transaction/user-message tags are
	// applied by higher-level callers, which we deliberately bypass here), so
	// the pre-image is exactly SHA2_256(payload).
	sig, err := sha2Signer.Sign(payload)
	if err != nil {
		return nil, fmt.Errorf("error while signing chip challenge: %w", err)
	}

	return sig, nil
}
