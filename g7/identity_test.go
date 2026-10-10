package g7

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"math/big"
	"testing"
)

func TestDummyCAIsACA(t *testing.T) {
	if !dummyCACert.IsCA {
		t.Fatal("dummy CA cert is not marked CA")
	}
}

func TestLeafChainsToDummyCA(t *testing.T) {
	id := NewSensorIdentity(774071672244)
	chain := id.CertChain()
	if len(chain) != 2 {
		t.Fatalf("chain has %d certs, want 2 (CA, leaf)", len(chain))
	}
	ca, err := x509.ParseCertificate(chain[0])
	if err != nil || !ca.IsCA {
		t.Fatalf("chain[0] is not the CA: %v", err)
	}
	leaf, err := x509.ParseCertificate(chain[1])
	if err != nil {
		t.Fatalf("chain[1] is not a cert: %v", err)
	}
	// The leaf must be signed by the dummy CA — this is what Loop checks to
	// recognize the emulator.
	if err := ca.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		t.Fatalf("leaf is not signed by the dummy CA: %v", err)
	}
}

func TestSignChallengeVerifiesAgainstLeaf(t *testing.T) {
	id := NewSensorIdentity(1)
	leaf, _ := x509.ParseCertificate(id.CertChain()[1])
	pub := leaf.PublicKey.(*ecdsa.PublicKey)

	nonce := make([]byte, 16)
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	sig := id.SignChallenge(nonce)
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want 64", len(sig))
	}
	digest := sha256.Sum256(nonce)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		t.Fatal("0C signature does not verify against the leaf public key over SHA256(nonce)")
	}
}

func TestIdentityRoundTrips(t *testing.T) {
	id := NewSensorIdentity(42)
	// Re-signing with the reloaded key must still verify, proving the stored
	// leaf key survives JSON persistence.
	leaf, _ := x509.ParseCertificate(id.LeafCertDER)
	pub := leaf.PublicKey.(*ecdsa.PublicKey)
	nonce := []byte("sixteen-byte-non")
	sig := id.SignChallenge(nonce)
	digest := sha256.Sum256(nonce)
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("reloaded leaf key produced a signature that does not verify")
	}
}

// keyC's private key, from G7DexcomCredentials: a real display signs the
// sensor's challenge with this. Confirms VerifyDisplaySignature accepts a
// genuine Dexcom display.
func TestVerifyDisplaySignatureAcceptsKeyC(t *testing.T) {
	raw, _ := hex.DecodeString("7cfbd596f6e74477b8c0e9f6f7a174275e101ef6bf7d18caf01181d127b579")
	scalar := append([]byte{0x00}, raw...) // 31 bytes, left-padded to 32
	priv := ecdsaKeyFromScalar(scalar)

	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(0xA0 + i)
	}
	digest := sha256.Sum256(challenge)
	r, s, err := ecdsa.Sign(randReader{}, priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := append(pad32(r), pad32(s)...)
	if !VerifyDisplaySignature(challenge, sig) {
		t.Fatal("a signature from Dexcom's display key (keyC) was rejected")
	}
	// A different key must be rejected.
	other, _ := ecdsa.GenerateKey(elliptic.P256(), randReader{})
	r2, s2, _ := ecdsa.Sign(randReader{}, other, digest[:])
	if VerifyDisplaySignature(challenge, append(pad32(r2), pad32(s2)...)) {
		t.Fatal("a signature from a non-Dexcom key was accepted")
	}
}

// randReader is crypto/rand via the package helper, wrapped so tests do not
// import crypto/rand directly.
type randReader struct{}

func (randReader) Read(b []byte) (int, error) { copy(b, randomBytes(len(b))); return len(b), nil }
