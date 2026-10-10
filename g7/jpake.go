package g7

import (
	"crypto/aes"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
)

// PCertLength is the size of one J-PAKE round on the wire: two P-256 points
// and a Schnorr proof scalar, each coordinate 32 bytes big-endian.
const PCertLength = 160

var curve = elliptic.P256()

// The party identifier the sensor signs its proofs with, and the one the
// client uses.
var (
	sensorParty = []byte{0x37, 0x56, 0x27, 0x67, 0x56, 0x27}
	clientParty = []byte("client")
)

type point struct{ x, y *big.Int }

func (p point) add(q point) point {
	x, y := curve.Add(p.x, p.y, q.x, q.y)
	return point{x, y}
}

func (p point) mul(k *big.Int) point {
	x, y := curve.ScalarMult(p.x, p.y, k.Bytes())
	return point{x, y}
}

func (p point) neg() point {
	return point{p.x, new(big.Int).Sub(curve.Params().P, p.y)}
}

func (p point) uncompressed() []byte {
	return elliptic.Marshal(curve, p.x, p.y)
}

func generator() point {
	return point{curve.Params().Gx, curve.Params().Gy}
}

// PCert is one round of the exchange: a public key and a Schnorr proof of
// knowledge of its private key.
type PCert struct {
	Public point
	Proof  point
	R      *big.Int
}

func (c PCert) Encode() []byte {
	out := make([]byte, 0, PCertLength)
	for _, v := range []*big.Int{c.Public.x, c.Public.y, c.Proof.x, c.Proof.y, c.R} {
		out = append(out, pad32(v)...)
	}
	return out
}

func DecodePCert(b []byte) (PCert, error) {
	if len(b) != PCertLength {
		return PCert{}, errors.New("pcert: wrong length")
	}
	n := func(i int) *big.Int { return new(big.Int).SetBytes(b[i*32 : i*32+32]) }
	c := PCert{Public: point{n(0), n(1)}, Proof: point{n(2), n(3)}, R: n(4)}
	if !curve.IsOnCurve(c.Public.x, c.Public.y) {
		return PCert{}, errors.New("pcert: public key not on curve")
	}
	return c, nil
}

func pad32(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}

func randomScalar() *big.Int {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	upper := new(big.Int).Sub(curve.Params().N, big.NewInt(2))
	k := new(big.Int).Mod(new(big.Int).SetBytes(b), upper)
	return k.Add(k, big.NewInt(1))
}

func transcriptHash(base, proof, public point, party []byte) *big.Int {
	h := sha256.New()
	for _, p := range [][]byte{base.uncompressed(), proof.uncompressed(), public.uncompressed(), party} {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		h.Write(l[:])
		h.Write(p)
	}
	return new(big.Int).Mod(new(big.Int).SetBytes(h.Sum(nil)), curve.Params().N)
}

func makeCert(base point, priv *big.Int, party []byte) PCert {
	n := curve.Params().N
	v := randomScalar()
	proof := base.mul(v)
	public := base.mul(priv)
	c := transcriptHash(base, proof, public, party)
	r := new(big.Int).Mod(new(big.Int).Sub(v, new(big.Int).Mul(c, priv)), n)
	return PCert{Public: public, Proof: proof, R: r}
}

func verifyCert(base point, cert PCert, party []byte) bool {
	c := transcriptHash(base, cert.Proof, cert.Public, party)
	got := base.mul(cert.R).add(cert.Public.mul(c))
	return got.x.Cmp(cert.Proof.x) == 0 && got.y.Cmp(cert.Proof.y) == 0
}

// JPAKE is the sensor's side of the pairing exchange over the 4-digit code.
type JPAKE struct {
	pin    *big.Int
	x3, x4 *big.Int

	clientRound1, clientRound2 *PCert
}

func NewJPAKE(code string) *JPAKE {
	return &JPAKE{
		pin: new(big.Int).SetBytes([]byte(code)),
		x3:  randomScalar(),
		x4:  randomScalar(),
	}
}

func (j *JPAKE) Round1() []byte { return makeCert(generator(), j.x3, sensorParty).Encode() }
func (j *JPAKE) Round2() []byte { return makeCert(generator(), j.x4, sensorParty).Encode() }

func (j *JPAKE) SetClientRound(round int, b []byte) (verified bool, err error) {
	c, err := DecodePCert(b)
	if err != nil {
		return false, err
	}
	switch round {
	case 0:
		j.clientRound1 = &c
	case 1:
		j.clientRound2 = &c
	default:
		return false, errors.New("jpake: bad round")
	}
	return verifyCert(generator(), c, clientParty), nil
}

// Round3 needs both of the client's rounds: its base is X1 + X2 + X3.
func (j *JPAKE) Round3() ([]byte, error) {
	if j.clientRound1 == nil || j.clientRound2 == nil {
		return nil, errors.New("jpake: round 3 before the client's rounds")
	}
	base := j.clientRound1.Public.add(j.clientRound2.Public).add(generator().mul(j.x3))
	return makeCert(base, j.blindedKey(), sensorParty).Encode(), nil
}

func (j *JPAKE) blindedKey() *big.Int {
	return new(big.Int).Mod(new(big.Int).Mul(j.x4, j.pin), curve.Params().N)
}

// SharedKey takes the client's round 3 and returns the 16-byte AES key both
// sides arrive at.
func (j *JPAKE) SharedKey(clientRound3 []byte) ([]byte, error) {
	if j.clientRound2 == nil {
		return nil, errors.New("jpake: shared key before the client's rounds")
	}
	a, err := DecodePCert(clientRound3)
	if err != nil {
		return nil, err
	}
	k := a.Public.add(j.clientRound2.Public.mul(j.blindedKey()).neg()).mul(j.x4)
	sum := sha256.Sum256(pad32(k.x))
	return sum[:16], nil
}

// EncryptChallenge is Dexcom's challenge transform: the 8 bytes doubled into
// one AES-128 block, ECB, first 8 bytes of the output.
func EncryptChallenge(challenge, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	in := append(append([]byte{}, challenge...), challenge...)
	out := make([]byte, 16)
	block.Encrypt(out, in)
	return out[:8]
}
