package model

import (
	"math"
	"strings"
	"testing"
	"time"
)

func fullMessage() *PPLIMessage {
	return &PPLIMessage{
		TrackID:       "TRK001",
		SourceJU:      "JU001",
		TrackNum:      1001,
		PlatformType:  PlatformAir,
		Latitude:      35.6812,
		Longitude:     139.7671,
		Altitude:      10000,
		Speed:         300,
		Course:        90,
		Identity:      "FRIEND",
		TimeOfTrack:   time.Unix(1700000000, 0),
		TimeOfMessage: time.Unix(1700000010, 0),
		NPGNumber:     6,
		MessageType:   "J2.2",
		Valid:         true,
	}
}

// E13: J2.2 word round-trip (IW+E0) preserves all 15 fields. The five
// standard-encoded fields (latitude, longitude, altitude, speed, course)
// are preserved within the J2.2 field quantization; the ten engine-context
// fields are preserved exactly.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	orig := fullMessage()
	p, err := orig.EncodeJ22()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(p) != J22PayloadSize {
		t.Fatalf("payload size = %d, want %d", len(p), J22PayloadSize)
	}
	got, err := DecodeJ22(p)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkRoundTrip(t, orig, got)
}

// E14: boundary values survive the J2.2 round-trip (latitude +/-90,
// longitude +/-180, altitude 0/50,000 ft, course 359.5, speed 0).
func TestRoundTripBoundaryValues(t *testing.T) {
	for _, m := range []*PPLIMessage{
		{Latitude: 90, Longitude: 180, Altitude: 50000, Speed: 0, Course: 359.5},
		{Latitude: -90, Longitude: -180, Altitude: 0, Speed: 2046, Course: 0},
		{Latitude: 0, Longitude: 0, Altitude: 25, Speed: 1, Course: 359},
	} {
		base := fullMessage()
		base.Latitude, base.Longitude = m.Latitude, m.Longitude
		base.Altitude, base.Speed, base.Course = m.Altitude, m.Speed, m.Course
		p, err := base.EncodeJ22()
		if err != nil {
			t.Fatalf("encode (%v): %v", m, err)
		}
		got, err := DecodeJ22(p)
		if err != nil {
			t.Fatalf("decode (%v): %v", m, err)
		}
		checkRoundTrip(t, base, got)
	}
}

// Known-bit check: the running-example message (TRK001) encodes to the
// exact 70-bit J2.2I and J2.2E0 data words listed in the paper's appendix
// (J2.2 PPLI Word Layout), keeping the code and the paper consistent.
func TestEncodeKnownWordBits(t *testing.T) {
	m := fullMessage() // 35.6812N / 139.7671E, 10,000 ft, 300 kt, 090
	p, err := m.EncodeJ22()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	iw := p[0:J22WordBytes]
	e0 := p[J22WordBytes : 2*J22WordBytes]
	wantIW := "0000010010001000000100000011000111100000000011001000000000000111100000"
	wantE0 := "1000110010010000011101001011000100110111001100101000101101000100101100"
	if got := dataBitsString(iw); got != wantIW {
		t.Fatalf("J2.2I data bits mismatch\n got %s\nwant %s", got, wantIW)
	}
	if got := dataBitsString(e0); got != wantE0 {
		t.Fatalf("J2.2E0 data bits mismatch\n got %s\nwant %s", got, wantE0)
	}
	// Parity is generated on encode and verified on decode.
	if err := verifyWordParity(iw); err != nil {
		t.Fatalf("IW parity: %v", err)
	}
	if err := verifyWordParity(e0); err != nil {
		t.Fatalf("E0 parity: %v", err)
	}
}

