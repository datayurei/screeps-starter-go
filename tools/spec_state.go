package main

// Spec, part 1: packed STATE. Every column is one int32 per object per tick,
// computed by a JS expression over `o` (the game object) plus helpers defined
// in js/host.js (HD, RI, ID, FX, HASH, ST, RS, BODY, STORE, MEM, ...). The
// `covers` patterns say which API properties the column makes readable in Go;
// `Structure*` matches every class whose name starts with "Structure".

type col struct {
	Name   string
	Expr   string
	Covers []string
}

type kindSpec struct {
	Name    string   // lower-case table name, also the Go prefix
	Cap     int      // max rows per tick (overflow is dropped)
	Find    string   // JS: array of objects for room `r` ("" => special)
	Pre     string   // JS statement run once per object before columns (uses `o`)
	Classes []string // API classes whose RoomObject props are covered
	Cols    []col
}

func c(name, expr string, covers ...string) col { return col{name, expr, covers} }

// objCols: columns every positioned game object has. `cls` are the classes
// whose pos/room/id/effects entries these cover.
func objCols(cls []string, hasID bool) []col {
	var pos, room, fx, id []string
	pos, room, fx = []string{"RoomObject.pos"}, []string{"RoomObject.room"}, []string{"RoomObject.effects"}
	for _, k := range cls {
		pos = append(pos, k+".pos")
		room = append(room, k+".room")
		fx = append(fx, k+".effects")
		id = append(id, k+".id")
	}
	cs := []col{
		c("x", "o.pos.x", pos...),
		c("y", "o.pos.y"),
		c("rm", "RI(o.pos.roomName)", room...),
		c("fx", "FX(o)", fx...),
	}
	if hasID {
		cs = append(cs, c("idA", "ID(o,0)", id...), c("idB", "ID(o,1)"), c("idC", "ID(o,2)"))
	}
	return cs
}

// storeCols: the Store of a holder. `cls` classes' .store (and deprecated
// energy/capacity aliases) are covered. Per-resource amounts beyond energy
// live in an arena of (resource, amount) pairs at [sOff, sOff+2*sN).
func storeCols(cls []string, extra ...string) []col {
	var st []string
	for _, k := range cls {
		st = append(st, k+".store")
	}
	st = append(st, extra...)
	return []col{
		c("sUsed", "sa.used", st...),
		c("sFree", "sa.free"),
		c("sCap", "sa.cap"),
		c("sEnergy", "sa.energy"),
		c("sEnergyCap", "sa.energyCap"),
		c("sEnergyFree", "sa.energyFree"),
		c("sOff", "sa.off"),
		c("sN", "sa.n"),
	}
}

func memCols(cls ...string) []col {
	var cv []string
	for _, k := range cls {
		cv = append(cv, k+".memory")
	}
	return []col{c("mem0", "MEM(o,0)", cv...), c("mem1", "MEM(o,1)"), c("mem2", "MEM(o,2)"), c("mem3", "MEM(o,3)")}
}

const storePre = "const sa=STORE(o);"

var bodyParts = []string{"tough", "work", "attack", "ranged_attack", "carry", "move", "heal", "claim"}

func creepCols() []col {
	cs := objCols([]string{"Creep"}, true)
	cs = append(cs,
		c("mine", "o.my?1:0", "Creep.my"),
		c("owner", "HASH(o.owner&&o.owner.username)", "Creep.owner"),
		c("nameHash", "HASH(o.name)", "Creep.name"),
		c("spawning", "o.spawning?1:0", "Creep.spawning"),
		c("fatigue", "o.fatigue|0", "Creep.fatigue"),
		c("hits", "o.hits|0", "Creep.hits"),
		c("hitsMax", "o.hitsMax|0", "Creep.hitsMax"),
		c("ttl", "o.ticksToLive===undefined?-1:o.ticksToLive", "Creep.ticksToLive"),
		c("saying", "o.saying?1:0"), // presence only; text is JS-side (see Creep.saying)
		c("boostMask", "bp.boost", "Creep.body"),
	)
	for i, p := range bodyParts {
		cs = append(cs, c("bt_"+p, "bp.t["+itoa(i)+"]"), c("ba_"+p, "bp.a["+itoa(i)+"]"))
	}
	cs = append(cs, storeCols([]string{"Creep"}, "Creep.carry", "Creep.carryCapacity")...)
	cs = append(cs, memCols("Creep")...)
	return cs
}

var powerIDs = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}

