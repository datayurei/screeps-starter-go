package main

// Spec, part 2: ACTIONS (ops), engine QUERIES, NATIVE helpers, and entries
// deliberately left in JS. Signature DSL:
//
//	Class.method[#tag](argKind, ...) [@approach]
//
// arg kinds: h handle · i int · io optional int (-1 = undefined) · b bool ·
// res resource · reso optional resource (-1) · st structure type · dir ·
// str string · rn room name · arr int[] · parts body parts · path (x,y,dir)* ·
// pos position · milli float*1000 · harr handle[] · preset:NAME (JS-side options
// object chosen by index). `#tag` names an overload. `@approach` adds a variant
// that moves toward the target on ERR_NOT_IN_RANGE. For RoomPosition methods the
// first arg (a pos) is the receiver.

type opSpec struct {
	Sig    string
	Tpl    string // optional JS template ($s receiver, $0.. args)
	Note   string
	Covers []string // extra API ids this op also covers (e.g. Structure* variants)
}

func op(sig string) opSpec                { return opSpec{Sig: sig} }
func opT(sig, tpl string) opSpec          { return opSpec{Sig: sig, Tpl: tpl} }
func opC(sig string, cv ...string) opSpec { return opSpec{Sig: sig, Covers: cv} }

// structure-class destroy/notifyWhenAttacked are identical on every class.
var structClasses = []string{
	"Structure", "OwnedStructure", "StructureContainer", "StructureController", "StructureExtension",
	"StructureExtractor", "StructureFactory", "StructureInvaderCore", "StructureKeeperLair", "StructureLab",
	"StructureLink", "StructureNuker", "StructureObserver", "StructurePowerBank", "StructurePowerSpawn",
	"StructurePortal", "StructureRampart", "StructureRoad", "StructureSpawn", "StructureStorage",
	"StructureTerminal", "StructureTower", "StructureWall",
}

func structCover(m string) []string {
	var r []string
	for _, k := range structClasses {
		r = append(r, k+"."+m)
	}
	return r
}

