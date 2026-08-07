package node

import (
	"fmt"
	"math"
	"time"

	"datalink-workflow/model"
)

// This file implements the J3.2 Air Track message-type extension (TODO C14).
// New nodes are added here and in config/j32_stages.json; the engine core
// (workflow package) is unchanged.

func extractJ32(data interface{}) *model.J32Message {
	if m, ok := data.(map[string]interface{}); ok {
		if msg, exists := m["message"]; exists {
			if p, ok := msg.(*model.J32Message); ok {
				return p
			}
		}
	}
	return nil
}

// j32FromData builds a J32Message from a token data map, or nil.
func j32FromData(data interface{}) *model.J32Message {
	m, ok := data.(map[string]interface{})
	if !ok {
		return nil
	}
	get := func(key string) string {
		if s, ok := m[key].(string); ok {
			return s
		}
		return ""
	}
	getF := func(key string) float64 {
		switch v := m[key].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
		return 0
	}
	if get("track_id") == "" {
		return nil
	}
	return &model.J32Message{
		TrackID:        get("track_id"),
		SourceJU:       get("source_ju"),
		Latitude:       getF("latitude"),
		Longitude:      getF("longitude"),
		Altitude:       getF("altitude"),
		Heading:        getF("heading"),
		Speed:          getF("speed"),
		Identity:       get("identity"),
		Classification: "AIR",
		TimeOfTrack:    time.Now(),
		TimeOfMessage:  time.Now(),
		NPGNumber:      6,
		MessageType:    "J3.2",
		Valid:          true,
	}
}

type InitJ32DataNode struct{}

func (n *InitJ32DataNode) Name() string { return "init_j32_data" }

func (n *InitJ32DataNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := j32FromData(ctx.Data)
	if msg == nil {
		msg = &model.J32Message{
			TrackID:        "ATK001",
			SourceJU:       "JU004",
			TrackNum:       3001,
			Latitude:       35.6812,
			Longitude:      139.7671,
			Altitude:       8000,
			Heading:        90,
			Speed:          420,
			Identity:       "FRIEND",
			Classification: "AIR",
			TimeOfTrack:    time.Now(),
			TimeOfMessage:  time.Now(),
			NPGNumber:      6,
			MessageType:    "J3.2",
			Valid:          true,
		}
	}
	return NodeOutput{"message": msg, "track_id": msg.TrackID, "init_success": true}, nil
}

type ClassifyJ32Node struct{}

func (n *ClassifyJ32Node) Name() string { return "classify_j32" }

func (n *ClassifyJ32Node) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	if msg.Altitude > 500 {
		msg.Classification = "AIR"
	} else if msg.Altitude >= 0 {
		msg.Classification = "SURFACE"
	} else {
		msg.Classification = "SUBSURFACE"
	}
	return NodeOutput{"message": msg, "classification": msg.Classification, "classified": true}, nil
}

type EncodeJ32MessageNode struct{}

func (n *EncodeJ32MessageNode) Name() string { return "encode_j32_message" }

func (n *EncodeJ32MessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	return NodeOutput{
		"message":        msg,
		"encoded_length": 320, // fixed-length J3.2 air track payload
		"encoding":       "complete",
	}, nil
}

type ValidateJ32MessageNode struct{}

func (n *ValidateJ32MessageNode) Name() string { return "validate_j32_message" }

func (n *ValidateJ32MessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return NodeOutput{"valid": true, "errors": []string{}, "validated": true}, nil
	}
	var errors []string
	if msg.Latitude < -90 || msg.Latitude > 90 {
		errors = append(errors, fmt.Sprintf("latitude out of range: %.2f", msg.Latitude))
	}
	if msg.Longitude < -180 || msg.Longitude > 180 {
		errors = append(errors, fmt.Sprintf("longitude out of range: %.2f", msg.Longitude))
	}
	if msg.Altitude < 0 || msg.Altitude > 100000 {
		errors = append(errors, fmt.Sprintf("altitude out of range: %.2f", msg.Altitude))
	}
	if msg.Heading < 0 || msg.Heading >= 360 {
		errors = append(errors, fmt.Sprintf("heading out of range: %.2f", msg.Heading))
	}
	return NodeOutput{"message": msg, "valid": len(errors) == 0, "errors": errors, "validated": true}, nil
}

