package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     Link-16 PPLI Processing Workflow Engine                 ║")
	fmt.Println("║     Tactical Data Link Message Processing System v1.0.1     ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	registry := node.NewNodeRegistry()
	trackDB := model.NewTrackDB()

	registerPPLINodes(registry, trackDB)
	registerCommonNodes(registry, trackDB)

	fmt.Println("[Main] Registered nodes:")
	for _, name := range registry.List() {
		fmt.Printf("  - %s\n", name)
	}
	fmt.Println()

	engine := workflow.NewEngine(registry)

	workflowDef, err := engine.LoadWorkflow("config/ppli_stages.json")
	if err != nil {
		log.Fatalf("[Main] Failed to load workflow: %v", err)
	}

	fmt.Println()

	instance := engine.NewInstance(workflowDef)

	// Scenario A: Send Preparation -> Send
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("SCENARIO A: Send Preparation and Transmission (Auto Trigger)")
	fmt.Println(strings.Repeat("=", 70))

	prepareData := map[string]interface{}{
		"track_id":  "TRK001",
		"source_ju": "JU001",
		"latitude":  35.6812,
		"longitude": 139.7671,
		"altitude":  10000.0,
		"speed":     300.0,
		"course":    90.0,
	}
	engine.InjectToken(instance, "prepare_send", prepareData)
	if err := engine.Run(instance, "prepare_send"); err != nil {
		log.Printf("[Main] prepare_send error: %v", err)
	}

	sendData := map[string]interface{}{
		"trigger_type": "auto",
		"npg_number":   6,
	}
	engine.InjectToken(instance, "send", sendData)
	if err := engine.Run(instance, "send"); err != nil {
		log.Printf("[Main] send error: %v", err)
	}

	// Scenario B: Receive NEW track
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("SCENARIO B: Receive NEW J2.2 Message (Correlation = New)")
	fmt.Println(strings.Repeat("=", 70))

	newMsg := &model.PPLIMessage{
		TrackID:       "NEW_TRK100",
		SourceJU:      "JU002",
		TrackNum:      1100,
		PlatformType:  model.PlatformAir,
		Latitude:      35.6900,
		Longitude:     139.7700,
		Altitude:      8500,
		Speed:         275,
		Course:        95,
		Identity:      "FRIEND",
		TimeOfTrack:   time.Now().Add(-2 * time.Second),
		TimeOfMessage: time.Now().Add(-2 * time.Second),
		NPGNumber:     6,
		MessageType:   "J2.2",
		Valid:         true,
	}
	receiveData := map[string]interface{}{
		"message":          newMsg,
		"correlation_mode": "auto",
	}
	engine.InjectToken(instance, "receive", receiveData)
	if err := engine.Run(instance, "receive"); err != nil {
		log.Printf("[Main] receive (new) error: %v", err)
	}

	// Scenario C: Receive UPDATE track
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("SCENARIO C: Receive UPDATE J2.2 Message (Correlation = Update)")
	fmt.Println(strings.Repeat("=", 70))

	updateMsg := &model.PPLIMessage{
		TrackID:       "NEW_TRK100",
		SourceJU:      "JU002",
		TrackNum:      1100,
		PlatformType:  model.PlatformAir,
		Latitude:      35.7000,
		Longitude:     139.7800,
		Altitude:      8200,
		Speed:         280,
		Course:        100,
		Identity:      "FRIEND",
		TimeOfTrack:   time.Now().Add(-1 * time.Second),
		TimeOfMessage: time.Now().Add(-1 * time.Second),
		NPGNumber:     6,
		MessageType:   "J2.2",
		Valid:         true,
	}
	receiveUpdateData := map[string]interface{}{
		"message":          updateMsg,
		"correlation_mode": "auto",
	}
	engine.InjectToken(instance, "receive", receiveUpdateData)
	if err := engine.Run(instance, "receive"); err != nil {
		log.Printf("[Main] receive (update) error: %v", err)
	}

	// Scenario D: Receive DUPLICATE track
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("SCENARIO D: Receive DUPLICATE J2.2 Message (Correlation = Duplicate)")
	fmt.Println(strings.Repeat("=", 70))

	dupMsg := &model.PPLIMessage{
		TrackID:       "NEW_TRK100",
		SourceJU:      "JU002",
		TrackNum:      1100,
		PlatformType:  model.PlatformAir,
		Latitude:      35.6900,
		Longitude:     139.7700,
		Altitude:      8500,
		Speed:         275,
		Course:        95,
		Identity:      "FRIEND",
		TimeOfTrack:   time.Now().Add(-2 * time.Second),
		TimeOfMessage: time.Now().Add(-2 * time.Second),
		NPGNumber:     6,
		MessageType:   "J2.2",
		Valid:         true,
	}
	receiveDupData := map[string]interface{}{
		"message":          dupMsg,
		"correlation_mode": "auto",
	}
	engine.InjectToken(instance, "receive", receiveDupData)
	if err := engine.Run(instance, "receive"); err != nil {
		log.Printf("[Main] receive (duplicate) error: %v", err)
	}

	// Scenario E: Clear Expired Tracks
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("SCENARIO E: Clear Expired Tracks")
	fmt.Println(strings.Repeat("=", 70))

	expiredMsg := &model.PPLIMessage{
		TrackID:       "TRK_EXPIRED",
		SourceJU:      "JU003",
		TrackNum:      2000,
		PlatformType:  model.PlatformSurface,
		Latitude:      35.5000,
		Longitude:     139.5000,
		Altitude:      0,
		Speed:         0,
		Course:        0,
		Identity:      "FRIEND",
		TimeOfTrack:   time.Now().Add(-10 * time.Minute),
		TimeOfMessage: time.Now().Add(-10 * time.Minute),
		NPGNumber:     6,
		MessageType:   "J2.2",
		Valid:         true,
	}
	_ = trackDB.Create("TRK_EXPIRED", expiredMsg)
	trackDB.SetTTL("TRK_EXPIRED", 60)

	fmt.Printf("\n[Main] Added expired track TRK_EXPIRED (TTL: 60s, age: 10min)\n")
	fmt.Printf("[Main] Track database size before clear: %d\n", trackDB.Size())

	clearData := map[string]interface{}{
		"track_id":  "TRK_EXPIRED",
		"source_ju": "JU003",
	}
	engine.InjectToken(instance, "clear", clearData)
	if err := engine.Run(instance, "clear"); err != nil {
		log.Printf("[Main] clear error: %v", err)
	}

	// Final State
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("FINAL TRACK DATABASE STATE")
	fmt.Println(strings.Repeat("=", 70))

	allTracks := trackDB.GetAll()
	fmt.Printf("\nTotal tracks in database: %d\n\n", len(allTracks))

	if len(allTracks) == 0 {
		fmt.Println("  (no tracks)")
	} else {
		fmt.Println("┌─────────────┬────────────┬─────────────┬────────────┬────────────┬─────────┐")
		fmt.Println("│ Track ID    │ Source JU  │ Latitude    │ Longitude  │ Altitude   │ Status  │")
		fmt.Println("├─────────────┼────────────┼─────────────┼────────────┼────────────┼─────────┤")
		for trackID, entry := range allTracks {
			fmt.Printf("│ %-11s │ %-10s │ %-11.4f │ %-10.4f │ %-10.1f │ %-7s │\n",
				trackID, entry.SourceJU, entry.Latitude, entry.Longitude, entry.Altitude, entry.Status)
		}
		fmt.Println("└─────────────┴────────────┴─────────────┴────────────┴────────────┴─────────┘")
	}

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("Workflow Engine Simulation Complete")
	fmt.Println(strings.Repeat("=", 70))
}

