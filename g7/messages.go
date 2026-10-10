package g7

import "encoding/binary"

const (
	opAuthChallengeTx = 0x02
	opAuthChallengeRx = 0x03
	opAuthAnswer      = 0x04
	opAuthStatus      = 0x05
	opFinalize        = 0x06
	opBondRequest     = 0x07
	opBondConfirmed   = 0x08
	opDisconnect      = 0x09
	opKeyRound        = 0x0A
	opCertificate     = 0x0B
	opKeyChallenge    = 0x0C

	opCalibrationBounds = 0x32
	opCalibrate         = 0x34
	opTransmitterVer    = 0x4A
	opGlucose           = 0x4E
	opExtendedVersion   = 0x52
	opBackfill          = 0x59
)

func le16(b []byte, v uint16) []byte { return append(b, byte(v), byte(v>>8)) }
func le32(b []byte, v uint32) []byte {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	return append(b, t[:]...)
}

// trendByte encodes mg/dL/min in tenths, 0x7f for none.
func trendByte(r *Reading) byte {
	if r.Trend == nil {
		return 0x7f
	}
	t := *r.Trend * 10
	if t > 126 {
		t = 126
	} else if t < -127 {
		t = -127
	}
	return byte(int8(t))
}

func glucoseWord(r *Reading) uint16 {
	if r.Glucose == nil {
		return 0xffff
	}
	return *r.Glucose & 0xfff
}

// GlucoseMessage is the 4e reply: the latest reading and how old it is.
func GlucoseMessage(r *Reading, sensorNow uint32) []byte {
	b := []byte{opGlucose, 0x00}
	b = le32(b, sensorNow)
	b = le16(b, uint16(r.Index))
	b = append(b, 0x00, 0x01)
	b = le16(b, uint16(sensorNow-r.Time))
	b = le16(b, glucoseWord(r))
	b = append(b, r.State, trendByte(r))
	predicted := uint16(0xffff)
	if r.Glucose != nil {
		predicted = *r.Glucose
	}
	b = le16(b, predicted)
	return append(b, r.flags())
}

// BackfillRecord is one 9-byte record on the backfill characteristic.
func BackfillRecord(r *Reading) []byte {
	b := []byte{byte(r.Time), byte(r.Time >> 8), byte(r.Time >> 16), 0x00}
	b = le16(b, glucoseWord(r))
	return append(b, r.State, r.flags(), trendByte(r))
}

func (r *Reading) flags() byte {
	f := byte(0x0f)
	if r.DisplayOnly {
		f |= 0x10
	}
	return f
}

func ExtendedVersionMessage(sessionLength uint32, warmup uint16) []byte {
	b := []byte{opExtendedVersion, 0x00}
	b = le32(b, sessionLength)
	b = le16(b, warmup)
	b = append(b, 0x00, 0x01, 0x0a, 0x04, 0xff)
	return le16(b, 0x0011)
}

func TransmitterVersionMessage(serial uint64) []byte {
	b := []byte{opTransmitterVer, 0x00, 0x2c, 0xc0, 0x69, 0x48}
	b = le32(b, 0x0000379c)
	b = le32(b, 0x41414731)
	for i := 0; i < 6; i++ {
		b = append(b, byte(serial>>(8*i)))
	}
	return b
}

func CalibrationBoundsMessage(cal *Calibration, sessionNumber byte) []byte {
	b := []byte{opCalibrationBounds, 0x00, sessionNumber}
	b = le32(b, 0)
	var glucose uint16
	var at uint32
	status := byte(1)
	display := byte(0)
	if cal != nil {
		glucose, at, status, display = cal.Glucose, cal.SensorTime, 3, cal.DisplayType
	}
	b = le16(b, glucose)
	b = le32(b, at)
	b = append(b, status, 0x01, display)
	return le32(b, at)
}
