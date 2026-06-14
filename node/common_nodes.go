package node

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"datalink-workflow/model"
)

// extractMessage extracts PPLIMessage from various data types
func extractMessage(data interface{}) *model.PPLIMessage {
	if data == nil {
		return nil
	}
	
	// Handle all map-like types
	if m, ok := data.(map[string]interface{}); ok {
		msg, exists := m["message"]
		if !exists {
			return nil
		}
		if p, ok := msg.(*model.PPLIMessage); ok {
			return p
		}
	}
	return nil
}

// getStringField extracts a string field from various data types
func getStringField(data interface{}, key string) string {
	if data == nil {
		return ""
	}
	
	// Handle all map-like types
	if m, ok := data.(map[string]interface{}); ok {
		if s, ok := m[key].(string); ok {
			return s
		}
	}
	return ""
}

type ValidateMessageNode struct{}

func (n *ValidateMessageNode) Name() string { return "validate_message" }

func (n *ValidateMessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
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
	if msg.Altitude < 0 || msg.Altitude > 50000 {
		errors = append(errors, fmt.Sprintf("altitude out of range: %.2f", msg.Altitude))
	}

	return NodeOutput{"message": msg, "valid": len(errors) == 0, "errors": errors, "validated": true}, nil
}

type CheckSendConditionNode struct{}

func (n *CheckSendConditionNode) Name() string { return "check_send_condition" }

func (n *CheckSendConditionNode) Execute(ctx NodeContext) (NodeOutput, error) {
	trigger := getStringField(ctx.Data, "trigger_type")
	if trigger == "" {
		if rand.Float32() > 0.5 {
			trigger = "auto"
		} else {
			trigger = "operator"
		}
	}

	return NodeOutput{"trigger": trigger, "conditions_met": true, "reasons": []string{"NPG slot available", "Message validated"}, "can_send": true}, nil
}

type AssignNPGSlotNode struct{}

func (n *AssignNPGSlotNode) Name() string { return "assign_npg_slot" }

func (n *AssignNPGSlotNode) Execute(ctx NodeContext) (NodeOutput, error) {
	return NodeOutput{"npg_number": 6, "slot_index": rand.Intn(1000) + 1, "assigned": true}, nil
}

type TransmitNode struct{}

func (n *TransmitNode) Name() string { return "transmit" }

func (n *TransmitNode) Execute(ctx NodeContext) (NodeOutput, error) {
	npgNum := 6
	slotIdx := 100

	if npg, ok := ctx.Data.(map[string]interface{}); ok {
		if v, exists := npg["npg_number"].(int); exists {
			npgNum = v
		}
		if v, exists := npg["slot_index"].(int); exists {
			slotIdx = v
		}
	}

	time.Sleep(10 * time.Millisecond)

	return NodeOutput{"transmitted": true, "npg_number": npgNum, "slot_index": slotIdx, "tx_status": "success"}, nil
}

type DecodeMessageNode struct{}

func (n *DecodeMessageNode) Name() string { return "decode_message" }

func (n *DecodeMessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := &model.PPLIMessage{
		TrackID:        "NEW_TRK001",
		SourceJU:       "JU002",
		TrackNum:       1002,
		PlatformType:   model.PlatformAir,
		Latitude:       35.6900,
		Longitude:      139.7700,
		Altitude:       8500,
		Speed:          275,
		Course:         95,
		Identity:       "FRIEND",
		TimeOfTrack:    time.Now().Add(-1 * time.Second),
		TimeOfMessage:  time.Now(),
		NPGNumber:      6,
		MessageType:    "J2.2",
		Valid:          true,
	}

	// Check if there's an incoming track ID override
	if trackID := getStringField(ctx.Data, "track_id"); trackID != "" {
		msg.TrackID = trackID
	}

	output := NodeOutput{
		"message":       msg,
		"track_id":      msg.TrackID,
		"decoded":       true,
		"decode_status": "success",
	}
	return output, nil
}

type ReceiveFilterNode struct{}

func (n *ReceiveFilterNode) Name() string { return "receive_filter" }

func (n *ReceiveFilterNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return NodeOutput{"accepted": true, "rejected": false, "reason": "no_message_to_filter"}, nil
	}

	accepted := true
	rejectReason := ""

	if time.Since(msg.TimeOfTrack) > 30*time.Second {
		accepted = false
		rejectReason = "stale_data"
	}

	if math.Abs(msg.Latitude) > 90 || math.Abs(msg.Longitude) > 180 {
		accepted = false
		rejectReason = "invalid_position"
	}

	return NodeOutput{"message": msg, "accepted": accepted, "rejected": !accepted, "reason": rejectReason}, nil
}

type CheckTTLNode struct {
	db *model.TrackDB
}

func NewCheckTTLNode(db *model.TrackDB) *CheckTTLNode {
	return &CheckTTLNode{db: db}
}

func (n *CheckTTLNode) Name() string { return "check_ttl" }

func (n *CheckTTLNode) Execute(ctx NodeContext) (NodeOutput, error) {
	trackID := getStringField(ctx.Data, "track_id")

	if trackID == "" {
		expiredCount := n.db.CleanExpired()
		return NodeOutput{"expired_count": expiredCount, "ttl_check": "bulk"}, nil
	}

	expired, err := n.db.CheckExpired(trackID)
	if err != nil {
		return NodeOutput{"track_id": trackID, "expired": false, "error": err.Error()}, nil
	}

	return NodeOutput{"track_id": trackID, "expired": expired, "ttl_check": "single"}, nil
}

type CheckSourceJUNode struct {
	db *model.TrackDB
}

func NewCheckSourceJUNode(db *model.TrackDB) *CheckSourceJUNode {
	return &CheckSourceJUNode{db: db}
}

func (n *CheckSourceJUNode) Name() string { return "check_source_ju" }

func (n *CheckSourceJUNode) Execute(ctx NodeContext) (NodeOutput, error) {
	trackID := getStringField(ctx.Data, "track_id")
	sourceJU := getStringField(ctx.Data, "source_ju")

	if sourceJU == "" {
		sourceJU = "JU001"
	}

	return NodeOutput{"track_id": trackID, "source_ju": sourceJU, "ju_active": true, "valid": true}, nil
}

type DetectAnomalyNode struct{}

func (n *DetectAnomalyNode) Name() string { return "detect_anomaly" }

func (n *DetectAnomalyNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)

	anomalyType := ""
	anomalyDetected := false

	if msg != nil {
		if len(msg.TrackID) >= 10 && msg.TrackID[:10] == "CONFLICT_" {
			anomalyType = "track_id_conflict"
			anomalyDetected = true
		}
		if msg.Altitude < 0 || msg.Altitude > 50000 {
			anomalyType = "field_out_of_range"
			anomalyDetected = true
		}
	}

	return NodeOutput{"message": msg, "anomaly_detected": anomalyDetected, "anomaly_type": anomalyType}, nil
}

type SkipNode struct{}

func (n *SkipNode) Name() string { return "skip" }

func (n *SkipNode) Execute(ctx NodeContext) (NodeOutput, error) {
	return NodeOutput{"skipped": true}, nil
}
