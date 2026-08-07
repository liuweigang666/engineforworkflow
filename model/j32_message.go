package model

import "time"

// J32Message represents a Link-16 J3.2 Air Track message: a surveillance
// track report carrying position, velocity, identity, and classification.
type J32Message struct {
	TrackID        string
	SourceJU       string
	TrackNum       int
	Latitude       float64
	Longitude      float64
	Altitude       float64
	Heading        float64 // degrees, 0-359
	Speed          float64 // knots
	Identity       string  // FRIEND / HOSTILE / UNKNOWN
	Classification string  // AIR / SURFACE / SUBSURFACE
	TimeOfTrack    time.Time
	TimeOfMessage  time.Time
	NPGNumber      int
	MessageType    string
	Valid          bool
}
