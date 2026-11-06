package screeps

import "syscall/js"

// Wrap raw values (e.g. from GetObjectByID or Room.Find) into typed objects.
// These don't check the actual type; wrap only what you know the value is.

func AsRoomObject(v js.Value) RoomObject { return RoomObject{v} }
func AsCreep(v js.Value) Creep           { return Creep{RoomObject{v}} }
func AsSource(v js.Value) Source         { return Source{RoomObject{v}} }
func AsStructure(v js.Value) Structure   { return Structure{RoomObject{v}} }
func AsSpawn(v js.Value) StructureSpawn {
	return StructureSpawn{OwnedStructure{Structure{RoomObject{v}}}}
}
func AsController(v js.Value) StructureController {
	return StructureController{OwnedStructure{Structure{RoomObject{v}}}}
}
func AsConstructionSite(v js.Value) ConstructionSite { return ConstructionSite{RoomObject{v}} }
func AsResource(v js.Value) Resource                 { return Resource{RoomObject{v}} }

// ---- RoomObject ---------------------------------------------------------------

// RoomObject is the base of everything with a position in a room.
type RoomObject struct{ v js.Value }

func (o RoomObject) JSValue() js.Value { return o.v }

// Exists reports whether the wrapped value is a real object (not null/undefined).
func (o RoomObject) Exists() bool { return o.v.Truthy() }

func (o RoomObject) Pos() RoomPosition { return RoomPosition{o.v.Get("pos")} }

// Room is the object's room. Can be missing for flags/sites in rooms without
// visibility; check Room.Exists.
func (o RoomObject) Room() Room { return Room{o.v.Get("room")} }

// ID is the object's unique id ("" for objects without one, e.g. flags).
func (o RoomObject) ID() string { return stringOr(o.v.Get("id"), "") }

// ---- Store --------------------------------------------------------------------

// Store is a resource container (creep.store, structure.store).
type Store struct{ v js.Value }

func (s Store) JSValue() js.Value { return s.v }

// GetFreeCapacity is store.getFreeCapacity(resource). Pass "" for the
// no-argument form (total free capacity for general-purpose stores).
// Returns 0 where the API returns null.
func (s Store) GetFreeCapacity(r ResourceType) int {
	if r == "" {
		return intOr(s.v.Call("getFreeCapacity"), 0)
	}
	return intOr(s.v.Call("getFreeCapacity", string(r)), 0)
}

// GetUsedCapacity is store.getUsedCapacity(resource); "" for the total.
func (s Store) GetUsedCapacity(r ResourceType) int {
	if r == "" {
		return intOr(s.v.Call("getUsedCapacity"), 0)
	}
	return intOr(s.v.Call("getUsedCapacity", string(r)), 0)
}

// GetCapacity is store.getCapacity(resource); "" for the total.
func (s Store) GetCapacity(r ResourceType) int {
	if r == "" {
		return intOr(s.v.Call("getCapacity"), 0)
	}
	return intOr(s.v.Call("getCapacity", string(r)), 0)
}

// Amount is store[resource] (0 if none).
func (s Store) Amount(r ResourceType) int { return intOr(s.v.Get(string(r)), 0) }

// ---- Creep --------------------------------------------------------------------

type Creep struct{ RoomObject }

func (c Creep) Name() string     { return c.v.Get("name").String() }
func (c Creep) My() bool         { return c.v.Get("my").Bool() }
func (c Creep) Spawning() bool   { return c.v.Get("spawning").Truthy() }
func (c Creep) Hits() int        { return intOr(c.v.Get("hits"), 0) }
func (c Creep) HitsMax() int     { return intOr(c.v.Get("hitsMax"), 0) }
func (c Creep) Fatigue() int     { return intOr(c.v.Get("fatigue"), 0) }
func (c Creep) TicksToLive() int { return intOr(c.v.Get("ticksToLive"), 0) }
func (c Creep) Store() Store     { return Store{c.v.Get("store")} }

