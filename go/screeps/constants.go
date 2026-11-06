package screeps

// Game constants, hard-coded to the official values so reading them costs no
// JS<->WASM crossing (the engine has never changed these numbers).

// ErrorCode is the return value of every Screeps action method (OK or ERR_*).
type ErrorCode int

const (
	OK                    ErrorCode = 0
	ErrNotOwner           ErrorCode = -1
	ErrNoPath             ErrorCode = -2
	ErrNameExists         ErrorCode = -3
	ErrBusy               ErrorCode = -4
	ErrNotFound           ErrorCode = -5
	ErrNotEnoughEnergy    ErrorCode = -6
	ErrNotEnoughResources ErrorCode = -6
	ErrInvalidTarget      ErrorCode = -7
	ErrFull               ErrorCode = -8
	ErrNotInRange         ErrorCode = -9
	ErrInvalidArgs        ErrorCode = -10
	ErrTired              ErrorCode = -11
	ErrNoBodypart         ErrorCode = -12
	ErrNotEnoughExtension ErrorCode = -6
	ErrRCLNotEnough       ErrorCode = -14
	ErrGCLNotEnough       ErrorCode = -15
)

// String returns the engine's constant name, e.g. "ERR_NOT_IN_RANGE".
func (e ErrorCode) String() string {
	switch e {
	case OK:
		return "OK"
	case ErrNotOwner:
		return "ERR_NOT_OWNER"
	case ErrNoPath:
		return "ERR_NO_PATH"
	case ErrNameExists:
		return "ERR_NAME_EXISTS"
	case ErrBusy:
		return "ERR_BUSY"
	case ErrNotFound:
		return "ERR_NOT_FOUND"
	case ErrNotEnoughEnergy:
		return "ERR_NOT_ENOUGH_RESOURCES"
	case ErrInvalidTarget:
		return "ERR_INVALID_TARGET"
	case ErrFull:
		return "ERR_FULL"
	case ErrNotInRange:
		return "ERR_NOT_IN_RANGE"
	case ErrInvalidArgs:
		return "ERR_INVALID_ARGS"
	case ErrTired:
		return "ERR_TIRED"
	case ErrNoBodypart:
		return "ERR_NO_BODYPART"
	case ErrRCLNotEnough:
		return "ERR_RCL_NOT_ENOUGH"
	case ErrGCLNotEnough:
		return "ERR_GCL_NOT_ENOUGH"
	}
	return "ERR_" + itoa(int(e))
}

// FindConstant selects what Room.Find / RoomPosition finders search for.
type FindConstant int

const (
	FindExitTop                  FindConstant = 1
	FindExitRight                FindConstant = 3
	FindExitBottom               FindConstant = 5
	FindExitLeft                 FindConstant = 7
	FindExit                     FindConstant = 10
	FindCreeps                   FindConstant = 101
	FindMyCreeps                 FindConstant = 102
	FindHostileCreeps            FindConstant = 103
	FindSourcesActive            FindConstant = 104
	FindSources                  FindConstant = 105
	FindDroppedResources         FindConstant = 106
	FindStructures               FindConstant = 107
	FindMyStructures             FindConstant = 108
	FindHostileStructures        FindConstant = 109
	FindFlags                    FindConstant = 110
	FindConstructionSites        FindConstant = 111
	FindMySpawns                 FindConstant = 112
	FindHostileSpawns            FindConstant = 113
	FindMyConstructionSites      FindConstant = 114
	FindHostileConstructionSites FindConstant = 115
	FindMinerals                 FindConstant = 116
	FindNukes                    FindConstant = 117
	FindTombstones               FindConstant = 118
	FindPowerCreeps              FindConstant = 119
	FindMyPowerCreeps            FindConstant = 120
	FindHostilePowerCreeps       FindConstant = 121
	FindDeposits                 FindConstant = 122
	FindRuins                    FindConstant = 123
)

// Direction is a movement direction for Creep.Move (TOP=1 clockwise to TOP_LEFT=8).
type Direction int

const (
	Top         Direction = 1
	TopRight    Direction = 2
	Right       Direction = 3
	BottomRight Direction = 4
	Bottom      Direction = 5
	BottomLeft  Direction = 6
	Left        Direction = 7
	TopLeft     Direction = 8
)

// BodyPart is a creep body part type.
type BodyPart string

const (
	Move         BodyPart = "move"
	Work         BodyPart = "work"
	Carry        BodyPart = "carry"
	Attack       BodyPart = "attack"
	RangedAttack BodyPart = "ranged_attack"
	Tough        BodyPart = "tough"
	Heal         BodyPart = "heal"
	Claim        BodyPart = "claim"
)

// Cost is the energy cost of one part (BODYPART_COST).
func (p BodyPart) Cost() int {
	switch p {
	case Move, Carry:
		return 50
	case Work:
		return 100
	case Attack:
		return 80
	case RangedAttack:
		return 150
	case Tough:
		return 10
	case Heal:
		return 250
	case Claim:
		return 600
	}
	return 0
}

// BodyCost is the total spawn energy cost of a body. Pure Go, no crossings.
func BodyCost(body []BodyPart) int {
	total := 0
	for _, p := range body {
		total += p.Cost()
	}
	return total
}

// ResourceType names a resource held in a Store.
type ResourceType string

const (
	ResourceEnergy    ResourceType = "energy"
	ResourcePower     ResourceType = "power"
	ResourceOps       ResourceType = "ops"
	ResourceHydrogen  ResourceType = "H"
	ResourceOxygen    ResourceType = "O"
	ResourceUtrium    ResourceType = "U"
	ResourceLemergium ResourceType = "L"
	ResourceKeanium   ResourceType = "K"
	ResourceZynthium  ResourceType = "Z"
	ResourceCatalyst  ResourceType = "X"
	ResourceGhodium   ResourceType = "G"
)

// StructureType is a structure's structureType string.
type StructureType string

const (
	StructureSpawnType      StructureType = "spawn"
	StructureExtension      StructureType = "extension"
	StructureRoad           StructureType = "road"
	StructureWall           StructureType = "constructedWall"
	StructureRampart        StructureType = "rampart"
	StructureKeeperLair     StructureType = "keeperLair"
	StructurePortal         StructureType = "portal"
	StructureControllerType StructureType = "controller"
	StructureLink           StructureType = "link"
	StructureStorage        StructureType = "storage"
	StructureTower          StructureType = "tower"
	StructureObserver       StructureType = "observer"
	StructurePowerBank      StructureType = "powerBank"
	StructurePowerSpawn     StructureType = "powerSpawn"
	StructureExtractor      StructureType = "extractor"
	StructureLab            StructureType = "lab"
	StructureTerminal       StructureType = "terminal"
	StructureContainer      StructureType = "container"
	StructureNuker          StructureType = "nuker"
	StructureFactory        StructureType = "factory"
	StructureInvaderCore    StructureType = "invaderCore"
)