var ops = []opSpec{
	// ---- Creep
	op("Creep.attack(h) @approach"), op("Creep.attackController(h) @approach"), op("Creep.build(h) @approach"),
	op("Creep.claimController(h) @approach"), op("Creep.dismantle(h) @approach"), op("Creep.drop(res, io)"),
	op("Creep.generateSafeMode(h) @approach"), op("Creep.harvest(h) @approach"), op("Creep.heal(h) @approach"),
	op("Creep.move(dir)"), op("Creep.moveByPath(path)"), op("Creep.moveTo(h, preset:move)"),
	op("Creep.moveTo#pos(pos, preset:move)"), op("Creep.notifyWhenAttacked(b)"), op("Creep.pickup(h) @approach"),
	op("Creep.pull(h)"), op("Creep.rangedAttack(h) @approach"), op("Creep.rangedHeal(h) @approach"),
	op("Creep.rangedMassAttack()"), op("Creep.repair(h) @approach"), op("Creep.reserveController(h) @approach"),
	op("Creep.say(str, b)"), op("Creep.signController(h, str) @approach"), op("Creep.suicide()"),
	op("Creep.transfer(h, res, io) @approach"), op("Creep.upgradeController(h) @approach"),
	op("Creep.withdraw(h, res, io) @approach"), op("Creep.cancelOrder(str)"),
	// ---- PowerCreep
	opT("PowerCreep.create(str, str)", "PowerCreep.create($0,$1)"),
	op("PowerCreep.cancelOrder(str)"), op("PowerCreep.delete(b)"), op("PowerCreep.drop(res, io)"),
	op("PowerCreep.enableRoom(h) @approach"), op("PowerCreep.move(dir)"), op("PowerCreep.moveByPath(path)"),
	op("PowerCreep.moveTo(h, preset:move)"), op("PowerCreep.moveTo#pos(pos, preset:move)"),
	op("PowerCreep.notifyWhenAttacked(b)"), op("PowerCreep.pickup(h) @approach"), op("PowerCreep.rename(str)"),
	op("PowerCreep.renew(h) @approach"), op("PowerCreep.say(str, b)"), op("PowerCreep.spawn(h) @approach"),
	op("PowerCreep.suicide()"), op("PowerCreep.transfer(h, res, io) @approach"), op("PowerCreep.upgrade(i)"),
	op("PowerCreep.usePower(i)"), op("PowerCreep.usePower#target(i, h)"),
	op("PowerCreep.withdraw(h, res, io) @approach"),
	// ---- Flag / ConstructionSite / Room / RoomPosition
	op("Flag.remove()"), op("Flag.setColor(i, io)"), op("Flag.setPosition(pos)"),
	op("ConstructionSite.remove()"),
	op("Room.createConstructionSite(pos, st, str)"), op("Room.createFlag(pos, str, io, io)"),
	op("RoomPosition.createConstructionSite(pos, st, str)"), op("RoomPosition.createFlag(pos, str, io, io)"),
	// ---- Structures
	opC("Structure.destroy()", structCover("destroy")...),
	opC("Structure.notifyWhenAttacked(b)", structCover("notifyWhenAttacked")...),
	op("StructureController.activateSafeMode()"), op("StructureController.unclaim()"),
	op("StructureFactory.produce(res)"), op("StructureLab.boostCreep(h, io) @approach"),
	op("StructureLab.reverseReaction(h, h)"), op("StructureLab.runReaction(h, h)"),
	op("StructureLab.unboostCreep(h)"), op("StructureLink.transferEnergy(h, io)"),
	op("StructureNuker.launchNuke(pos)"), op("StructureObserver.observeRoom(rn)"),
	op("StructurePowerSpawn.processPower()"), op("StructureRampart.setPublic(b)"),
	opT("StructureSpawn.spawnCreep(parts, str, preset:spawn)", "$s.spawnCreep($0,$1,$2)"),
	op("StructureSpawn.recycleCreep(h) @approach"), op("StructureSpawn.renewCreep(h) @approach"),
	opT("StructureSpawn.createCreep(parts, str, preset:spawn)", "$s.createCreep($0,$1,($2||{}).memory)"),
	op("StructureSpawn.Spawning.cancel()"), op("StructureSpawn.Spawning.setDirections(arr)"),
	op("StructureTerminal.send(res, i, rn, str)"), op("StructureTower.attack(h)"),
	op("StructureTower.heal(h)"), op("StructureTower.repair(h)"),
	// ---- Game
	op("Game.notify(str, io)"), op("Game.cpu.halt()"), op("Game.cpu.unlock()"),
	op("Game.cpu.generatePixel()"), opT("Game.cpu.setShardLimits(preset:limits)", "Game.cpu.setShardLimits($0)"),
	op("Game.shard.activateAccess()"),
	// ---- Game.market
	op("Game.market.cancelOrder(h)"), op("Game.market.changeOrderPrice(h, milli)"),
	opT("Game.market.createOrder(i, res, milli, i, rn)",
		"Game.market.createOrder({type:$0?ORDER_BUY:ORDER_SELL,resourceType:$1,price:$2,totalAmount:$3,roomName:$4})"),
	op("Game.market.deal(h, i, rn)"), op("Game.market.extendOrder(h, i)"),
	// ---- Memory segments
	op("RawMemory.setActiveSegments(arr)"), op("RawMemory.setActiveForeignSegment(str, io)"),
	op("RawMemory.setDefaultPublicSegment(io)"), op("RawMemory.setPublicSegments(arr)"),
	// ---- Pathfinding matrices (JS-side registry, addressed by index)
	opT("PathFinder.CostMatrix.set(i, i, i, i)", "MATRIX($0).set($1,$2,$3)"),
	// ---- Visuals (cosmetic; positions are whole tiles, styles are presets)
	op("RoomVisual.line(pos, pos, preset:style)"), op("RoomVisual.circle(pos, preset:style)"),
	op("RoomVisual.rect(pos, i, i, preset:style)"), op("RoomVisual.text(str, pos, preset:style)"),
	opT("RoomVisual.poly(arr, preset:style)", "$s.poly(POINTS($0),$1)"), op("RoomVisual.clear()"),
	op("Game.map-visual.line(pos, pos, preset:style)"), op("Game.map-visual.circle(pos, preset:style)"),
	op("Game.map-visual.rect(pos, i, i, preset:style)"), op("Game.map-visual.text(str, pos, preset:style)"),
	opT("Game.map-visual.poly(arr, preset:style)", "$s.poly(POINTS($0),$1)"), op("Game.map-visual.clear()"),
	// ---- Numeric memory slots (the packed form of `.memory`; see MEM in host.js)
	opT("Memory.set(h, i, i)", "MEMSET($0,$1,$2)"),
}

