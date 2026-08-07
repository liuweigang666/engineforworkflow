package model

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// J2.2 PPLI word codec (MIL-STD-6016 / STANAG 5516 word structure).
//
// Each J2.2 word is 75 bits: 70 data bits plus a 5-bit parity field. The
// codec packs each word into 10 bytes, bit 0 first (bit 0 is the MSB of
// byte 0; the final 5 bits of the 80-bit buffer are padding). The parity
// field is a 5-bit checksum over the 70 data bits (XOR of the fourteen
// 5-bit groups); validation of the exact MIL-STD-6016 parity tables against
// a certified reference implementation remains future work.
//
// The encoded payload carries the two standard words followed by an
// engine-context block holding the ten non-standard message fields:
//   [0:10]   J2.2I Initial Word (75 bits + padding)
//   [10:20]  J2.2E0 Extension Word (75 bits + padding)
//   [20:84]  engine-context block
//
// Context block layout (64 bytes, big-endian numerics, zero-padded ASCII):
//   [0:8]   TrackID
//   [8:16]  SourceJU
//   [16:20] TrackNum (uint32)
//   [20]    PlatformType (0 unknown, 1 air, 2 surface, 3 subsurface, 4 land)
//   [21:29] TimeOfTrack (unix seconds, int64)
//   [29:37] TimeOfMessage (unix seconds, int64)
//   [37:39] NPGNumber (uint16)
//   [39:43] MessageType
//   [43]    Valid (0/1)
//   [44:52] Identity
//   [52:64] reserved
const (
	J22WordBytes    = 10 // one 75-bit J2.2 word packed into 10 bytes
	J22ContextBytes = 64 // engine-context block
	J22PayloadSize  = 2*J22WordBytes + J22ContextBytes
)

// EncodeJ22 serializes the message into the J2.2 payload: the J2.2I and
// J2.2E0 words (75 bits each, parity included) followed by the
// engine-context block. The standard-encoded fields (latitude, longitude,
// altitude, speed, course) use the field bit positions and resolutions of
// the J2.2 word layout; the remaining ten fields are carried in the context
// block so the message-level round-trip preserves all 15 fields.
func (m *PPLIMessage) EncodeJ22() ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("encode j2.2: nil message")
	}
	p := make([]byte, J22PayloadSize)
	iw := p[0:J22WordBytes]
	e0 := p[J22WordBytes : 2*J22WordBytes]
	ctx := p[2*J22WordBytes:]

	encodeInitialWord(iw, m)
	encodeExtensionWord(e0, m)

	putASCII(ctx[0:8], m.TrackID)
	putASCII(ctx[8:16], m.SourceJU)
	binary.BigEndian.PutUint32(ctx[16:20], uint32(m.TrackNum))
	ctx[20] = platformByte(m.PlatformType)
	binary.BigEndian.PutUint64(ctx[21:29], uint64(m.TimeOfTrack.Unix()))
	binary.BigEndian.PutUint64(ctx[29:37], uint64(m.TimeOfMessage.Unix()))
	binary.BigEndian.PutUint16(ctx[37:39], uint16(m.NPGNumber))
	putASCII(ctx[39:43], m.MessageType)
	if m.Valid {
		ctx[43] = 1
	}
	putASCII(ctx[44:52], m.Identity)
	return p, nil
}

