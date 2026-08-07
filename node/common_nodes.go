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

// getBoolField reads a boolean field from a token data map.
func getBoolField(data interface{}, key string) (bool, bool) {
	if m, ok := data.(map[string]interface{}); ok {
		if v, ok := m[key].(bool); ok {
			return v, true
		}
	}
	return false, false
}

// messageFromData builds a PPLIMessage from a token's data map, or nil if
// the map does not contain usable fields.
func messageFromData(data interface{}) *model.PPLIMessage {
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
	return &model.PPLIMessage{
		TrackID:       get("track_id"),
		SourceJU:      get("source_ju"),
		Latitude:      getF("latitude"),
		Longitude:     getF("longitude"),
		Altitude:      getF("altitude"),
		Speed:         getF("speed"),
		Course:        getF("course"),
		Identity:      get("identity"),
		MessageType:   "J2.2",
		TimeOfTrack:   time.Now(),
		TimeOfMessage: time.Now(),
		NPGNumber:     6,
		Valid:         true,
	}
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
	canSend := true
	reasons := []string{"NPG slot available", "Message validated"}
	if deny, ok := getBoolField(ctx.Data, "force_deny"); ok && deny {
		canSend = false
		reasons = []string{"Transmission window closed"}
	}
	return NodeOutput{
		"trigger":        trigger,
		"conditions_met": canSend,
		"reasons":        reasons,
		"can_send":       canSend,
	}, nil
}

type AssignNPGSlotNode struct{}

func (n *AssignNPGSlotNode) Name() string { return "assign_npg_slot" }

func (n *AssignNPGSlotNode) Execute(ctx NodeContext) (NodeOutput, error) {
	return NodeOutput{"npg_number": 6, "slot_index": rand.Intn(1000) + 1, "assigned": true}, nil
}

type TransmitNode struct{}

func (n *TransmitNode) Name() string { return "transmit" }

// TransmitDelay simulates the network transmission latency. Benchmarks set
// it to zero so that latency measurements reflect engine overhead rather
// than the simulated transmission wait.
var TransmitDelay = 10 * time.Millisecond

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

	time.Sleep(TransmitDelay)

	return NodeOutput{"transmitted": true, "npg_number": npgNum, "slot_index": slotIdx, "tx_status": "success"}, nil
}

type DecodeMessageNode struct{}

func (n *DecodeMessageNode) Name() string { return "decode_message" }

func (n *DecodeMessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	// J2.2 word path: decode the J2.2 IW+E0 payload produced by
	// encode_ppli_message back into a PPLIMessage (deterministic round-trip).
	if payload, ok := getJ22Field(ctx.Data); ok {
		msg, err := model.DecodeJ22(payload)
		if err != nil {
			return nil, fmt.Errorf("decode failed: %v", err)
		}
		return NodeOutput{
			"message":       msg,
			"track_id":      msg.TrackID,
			"decoded":       true,
			"decode_status": "success",
		}, nil
	}
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("decode failed: no message data in context")
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

	output := NodeOutput{
		"message":       msg,
		"track_id":      msg.TrackID,
		"decoded":       true,
		"decode_status": "success",
	}
	return output, nil
}

// getJ22Field reads a []byte J2.2 payload from a token data map.
func getJ22Field(data interface{}) ([]byte, bool) {
	if m, ok := data.(map[string]interface{}); ok {
		if p, ok := m["j22"].([]byte); ok {
			return p, true
		}
	}
	return nil, false
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
		// Bulk scan: the database has already been purged, so no per-entry
		// expiry branch should fire; expired=false routes the stage to the
		// retain/terminate branch.
		return NodeOutput{"expired_count": expiredCount, "expired": false, "ttl_check": "bulk"}, nil
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

// TimerTriggerNode generates timer-based trigger events. It is the
// cross-stage node in the processing node inventory (Appendix B), used to
// drive timer-triggered stages such as T4 (Clear).
type TimerTriggerNode struct{}

func (n *TimerTriggerNode) Name() string { return "timer_trigger" }

func (n *TimerTriggerNode) Execute(ctx NodeContext) (NodeOutput, error) {
	return NodeOutput{
		"trigger":       "timer",
		"timer_trigger": true,
	}, nil
}