type DecodeJ32MessageNode struct{}

func (n *DecodeJ32MessageNode) Name() string { return "decode_j32_message" }

func (n *DecodeJ32MessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("decode failed: no J3.2 message data in context")
	}
	if !msg.Valid {
		return nil, fmt.Errorf("decode failed: message marked invalid (track %s)", msg.TrackID)
	}
	if msg.TrackID == "" {
		return nil, fmt.Errorf("decode failed: missing track id")
	}
	if msg.Latitude < -90 || msg.Latitude > 90 || msg.Longitude < -180 || msg.Longitude > 180 {
		return nil, fmt.Errorf("decode failed: position out of range (lat=%v lon=%v)", msg.Latitude, msg.Longitude)
	}
	return NodeOutput{"message": msg, "track_id": msg.TrackID, "decoded": true, "decode_status": "success"}, nil
}

type J32ReceiveFilterNode struct{}

func (n *J32ReceiveFilterNode) Name() string { return "j32_receive_filter" }

func (n *J32ReceiveFilterNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return NodeOutput{"accepted": true, "rejected": false, "reason": "no_message_to_filter"}, nil
	}
	accepted := true
	reason := ""
	if time.Since(msg.TimeOfTrack) > 30*time.Second {
		accepted = false
		reason = "stale_data"
	}
	if math.Abs(msg.Latitude) > 90 || math.Abs(msg.Longitude) > 180 {
		accepted = false
		reason = "invalid_position"
	}
	return NodeOutput{"message": msg, "accepted": accepted, "rejected": !accepted, "reason": reason}, nil
}

type J32CorrelateNode struct {
	db *model.J32TrackDB
}

func NewJ32CorrelateNode(db *model.J32TrackDB) *J32CorrelateNode {
	return &J32CorrelateNode{db: db}
}

func (n *J32CorrelateNode) Name() string { return "j32_correlate" }

func (n *J32CorrelateNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	mode := getStringField(ctx.Data, "correlation_mode")
	if mode == "" {
		mode = "auto"
	}
	var result model.CorrelationResult
	if mode == "manual" {
		switch getStringField(ctx.Data, "correlation_hint") {
		case "update":
			result = model.CorrelationUpdate
		case "duplicate":
			result = model.CorrelationDuplicate
		default:
			result = model.CorrelationNew
		}
	} else {
		entry, err := n.db.Get(msg.TrackID)
		if err != nil {
			result = model.CorrelationNew
		} else if msg.TimeOfMessage.After(entry.J32Message.TimeOfMessage) {
			result = model.CorrelationUpdate
		} else {
			result = model.CorrelationDuplicate
		}
	}
	return NodeOutput{
		"message":            msg,
		"correlation_result": string(result),
		"correlation_mode":   mode,
		"correlation":        string(result),
	}, nil
}

type CreateJ32EntryNode struct {
	db *model.J32TrackDB
}

func NewCreateJ32EntryNode(db *model.J32TrackDB) *CreateJ32EntryNode {
	return &CreateJ32EntryNode{db: db}
}

func (n *CreateJ32EntryNode) Name() string { return "create_j32_entry" }

func (n *CreateJ32EntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	if err := n.db.Create(msg.TrackID, msg); err != nil {
		return nil, err
	}
	return NodeOutput{"message": msg, "action": "created", "track_id": msg.TrackID, "new_track": true}, nil
}

type UpdateJ32EntryNode struct {
	db *model.J32TrackDB
}

func NewUpdateJ32EntryNode(db *model.J32TrackDB) *UpdateJ32EntryNode {
	return &UpdateJ32EntryNode{db: db}
}

func (n *UpdateJ32EntryNode) Name() string { return "update_j32_entry" }

func (n *UpdateJ32EntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	if err := n.db.Update(msg.TrackID, msg); err != nil {
		return nil, err
	}
	return NodeOutput{"message": msg, "action": "updated", "track_id": msg.TrackID, "new_track": false}, nil
}

type DuplicateJ32ResolveNode struct{}

func (n *DuplicateJ32ResolveNode) Name() string { return "duplicate_j32_resolve" }