func powerCreepCols() []col {
	cs := objCols([]string{"PowerCreep"}, true)
	cs = append(cs,
		c("mine", "o.my?1:0", "PowerCreep.my"),
		c("owner", "HASH(o.owner&&o.owner.username)", "PowerCreep.owner"),
		c("nameHash", "HASH(o.name)", "PowerCreep.name"),
		c("class", "PCLASS(o.className)", "PowerCreep.className"),
		c("level", "o.level|0", "PowerCreep.level"),
		c("hits", "o.hits|0", "PowerCreep.hits"),
		c("hitsMax", "o.hitsMax|0", "PowerCreep.hitsMax"),
		c("ttl", "o.ticksToLive===undefined?-1:o.ticksToLive", "PowerCreep.ticksToLive"),
		c("spawnCooldown", "o.spawnCooldownTime||0", "PowerCreep.spawnCooldownTime"),
		c("deleteTime", "o.deleteTime||0", "PowerCreep.deleteTime"),
		c("shard", "HASH(o.shard)", "PowerCreep.shard"),
		c("saying", "o.saying?1:0", "PowerCreep.saying"),
	)
	for _, id := range powerIDs {
		n := itoa(id)
		cs = append(cs, c("pwLvl"+n, "PW(o,"+n+",0)", "PowerCreep.powers"), c("pwCd"+n, "PW(o,"+n+",1)"))
	}
	cs = append(cs, storeCols([]string{"PowerCreep"}, "PowerCreep.carry", "PowerCreep.carryCapacity")...)
	cs = append(cs, memCols("PowerCreep")...)
	return cs
}

func structureCols() []col {
	const S = "Structure*."
	cs := objCols([]string{"Structure", "OwnedStructure", "Structure*"}, true)
	cs = append(cs,
		c("type", "ST[o.structureType]", "Structure.structureType", "OwnedStructure.structureType", S+"structureType"),
		c("mine", "o.my?1:0", "OwnedStructure.my", S+"my"),
		c("owner", "HASH(o.owner&&o.owner.username)", "OwnedStructure.owner", S+"owner"),
		c("hits", "o.hits|0", "Structure.hits", "OwnedStructure.hits", S+"hits"),
		c("hitsMax", "o.hitsMax|0", "Structure.hitsMax", "OwnedStructure.hitsMax", S+"hitsMax"),
		c("decay", "o.ticksToDecay|0", S+"ticksToDecay"),
		c("cooldown", "o.cooldown|0", S+"cooldown"),
		c("level", "o.level|0", S+"level"),
		// Controller
		c("progress", "o.progress|0", "StructureController.progress"),
		c("progressTotal", "o.progressTotal|0", "StructureController.progressTotal"),
		c("downgrade", "o.ticksToDowngrade|0", "StructureController.ticksToDowngrade"),
		c("upgradeBlocked", "o.upgradeBlocked|0", "StructureController.upgradeBlocked"),
		c("safeMode", "o.safeMode|0", "StructureController.safeMode"),
		c("safeModeAvail", "o.safeModeAvailable|0", "StructureController.safeModeAvailable"),
		c("safeModeCd", "o.safeModeCooldown|0", "StructureController.safeModeCooldown"),
		c("powerEnabled", "o.isPowerEnabled?1:0", "StructureController.isPowerEnabled"),
		c("resvTtl", "o.reservation?o.reservation.ticksToEnd:0", "StructureController.reservation"),
		c("resvUser", "HASH(o.reservation&&o.reservation.username)"),
		c("signUser", "HASH(o.sign&&o.sign.username)", "StructureController.sign"),
		c("signTime", "o.sign?o.sign.time:0"),
		// Misc per-type
		c("isPublic", "o.isPublic?1:0", "StructureRampart.isPublic"),
		c("power", "o.power|0", "StructurePowerBank.power"),
		c("deploy", "o.ticksToDeploy|0", "StructureInvaderCore.ticksToDeploy"),
		c("lairSpawn", "o.ticksToSpawn|0", "StructureKeeperLair.ticksToSpawn"),
		c("spawning", "o.spawning?1:0", "StructureInvaderCore.spawning", "StructureSpawn.spawning"),
		c("spNeed", "SPAWNING(o,0)", "StructureSpawn.Spawning.needTime"),
		c("spRemain", "SPAWNING(o,1)", "StructureSpawn.Spawning.remainingTime"),
		c("spDirs", "SPAWNING(o,2)", "StructureSpawn.Spawning.directions"),
		c("spCreep", "SPAWNING(o,3)", "StructureSpawn.Spawning.name", "StructureSpawn.Spawning.spawn"),
		c("destX", "DEST(o,0)", "StructurePortal.destination"),
		c("destY", "DEST(o,1)"),
		c("destRoom", "DEST(o,2)"),
		c("mineral", "RS(o.mineralType)", "StructureLab.mineralType"),
		c("nameHash", "HASH(o.name)", "StructureSpawn.name"),
	)
	cs = append(cs, storeCols([]string{"Structure*"},
		"StructureContainer.storeCapacity", "StructureFactory.storeCapacity", "StructureStorage.storeCapacity", "StructureTerminal.storeCapacity",
		"StructureExtension.energy", "StructureExtension.energyCapacity",
		"StructureLab.energy", "StructureLab.energyCapacity", "StructureLab.mineralAmount", "StructureLab.mineralCapacity",
		"StructureLink.energy", "StructureLink.energyCapacity",
		"StructureNuker.energy", "StructureNuker.energyCapacity", "StructureNuker.ghodium", "StructureNuker.ghodiumCapacity",
		"StructurePowerSpawn.energy", "StructurePowerSpawn.energyCapacity", "StructurePowerSpawn.power", "StructurePowerSpawn.powerCapacity",
		"StructureSpawn.energy", "StructureSpawn.energyCapacity",
		"StructureTower.energy", "StructureTower.energyCapacity")...)
	cs = append(cs, memCols("StructureSpawn")...)
	return cs
}

