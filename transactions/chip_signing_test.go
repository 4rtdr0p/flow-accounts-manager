package transactions

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
)

// The challenge shape the escrow-activation consumer will sign:
// "{nonce}:{buyer}:{escrowId}" (EscrowModule.buildChallenge).
const testChipChallenge = "42:0xabc:7"

// newTestSigner generates a fresh in-memory key of the given algorithm, built
// with a SHA3_256 hasher on purpose — that mirrors a real custodial account's
// default signer (DEFAULT_HASH_ALGO=SHA3_256), so the tests prove the chip
// primitive really switches to SHA2_256 rather than reusing the stored hasher.
func newTestSigner(t *testing.T, sigAlgo crypto.SignatureAlgorithm) (crypto.InMemorySigner, crypto.PrivateKey) {
	t.Helper()

	seed := make([]byte, crypto.MinSeedLength)
	if _, err := rand.Read(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	pk, err := crypto.GeneratePrivateKey(sigAlgo, seed)
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}

	signer, err := crypto.NewInMemorySigner(pk, crypto.SHA3_256)
	if err != nil {
		t.Fatalf("NewInMemorySigner: %v", err)
	}

	return signer, pk
}

// verifyWithGoCrypto independently checks a 64-byte raw r||s ECDSA-P256
// signature over payload using ONLY Go's standard library. It reproduces the
// exact on-chain check: SHA-256 of the raw UTF-8 payload, r||s split into two
// 32-byte big.Ints, ecdsa.Verify against the pubkey. No wallet-api code is used
// here, so a pass means the bytes are genuinely valid, not merely self-consistent.
func verifyWithGoCrypto(t *testing.T, pub crypto.PublicKey, payload, sig []byte) bool {
	t.Helper()

	if len(sig) != 64 {
		t.Fatalf("expected 64-byte signature for cross-check, got %d", len(sig))
	}

	// onflow/crypto encodes an ECDSA public key as raw X||Y (32+32), no 0x04 prefix.
	raw := pub.Encode()
	if len(raw) != 64 {
		t.Fatalf("expected 64-byte raw pubkey, got %d", len(raw))
	}

	goPub := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(raw[:32]),
		Y:     new(big.Int).SetBytes(raw[32:]),
	}

	digest := sha256.Sum256(payload)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])

	return ecdsa.Verify(goPub, digest[:], r, s)
}

// TestSignChipChallengeRaw_CrossCheck is the load-bearing correctness test: it
// proves the raw-sign primitive emits bytes that satisfy the exact on-chain
// verify (SHA2_256, empty domain tag, 64-byte raw r||s), cross-checked with Go's
// own crypto/ecdsa.
func TestSignChipChallengeRaw_CrossCheck(t *testing.T) {
	signer, pk := newTestSigner(t, crypto.ECDSA_P256)
	payload := []byte(testChipChallenge)

	sig, err := signChipChallengeRaw(signer, payload)
	if err != nil {
		t.Fatalf("signChipChallengeRaw: %v", err)
	}

	// (c) output is exactly 64 bytes (raw r||s, not DER).
	if len(sig) != 64 {
		t.Fatalf("expected 64-byte r||s signature, got %d bytes", len(sig))
	}

	// (a) a correct signature verifies under the independent Go check.
	if !verifyWithGoCrypto(t, pk.PublicKey(), payload, sig) {
		t.Fatal("correct signature failed independent crypto/ecdsa verification")
	}

	// (b) a signature over a different payload does NOT verify against the original.
	if verifyWithGoCrypto(t, pk.PublicKey(), []byte("99:0xdef:8"), sig) {
		t.Fatal("signature verified against a different payload; anti-replay broken")
	}
}

// TestSignChipChallengeRaw_UsesSHA2Not3 proves the primitive genuinely hashes
// with SHA2_256, not the stored SHA3_256: the same key + payload signed by the
// account's stored SHA3 signer must NOT verify under the SHA2 on-chain check.
func TestSignChipChallengeRaw_UsesSHA2Not3(t *testing.T) {
	signer, pk := newTestSigner(t, crypto.ECDSA_P256) // signer is SHA3_256
	payload := []byte(testChipChallenge)

	sha3Sig, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sha3 sign: %v", err)
	}
	if verifyWithGoCrypto(t, pk.PublicKey(), payload, sha3Sig) {
		t.Fatal("a SHA3_256 signature verified under the SHA2_256 check; the test can't tell the hashes apart")
	}

	chipSig, err := signChipChallengeRaw(signer, payload)
	if err != nil {
		t.Fatalf("signChipChallengeRaw: %v", err)
	}
	if !verifyWithGoCrypto(t, pk.PublicKey(), payload, chipSig) {
		t.Fatal("chip signature did not verify under SHA2_256; primitive is not hashing with SHA2_256")
	}
}

