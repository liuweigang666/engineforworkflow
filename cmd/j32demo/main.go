// Command j32demo demonstrates the J3.2 Air Track message-type extension
// (TODO C14): the same five processing stages, driven by the declarative
// workflow in config/j32_stages.json, with zero changes to the engine core.
package main

import (
	"fmt"
	"log"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

func main() {
	db := model.NewJ32TrackDB()
	engine := newJ32Engine(db)
	def, err := engine.LoadWorkflow("config/j32_stages.json")
	if err != nil {
		log.Fatalf("load j32 workflow: %v", err)
	}
	inst := engine.NewInstance(def)

	// Scenario A: send preparation + transmission.
	engine.InjectToken(inst, "prepare_send", map[string]interface{}{
		"track_id": "ATK001", "source_ju": "JU004", "latitude": 35.68, "longitude": 139.77,
		"altitude": 8000, "heading": 90, "speed": 420,
	})
	if err := engine.Run(inst, "prepare_send"); err != nil {
		log.Fatalf("prepare_send: %v", err)
	}
	engine.InjectToken(inst, "send", map[string]interface{}{"trigger_type": "auto", "npg_number": 6})
	if err := engine.Run(inst, "send"); err != nil {
		log.Fatalf("send: %v", err)
	}

	// Scenario B: receive new air track.
	b := j32Msg("ATK_NEW01", -2*time.Second, 35.69, 139.77, 8500, 95, 400)
	engine.InjectToken(inst, "receive", map[string]interface{}{"message": b, "correlation_mode": "auto"})
	if err := engine.Run(inst, "receive"); err != nil {
		log.Fatalf("receive new: %v", err)
	}

	// Scenario C: receive track update.
	c := j32Msg("ATK_NEW01", -1*time.Second, 35.71, 139.80, 8000, 100, 410)
	engine.InjectToken(inst, "receive", map[string]interface{}{"message": c, "correlation_mode": "auto"})
	if err := engine.Run(inst, "receive"); err != nil {
		log.Fatalf("receive update: %v", err)
	}

	// Scenario D: receive duplicate (older than the stored update).
	d := j32Msg("ATK_NEW01", -4*time.Second, 35.69, 139.77, 8500, 95, 400)
	engine.InjectToken(inst, "receive", map[string]interface{}{"message": d, "correlation_mode": "auto"})
	if err := engine.Run(inst, "receive"); err != nil {
		log.Fatalf("receive duplicate: %v", err)
	}

	// Scenario E: clear expired air track.
	exp := j32Msg("ATK_EXP", -10*time.Minute, 35.5, 139.5, 5000, 90, 300)
	_ = db.Create("ATK_EXP", exp)
	_ = db.SetTTL("ATK_EXP", 60)
	engine.InjectToken(inst, "clear", map[string]interface{}{"track_id": "ATK_EXP", "source_ju": "JU004"})
	if err := engine.Run(inst, "clear"); err != nil {
		log.Fatalf("clear: %v", err)
	}

	fmt.Println("\nJ3.2 demo complete. Final air-track database state:")
	for id := range db.GetAll() {
		entry, _ := db.Get(id)
		fmt.Printf("  %-10s lat=%.4f lon=%.4f alt=%.0f v%d\n", id, entry.Latitude, entry.Longitude, entry.Altitude, entry.Version)
	}
}

func newJ32Engine(db *model.J32TrackDB) *workflow.Engine {
	registry := node.NewNodeRegistry()
	nodes := []node.Node{
		&node.InitJ32DataNode{}, &node.ClassifyJ32Node{}, &node.EncodeJ32MessageNode{},
		&node.ValidateJ32MessageNode{}, &node.DecodeJ32MessageNode{}, &node.J32ReceiveFilterNode{},
		node.NewJ32CorrelateNode(db), node.NewCreateJ32EntryNode(db), node.NewUpdateJ32EntryNode(db),
		&node.DuplicateJ32ResolveNode{}, node.NewStoreJ32Node(db), node.NewCheckTTLJ32Node(db),
		node.NewRetainJ32EntryNode(db), node.NewDeleteJ32EntryNode(db), &node.J32DetectAnomalyNode{},
		&node.J32ClampFieldNode{},
		// Shared/generic nodes reused across message types.
		&node.CheckSendConditionNode{}, &node.AssignNPGSlotNode{}, &node.TransmitNode{},
		&node.CheckSourceJUNode{}, &node.ResolveConflictNode{}, &node.LogResultNode{},
		&node.TimerTriggerNode{},
	}
	for _, n := range nodes {
		if err := registry.Register(n); err != nil {
			log.Fatalf("register %s: %v", n.Name(), err)
		}
	}
	return workflow.NewEngine(registry)
}

func j32Msg(id string, age time.Duration, lat, lon, alt, hdg, spd float64) *model.J32Message {
	return &model.J32Message{
		TrackID:        id,
		SourceJU:       "JU004",
		Latitude:       lat,
		Longitude:      lon,
		Altitude:       alt,
		Heading:        hdg,
		Speed:          spd,
		Identity:       "FRIEND",
		Classification: "AIR",
		TimeOfTrack:    time.Now().Add(age),
		TimeOfMessage:  time.Now().Add(age),
		NPGNumber:      6,
		MessageType:    "J3.2",
		Valid:          true,
	}
}
