// ABOUTME: Derives PACT Ed25519 key IDs and signs canonical PACT body digests.
// ABOUTME: Keeps signing bound to raw SHA-256 digest bytes rather than digest text.
package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"

	"pact/internal/canonical"
)

// KeyID returns the PACT identifier for one raw Ed25519 public key.
func KeyID(public ed25519.PublicKey) (string, error) {
	if len(public) != ed25519.PublicKeySize {
		return "", fmt.Errorf("Ed25519 public keys must contain exactly %d bytes", ed25519.PublicKeySize)
	}
	return "ed25519:" + canonical.Digest(public), nil
}

// SignBody canonicalizes body, hashes it, and signs the raw SHA-256 digest.
func SignBody(body any, private ed25519.PrivateKey) (bodyDigest string, signature []byte, err error) {
	if len(private) != ed25519.PrivateKeySize {
		return "", nil, fmt.Errorf("Ed25519 private keys must contain exactly %d bytes", ed25519.PrivateKeySize)
	}
	encoded, err := canonical.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("canonicalize body: %w", err)
	}
	bodyDigest = canonical.Digest(encoded)
	digestBytes, err := digestBytes(bodyDigest)
	if err != nil {
		return "", nil, err
	}
	return bodyDigest, ed25519.Sign(private, digestBytes), nil
}

// VerifyBody checks the canonical body digest and its Ed25519 signature.
func VerifyBody(body any, bodyDigest string, public ed25519.PublicKey, signature []byte) error {
	if len(public) != ed25519.PublicKeySize {
		return fmt.Errorf("Ed25519 public keys must contain exactly %d bytes", ed25519.PublicKeySize)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("Ed25519 signatures must contain exactly %d bytes", ed25519.SignatureSize)
	}
	encoded, err := canonical.Marshal(body)
	if err != nil {
		return fmt.Errorf("canonicalize body: %w", err)
	}
	expectedDigest := canonical.Digest(encoded)
	if bodyDigest != expectedDigest {
		return fmt.Errorf("body digest mismatch: expected %s, got %q", expectedDigest, bodyDigest)
	}
	digest, err := digestBytes(bodyDigest)
	if err != nil {
		return err
	}
	if !ed25519.Verify(public, digest, signature) {
		return fmt.Errorf("Ed25519 signature verification failed")
	}
	return nil
}

func digestBytes(value string) ([]byte, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return nil, fmt.Errorf("invalid SHA-256 digest")
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(digest) != 32 {
		return nil, fmt.Errorf("invalid SHA-256 digest")
	}
	return digest, nil
}