// ---- Queries: the engine computes, Go asks in round N and reads in round N+1.
//
// Ret: int | bool | handle | handles | steps | milli | none | custom:NAME
// (custom marshallers live in js/host.js MARSHAL).

type querySpec struct {
	Sig    string
	Ret    string
	Tpl    string
	Covers []string
}

func q(sig, ret string) querySpec                { return querySpec{Sig: sig, Ret: ret} }
func qT(sig, ret, tpl string) querySpec          { return querySpec{Sig: sig, Ret: ret, Tpl: tpl} }
func qC(sig, ret string, cv ...string) querySpec { return querySpec{Sig: sig, Ret: ret, Covers: cv} }

var queries = []querySpec{
	// RoomPosition (receiver is the first pos)
	q("RoomPosition.findClosestByPath(pos, i, preset:find)", "handle"),
	q("RoomPosition.findClosestByPath#objects(pos, harr, preset:find)", "handle"),
	q("RoomPosition.findClosestByRange(pos, i, preset:find)", "handle"),
	q("RoomPosition.findClosestByRange#objects(pos, harr, preset:find)", "handle"),
	q("RoomPosition.findInRange(pos, i, i, preset:find)", "handles"),
	q("RoomPosition.findInRange#objects(pos, harr, i, preset:find)", "handles"),
	q("RoomPosition.findPathTo(pos, pos, preset:path)", "steps"),
	q("RoomPosition.findPathTo#object(pos, h, preset:path)", "steps"),
	q("RoomPosition.look(pos)", "custom:look"),
	q("RoomPosition.lookFor(pos, look)", "custom:lookFor"),
	// Room
	q("Room.find(i, preset:find)", "handles"),
	q("Room.findExitTo(rn)", "int"),
	q("Room.findPath(pos, pos, preset:path)", "steps"),
	q("Room.getEventLog()", "custom:events"),
	q("Room.lookAt(pos)", "custom:look"),
	qT("Room.lookAtArea(i, i, i, i)", "custom:lookArea", "$s.lookAtArea($0,$1,$2,$3,true)"),
	q("Room.lookForAt(look, pos)", "custom:lookFor"),
	qT("Room.lookForAtArea(look, i, i, i, i)", "custom:lookForArea", "$s.lookForAtArea($0,$1,$2,$3,$4,true)"),
	q("Room.Terrain.get(i, i)", "int"),
	// Structures / spawn
	qC("Structure.isActive()", "bool", structCover("isActive")...),
	q("StructureSpawn.canCreateCreep(parts, str)", "int"),
	// Store (exact per-resource answers; the packed columns cover the common cases)
	q("Store.getCapacity(reso)", "int"), q("Store.getFreeCapacity(reso)", "int"),
	q("Store.getUsedCapacity(reso)", "int"),
	// Game
	qT("Game.getObjectById(i, i, i)", "handle", "OBJ_BY_ID($0,$1,$2)"),
	q("Game.cpu.getUsed()", "milli"), q("Game.cpu.getHeapStatistics()", "custom:heap"),
	q("Game.map.describeExits(rn)", "custom:exits"), q("Game.map.findExit(rn, rn, preset:route)", "int"),
	q("Game.map.findRoute(rn, rn, preset:route)", "custom:route"),
	q("Game.map.getRoomLinearDistance(rn, rn, b)", "int"), q("Game.map.getRoomTerrain(rn)", "handle"),
	q("Game.map.getTerrainAt(i, i, rn)", "custom:terrain"), q("Game.map.getWorldSize()", "int"),
	q("Game.map.isRoomAvailable(rn)", "bool"), q("Game.map.getRoomStatus(rn)", "custom:status"),
	q("Game.map-visual.getSize()", "int"), q("RoomVisual.getSize()", "int"),
	q("Game.market.calcTransactionCost(i, rn, rn)", "int"),
	qT("Game.market.getAllOrders(preset:ofilter)", "custom:orders", "Game.market.getAllOrders($0)"),
	q("Game.market.getHistory(reso)", "custom:history"),
	// Pathfinding
	qT("PathFinder.search(pos, pos, i, preset:search)", "custom:search", "PathFinder.search($0,{pos:$1,range:$2},$3)"),
	qT("PathFinder.CostMatrix.get(i, i, i)", "int", "MATRIX($0).get($1,$2)"),
	qT("PathFinder.CostMatrix.clone(i)", "int", "MATRIX_ADD(MATRIX($0).clone())"),
	qT("PathFinder.CostMatrix.new()", "int", "MATRIX_ADD(new PathFinder.CostMatrix)"),
}