func TestLongStringsTruncated(t *testing.T) {
	orig := fullMessage()
	orig.TrackID = "VERYLONGTRACKID123"
	orig.SourceJU = "JU999999"
	orig.Identity = "IDENTITY12345"
	p, err := orig.EncodeJ22()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeJ22(p)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TrackID != "VERYLONG" {
		t.Errorf("TrackID truncation = %q, want %q", got.TrackID, "VERYLONG")
	}
	if got.SourceJU != "JU999999" {
		t.Errorf("SourceJU = %q, want %q", got.SourceJU, "JU999999")
	}
	if got.Identity != "IDENTITY" {
		t.Errorf("Identity truncation = %q, want %q", got.Identity, "IDENTITY")
	}
}

func TestDecodeJ22Errors(t *testing.T) {
	if _, err := DecodeJ22(make([]byte, 64)); err == nil {
		t.Error("short payload: expected error")
	}
	p, err := fullMessage().EncodeJ22()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	bad := make([]byte, J22PayloadSize)
	copy(bad, p)
	bad[0] ^= 0x80 // corrupt bit 0 of the initial word -> parity mismatch
	if _, err := DecodeJ22(bad); err == nil || !strings.Contains(err.Error(), "parity") {
		t.Errorf("corrupted word: expected parity error, got %v", err)
	}
	badCtx := make([]byte, J22PayloadSize)
	copy(badCtx, p)
	badCtx[2*J22WordBytes+20] = 9 // invalid platform byte
	if _, err := DecodeJ22(badCtx); err == nil {
		t.Error("invalid platform byte: expected error")
	}
}

// checkRoundTrip compares all 15 fields: exact for the ten engine-context
// fields, within the J2.2 quantization tolerance for the five
// standard-encoded fields.
func checkRoundTrip(t *testing.T, orig, got *PPLIMessage) {
	t.Helper()
	if got.TrackID != orig.TrackID {
		t.Errorf("TrackID = %q, want %q", got.TrackID, orig.TrackID)
	}
	if got.SourceJU != orig.SourceJU {
		t.Errorf("SourceJU = %q, want %q", got.SourceJU, orig.SourceJU)
	}
	if got.TrackNum != orig.TrackNum {
		t.Errorf("TrackNum = %d, want %d", got.TrackNum, orig.TrackNum)
	}
	if got.PlatformType != orig.PlatformType {
		t.Errorf("PlatformType = %q, want %q", got.PlatformType, orig.PlatformType)
	}
	if math.Abs(got.Latitude-orig.Latitude) > 5e-5 {
		t.Errorf("Latitude = %v, want ~%v", got.Latitude, orig.Latitude)
	}
	if math.Abs(got.Longitude-orig.Longitude) > 5e-5 {
		t.Errorf("Longitude = %v, want ~%v", got.Longitude, orig.Longitude)
	}
	if math.Abs(got.Altitude-orig.Altitude) > 13 {
		t.Errorf("Altitude = %v, want ~%v", got.Altitude, orig.Altitude)
	}
	if math.Abs(got.Speed-orig.Speed) > 0.6 {
		t.Errorf("Speed = %v, want ~%v", got.Speed, orig.Speed)
	}
	if math.Abs(got.Course-orig.Course) > 0.6 {
		t.Errorf("Course = %v, want ~%v", got.Course, orig.Course)
	}
	if got.Identity != orig.Identity {
		t.Errorf("Identity = %q, want %q", got.Identity, orig.Identity)
	}
	if !got.TimeOfTrack.Equal(orig.TimeOfTrack) {
		t.Errorf("TimeOfTrack = %v, want %v", got.TimeOfTrack, orig.TimeOfTrack)
	}
	if !got.TimeOfMessage.Equal(orig.TimeOfMessage) {
		t.Errorf("TimeOfMessage = %v, want %v", got.TimeOfMessage, orig.TimeOfMessage)
	}
	if got.NPGNumber != orig.NPGNumber {
		t.Errorf("NPGNumber = %d, want %d", got.NPGNumber, orig.NPGNumber)
	}
	if got.MessageType != orig.MessageType {
		t.Errorf("MessageType = %q, want %q", got.MessageType, orig.MessageType)
	}
	if got.Valid != orig.Valid {
		t.Errorf("Valid = %v, want %v", got.Valid, orig.Valid)
	}
}
