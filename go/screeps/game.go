package screeps

import "syscall/js"

// The Game and Memory globals are re-read on every call: the JS loader
// re-bridges them onto globalThis each tick, so a cached value would go stale.
// If you call several of these per tick, each costs one extra crossing to
// fetch Game; that is usually negligible next to the call itself.

func game() js.Value { return js.Global().Get("Game") }

// Time is Game.time, the current game tick.
func Time() int { return game().Get("time").Int() }

// Creeps returns all your creeps (Game.creeps), in Object.keys order.
func Creeps() []Creep {
	all := game().Get("creeps")
	names := Keys(all)
	out := make([]Creep, len(names))
	for i, n := range names {
		out[i] = Creep{RoomObject{all.Get(n)}}
	}
	return out
}

// CreepCount is the number of your creeps (cheaper than len(Creeps())).
func CreepCount() int {
	return jsObject.Call("keys", game().Get("creeps")).Length()
}

// GetCreep returns Game.creeps[name].
func GetCreep(name string) (Creep, bool) {
	v := game().Get("creeps").Get(name)
	return Creep{RoomObject{v}}, v.Truthy()
}

// Spawns returns all your spawns (Game.spawns), in Object.keys order.
func Spawns() []StructureSpawn {
	all := game().Get("spawns")
	names := Keys(all)
	out := make([]StructureSpawn, len(names))
	for i, n := range names {
		out[i] = StructureSpawn{OwnedStructure{Structure{RoomObject{all.Get(n)}}}}
	}
	return out
}

// GetSpawn returns Game.spawns[name].
func GetSpawn(name string) (StructureSpawn, bool) {
	v := game().Get("spawns").Get(name)
	return StructureSpawn{OwnedStructure{Structure{RoomObject{v}}}}, v.Truthy()
}

// Rooms returns every room you have visibility in (Game.rooms).
func Rooms() []Room {
	all := game().Get("rooms")
	names := Keys(all)
	out := make([]Room, len(names))
	for i, n := range names {
		out[i] = Room{all.Get(n)}
	}
	return out
}

// GetRoom returns Game.rooms[name]; ok is false without visibility.
func GetRoom(name string) (Room, bool) {
	v := game().Get("rooms").Get(name)
	return Room{v}, v.Truthy()
}

// GetObjectByID is Game.getObjectById. The result is raw because the type
// depends on the id; wrap it yourself, e.g. screeps.AsSource(v).
func GetObjectByID(id string) (js.Value, bool) {
	v := game().Call("getObjectById", id)
	return v, v.Truthy()
}

// CPU is Game.cpu.
func CPU() CPUInfo { return CPUInfo{game().Get("cpu")} }

// CPUInfo wraps Game.cpu.
type CPUInfo struct{ v js.Value }

func (c CPUInfo) JSValue() js.Value { return c.v }

// Limit is your assigned CPU limit for the current shard.
func (c CPUInfo) Limit() int { return intOr(c.v.Get("limit"), 0) }

// TickLimit is the CPU available this tick, including the bucket.
func (c CPUInfo) TickLimit() int { return intOr(c.v.Get("tickLimit"), 0) }

// Bucket is the unused CPU accumulated in your bucket.
func (c CPUInfo) Bucket() int { return intOr(c.v.Get("bucket"), 0) }

// GetUsed is Game.cpu.getUsed(): CPU used since the start of this tick.
func (c CPUInfo) GetUsed() float64 { return c.v.Call("getUsed").Float() }

// Memory is the global Memory object. It's an untyped JSON tree, so it is
// exposed raw; see Creep.Memory for per-creep memory.
func Memory() js.Value { return js.Global().Get("Memory") }

// CleanDeadCreepMemory deletes Memory.creeps entries whose creep no longer
// exists. Call once per tick (or every N ticks) to keep Memory from growing
// without bound as creeps die. Returns the number of entries removed.
func CleanDeadCreepMemory() int {
	mem := Memory()
	if !mem.Truthy() {
		return 0
	}
	creepsMem := mem.Get("creeps")
	if !creepsMem.Truthy() {
		return 0
	}
	live := game().Get("creeps")
	removed := 0
	for _, name := range Keys(creepsMem) {
		if !live.Get(name).Truthy() {
			creepsMem.Delete(name)
			removed++
		}
	}
	return removed
}