// DecodeJ22 parses a J2.2 payload back into a PPLIMessage, verifying the
// parity field of both words and the context block's platform byte.
func DecodeJ22(p []byte) (*PPLIMessage, error) {
	if len(p) != J22PayloadSize {
		return nil, fmt.Errorf("decode j2.2: invalid length %d (want %d)", len(p), J22PayloadSize)
	}
	iw := p[0:J22WordBytes]
	e0 := p[J22WordBytes : 2*J22WordBytes]
	ctx := p[2*J22WordBytes:]
	if err := verifyWordParity(iw); err != nil {
		return nil, fmt.Errorf("decode j2.2: initial word: %v", err)
	}
	if err := verifyWordParity(e0); err != nil {
		return nil, fmt.Errorf("decode j2.2: extension word: %v", err)
	}
	pt, err := platformFromByte(ctx[20])
	if err != nil {
		return nil, err
	}

	latCount := int64(getBits(e0, 2, 23))
	if latCount >= 1<<22 {
		latCount -= 1 << 23
	}
	lonCount := int64(getBits(e0, 25, 24))
	if lonCount >= 1<<23 {
		lonCount -= 1 << 24
	}

	return &PPLIMessage{
		TrackID:       getASCII(ctx[0:8]),
		SourceJU:      getASCII(ctx[8:16]),
		TrackNum:      int(binary.BigEndian.Uint32(ctx[16:20])),
		PlatformType:  pt,
		Latitude:      float64(latCount) * 0.0013 / 60,
		Longitude:     float64(lonCount) * 0.0013 / 60,
		Altitude:      float64(getBits(iw, 40, 13)) * 25,
		Speed:         float64(getBits(e0, 59, 11)),
		Course:        float64(getBits(e0, 50, 9)),
		Identity:      getASCII(ctx[44:52]),
		TimeOfTrack:   time.Unix(int64(binary.BigEndian.Uint64(ctx[21:29])), 0),
		TimeOfMessage: time.Unix(int64(binary.BigEndian.Uint64(ctx[29:37])), 0),
		NPGNumber:     int(binary.BigEndian.Uint16(ctx[37:39])),
		MessageType:   getASCII(ctx[39:43]),
		Valid:         ctx[43] == 1,
	}, nil
}

// encodeInitialWord fills the J2.2I word's 70 data bits from the message
// (WORD FORMAT 00, LABEL/SUBLABEL 2/2, MESSAGE LENGTH INDICATOR 1, the
// J2.2-specific fields, and representative indicator/quality values) and
// appends the 5-bit parity field.
func encodeInitialWord(w []byte, m *PPLIMessage) {
	setBits(w, 0, 2, 0) // WORD FORMAT = 00 (Initial Word)
	setBits(w, 2, 5, 2) // LABEL, J-SERIES = 2 (J2.2)
	setBits(w, 7, 3, 2) // SUBLABEL, J-SERIES = 2 (J2.2)
	setBits(w, 10, 3, 1) // MESSAGE LENGTH INDICATOR = 1 (IW+E0)
	abn := uint64(0)
	if m.PlatformType == PlatformAir {
		abn = 1
	}
	setBits(w, 13, 7, abn) // EX..SIM = 0, ABN = airborne indicator
	setBits(w, 20, 1, 0)   // FLIGHT LEADER INDICATOR
	setBits(w, 21, 1, 0)   // ACTIVE RELAY INDICATOR, WAN
	setBits(w, 22, 1, 0)   // RTT REPLY STATUS
	setBits(w, 23, 4, 1)   // NPS = 1 (ACTIVE, non-specific)
	setBits(w, 27, 4, 8)   // TIME QUALITY = 8 (representative)
	setBits(w, 31, 4, 15)  // GEODETIC POSITION QUALITY = 15 (representative)
	setBits(w, 35, 4, 0)   // STRENGTH = 0
	setBits(w, 39, 1, 0)   // BAILOUT INDICATOR
	alt := clampInt64(int64(math.Round(m.Altitude/25)), 0, 8191)
	setBits(w, 40, 13, uint64(alt)) // ALTITUDE, 25 FT
	setBits(w, 53, 7, 0)            // NET NUMBER, NONC2
	setBits(w, 60, 1, 0)            // NONC2 JU-TO-NONC2 JU NPG
	setBits(w, 61, 4, 15)           // ALTITUDE QUALITY = 15 (representative)
	setBits(w, 65, 5, 0)            // SPARE
	setParity(w)
}

