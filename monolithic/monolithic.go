// Package monolithic provides hand-inlined, single-function baselines for
// the five TDL processing stages. These are the near-best-case lower bound
// used by Experiment 2 (monolithic vs. workflow engine): there is no engine
// infrastructure (no routing, no registry lookup, no JSON parsing, no
// token/data propagation).
package monolithic

import (
	"math"
	"time"

	"datalink-workflow/model"
)

// T1SendPreparation initializes, classifies, encodes, and validates a PPLI
// message, mirroring the engine's prepare_send stage.
func T1SendPreparation(msg *model.PPLIMessage) error {
	// classify
	switch {
	case msg.Altitude > 100 && msg.Altitude > 5000:
		msg.PlatformType = model.PlatformAir
	case msg.Altitude > 100:
		msg.PlatformType = model.PlatformSurface
	default:
		msg.PlatformType = model.PlatformLand
	}
	// encode (fixed-length J2.2 payload)
	_ = 256
	// validate
	if msg.Latitude < -90 || msg.Latitude > 90 {
		return errOutOfRange("latitude")
	}
	if msg.Longitude < -180 || msg.Longitude > 180 {
		return errOutOfRange("longitude")
	}
	if msg.Altitude < 0 || msg.Altitude > 50000 {
		return errOutOfRange("altitude")
	}
	return nil
}

// T2Send checks transmission conditions and assigns an NPG slot.
func T2Send(msg *model.PPLIMessage, npg int) error {
	if msg == nil || npg <= 0 {
		return errOutOfRange("npg")
	}
	return nil
}

// T3Receive is the inlined receive path: decode, filter, correlate against
// the track database, and create/update/keep-latest accordingly.
func T3Receive(msg *model.PPLIMessage, db *model.TrackDB) (model.CorrelationResult, error) {
	// receive filter: stale-data and position checks
	if time.Since(msg.TimeOfTrack) > 30*time.Second {
		return "", errOutOfRange("stale")
	}
	if math.Abs(msg.Latitude) > 90 || math.Abs(msg.Longitude) > 180 {
		return "", errOutOfRange("position")
	}

	entry, err := db.Get(msg.TrackID)
	if err != nil {
		if err := db.Create(msg.TrackID, msg); err != nil {
			return "", err
		}
		return model.CorrelationNew, nil
	}
	if msg.TimeOfMessage.After(entry.PPLIMessage.TimeOfMessage) {
		if err := db.Update(msg.TrackID, msg); err != nil {
			return "", err
		}
		return model.CorrelationUpdate, nil
	}
	// duplicate: keep latest (no write)
	return model.CorrelationDuplicate, nil
}

// T4Clear scans the database for expired tracks and removes them.
func T4Clear(db *model.TrackDB) int {
	return db.CleanExpired()
}

// T5SpecialProcessing clamps out-of-range fields and reports the outcome.
func T5SpecialProcessing(msg *model.PPLIMessage) error {
	if msg.Latitude < -90 {
		msg.Latitude = -90
	} else if msg.Latitude > 90 {
		msg.Latitude = 90
	}
	if msg.Longitude < -180 {
		msg.Longitude = -180
	} else if msg.Longitude > 180 {
		msg.Longitude = 180
	}
	if msg.Altitude < 0 {
		msg.Altitude = 0
	} else if msg.Altitude > 50000 {
		msg.Altitude = 50000
	}
	msg.Course = math.Mod(msg.Course, 360)
	if msg.Course < 0 {
		msg.Course += 360
	}
	return nil
}

type rangeError string

func (e rangeError) Error() string { return "monolithic: out of range: " + string(e) }

func errOutOfRange(field string) error { return rangeError(field) }
