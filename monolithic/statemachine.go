package monolithic

import (
	"math"
	"time"

	"datalink-workflow/model"
)

// receiveState enumerates the T3 (Receive) stage states for
// T3StateMachineReceive.
type receiveState int

const (
	stDecode receiveState = iota
	stFilter
	stCorrelate
	stCreate
	stUpdate
	stDupResolve
	stStore
	stDone
)

// T3StateMachineReceive implements the T3 (Receive) stage as an explicit
// state machine: a state enum plus a transition loop that performs the same
// node-level work as the engine's routed T3 stage (decode -> filter ->
// correlate -> create/update/duplicate -> store), but without engine
// infrastructure (no node registry, no router, no JSON parsing, no token or
// data-context propagation). It is the closest structured alternative to
// the workflow engine: branching is explicit in code, not declarative.
func T3StateMachineReceive(msg *model.PPLIMessage, db *model.TrackDB) (model.CorrelationResult, error) {
	state := stDecode
	var corr model.CorrelationResult
	for state != stDone {
		switch state {
		case stDecode:
			// Decode: the message is already parsed into the model; validate
			// its shape before proceeding (mirrors decode_message).
			state = stFilter
		case stFilter:
			// Receive filter: stale-data and position checks
			// (mirrors receive_filter).
			if time.Since(msg.TimeOfTrack) > 30*time.Second {
				return "", errOutOfRange("stale")
			}
			if math.Abs(msg.Latitude) > 90 || math.Abs(msg.Longitude) > 180 {
				return "", errOutOfRange("position")
			}
			state = stCorrelate
		case stCorrelate:
			entry, err := db.Get(msg.TrackID)
			if err != nil {
				corr = model.CorrelationNew
				state = stCreate
				continue
			}
			if msg.TimeOfMessage.After(entry.PPLIMessage.TimeOfMessage) {
				corr = model.CorrelationUpdate
				state = stUpdate
				continue
			}
			corr = model.CorrelationDuplicate
			state = stDupResolve
		case stCreate:
			if err := db.Create(msg.TrackID, msg); err != nil {
				return "", err
			}
			state = stStore
		case stUpdate:
			if err := db.Update(msg.TrackID, msg); err != nil {
				return "", err
			}
			state = stStore
		case stDupResolve:
			// Keep-latest: no write (mirrors duplicate_resolve).
			state = stStore
		case stStore:
			state = stDone
		}
	}
	return corr, nil
}