var kinds = []kindSpec{
	{Name: "creep", Cap: 512, Find: "r.find(FIND_CREEPS)", Pre: "const bp=BODY(o);" + storePre, Cols: creepCols()},
	{Name: "powercreep", Cap: 16, Find: "r.find(FIND_POWER_CREEPS)", Pre: storePre, Cols: powerCreepCols()},
	{Name: "structure", Cap: 1536, Find: "r.find(FIND_STRUCTURES)", Pre: storePre, Cols: structureCols()},
	{Name: "source", Cap: 64, Find: "r.find(FIND_SOURCES)", Cols: append(objCols([]string{"Source"}, true),
		c("energy", "o.energy|0", "Source.energy"),
		c("energyCap", "o.energyCapacity|0", "Source.energyCapacity"),
		c("regen", "o.ticksToRegeneration|0", "Source.ticksToRegeneration"))},
	{Name: "mineral", Cap: 32, Find: "r.find(FIND_MINERALS)", Cols: append(objCols([]string{"Mineral"}, true),
		c("density", "o.density|0", "Mineral.density"),
		c("amount", "o.mineralAmount|0", "Mineral.mineralAmount"),
		c("type", "RS(o.mineralType)", "Mineral.mineralType"),
		c("regen", "o.ticksToRegeneration|0", "Mineral.ticksToRegeneration"))},
	{Name: "deposit", Cap: 32, Find: "r.find(FIND_DEPOSITS)", Cols: append(objCols([]string{"Deposit"}, true),
		c("cooldown", "o.cooldown|0", "Deposit.cooldown"),
		c("type", "RS(o.depositType)", "Deposit.depositType"),
		c("lastCooldown", "o.lastCooldown|0", "Deposit.lastCooldown"),
		c("decay", "o.ticksToDecay|0", "Deposit.ticksToDecay"))},
	{Name: "site", Cap: 256, Find: "r.find(FIND_CONSTRUCTION_SITES)", Cols: append(objCols([]string{"ConstructionSite"}, true),
		c("mine", "o.my?1:0", "ConstructionSite.my"),
		c("owner", "HASH(o.owner&&o.owner.username)", "ConstructionSite.owner"),
		c("progress", "o.progress|0", "ConstructionSite.progress"),
		c("progressTotal", "o.progressTotal|0", "ConstructionSite.progressTotal"),
		c("type", "ST[o.structureType]", "ConstructionSite.structureType"))},
	{Name: "resource", Cap: 256, Find: "r.find(FIND_DROPPED_RESOURCES)", Cols: append(objCols([]string{"Resource"}, true),
		c("amount", "o.amount|0", "Resource.amount"),
		c("type", "RS(o.resourceType)", "Resource.resourceType"))},
	{Name: "tombstone", Cap: 128, Find: "r.find(FIND_TOMBSTONES)", Pre: storePre, Cols: append(append(objCols([]string{"Tombstone"}, true),
		c("creep", "HD(o.creep)", "Tombstone.creep"),
		c("death", "o.deathTime|0", "Tombstone.deathTime"),
		c("decay", "o.ticksToDecay|0", "Tombstone.ticksToDecay")),
		storeCols([]string{"Tombstone"})...)},
	{Name: "ruin", Cap: 128, Find: "r.find(FIND_RUINS)", Pre: storePre, Cols: append(append(objCols([]string{"Ruin"}, true),
		c("structure", "HD(o.structure)", "Ruin.structure"),
		c("structType", "ST[o.structure&&o.structure.structureType]"),
		c("destroyTime", "o.destroyTime|0", "Ruin.destroyTime"),
		c("decay", "o.ticksToDecay|0", "Ruin.ticksToDecay")),
		storeCols([]string{"Ruin"})...)},
	{Name: "nuke", Cap: 32, Find: "r.find(FIND_NUKES)", Cols: append(objCols([]string{"Nuke"}, true),
		c("launchRoom", "RN2(o.launchRoomName)", "Nuke.launchRoomName"),
		c("landIn", "o.timeToLand|0", "Nuke.timeToLand"))},
	{Name: "flag", Cap: 128, Find: "r.find(FIND_FLAGS)", Cols: append(append(objCols([]string{"Flag"}, false),
		c("nameHash", "HASH(o.name)", "Flag.name"),
		c("color", "o.color|0", "Flag.color"),
		c("color2", "o.secondaryColor|0", "Flag.secondaryColor")),
		memCols("Flag")...)},
}

