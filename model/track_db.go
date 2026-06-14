package model

import (
	"fmt"
	"sync"
	"time"
)

type TrackEntry struct {
	PPLIMessage
	Status     TrackStatus
	LastUpdate time.Time
	TTL        int
	Version    int
}

type TrackDB struct {
	mu     sync.RWMutex
	tracks map[string]*TrackEntry
}

func NewTrackDB() *TrackDB {
	return &TrackDB{tracks: make(map[string]*TrackEntry)}
}

func (db *TrackDB) Create(trackID string, msg *PPLIMessage) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tracks[trackID]; exists {
		return fmt.Errorf("track %s already exists", trackID)
	}

	entry := &TrackEntry{
		PPLIMessage: *msg,
		Status:      TrackStatusActive,
		LastUpdate:  time.Now(),
		TTL:         300,
		Version:     1,
	}
	db.tracks[trackID] = entry
	return nil
}

func (db *TrackDB) Update(trackID string, msg *PPLIMessage) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	entry, exists := db.tracks[trackID]
	if !exists {
		return fmt.Errorf("track %s not found", trackID)
	}

	entry.PPLIMessage = *msg
	entry.LastUpdate = time.Now()
	entry.Version++
	return nil
}

func (db *TrackDB) Delete(trackID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tracks[trackID]; !exists {
		return fmt.Errorf("track %s not found", trackID)
	}

	delete(db.tracks, trackID)
	return nil
}

func (db *TrackDB) Get(trackID string) (*TrackEntry, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	entry, exists := db.tracks[trackID]
	if !exists {
		return nil, fmt.Errorf("track %s not found", trackID)
	}
	return entry, nil
}

func (db *TrackDB) GetAll() map[string]*TrackEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[string]*TrackEntry, len(db.tracks))
	for k, v := range db.tracks {
		result[k] = v
	}
	return result
}

func (db *TrackDB) CheckExpired(trackID string) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	entry, exists := db.tracks[trackID]
	if !exists {
		return false, fmt.Errorf("track %s not found", trackID)
	}

	elapsed := time.Since(entry.LastUpdate).Seconds()
	return elapsed > float64(entry.TTL), nil
}

func (db *TrackDB) CleanExpired() int {
	db.mu.Lock()
	defer db.mu.Unlock()

	count := 0
	for trackID, entry := range db.tracks {
		elapsed := time.Since(entry.LastUpdate).Seconds()
		if elapsed > float64(entry.TTL) {
			delete(db.tracks, trackID)
			count++
		}
	}
	return count
}

func (db *TrackDB) SetTTL(trackID string, ttl int) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	entry, exists := db.tracks[trackID]
	if !exists {
		return fmt.Errorf("track %s not found", trackID)
	}

	entry.TTL = ttl
	return nil
}

func (db *TrackDB) Size() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.tracks)
}
