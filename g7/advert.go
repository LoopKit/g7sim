package g7

import "strconv"

// SerialChecksum is the CRC-16/XMODEM of the serial's ASCII digits, which
// the advertisement carries so a scanner can match the package it scanned.
func SerialChecksum(serial uint64) uint16 {
	var crc uint16
	for _, b := range []byte(strconv.FormatUint(serial, 10)) {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
