package model

import (
	"fmt"
	"sync"
	"time"
)

// J32TrackEntry is a track-database record for J3.2 air tracks.
type J32TrackEntry struct {
	J32Message
	Status     TrackStatus
	LastUpdate time.Time
	TTL        int
	Version    int
}

// J32TrackDB is a concurrent-safe, in-memory store for J3.2 air tracks,
// mirroring TrackDB but for the J3.2 message type.
type J32TrackDB struct {
	mu     sync.RWMutex
	tracks map[string]*J32TrackEntry
}

func NewJ32TrackDB() *J32TrackDB {
	return &J32TrackDB{tracks: make(map[string]*J32TrackEntry)}
}

func (db *J32TrackDB) Create(trackID string, msg *J32Message) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.tracks[trackID]; exists {
		return fmt.Errorf("track %s already exists", trackID)
	}
	db.tracks[trackID] = &J32TrackEntry{
		J32Message: *msg,
		Status:     TrackStatusActive,
		LastUpdate: time.Now(),
		TTL:        300,
		Version:    1,
	}
	return nil
}

func (db *J32TrackDB) Update(trackID string, msg *J32Message) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	entry, exists := db.tracks[trackID]
	if !exists {
		return fmt.Errorf("track %s not found", trackID)
	}
	entry.J32Message = *msg
	entry.LastUpdate = time.Now()
	entry.Version++
	return nil
}

func (db *J32TrackDB) Delete(trackID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.tracks[trackID]; !exists {
		return fmt.Errorf("track %s not found", trackID)
	}
	delete(db.tracks, trackID)
	return nil
}

func (db *J32TrackDB) Get(trackID string) (*J32TrackEntry, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	entry, exists := db.tracks[trackID]
	if !exists {
		return nil, fmt.Errorf("track %s not found", trackID)
	}
	return entry, nil
}

func (db *J32TrackDB) Size() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.tracks)
}

// GetAll returns a snapshot of all track entries.
func (db *J32TrackDB) GetAll() map[string]*J32TrackEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()
	result := make(map[string]*J32TrackEntry, len(db.tracks))
	for k, v := range db.tracks {
		result[k] = v
	}
	return result
}

// CheckExpired evaluates TTL against the message's own timestamp.
func (db *J32TrackDB) CheckExpired(trackID string) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	entry, exists := db.tracks[trackID]
	if !exists {
		return false, fmt.Errorf("track %s not found", trackID)
	}
	elapsed := time.Since(entry.J32Message.TimeOfMessage).Seconds()
	return elapsed > float64(entry.TTL), nil
}

// CleanExpired removes all expired tracks and returns the count removed.
func (db *J32TrackDB) CleanExpired() int {
	db.mu.Lock()
	defer db.mu.Unlock()
	count := 0
	for trackID, entry := range db.tracks {
		elapsed := time.Since(entry.J32Message.TimeOfMessage).Seconds()
		if elapsed > float64(entry.TTL) {
			delete(db.tracks, trackID)
			count++
		}
	}
	return count
}

func (db *J32TrackDB) SetTTL(trackID string, ttl int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	entry, exists := db.tracks[trackID]
	if !exists {
		return fmt.Errorf("track %s not found", trackID)
	}
	entry.TTL = ttl
	return nil
}
