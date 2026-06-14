package model

import "time"

// PlatformType represents the type of platform reporting PPLI
type PlatformType string

const (
	PlatformAir        PlatformType = "air"
	PlatformSurface    PlatformType = "surface"
	PlatformSubsurface PlatformType = "subsurface"
	PlatformLand       PlatformType = "land"
	PlatformUnknown    PlatformType = "unknown"
)

// PPLIMessage represents a Link-16 J2.2 Position, Point, Location, and Identity message
type PPLIMessage struct {
	TrackID       string       `yaml:"track_id" json:"track_id"`
	SourceJU      string       `yaml:"source_ju" json:"source_ju"`
	TrackNum      int          `yaml:"track_num" json:"track_num"`
	PlatformType  PlatformType `yaml:"platform_type" json:"platform_type"`
	Latitude      float64      `yaml:"latitude" json:"latitude"`
	Longitude     float64      `yaml:"longitude" json:"longitude"`
	Altitude      float64      `yaml:"altitude" json:"altitude"`
	Speed         float64      `yaml:"speed" json:"speed"`
	Course        float64      `yaml:"course" json:"course"`
	Identity      string       `yaml:"identity" json:"identity"`
	TargetType    string       `yaml:"target_type" json:"target_type"`
	TimeOfTrack   time.Time    `yaml:"time_of_track" json:"time_of_track"`
	TimeOfMessage time.Time    `yaml:"time_of_message" json:"time_of_message"`
	NPGNumber     int          `yaml:"npg_number" json:"npg_number"`
	SlotIndex     int          `yaml:"slot_index" json:"slot_index"`
	MessageType   string       `yaml:"message_type" json:"message_type"`
	Valid         bool         `yaml:"valid" json:"valid"`
}

type CorrelationResult string

const (
	CorrelationNew       CorrelationResult = "new"
	CorrelationUpdate    CorrelationResult = "update"
	CorrelationDuplicate CorrelationResult = "duplicate"
)

type TrackStatus string

const (
	TrackStatusActive  TrackStatus = "active"
	TrackStatusPending TrackStatus = "pending"
	TrackStatusDeleted TrackStatus = "deleted"
)
