package lib

// The original-API facade: Pos with its methods, the Store view, generic range
// finders, and the Game object. Everything here is computed from the packed
// snapshot in Ctx; no engine call, no extra round. (Semantics are the same as
// the rest of the layer: a start-of-tick snapshot, valid for one tick.)

// Locatable is anything with a position: every wrapper with x/y/room columns
// (Creep, Source, Structure, ...) and Pos itself.
type Locatable interface{ Pos() Pos }

// Pos returns itself, so a Pos can be passed wherever a Locatable is expected.
func (p Pos) Pos() Pos { return p }

func (p Pos) global() (int32, int32) { return Global(int32(p.X()), int32(p.Y()), p.Room()) }

// GetRangeTo is RoomPosition.getRangeTo (works across rooms via world coordinates).
func (p Pos) GetRangeTo(t Locatable) int32 {
	ax, ay := p.global()
	bx, by := t.Pos().global()
	return Range(ax, ay, bx, by)
}

// InRangeTo is RoomPosition.inRangeTo.
func (p Pos) InRangeTo(t Locatable, rng int32) bool { return p.GetRangeTo(t) <= rng }

// IsNearTo is RoomPosition.isNearTo.
func (p Pos) IsNearTo(t Locatable) bool { return p.GetRangeTo(t) <= 1 }

// IsEqualTo is RoomPosition.isEqualTo.
func (p Pos) IsEqualTo(t Locatable) bool { return p == t.Pos() }

// GetDirectionTo is RoomPosition.getDirectionTo; 0 if the positions coincide.
func (p Pos) GetDirectionTo(t Locatable) int32 {
	ax, ay := p.global()
	bx, by := t.Pos().global()
	return DirectionTo(ax, ay, bx, by)
}

// FindClosestByRange is RoomPosition.findClosestByRange(objects): the element
// nearest to from by straight-line range, across rooms. Ties go to the earlier
// element. (Go methods cannot be generic, so this is a function.)
func FindClosestByRange[T Locatable](from Locatable, xs []T) (best T, ok bool) {
	fp := from.Pos()
	bestD := int32(1 << 30)
	for _, x := range xs {
		if d := fp.GetRangeTo(x); d < bestD {
			best, bestD, ok = x, d, true
		}
	}
	return
}

// FindInRange is RoomPosition.findInRange(objects, range).
func FindInRange[T Locatable](from Locatable, xs []T, rng int32) []T {
	fp := from.Pos()
	return Filter(xs, func(x T) bool { return fp.GetRangeTo(x) <= rng })
}

// ---- Store ---------------------------------------------------------------------

// Store is the original API's creep.store / structure.store view.
//
// Totals and energy come straight from columns. Other resources' used amounts
// come from the store arena. For restricted stores (labs, nukers, power
// spawns) the engine reports no general capacity, so per-resource capacities
// other than energy read 0 here; use the QStoreGetCapacity query for those.
type Store struct {
	c                                                      *Ctx
	used, free, cap, energy, energyCap, energyFree, off, n int32
}

func firstRes(res []int32) (int32, bool) {
	if len(res) == 0 || res[0] < 0 {
		return 0, false
	}
	return res[0], true
}

// GetUsedCapacity is store.getUsedCapacity([resource]).
func (s Store) GetUsedCapacity(res ...int32) int32 {
	r, ok := firstRes(res)
	switch {
	case !ok:
		return s.used
	case r == ResEnergy:
		return s.energy
	}
	return s.c.StoreAmount(s.off, s.n, r)
}

// GetFreeCapacity is store.getFreeCapacity([resource]).
func (s Store) GetFreeCapacity(res ...int32) int32 {
	r, ok := firstRes(res)
	switch {
	case !ok:
		return s.free
	case r == ResEnergy:
		return s.energyFree
	case s.cap > 0:
		return s.free
	}
	return 0
}

// GetCapacity is store.getCapacity([resource]).
func (s Store) GetCapacity(res ...int32) int32 {
	r, ok := firstRes(res)
	switch {
	case !ok:
		return s.cap
	case r == ResEnergy:
		return s.energyCap
	case s.cap > 0:
		return s.cap
	}
	return 0
}

// Get is store[resource].
func (s Store) Get(res int32) int32 { return s.GetUsedCapacity(res) }

// ---- identity: names and ids -----------------------------------------------------

// NameHash is the hash the snapshot uses for names (FNV-1a over the bytes, as
// js/host.js HASH does). Equal to JS for ASCII names, which covers normal names.
func NameHash(s string) int32 {
	h := uint32(0x811c9dc5)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 0x01000193
	}
	return int32(h)
}

// IDWords splits an object id string into the three words stored in the
// idA/idB/idC columns. ok is false if the id has a non-hex character.
func IDWords(id string) (a, b, d int32, ok bool) {
	var w [3]int32
	for k := 0; k < 3; k++ {
		lo, hi := k*8, k*8+8
		if lo > len(id) {
			lo = len(id)
		}
		if hi > len(id) {
			hi = len(id)
		}
		var v uint32
		for _, ch := range []byte(id[lo:hi]) {
			var n byte
			switch {
			case ch >= '0' && ch <= '9':
				n = ch - '0'
			case ch >= 'a' && ch <= 'f':
				n = ch - 'a' + 10
			case ch >= 'A' && ch <= 'F':
				n = ch - 'A' + 10
			default:
				return 0, 0, 0, false
			}
			v = v<<4 | uint32(n)
		}
		w[k] = int32(v)
	}
	return w[0], w[1], w[2], true
}

