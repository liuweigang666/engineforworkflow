package node

import (
	"fmt"
	"math"
	"time"

	"datalink-workflow/model"
)

type InitPPLIDataNode struct{}

func (n *InitPPLIDataNode) Name() string { return "init_ppli_data" }

func (n *InitPPLIDataNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := messageFromData(ctx.Data)
	if msg == nil {
		msg = &model.PPLIMessage{
			TrackID:        "TRK001",
			SourceJU:       "JU001",
			TrackNum:       1001,
			PlatformType:   model.PlatformAir,
			Latitude:       35.6812,
			Longitude:      139.7671,
			Altitude:       10000,
			Speed:          300,
			Course:         90,
			Identity:       "FRIEND",
			TimeOfTrack:    time.Now(),
			TimeOfMessage:  time.Now(),
			NPGNumber:      6,
			MessageType:    "J2.2",
			Valid:          true,
		}
	}

	return NodeOutput{
		"message":       msg,
		"platform_type": string(msg.PlatformType),
		"track_id":      msg.TrackID,
		"init_success":  true,
	}, nil
}

type ClassifyPlatformNode struct{}

func (n *ClassifyPlatformNode) Name() string { return "classify_platform" }

func (n *ClassifyPlatformNode) Execute(ctx NodeContext) (NodeOutput, error) {
	data := ctx.Data

	// Handle different data types
	var msg *model.PPLIMessage
	switch v := data.(type) {
	case *model.PPLIMessage:
		msg = v
	case map[string]interface{}:
		if m, ok := v["message"].(*model.PPLIMessage); ok {
			msg = m
		} else {
			msg = &model.PPLIMessage{PlatformType: model.PlatformAir, Altitude: 8000, Speed: 250}
		}
	case NodeOutput:
		if m, ok := v["message"].(*model.PPLIMessage); ok {
			msg = m
		} else {
			msg = &model.PPLIMessage{PlatformType: model.PlatformAir, Altitude: 8000, Speed: 250}
		}
	default:
		msg = &model.PPLIMessage{PlatformType: model.PlatformAir, Altitude: 8000, Speed: 250}
	}

	var platformType model.PlatformType
	if msg.Altitude > 100 {
		if msg.Altitude > 5000 {
			platformType = model.PlatformAir
		} else {
			platformType = model.PlatformSurface
		}
	} else {
		platformType = model.PlatformLand
	}

	msg.PlatformType = platformType

	return NodeOutput{
		"message":        msg,
		"platform_type":  string(platformType),
		"classification": "complete",
	}, nil
}

type EncodePPLIMessageNode struct{}

func (n *EncodePPLIMessageNode) Name() string { return "encode_ppli_message" }

func (n *EncodePPLIMessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}

	payload, err := msg.EncodeJ22()
	if err != nil {
		return nil, err
	}
	return NodeOutput{
		"message":        msg,
		"j22":            payload,
		"encoded_length": len(payload),
		"encoding":       "complete",
	}, nil
}

// PPLICorrelateNode decides whether an incoming message is a new track,
// an update to an existing track, or a duplicate, based on the state of
// the track database (D3). No naming conventions are involved.
type PPLICorrelateNode struct {
	db *model.TrackDB
}

func NewPPLICorrelateNode(db *model.TrackDB) *PPLICorrelateNode {
	return &PPLICorrelateNode{db: db}
}

func (n *PPLICorrelateNode) Name() string { return "ppli_correlate" }

func (n *PPLICorrelateNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}

	correlationMode := getStringField(ctx.Data, "correlation_mode")
	if correlationMode == "" {
		correlationMode = "auto"
	}

	var result model.CorrelationResult
	if correlationMode == "manual" {
		// Manual override for testing/operational use.
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
			// Track not present -> new.
			result = model.CorrelationNew
		} else if msg.TimeOfMessage.After(entry.PPLIMessage.TimeOfMessage) {
			// Track present and message newer -> update.
			result = model.CorrelationUpdate
		} else {
			// Track present and message not newer (equal or older) ->
			// duplicate (keep latest).
			result = model.CorrelationDuplicate
		}
	}

	return NodeOutput{
		"message":            msg,
		"correlation_result": string(result),
		"correlation_mode":   correlationMode,
		"correlation":        string(result),
	}, nil
}

type StorePPLINode struct {
	db *model.TrackDB
}

func NewStorePPLINode(db *model.TrackDB) *StorePPLINode {
	return &StorePPLINode{db: db}
}

func (n *StorePPLINode) Name() string { return "store_ppli" }

