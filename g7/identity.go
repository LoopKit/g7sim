package g7

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

// A real G7 proves its identity during pairing: it streams an X.509 P-256
// certificate chain (leaf → Dexcom intermediate "DEX01PG2" → root "DEX00PG1")
// on the certificate characteristic, and signs the display's key-challenge
// nonce with its leaf key (ECDSA-P256 over SHA256(nonce), raw r‖s) on the
// 0C block. Captured from a real sensor 2026-10-10.
//
// The emulator mirrors this with its own fixed dummy CA, so a display can
// tell a simulated sensor from a genuine one: a genuine leaf chains to
// DEX01PG2, the emulator's leaf chains to this dummy CA. The dummy CA key is
// shipped in the open — it is a "this is simulated data" marker, not a secret.

// dummyCAPrivHex is the emulator's fixed CA signing key (public by design).
const dummyCAPrivHex = "7ad3246d555e63b4f7d4ff06bc037a64c108965d0c50036d8a56abfc6c2ea1c7"

// keyCPubHex is Dexcom's fixed display public key: a legitimate display signs
// the sensor's key challenge with the matching private key, so the emulator
// (like a real sensor) can check the display is an authorized Dexcom app.
// From G7SensorKit's G7DexcomCredentials / the community reverse engineering.
const keyCPubHex = "045118C35E9E41E7E0654FEE801C52A9C5DFC510EF09597D5CCA8461E4AF9C666714834F2BC903F16FABFC45755B0183F1A09745CDFFCB4E2F799E50BED9A6B58C"

var (
	dummyCAKey     *ecdsa.PrivateKey
	dummyCACert    *x509.Certificate
	dummyCACertDER []byte
	keyCPub        *ecdsa.PublicKey
)

func init() {
	scalar, err := hex.DecodeString(dummyCAPrivHex)
	if err != nil {
		panic(err)
	}
	dummyCAKey = ecdsaKeyFromScalar(scalar)
	dummyCACertDER, dummyCACert = makeDummyCACert()
	keyCPub = ecdsaPubFromUncompressed(mustHex(keyCPubHex))
}

func ecdsaKeyFromScalar(d []byte) *ecdsa.PrivateKey {
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(d)
	k.PublicKey.Curve = elliptic.P256()
	k.PublicKey.X, k.PublicKey.Y = elliptic.P256().ScalarBaseMult(d)
	return k
}

func ecdsaPubFromUncompressed(b []byte) *ecdsa.PublicKey {
	x, y := elliptic.Unmarshal(elliptic.P256(), b)
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func makeDummyCACert() ([]byte, *x509.Certificate) {
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "G7SIM-CA"},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2046, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &dummyCAKey.PublicKey, dummyCAKey)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return der, cert
}

// SensorIdentity is one emulated sensor's leaf keypair and certificate, signed
// by the dummy CA, mirroring a real G7's per-sensor certificate. Persisted in
// the state file so the sensor keeps a stable identity across restarts.
type SensorIdentity struct {
	LeafKeyHex  string `json:"leafKey"`
	LeafCertDER []byte `json:"leafCert"`
}

// NewSensorIdentity makes a fresh leaf keypair and a leaf certificate signed by
// the dummy CA.
func NewSensorIdentity(serial uint64) *SensorIdentity {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          new(big.Int).SetUint64(serial),
		Subject:               pkix.Name{CommonName: fmt.Sprintf("G7SIM-%d", serial)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(400 * 24 * time.Hour),
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, dummyCACert, &key.PublicKey, dummyCAKey)
	if err != nil {
		panic(err)
	}
	return &SensorIdentity{
		LeafKeyHex:  hex.EncodeToString(pad32(key.D)),
		LeafCertDER: der,
	}
}

func (id *SensorIdentity) leafKey() *ecdsa.PrivateKey {
	return ecdsaKeyFromScalar(mustHex(id.LeafKeyHex))
}

// CertChain is what the sensor streams in the certificate exchange: the CA at
// index 0 and the leaf at index 1, the order a real G7 uses.
func (id *SensorIdentity) CertChain() [][]byte {
	return [][]byte{dummyCACertDER, id.LeafCertDER}
}

// SignChallenge produces the 0C key-challenge block: an ECDSA-P256 signature
// over SHA256(displayNonce) with the leaf key, as raw r‖s (64 bytes), exactly
// as a real sensor does.
func (id *SensorIdentity) SignChallenge(displayNonce []byte) []byte {
	digest := sha256.Sum256(displayNonce)
	r, s, err := ecdsa.Sign(rand.Reader, id.leafKey(), digest[:])
	if err != nil {
		panic(err)
	}
	return append(pad32(r), pad32(s)...)
}

// VerifyDisplaySignature checks that the display proved possession of Dexcom's
// display key by signing SHA256(sensorChallenge) with keyC, the way a real
// sensor authenticates the display.
func VerifyDisplaySignature(sensorChallenge, sig []byte) bool {
	if len(sig) != 64 || len(sensorChallenge) == 0 {
		return false
	}
	digest := sha256.Sum256(sensorChallenge)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	return ecdsa.Verify(keyCPub, digest[:], r, s)
}