// encodeExtensionWord fills the J2.2E0 word's 70 data bits (WORD FORMAT 10,
// latitude/longitude at 0.0013-minute resolution, course in degrees, speed
// in knots) and appends the 5-bit parity field.
func encodeExtensionWord(w []byte, m *PPLIMessage) {
	setBits(w, 0, 2, 2) // WORD FORMAT = 10 (Extension Word)
	latCount := signedToCount(m.Latitude*60/0.0013, 23)
	setBits(w, 2, 23, uint64(latCount))
	lonCount := signedToCount(m.Longitude*60/0.0013, 24)
	setBits(w, 25, 24, uint64(lonCount))
	setBits(w, 49, 1, 0) // SPARE
	course := clampInt64(int64(math.Round(m.Course)), 0, 359)
	setBits(w, 50, 9, uint64(course)) // COURSE, degrees (360 illegal)
	speed := clampInt64(int64(math.Round(m.Speed)), 0, 2046)
	setBits(w, 59, 11, uint64(speed)) // SPEED, knots (2047 no statement)
	setParity(w)
}

// signedToCount clamps v into the signed n-bit field range and converts a
// negative value into its two's-complement representation.
func signedToCount(v float64, n int) int64 {
	max := int64(1<<(n-1) - 1)
	min := -int64(1 << (n - 1))
	c := clampInt64(int64(math.Round(v)), min, max)
	if c < 0 {
		c += 1 << n
	}
	return c
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wordParity computes the 5-bit checksum over the 70 data bits of a word:
// the XOR of the fourteen 5-bit groups.
func wordParity(w []byte) byte {
	var p byte
	for g := 0; g < 14; g++ {
		p ^= byte(getBits(w, g*5, 5))
	}
	return p & 0x1F
}

func setParity(w []byte) {
	setBits(w, 70, 5, uint64(wordParity(w)))
}

func verifyWordParity(w []byte) error {
	if got, want := byte(getBits(w, 70, 5)), wordParity(w); got != want {
		return fmt.Errorf("parity mismatch (got %d, want %d)", got, want)
	}
	return nil
}

// dataBitsString renders the 70 data bits of a word as a string (bit 0
// first), used by the known-bit test that mirrors the paper's appendix.
func dataBitsString(w []byte) string {
	const digits = "01"
	out := make([]byte, 70)
	for i := 0; i < 70; i++ {
		out[i] = digits[getBits(w, i, 1)]
	}
	return string(out)
}

// setBits writes the low n bits of v into dst starting at bit start,
// MSB first (bit 0 = MSB of byte 0).
func setBits(dst []byte, start, n int, v uint64) {
	for i := 0; i < n; i++ {
		bit := start + i
		mask := byte(1 << (7 - bit%8))
		if v&(1<<uint(n-1-i)) != 0 {
			dst[bit/8] |= mask
		} else {
			dst[bit/8] &^= mask
		}
	}
}

// getBits reads n bits from src starting at bit start, MSB first.
func getBits(src []byte, start, n int) uint64 {
	var v uint64
	for i := 0; i < n; i++ {
		bit := start + i
		if src[bit/8]&(1<<(7-bit%8)) != 0 {
			v |= 1 << uint(n-1-i)
		}
	}
	return v
}

func putASCII(dst []byte, s string) {
	for i := 0; i < len(dst); i++ {
		if i < len(s) {
			dst[i] = s[i]
		} else {
			dst[i] = 0
		}
	}
}

func getASCII(b []byte) string {
	end := len(b)
	for i, c := range b {
		if c == 0 {
			end = i
			break
		}
	}
	return string(b[:end])
}

func platformByte(pt PlatformType) byte {
	switch pt {
	case PlatformAir:
		return 1
	case PlatformSurface:
		return 2
	case PlatformSubsurface:
		return 3
	case PlatformLand:
		return 4
	default:
		return 0
	}
}

func platformFromByte(b byte) (PlatformType, error) {
	switch b {
	case 0:
		return PlatformUnknown, nil
	case 1:
		return PlatformAir, nil
	case 2:
		return PlatformSurface, nil
	case 3:
		return PlatformSubsurface, nil
	case 4:
		return PlatformLand, nil
	default:
		return PlatformUnknown, fmt.Errorf("decode j2.2: invalid platform byte %d", b)
	}
}
