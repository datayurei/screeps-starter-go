// Package brain is the bot's decision logic. It reads the game state JS packed
// into lib.Ctx through typed row wrappers (creep.Hits(), source.Energy()) and
// queues actions on them (creep.Harvest(src)). It does NOT import syscall/js, so
// a tick costs zero JS<->WASM crossings and the package is testable with
// `go test`.
//
// Reads are an instant snapshot taken at the start of the tick. Actions are
// queued and run after the last round; their result codes arrive next tick
// (c.PrevResult). The plumbing, wire layout and wrappers live in package lib
// (dot-imported here so names stay unqualified); lib/gen.go and lib/obj_gen.go are
// generated from tools/spec_*.go. Put your own logic in this package.
package brain

import (
	"strconv"

	. "screeps/lib"
)

// Preset indices, defined by PRESETS in js/host.js.
const (
	MoveDefault   Preset = 0
	MoveGreenPath Preset = 1

	SpawnDefault   Preset = 0
	SpawnHarvester Preset = 1 // memory { role: "harvester" }
)

var workerBody = []int32{PartWork, PartWork, PartCarry, PartMove}
var haulerBody = []int32{PartCarry, PartWork, PartMove, PartMove}

// Tick is called once per round; this bot never asks queries, so it acts in
// round 0 only. See Ctx.Answer for how multi-round (query) logic works.
func Tick(c *Ctx, round int) {
	if round != 0 {
		return
	}

	game := c.Game()
	spawns := game.Spawns()
	if len(spawns) == 0 {
		return // no spawn owned: nothing to do
	}
	spawn := spawns[0]

	creeps := game.Creeps()
	if len(creeps) == 0 && spawn.Spawning() == 0 {
		spawn.SpawnCreep(workerBody, "harvester_"+strconv.Itoa(int(game.Time)), SpawnHarvester)
	}

	sources := Filter(c.Sources(), func(s Source) bool { return s.Energy() > 0 })
	for _, cr := range creeps {
		if !cr.IsSpawning() {
			runHarvester(cr, sources)
		}
	}
}

// runHarvester: harvest the nearest active source in the room until full,
// then upgrade the room's controller.
func runHarvester(cr Creep, sources []Source) {
	if cr.FreeCapacity() > 0 {
		if src, ok := Closest(cr, sources); ok { // same room: creeps can't harvest across rooms
			cr.HarvestApproach(src, MoveGreenPath)
		}
		return
	}
	if room := cr.Room(); room.Valid() {
		if ctrl := room.Controller(); ctrl >= 0 {
			cr.UpgradeControllerApproach(ctrl, MoveGreenPath)
		}
	}
}