// Memory is creep.memory (Memory.creeps[name]), an untyped JSON object.
func (c Creep) Memory() js.Value { return c.v.Get("memory") }

// GetActiveBodyparts counts the creep's undamaged parts of type p.
func (c Creep) GetActiveBodyparts(p BodyPart) int {
	return intOr(c.v.Call("getActiveBodyparts", string(p)), 0)
}

func (c Creep) Harvest(t Target) ErrorCode { return errCode(c.v.Call("harvest", t.JSValue())) }
func (c Creep) Build(site ConstructionSite) ErrorCode {
	return errCode(c.v.Call("build", site.v))
}
func (c Creep) Repair(t Target) ErrorCode { return errCode(c.v.Call("repair", t.JSValue())) }
func (c Creep) UpgradeController(ctrl StructureController) ErrorCode {
	return errCode(c.v.Call("upgradeController", ctrl.v))
}
func (c Creep) Attack(t Target) ErrorCode { return errCode(c.v.Call("attack", t.JSValue())) }
func (c Creep) RangedAttack(t Target) ErrorCode {
	return errCode(c.v.Call("rangedAttack", t.JSValue()))
}
func (c Creep) Heal(t Target) ErrorCode     { return errCode(c.v.Call("heal", t.JSValue())) }
func (c Creep) Pickup(r Resource) ErrorCode { return errCode(c.v.Call("pickup", r.v)) }

// Transfer moves all of resource r to t. Use TransferAmount for a partial transfer.
func (c Creep) Transfer(t Target, r ResourceType) ErrorCode {
	return errCode(c.v.Call("transfer", t.JSValue(), string(r)))
}
func (c Creep) TransferAmount(t Target, r ResourceType, amount int) ErrorCode {
	return errCode(c.v.Call("transfer", t.JSValue(), string(r), amount))
}

// Withdraw takes all of resource r from t. Use WithdrawAmount for a partial withdraw.
func (c Creep) Withdraw(t Target, r ResourceType) ErrorCode {
	return errCode(c.v.Call("withdraw", t.JSValue(), string(r)))
}
func (c Creep) WithdrawAmount(t Target, r ResourceType, amount int) ErrorCode {
	return errCode(c.v.Call("withdraw", t.JSValue(), string(r), amount))
}
func (c Creep) Drop(r ResourceType) ErrorCode { return errCode(c.v.Call("drop", string(r))) }

func (c Creep) Move(d Direction) ErrorCode { return errCode(c.v.Call("move", int(d))) }

// MoveTo pathfinds toward t. opts may be nil for engine defaults.
func (c Creep) MoveTo(t Target, opts *MoveToOpts) ErrorCode {
	if opts == nil {
		return errCode(c.v.Call("moveTo", t.JSValue()))
	}
	return errCode(c.v.Call("moveTo", t.JSValue(), opts.js()))
}

func (c Creep) Say(msg string, public bool) ErrorCode {
	return errCode(c.v.Call("say", msg, public))
}
func (c Creep) Suicide() ErrorCode { return errCode(c.v.Call("suicide")) }

// MoveToOpts is the subset of moveTo options most bots use. Zero values mean
// "engine default" and are not sent.
type MoveToOpts struct {
	ReusePath       int    // ticks to reuse a found path (engine default 5)
	Range           int    // stop within this range of the target
	IgnoreCreeps    bool   // treat other creeps as passable when pathing
	VisualizeStroke string // if set, draw the path in this color (e.g. "#00ff88")
}

func (o *MoveToOpts) js() js.Value {
	obj := NewObject()
	if o.ReusePath != 0 {
		obj.Set("reusePath", o.ReusePath)
	}
	if o.Range != 0 {
		obj.Set("range", o.Range)
	}
	if o.IgnoreCreeps {
		obj.Set("ignoreCreeps", true)
	}
	if o.VisualizeStroke != "" {
		style := NewObject()
		style.Set("stroke", o.VisualizeStroke)
		obj.Set("visualizePathStyle", style)
	}
	return obj
}