func registerPPLINodes(registry *node.NodeRegistry, db *model.TrackDB) {
	registry.Register(&node.InitPPLIDataNode{})
	registry.Register(&node.ClassifyPlatformNode{})
	registry.Register(&node.EncodePPLIMessageNode{})
	registry.Register(node.NewPPLICorrelateNode(db))
	registry.Register(node.NewStorePPLINode(db))
	registry.Register(node.NewCreateEntryNode(db))
	registry.Register(node.NewUpdateEntryNode(db))
	registry.Register(&node.DuplicateResolveNode{})
	registry.Register(&node.ResolveConflictNode{})
	registry.Register(&node.ClampFieldNode{})
	registry.Register(&node.LogResultNode{})
	registry.Register(node.NewRetainEntryNode(db))
	registry.Register(node.NewDeleteEntryNode(db))
}

func registerCommonNodes(registry *node.NodeRegistry, db *model.TrackDB) {
	registry.Register(&node.ValidateMessageNode{})
	registry.Register(&node.CheckSendConditionNode{})
	registry.Register(&node.AssignNPGSlotNode{})
	registry.Register(&node.TransmitNode{})
	registry.Register(&node.DecodeMessageNode{})
	registry.Register(&node.ReceiveFilterNode{})
	registry.Register(node.NewCheckTTLNode(db))
	registry.Register(node.NewCheckSourceJUNode(db))
	registry.Register(&node.DetectAnomalyNode{})
	registry.Register(&node.TimerTriggerNode{})
}
