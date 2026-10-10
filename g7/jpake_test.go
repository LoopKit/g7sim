package g7

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"
)

// client mirrors G7SensorKit's G7JPAKE so the two sides can be run against
// each other.
type client struct {
	pin    *big.Int
	x1, x2 *big.Int
}

func (c *client) round3(s1, s2 PCert) []byte {
	base := generator().mul(c.x1).add(s1.Public).add(s2.Public)
	key := new(big.Int).Mod(new(big.Int).Mul(c.x2, c.pin), curve.Params().N)
	return makeCert(base, key, clientParty).Encode()
}

func (c *client) shared(s2, s3 PCert) []byte {
	key := new(big.Int).Mod(new(big.Int).Mul(c.x2, c.pin), curve.Params().N)
	k := s3.Public.add(s2.Public.mul(key).neg()).mul(c.x2)
	sum := sha256.Sum256(pad32(k.x))
	return sum[:16]
}

func TestJPAKEAgreesWithClient(t *testing.T) {
	for _, code := range []string{"1234", "0000", "9876"} {
		sensor := NewJPAKE(code)
		c := &client{pin: new(big.Int).SetBytes([]byte(code)), x1: randomScalar(), x2: randomScalar()}

		s1, _ := DecodePCert(sensor.Round1())
		if ok, err := sensor.SetClientRound(0, makeCert(generator(), c.x1, clientParty).Encode()); !ok || err != nil {
			t.Fatalf("client round 1 did not verify: %v", err)
		}
		s2, _ := DecodePCert(sensor.Round2())
		if ok, err := sensor.SetClientRound(1, makeCert(generator(), c.x2, clientParty).Encode()); !ok || err != nil {
			t.Fatalf("client round 2 did not verify: %v", err)
		}
		r3, err := sensor.Round3()
		if err != nil {
			t.Fatal(err)
		}
		s3, _ := DecodePCert(r3)

		base := generator().mul(c.x1).add(generator().mul(c.x2)).add(s1.Public)
		if !verifyCert(base, s3, sensorParty) {
			t.Error("sensor round 3 proof does not verify")
		}

		key, err := sensor.SharedKey(c.round3(s1, s2))
		if err != nil {
			t.Fatal(err)
		}
		if want := c.shared(s2, s3); !bytes.Equal(key, want) {
			t.Fatalf("code %s: sensor %x, client %x", code, key, want)
		}
	}
}

func TestWrongCodeDisagrees(t *testing.T) {
	sensor := NewJPAKE("1234")
	c := &client{pin: new(big.Int).SetBytes([]byte("1235")), x1: randomScalar(), x2: randomScalar()}
	s1, _ := DecodePCert(sensor.Round1())
	sensor.SetClientRound(0, makeCert(generator(), c.x1, clientParty).Encode())
	s2, _ := DecodePCert(sensor.Round2())
	sensor.SetClientRound(1, makeCert(generator(), c.x2, clientParty).Encode())
	r3, _ := sensor.Round3()
	s3, _ := DecodePCert(r3)
	key, _ := sensor.SharedKey(c.round3(s1, s2))
	if bytes.Equal(key, c.shared(s2, s3)) {
		t.Fatal("keys agree with the wrong code")
	}
}

func TestEncryptChallengeMatchesApp(t *testing.T) {
	// The known answers from G7SensorKit's G7AuthCryptoTests.
	cases := [][3]string{
		{"0011223344556677", "000102030405060708090a0b0c0d0e0f", "28b454bdf4d00600"},
		{"a1b2c3d4e5f60718", "ffeeddccbbaa99887766554433221100", "19b22de101061935"},
	}
	for _, c := range cases {
		challenge, _ := hex.DecodeString(c[0])
		key, _ := hex.DecodeString(c[1])
		if got := hex.EncodeToString(EncryptChallenge(challenge, key)); got != c[2] {
			t.Errorf("challenge %s: got %s, want %s", c[0], got, c[2])
		}
	}
}