// ---- Room finders beyond the generated per-kind ones -------------------------------

// MyCreeps are your creeps in this room.
func (o Room) MyCreeps() []Creep { return Filter(o.Creeps(), func(x Creep) bool { return x.IsMine() }) }

// HostileCreeps are other players' creeps in this room.
func (o Room) HostileCreeps() []Creep {
	return Filter(o.Creeps(), func(x Creep) bool { return !x.IsMine() })
}

// StructuresOfType are the room's structures of one St* type.
func (o Room) StructuresOfType(st int32) []Structure {
	return Filter(o.Structures(), func(x Structure) bool { return x.Type() == st })
}

// ---- Game ----------------------------------------------------------------------------

// CPU mirrors Game.cpu. Used is the CPU spent when the snapshot was packed (ms).
type CPU struct {
	Limit, TickLimit, Bucket int32
	Unlocked                 bool
	UnlockedTime             int32
	Used                     float64
}

// Level mirrors Game.gcl / Game.gpl.
type Level struct{ Level, Progress, ProgressTotal int32 }

// Shard mirrors the numeric part of Game.shard (the name is a hash).
type Shard struct {
	NameHash int32
	Ptr      bool
}

// Resources mirrors Game.resources.
type Resources struct{ Pixel, CPUUnlock, AccessKey int32 }

// Game mirrors the global Game object. Scalars are copied when it is created
// (c.Game()); lists and lookups are computed from the tables on demand.
type Game struct {
	c         *Ctx
	Time      int32
	CPU       CPU
	GCL, GPL  Level
	Credits   float64
	Shard     Shard
	Resources Resources
}

// Game returns the facade for this tick's snapshot.
func (c *Ctx) Game() Game {
	return Game{
		c:    c,
		Time: c.Time(),
		CPU: CPU{Limit: c.CpuLimit(), TickLimit: c.CpuTickLimit(), Bucket: c.CpuBucket(),
			Unlocked: c.CpuUnlocked() != 0, UnlockedTime: c.CpuUnlockedTime(), Used: float64(c.CpuUsed()) / 1000},
		GCL:       Level{c.GclLevel(), c.GclProgress(), c.GclTotal()},
		GPL:       Level{c.GplLevel(), c.GplProgress(), c.GplTotal()},
		Credits:   float64(c.Credits()) / 1000,
		Shard:     Shard{c.ShardHash(), c.ShardPtr() != 0},
		Resources: Resources{c.ResPixel(), c.ResCpuUnlock(), c.ResAccessKey()},
	}
}

// Creeps is Game.creeps: your creeps only.
func (g Game) Creeps() []Creep { return Filter(g.c.Creeps(), func(x Creep) bool { return x.IsMine() }) }

// PowerCreeps is Game.powerCreeps: your power creeps only.
func (g Game) PowerCreeps() []PowerCreep {
	return Filter(g.c.PowerCreeps(), func(x PowerCreep) bool { return x.IsMine() })
}

// Structures is Game.structures: your structures only.
func (g Game) Structures() []Structure {
	return Filter(g.c.Structures(), func(x Structure) bool { return x.IsMine() })
}

// Spawns is Game.spawns: your spawns.
func (g Game) Spawns() []Structure {
	return Filter(g.c.Structures(), func(x Structure) bool { return x.IsMine() && x.Type() == StSpawn })
}

// ConstructionSites is Game.constructionSites: your sites only.
func (g Game) ConstructionSites() []Site {
	return Filter(g.c.Sites(), func(x Site) bool { return x.IsMine() })
}

// Flags is Game.flags.
func (g Game) Flags() []Flag { return g.c.Flags() }

// Rooms is Game.rooms: the rooms you currently have vision of (up to 64).
func (g Game) Rooms() []Room { return g.c.Rooms() }

// Creep is Game.creeps[name], matched by name hash among your creeps.
func (g Game) Creep(name string) (Creep, bool) {
	h := NameHash(name)
	for _, x := range g.Creeps() {
		if x.NameHash() == h {
			return x, true
		}
	}
	return Creep{}, false
}

// Spawn is Game.spawns[name], matched by name hash.
func (g Game) Spawn(name string) (Structure, bool) {
	h := NameHash(name)
	for _, x := range g.Spawns() {
		if x.NameHash() == h {
			return x, true
		}
	}
	return Structure{}, false
}

// Flag is Game.flags[name], matched by name hash.
func (g Game) Flag(name string) (Flag, bool) {
	h := NameHash(name)
	for _, x := range g.c.Flags() {
		if x.NameHash() == h {
			return x, true
		}
	}
	return Flag{}, false
}

// GetObjectById resolves an id string against the packed tables; the result is
// a Handle (usable as an Object), or -1 if not found among the packed objects.
func (g Game) GetObjectById(id string) Handle {
	a, b, d, ok := IDWords(id)
	if !ok {
		return -1
	}
	return g.c.FindByID(a, b, d)
}
