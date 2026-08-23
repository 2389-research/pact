// ABOUTME: Tests PACT Ed25519 key identity and canonical body signatures.
// ABOUTME: Uses only the public RFC 8032 test seed as deterministic fixture data.
package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

const rfc8032SeedHex = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

func TestKeyIDAndSignBodyMatchPACTVector(t *testing.T) {
	seed := mustDecodeHex(t, rfc8032SeedHex)
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)

	if got := hex.EncodeToString(public); got != "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a" {
		t.Fatalf("public key = %s", got)
	}
	keyID, err := KeyID(public)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}
	if want := "ed25519:sha256:21fe31dfa154a261626bf854046fd2271b7bed4b6abe45aa58877ef47f9721b9"; keyID != want {
		t.Errorf("KeyID() = %q, want %q", keyID, want)
	}

	body := map[string]any{"z": int64(0), "label": "e\u0301"}
	bodyDigest, signature, err := SignBody(body, private)
	if err != nil {
		t.Fatalf("SignBody() error = %v", err)
	}
	if want := "sha256:e9ee2fa7c420804dd1bde3bb2466ff8f5b900730b505a08e643e2fd267981bbd"; bodyDigest != want {
		t.Errorf("body digest = %q, want %q", bodyDigest, want)
	}
	if got := hex.EncodeToString(signature); got != "ebee3147e4d02203c3bd5d0f2936e1710252b216c74cc65b7a7fe9597b97e43e9619d3edb54c5d8590c621b02a2073cd7eab1a80e6e62efd99a3e824539bc506" {
		t.Errorf("signature = %s", got)
	}
	digestBytes := mustDecodeHex(t, strings.TrimPrefix(bodyDigest, "sha256:"))
	if !ed25519.Verify(public, digestBytes, signature) {
		t.Error("signature does not verify the raw digest bytes")
	}
	if ed25519.Verify(public, []byte(bodyDigest), signature) {
		t.Error("signature unexpectedly verifies the digest text")
	}
	if err := VerifyBody(body, bodyDigest, public, signature); err != nil {
		t.Errorf("VerifyBody() error = %v", err)
	}
}

func TestIdentityRejectsAlteredInputsAndInvalidLengths(t *testing.T) {
	seed := mustDecodeHex(t, rfc8032SeedHex)
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	body := map[string]any{"label": "PACT", "z": int64(0)}
	digest, signature, err := SignBody(body, private)
	if err != nil {
		t.Fatalf("SignBody() error = %v", err)
	}

	alteredSignature := append([]byte(nil), signature...)
	alteredSignature[0] ^= 1
	for name, input := range map[string]struct {
		body      any
		digest    string
		public    ed25519.PublicKey
		signature []byte
	}{
		"body":      {body: map[string]any{"label": "changed", "z": int64(0)}, digest: digest, public: public, signature: signature},
		"digest":    {body: body, digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", public: public, signature: signature},
		"key":       {body: body, digest: digest, public: ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)), signature: signature},
		"signature": {body: body, digest: digest, public: public, signature: alteredSignature},
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifyBody(input.body, input.digest, input.public, input.signature); err == nil {
				t.Fatal("VerifyBody() error = nil, want rejection")
			}
		})
	}

	if _, err := KeyID(ed25519.PublicKey{1}); err == nil {
		t.Error("KeyID() accepted short public key")
	}
	if _, _, err := SignBody(body, ed25519.PrivateKey{1}); err == nil {
		t.Error("SignBody() accepted short private key")
	}
	if err := VerifyBody(body, digest, ed25519.PublicKey{1}, signature); err == nil {
		t.Error("VerifyBody() accepted short public key")
	}
	if err := VerifyBody(body, digest, public, signature[:ed25519.SignatureSize-1]); err == nil {
		t.Error("VerifyBody() accepted short signature")
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return decoded
}