// roomCols: one row per visible room (Game.rooms). Rooms are packed first, so
// every other table's `rm` column indexes into this table.
var roomCols = append([]col{
	c("ref", "RREF(o.name)", "Room.name"),
	c("mine", "o.controller&&o.controller.my?1:0"),
	c("energyAvail", "o.energyAvailable|0", "Room.energyAvailable"),
	c("energyCap", "o.energyCapacityAvailable|0", "Room.energyCapacityAvailable"),
	c("controller", "HD(o.controller)", "Room.controller"),
	c("storage", "HD(o.storage)", "Room.storage"),
	c("terminal", "HD(o.terminal)", "Room.terminal"),
	c("terrain", "TERRAIN(o)", "Room.getTerrain", "Room.Terrain.getRawBuffer"),
}, memCols("Room")...)

// headerCols: scalar game state, one word each, at the front of the input.
var headerCols = []col{
	c("time", "Game.time", "Game.time"),
	c("cpuLimit", "(Game.cpu&&Game.cpu.limit)|0", "Game.cpu.limit"),
	c("cpuTickLimit", "Game.cpu.tickLimit|0", "Game.cpu.tickLimit"),
	c("cpuBucket", "Game.cpu.bucket|0", "Game.cpu.bucket"),
	c("cpuUnlocked", "Game.cpu.unlocked?1:0", "Game.cpu.unlocked"),
	c("cpuUnlockedTime", "Game.cpu.unlockedTime|0", "Game.cpu.unlockedTime"),
	c("cpuUsed", "Math.round(Game.cpu.getUsed()*1000)"), // µs at pack time
	c("gclLevel", "(Game.gcl&&Game.gcl.level)|0", "Game.gcl"),
	c("gclProgress", "CLIP(Game.gcl&&Game.gcl.progress)"),
	c("gclTotal", "CLIP(Game.gcl&&Game.gcl.progressTotal)"),
	c("gplLevel", "(Game.gpl&&Game.gpl.level)|0", "Game.gpl"),
	c("gplProgress", "CLIP(Game.gpl&&Game.gpl.progress)"),
	c("gplTotal", "CLIP(Game.gpl&&Game.gpl.progressTotal)"),
	c("credits", "CLIP(Math.round((Game.market&&Game.market.credits||0)*1000))", "Game.market.credits"), // milli-credits
	c("shardHash", "HASH(Game.shard&&Game.shard.name)", "Game.shard.name", "Game.shard"),
	c("shardPtr", "Game.shard&&Game.shard.ptr?1:0", "Game.shard.ptr"),
	c("resPixel", "(Game.resources&&Game.resources.pixel)|0", "Game.resources"),
	c("resCpuUnlock", "(Game.resources&&Game.resources.cpuUnlock)|0"),
	c("resAccessKey", "(Game.resources&&Game.resources.accessKey)|0"),
}