// ---- Source / Resource / ConstructionSite -------------------------------------

type Source struct{ RoomObject }

func (s Source) Energy() int              { return intOr(s.v.Get("energy"), 0) }
func (s Source) EnergyCapacity() int      { return intOr(s.v.Get("energyCapacity"), 0) }
func (s Source) TicksToRegeneration() int { return intOr(s.v.Get("ticksToRegeneration"), 0) }

// Resource is a dropped resource pile.
type Resource struct{ RoomObject }

func (r Resource) Amount() int { return intOr(r.v.Get("amount"), 0) }
func (r Resource) ResourceType() ResourceType {
	return ResourceType(stringOr(r.v.Get("resourceType"), ""))
}

type ConstructionSite struct{ RoomObject }

func (s ConstructionSite) My() bool           { return s.v.Get("my").Bool() }
func (s ConstructionSite) Progress() int      { return intOr(s.v.Get("progress"), 0) }
func (s ConstructionSite) ProgressTotal() int { return intOr(s.v.Get("progressTotal"), 0) }
func (s ConstructionSite) StructureType() StructureType {
	return StructureType(stringOr(s.v.Get("structureType"), ""))
}
func (s ConstructionSite) Remove() ErrorCode { return errCode(s.v.Call("remove")) }

// ---- Structures ---------------------------------------------------------------

type Structure struct{ RoomObject }

func (s Structure) StructureType() StructureType {
	return StructureType(stringOr(s.v.Get("structureType"), ""))
}
func (s Structure) Hits() int    { return intOr(s.v.Get("hits"), 0) }
func (s Structure) HitsMax() int { return intOr(s.v.Get("hitsMax"), 0) }

// Store is structure.store; check Store.JSValue().Truthy() for structures without one.
func (s Structure) Store() Store { return Store{s.v.Get("store")} }

type OwnedStructure struct{ Structure }

func (s OwnedStructure) My() bool { return s.v.Get("my").Bool() }

type StructureSpawn struct{ OwnedStructure }

func (s StructureSpawn) Name() string { return s.v.Get("name").String() }

// Spawning reports whether the spawn is currently building a creep.
func (s StructureSpawn) Spawning() bool { return s.v.Get("spawning").Truthy() }

// SpawnCreep starts spawning a creep. memory may be nil; otherwise it becomes
// the creep's Memory.creeps[name] (values: string, int, float64, bool, nested
// map[string]any / []any).
func (s StructureSpawn) SpawnCreep(body []BodyPart, name string, memory map[string]any) ErrorCode {
	parts := jsArray.New(len(body))
	for i, p := range body {
		parts.SetIndex(i, string(p))
	}
	if memory == nil {
		return errCode(s.v.Call("spawnCreep", parts, name))
	}
	opts := NewObject()
	opts.Set("memory", js.ValueOf(memory))
	return errCode(s.v.Call("spawnCreep", parts, name, opts))
}

type StructureController struct{ OwnedStructure }

func (c StructureController) Level() int         { return intOr(c.v.Get("level"), 0) }
func (c StructureController) Progress() int      { return intOr(c.v.Get("progress"), 0) }
func (c StructureController) ProgressTotal() int { return intOr(c.v.Get("progressTotal"), 0) }
func (c StructureController) TicksToDowngrade() int {
	return intOr(c.v.Get("ticksToDowngrade"), 0)
}

// ---- Room ---------------------------------------------------------------------

type Room struct{ v js.Value }

func (r Room) JSValue() js.Value { return r.v }
func (r Room) Exists() bool      { return r.v.Truthy() }
func (r Room) Name() string      { return stringOr(r.v.Get("name"), "") }

func (r Room) EnergyAvailable() int         { return intOr(r.v.Get("energyAvailable"), 0) }
func (r Room) EnergyCapacityAvailable() int { return intOr(r.v.Get("energyCapacityAvailable"), 0) }

// Controller is room.controller; ok is false for rooms without one.
func (r Room) Controller() (StructureController, bool) {
	v := r.v.Get("controller")
	return AsController(v), v.Truthy()
}