// Collections and live lists that the packed tables already expose.
var collections = map[string]string{
	"Game.creeps":             "creep table (Game.creeps ∪ hostile creeps; filter `mine`)",
	"Game.powerCreeps":        "powercreep table",
	"Game.spawns":             "structure table, type=spawn, mine=1",
	"Game.structures":         "structure table, mine=1",
	"Game.constructionSites":  "site table, mine=1",
	"Game.flags":              "flag table",
	"Game.rooms":              "room table",
	"Game.cpu":                "namespace: header cpu* columns + cpu ops/queries",
	"Game.map":                "namespace: Game.map.* queries",
	"Game.market":             "namespace: Game.market.* ops/queries + credits column",
	"Game.shard":              "namespace: shard* columns",
	"RoomPosition.x":          "x column of every table; Pos encoding (MakePos)",
	"RoomPosition.y":          "y column of every table; Pos encoding (MakePos)",
	"RoomPosition.roomName":   "rm column (room table index) and ref column (RoomRef); Pos encoding",
	"Room.visual":             "namespace: RoomVisual ops",
	"StructureSpawn.spawning": "spawn columns spawning/spNeed/spRemain/spDirs/spCreep + Spawning ops",
}

// ---- Native helpers: pure functions over packed data, no engine call.

type nativeSpec struct{ ID, Go, Note string }

var natives = []nativeSpec{
	{"RoomPosition.getRangeTo", "Range / RangeG", "Chebyshev range; cross-room via world coordinates"},
	{"RoomPosition.inRangeTo", "InRange", ""},
	{"RoomPosition.isNearTo", "IsNear", ""},
	{"RoomPosition.isEqualTo", "IsEqual", ""},
	{"RoomPosition.getDirectionTo", "DirectionTo", "same-room"},
	{"Creep.getActiveBodyparts", "(*Ctx).CreepActive", "from the ba_* columns"},
	{"Store.getUsedCapacity", "(*Ctx).StoreAmount", "non-energy amounts from the store arena; energy and totals are columns"},
	{"Room.getPositionAt", "MakePos", "packs x,y,room into a Pos"},
	{"Game.getObjectById", "(*Ctx).FindByID", "linear scan on idA/idB/idC (Go-side); the query form resolves any id"},
}

// ---- Intentionally JS-side. Every entry needs a reason; none is a silent gap.

var jsOnly = map[string]string{
	"InterShardMemory.getLocal":         "string blob; InterShardMemory is a JS-side channel (use host.js)",
	"InterShardMemory.setLocal":         "string blob; see above",
	"InterShardMemory.getRemote":        "string blob; see above",
	"RawMemory.get":                     "string blob; Go state is exchanged through the numeric memory slots instead",
	"RawMemory.set":                     "string blob; see above",
	"RawMemory.segments":                "string segments; JS-side",
	"RawMemory.foreignSegment":          "string segment; JS-side",
	"RawMemory.interShardSegment":       "deprecated string channel; JS-side",
	"Room.serializePath":                "returns a string; Go works with `path` triples directly",
	"Room.deserializePath":              "takes a string; Go works with `path` triples directly",
	"Game.map-visual.export":            "string",
	"Game.map-visual.import":            "string",
	"RoomVisual.export":                 "string",
	"RoomVisual.import":                 "string",
	"RoomVisual.roomName":               "string; the visual is addressed by its room handle",
	"PathFinder.use":                    "deprecated no-op on current servers",
	"PathFinder.CostMatrix.serialize":   "string; matrices live in the JS registry and are addressed by index",
	"PathFinder.CostMatrix.deserialize": "string; see above",
	"Game.market.getOrderById":          "order ids are strings; orders are addressed by handle (see Game.market.getAllOrders)",
	"Game.market.orders":                "object keyed by string ids; use getAllOrders with the owner filter preset",
	"Game.market.incomingTransactions":  "list of string-bearing records; read in JS when needed",
	"Game.market.outgoingTransactions":  "list of string-bearing records; read in JS when needed",
	"Game.shard.type":                   "string",
	"Game.shard.access":                 "string list",
	"Game.shard.accessTime":             "rarely used; JS-side",
	"Game.cpu.shardLimits":              "object keyed by shard name; JS-side (set via preset:limits)",
	"Creep.saying":                      "string; the `saying` column carries presence only",
	"PowerCreep.saying":                 "string; the `saying` column carries presence only",
}