func (n *StorePPLINode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}

	if _, err := n.db.Get(msg.TrackID); err != nil {
		// Safety net: if the branch path did not persist the entry (e.g., a
		// misconfigured new-correlation path), create it here.
		if cerr := n.db.Create(msg.TrackID, msg); cerr != nil {
			return nil, fmt.Errorf("failed to store track %s: %w", msg.TrackID, cerr)
		}
		return NodeOutput{"message": msg, "store_result": "created", "track_count": n.db.Size()}, nil
	}

	// The entry was already persisted by create_entry/update_entry; verify
	// and record the outcome without double-writing (duplicates must not
	// overwrite the newer state).
	return NodeOutput{"message": msg, "store_result": "verified", "track_id": msg.TrackID, "track_count": n.db.Size()}, nil
}

type CreateEntryNode struct {
	db *model.TrackDB
}

func NewCreateEntryNode(db *model.TrackDB) *CreateEntryNode {
	return &CreateEntryNode{db: db}
}

func (n *CreateEntryNode) Name() string { return "create_entry" }

func (n *CreateEntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}

	if err := n.db.Create(msg.TrackID, msg); err != nil {
		return nil, err
	}

	return NodeOutput{"message": msg, "action": "created", "track_id": msg.TrackID, "new_track": true}, nil
}

type UpdateEntryNode struct {
	db *model.TrackDB
}

func NewUpdateEntryNode(db *model.TrackDB) *UpdateEntryNode {
	return &UpdateEntryNode{db: db}
}

func (n *UpdateEntryNode) Name() string { return "update_entry" }

func (n *UpdateEntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}

	if err := n.db.Update(msg.TrackID, msg); err != nil {
		return nil, err
	}

	return NodeOutput{"message": msg, "action": "updated", "track_id": msg.TrackID, "new_track": false}, nil
}

type DuplicateResolveNode struct{}

func (n *DuplicateResolveNode) Name() string { return "duplicate_resolve" }

func (n *DuplicateResolveNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
	if msg == nil {
		return nil, fmt.Errorf("message not found in context")
	}

	return NodeOutput{"message": msg, "action": "duplicate_resolved", "resolution": "keep_latest", "track_id": msg.TrackID}, nil
}

type ResolveConflictNode struct{}

func (n *ResolveConflictNode) Name() string { return "resolve_conflict" }

func (n *ResolveConflictNode) Execute(ctx NodeContext) (NodeOutput, error) {
	conflictType := getStringField(ctx.Data, "anomaly_type")
	resolvedID := fmt.Sprintf("TRK%d", int(time.Now().UnixNano()%10000))

	return NodeOutput{"conflict_type": conflictType, "resolved_id": resolvedID, "resolution": "new_id_assigned"}, nil
}

type ClampFieldNode struct{}

func (n *ClampFieldNode) Name() string { return "clamp_field" }

func (n *ClampFieldNode) Execute(ctx NodeContext) (NodeOutput, error) {
	msg := extractMessage(ctx.Data)
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
	} else if msg.Altitude > 50000 {
		msg.Altitude = 50000
	}

	msg.Course = math.Mod(msg.Course, 360)
	if msg.Course < 0 {
		msg.Course += 360
	}

	return NodeOutput{"message": msg, "clamped": true, "clamp_type": "range_normalized"}, nil
}

type LogResultNode struct{}

func (n *LogResultNode) Name() string { return "log_result" }

func (n *LogResultNode) Execute(ctx NodeContext) (NodeOutput, error) {
	result := getStringField(ctx.Data, "result")
	if result == "" {
		result = "unknown"
	}

	return NodeOutput{"result": result, "logged": true, "timestamp": time.Now().Format(time.RFC3339)}, nil
}

type RetainEntryNode struct {
	db *model.TrackDB
}

func NewRetainEntryNode(db *model.TrackDB) *RetainEntryNode {
	return &RetainEntryNode{db: db}
}

func (n *RetainEntryNode) Name() string { return "retain_entry" }

func (n *RetainEntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	trackID := getStringField(ctx.Data, "track_id")

	return NodeOutput{"track_id": trackID, "action": "retained", "track_count": n.db.Size()}, nil
}

type DeleteEntryNode struct {
	db *model.TrackDB
}

func NewDeleteEntryNode(db *model.TrackDB) *DeleteEntryNode {
	return &DeleteEntryNode{db: db}
}

func (n *DeleteEntryNode) Name() string { return "delete_entry" }

func (n *DeleteEntryNode) Execute(ctx NodeContext) (NodeOutput, error) {
	trackID := getStringField(ctx.Data, "track_id")

	if err := n.db.Delete(trackID); err != nil {
		return NodeOutput{"track_id": trackID, "action": "delete_failed", "reason": err.Error()}, nil
	}

	return NodeOutput{"track_id": trackID, "action": "deleted", "track_count": n.db.Size()}, nil
}