// Find is room.find(c), returning raw values. Prefer the typed finders below;
// use this plus an As* wrapper for anything they don't cover.
func (r Room) Find(c FindConstant) []js.Value {
	return wrapArray(r.v.Call("find", int(c)), func(v js.Value) js.Value { return v })
}

func (r Room) FindSources() []Source { return wrapArray(r.v.Call("find", int(FindSources)), AsSource) }
func (r Room) FindActiveSources() []Source {
	return wrapArray(r.v.Call("find", int(FindSourcesActive)), AsSource)
}
func (r Room) FindMyCreeps() []Creep { return wrapArray(r.v.Call("find", int(FindMyCreeps)), AsCreep) }
func (r Room) FindHostileCreeps() []Creep {
	return wrapArray(r.v.Call("find", int(FindHostileCreeps)), AsCreep)
}
func (r Room) FindMySpawns() []StructureSpawn {
	return wrapArray(r.v.Call("find", int(FindMySpawns)), AsSpawn)
}
func (r Room) FindStructures() []Structure {
	return wrapArray(r.v.Call("find", int(FindStructures)), AsStructure)
}
func (r Room) FindMyStructures() []Structure {
	return wrapArray(r.v.Call("find", int(FindMyStructures)), AsStructure)
}
func (r Room) FindConstructionSites() []ConstructionSite {
	return wrapArray(r.v.Call("find", int(FindMyConstructionSites)), AsConstructionSite)
}
func (r Room) FindDroppedResources() []Resource {
	return wrapArray(r.v.Call("find", int(FindDroppedResources)), AsResource)
}

// CreateConstructionSite places a construction site at (x, y).
func (r Room) CreateConstructionSite(x, y int, t StructureType) ErrorCode {
	return errCode(r.v.Call("createConstructionSite", x, y, string(t)))
}

// ---- RoomPosition -------------------------------------------------------------

type RoomPosition struct{ v js.Value }

// NewRoomPosition is `new RoomPosition(x, y, roomName)`.
func NewRoomPosition(x, y int, roomName string) RoomPosition {
	return RoomPosition{js.Global().Get("RoomPosition").New(x, y, roomName)}
}

func (p RoomPosition) JSValue() js.Value { return p.v }
func (p RoomPosition) X() int            { return p.v.Get("x").Int() }
func (p RoomPosition) Y() int            { return p.v.Get("y").Int() }
func (p RoomPosition) RoomName() string  { return p.v.Get("roomName").String() }

func (p RoomPosition) GetRangeTo(t Target) int { return intOr(p.v.Call("getRangeTo", t.JSValue()), 0) }
func (p RoomPosition) InRangeTo(t Target, r int) bool {
	return p.v.Call("inRangeTo", t.JSValue(), r).Bool()
}
func (p RoomPosition) IsNearTo(t Target) bool  { return p.v.Call("isNearTo", t.JSValue()).Bool() }
func (p RoomPosition) IsEqualTo(t Target) bool { return p.v.Call("isEqualTo", t.JSValue()).Bool() }

// ClosestByPath returns the element of targets with the shortest path from
// `from` (pos.findClosestByPath). ok is false if none is reachable or the
// slice is empty. Generic, so it returns your concrete type:
//
//	src, ok := screeps.ClosestByPath(creep.Pos(), room.FindActiveSources())
func ClosestByPath[T Target](from RoomPosition, targets []T) (T, bool) {
	if len(targets) == 0 {
		var zero T
		return zero, false
	}
	return pick(targets, from.v.Call("findClosestByPath", toArray(targets)))
}

// ClosestByRange is like ClosestByPath but uses linear range (much cheaper:
// no pathfinding).
func ClosestByRange[T Target](from RoomPosition, targets []T) (T, bool) {
	if len(targets) == 0 {
		var zero T
		return zero, false
	}
	return pick(targets, from.v.Call("findClosestByRange", toArray(targets)))
}
