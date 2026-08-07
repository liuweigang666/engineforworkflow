package model

import (
	"testing"
	"time"
)

func testMsg(trackID string, age time.Duration) *PPLIMessage {
	return &PPLIMessage{
		TrackID:       trackID,
		SourceJU:      "JU001",
		TimeOfTrack:   time.Now().Add(age),
		TimeOfMessage: time.Now().Add(age),
		MessageType:   "J2.2",
		Valid:         true,
	}
}

func TestTrackDBCRUD(t *testing.T) {
	db := NewTrackDB()
	if err := db.Create("T1", testMsg("T1", 0)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Create("T1", testMsg("T1", 0)); err == nil {
		t.Fatal("duplicate create should fail")
	}
	if err := db.Update("T1", testMsg("T1", 0)); err != nil {
		t.Fatalf("update: %v", err)
	}
	entry, err := db.Get("T1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry.Version != 2 {
		t.Fatalf("expected version 2, got %d", entry.Version)
	}
	if db.Size() != 1 {
		t.Fatalf("expected size 1, got %d", db.Size())
	}
	if err := db.Delete("T1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := db.Get("T1"); err == nil {
		t.Fatal("deleted track should be gone")
	}
}

func TestTrackDBCheckExpired(t *testing.T) {
	db := NewTrackDB()
	// Old message (10 minutes) with a 60s TTL -> expired.
	if err := db.Create("OLD", testMsg("OLD", -10*time.Minute)); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = db.SetTTL("OLD", 60)
	expired, err := db.CheckExpired("OLD")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !expired {
		t.Fatal("expected OLD to be expired")
	}

	// Fresh message with a long TTL -> not expired.
	if err := db.Create("NEW", testMsg("NEW", 0)); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = db.SetTTL("NEW", 3600)
	expired, err = db.CheckExpired("NEW")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if expired {
		t.Fatal("expected NEW not to be expired")
	}
}

func TestTrackDBCleanExpired(t *testing.T) {
	db := NewTrackDB()
	_ = db.Create("OLD", testMsg("OLD", -10*time.Minute))
	_ = db.SetTTL("OLD", 60)
	_ = db.Create("KEEP", testMsg("KEEP", 0))
	_ = db.SetTTL("KEEP", 3600)

	if removed := db.CleanExpired(); removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if db.Size() != 1 {
		t.Fatalf("expected 1 remaining, got %d", db.Size())
	}
}