// TestSignChipChallengeRaw_RejectsWrongCurve guards the AWS-KMS-style case:
// secp256k1 keys are not P-256 and must be rejected up front.
func TestSignChipChallengeRaw_RejectsWrongCurve(t *testing.T) {
	signer, _ := newTestSigner(t, crypto.ECDSA_secp256k1)

	if _, err := signChipChallengeRaw(signer, []byte(testChipChallenge)); err == nil {
		t.Fatal("expected an error for a non-P256 key, got nil")
	}
}

// --- full-method test through a fake keys.Manager ---

// fakeKeyManager implements keys.Manager; only UserAuthorizer is meaningful.
type fakeKeyManager struct {
	authorizer keys.Authorizer
	err        error
}

func (f *fakeKeyManager) UserAuthorizer(ctx context.Context, address flow.Address) (keys.Authorizer, error) {
	if f.err != nil {
		return keys.Authorizer{}, f.err
	}
	return f.authorizer, nil
}

func (f *fakeKeyManager) Generate(ctx context.Context, keyIndex, weight int) (*flow.AccountKey, *keys.Private, error) {
	return nil, nil, errors.New("not implemented")
}
func (f *fakeKeyManager) GenerateDefault(ctx context.Context) (*flow.AccountKey, *keys.Private, error) {
	return nil, nil, errors.New("not implemented")
}
func (f *fakeKeyManager) Save(keys.Private) (keys.Storable, error) {
	return keys.Storable{}, errors.New("not implemented")
}
func (f *fakeKeyManager) Load(keys.Storable) (keys.Private, error) {
	return keys.Private{}, errors.New("not implemented")
}
func (f *fakeKeyManager) AdminAuthorizer(ctx context.Context) (keys.Authorizer, error) {
	return keys.Authorizer{}, errors.New("not implemented")
}
func (f *fakeKeyManager) CheckAdminProposalKeyCount(ctx context.Context) error {
	return errors.New("not implemented")
}
func (f *fakeKeyManager) InitAdminProposalKeys(ctx context.Context) (uint16, error) {
	return 0, errors.New("not implemented")
}
func (f *fakeKeyManager) AdminProposalKey(ctx context.Context) (keys.Authorizer, error) {
	return keys.Authorizer{}, errors.New("not implemented")
}

// a valid emulator address (the service account) for ValidateAddress.
const testChipAddress = "0xf8d6e0586b0a20c7"

func newChipTestService(km keys.Manager, guard CustodialSigningGuard) *ServiceImpl {
	return &ServiceImpl{
		km:                    km,
		cfg:                   &configs.Config{ChainID: flow.Emulator},
		custodialSigningGuard: guard,
	}
}

// TestSignChipChallenge_HappyPath exercises the full service method end to end
// (guard -> UserAuthorizer -> SHA2 re-sign) and cross-checks the output.
func TestSignChipChallenge_HappyPath(t *testing.T) {
	signer, pk := newTestSigner(t, crypto.ECDSA_P256)
	km := &fakeKeyManager{authorizer: keys.Authorizer{Signer: signer}}
	svc := newChipTestService(km, nil)

	payload := []byte(testChipChallenge)
	sig, err := svc.SignChipChallenge(context.Background(), testChipAddress, payload)
	if err != nil {
		t.Fatalf("SignChipChallenge: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("expected 64 bytes, got %d", len(sig))
	}
	if !verifyWithGoCrypto(t, pk.PublicKey(), payload, sig) {
		t.Fatal("service output failed independent verification")
	}
}

// TestSignChipChallenge_GuardRejects proves the CustodialSigningGuard gates the
// primitive: a guard that refuses the address blocks signing.
func TestSignChipChallenge_GuardRejects(t *testing.T) {
	signer, _ := newTestSigner(t, crypto.ECDSA_P256)
	km := &fakeKeyManager{authorizer: keys.Authorizer{Signer: signer}}
	guardErr := errors.New("not a custodial account")
	svc := newChipTestService(km, func(string) error { return guardErr })

	if _, err := svc.SignChipChallenge(context.Background(), testChipAddress, []byte(testChipChallenge)); !errors.Is(err, guardErr) {
		t.Fatalf("expected guard error, got %v", err)
	}
}

// TestSignChipChallenge_RejectsNonP256 proves a secp256k1 (AWS-KMS-style) key
// is rejected by the full method too.
func TestSignChipChallenge_RejectsNonP256(t *testing.T) {
	signer, _ := newTestSigner(t, crypto.ECDSA_secp256k1)
	km := &fakeKeyManager{authorizer: keys.Authorizer{Signer: signer}}
	svc := newChipTestService(km, nil)

	if _, err := svc.SignChipChallenge(context.Background(), testChipAddress, []byte(testChipChallenge)); err == nil {
		t.Fatal("expected an error for a non-P256 chip account, got nil")
	}
}