func (n *DuplicateJ32ResolveNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	return NodeOutput{"message": msg, "action": "duplicate_resolved", "resolution": "keep_latest", "track_id": msg.TrackID}, nil
}

type StoreJ32Node struct {
	db *model.J32TrackDB
}

func NewStoreJ32Node(db *model.J32TrackDB) *StoreJ32Node {
	return &StoreJ32Node{db: db}
}

func (n *StoreJ32Node) Name() string { return "store_j32" }

func (n *StoreJ32Node) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
	if _, err := n.db.Get(msg.TrackID); err != nil {
		if cerr := n.db.Create(msg.TrackID, msg); cerr != nil {
			return nil, fmt.Errorf("failed to store track %s: %w", msg.TrackID, cerr)
		}
		return NodeOutput{"message": msg, "store_result": "created", "track_count": n.db.Size()}, nil
	}
	return NodeOutput{"message": msg, "store_result": "verified", "track_id": msg.TrackID, "track_count": n.db.Size()}, nil
}

type CheckTTLJ32Node struct {
	db *model.J32TrackDB
}

func NewCheckTTLJ32Node(db *model.J32TrackDB) *CheckTTLJ32Node {
	return &CheckTTLJ32Node{db: db}
}

func (n *CheckTTLJ32Node) Name() string { return "check_ttl_j32" }

func (n *CheckTTLJ32Node) Execute(ctx NodeContext) (NodeOutput, error) {
	trackID := getStringField(ctx.Data, "track_id")
	if trackID == "" {
		return NodeOutput{"expired_count": n.db.CleanExpired(), "ttl_check": "bulk"}, nil
	}
	expired, err := n.db.CheckExpired(trackID)
	if err != nil {
		return NodeOutput{"track_id": trackID, "expired": false, "error": err.Error()}, nil
	}
	return NodeOutput{"track_id": trackID, "expired": expired, "ttl_check": "single"}, nil
}

type RetainJ32EntryNode struct {
	db *model.J32TrackDB
}

func NewRetainJ32EntryNode(db *model.J32TrackDB) *RetainJ32EntryNode {
	return &RetainJ32EntryNode{db: db}
}

func (n *RetainJ32EntryNode) Name() string { return "retain_j32_entry" }

func (n *RetainJ32EntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	return NodeOutput{"track_id": getStringField(ctx.Data, "track_id"), "action": "retained", "track_count": n.db.Size()}, nil
}

type DeleteJ32EntryNode struct {
	db *model.J32TrackDB
}

func NewDeleteJ32EntryNode(db *model.J32TrackDB) *DeleteJ32EntryNode {
	return &DeleteJ32EntryNode{db: db}
}

func (n *DeleteJ32EntryNode) Name() string { return "delete_j32_entry" }

func (n *DeleteJ32EntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	trackID := getStringField(ctx.Data, "track_id")
	if err := n.db.Delete(trackID); err != nil {
		return NodeOutput{"track_id": trackID, "action": "delete_failed", "reason": err.Error()}, nil
	}
	return NodeOutput{"track_id": trackID, "action": "deleted", "track_count": n.db.Size()}, nil
}

type J32DetectAnomalyNode struct{}

func (n *J32DetectAnomalyNode) Name() string { return "j32_detect_anomaly" }

func (n *J32DetectAnomalyNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	anomalyType := ""
	anomalyDetected := false
	if msg != nil {
		if msg.Altitude < 0 || msg.Altitude > 100000 {
			anomalyType = "field_out_of_range"
			anomalyDetected = true
		}
		if msg.Heading < 0 || msg.Heading >= 360 {
			anomalyType = "heading_out_of_range"
			anomalyDetected = true
		}
	}
	return NodeOutput{"message": msg, "anomaly_detected": anomalyDetected, "anomaly_type": anomalyType}, nil
}

type J32ClampFieldNode struct{}

func (n *J32ClampFieldNode) Name() string { return "j32_clamp_field" }

func (n *J32ClampFieldNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractJ32(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}
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
	} else if msg.Altitude > 100000 {
		msg.Altitude = 100000
	}
	msg.Heading = math.Mod(msg.Heading, 360)
	if msg.Heading < 0 {
		msg.Heading += 360
	}
	return NodeOutput{"message": msg, "clamped": true, "clamp_type": "range_normalized"}, nil
}
