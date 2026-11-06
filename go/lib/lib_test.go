package lib_test

import (
	"testing"

	. "screeps/lib"
	"screeps/lib/libtest"
)

func TestOpEncodingAndArenas(t *testing.T) {
	c := new(Ctx)
	c.CreepTransfer(5, 6, ResEnergy, -1)
	c.CreepSay(5, "hi", true)
	c.CreepMoveByPath(5, []int32{1, 2, 3, 4, 5, 6})
	c.GameMarketCreateOrder(1, ResEnergy, 1.2346, 500, MakeRoomRef(1, -2))
	if c.Out[OutNCmd] != 4 {
		t.Fatalf("emitted %d", c.Out[OutNCmd])
	}
	_, _, w := libtest.Cmd(c, 0)
	if w[1] != 5 || w[2] != 6 || w[3] != ResEnergy || w[4] != -1 {
		t.Errorf("transfer words %v", w)
	}
	_, _, w = libtest.Cmd(c, 1)
	if r := w[2]; r>>12 != 0 || r&4095 != 2 || string(c.Bytes[0:2]) != "hi" || w[3] != 1 {
		t.Errorf("say words %v bytes %q", w, c.Bytes[:2])
	}
	_, _, w = libtest.Cmd(c, 2)
	if w[2]&4095 != 6 || c.Arena[w[2]>>12+5] != 6 {
		t.Errorf("path ref %v", w)
	}
	_, _, w = libtest.Cmd(c, 3)
	if w[4] != 1235 { // 1.2346 -> 1235 milli
		t.Errorf("price milli = %d", w[4])
	}
}

func TestCommandOverflowIsCountedNotFatal(t *testing.T) {
	c := new(Ctx)
	for i := 0; i < CmdCap+3; i++ {
		c.CreepSuicide(1)
	}
	if c.Out[OutNCmd] != CmdCap || c.Out[OutOverflow] != 3 {
		t.Errorf("ncmd=%d overflow=%d", c.Out[OutNCmd], c.Out[OutOverflow])
	}
}

func TestQueryRoundTrip(t *testing.T) {
	c := new(Ctx)
	q := c.QRoomFind(9, 101, Preset(0))
	if q != 0 || c.Answered(q) || c.Answer(q) != nil {
		t.Fatal("fresh query must be unanswered")
	}
	if c.Qry[0] != QRoomFind || c.Qry[1] != 9 || c.Qry[2] != 101 {
		t.Errorf("query words %v", c.Qry[:7])
	}
	// JS answers: 3 handles at Resp[0:3]
	copy(c.Resp[:], []int32{4, 5, 6})
	c.RespIdx[0], c.RespIdx[1] = 0, 3
	c.Out[OutNAnswered] = 1
	if a := c.Answer(q); len(a) != 3 || a[2] != 6 {
		t.Errorf("answer %v", a)
	}
}

func TestPosAndRoomRefRoundTrip(t *testing.T) {
	for _, r := range []RoomRef{MakeRoomRef(0, 0), MakeRoomRef(-1, -1), MakeRoomRef(-11, 5), MakeRoomRef(60, -60)} {
		p := MakePos(49, 3, r)
		if p.X() != 49 || p.Y() != 3 || p.Room() != r {
			t.Errorf("pos round trip for %d: got %d,%d,%d", r, p.X(), p.Y(), p.Room())
		}
		if MakeRoomRef(r.X(), r.Y()) != r {
			t.Errorf("room ref round trip %d", r)
		}
	}
}

func TestNatives(t *testing.T) {
	if Range(1, 1, 4, 3) != 3 || !IsNear(1, 1, 2, 2) || IsNear(1, 1, 3, 1) || !IsEqual(2, 2, 2, 2) || !InRange(0, 0, 3, 3, 3) {
		t.Error("range helpers")
	}
	if RangeG(49, 25, MakeRoomRef(0, 0), 1, 25, MakeRoomRef(1, 0)) != 2 {
		t.Error("cross-room range")
	}
	cases := []struct{ dx, dy, want int32 }{
		{0, 0, 0}, {0, -5, Top}, {5, 0, Right}, {0, 5, Bottom}, {-5, 0, Left},
		{3, -3, TopRight}, {3, 3, BottomRight}, {-3, 3, BottomLeft}, {-3, -3, TopLeft}, {5, 1, Right}, {1, -5, Top},
	}
	for _, k := range cases {
		if got := DirectionTo(10, 10, 10+k.dx, 10+k.dy); got != k.want {
			t.Errorf("direction (%d,%d) = %d, want %d", k.dx, k.dy, got, k.want)
		}
	}
}

func TestFindByIDAndCreepParts(t *testing.T) {
	f := libtest.New()
	f.Source(0, 40, 1, 1, 0, 1)
	b := SourceBase
	f.C.In[b+SourceColIdA], f.C.In[b+SourceColIdB], f.C.In[b+SourceColIdC] = 11, 22, 33
	if f.C.FindByID(11, 22, 33) != 40 || f.C.FindByID(11, 22, 34) != -1 {
		t.Error("FindByID")
	}
	f.Creep(0, 30, 0, 0, 0, 0, true, false)
	f.C.In[CreepBase+CreepColBtWork], f.C.In[CreepBase+CreepColBaWork] = 5, 3
	if tot, act := f.C.CreepParts(0, PartWork); tot != 5 || act != 3 {
		t.Errorf("CreepParts = %d,%d", tot, act)
	}
}

func TestLayoutSanity(t *testing.T) {
	// Constants must agree with each other: the tables must end exactly at InLen.
	end := CreepBase + CreepCap*CreepStride
	if StructureBase < end || InLen <= StructureBase {
		t.Errorf("table ordering/geometry is off: creep end %d, structure base %d", end, StructureBase)
	}
	if NumOps <= OpStructureSpawnSpawnCreep || NumQueries <= QRoomFind {
		t.Error("NumOps/NumQueries must exceed every known code")
	}
}

func TestWrappersReadAndActLikeTheAccessors(t *testing.T) {
	f := libtest.New()
	f.Room(0, 50)
	f.C.In[RoomBase+RoomColRef] = int32(MakeRoomRef(-2, -2))
	f.Creep(0, 30, 10, 11, 0, 25, true, false)
	f.Source(0, 20, 12, 10, 0, 100)

	cr := f.C.Creeps()[0]
	if cr.H() != 30 || cr.X() != 10 || cr.Y() != 11 || cr.FreeCapacity() != 25 || !cr.IsMine() || cr.IsSpawning() {
		t.Errorf("creep wrapper reads wrong: h=%d x=%d y=%d free=%d", cr.H(), cr.X(), cr.Y(), cr.FreeCapacity())
	}
	if p := cr.Pos(); p.X() != 10 || p.Y() != 11 || p.Room() != MakeRoomRef(-2, -2) {
		t.Errorf("Pos() = %d,%d room %d", p.X(), p.Y(), p.Room())
	}
	if !cr.Room().Valid() || cr.Room().Controller() != 50 {
		t.Error("Room()/Controller()")
	}

	// A wrapper, a bare Handle and the Ctx builder are interchangeable.
	src := f.C.Sources()[0]
	cr.Harvest(src)
	cr.Harvest(Handle(21))
	f.C.CreepHarvest(30, 20)
	for i, want := range []int32{20, 21, 20} {
		if _, _, w := libtest.Cmd(f.C, i); w[1] != 30 || w[2] != want {
			t.Errorf("command %d: recv=%d target=%d, want 30,%d", i, w[1], w[2], want)
		}
	}
}

func TestClosestFilterAndRanges(t *testing.T) {
	f := libtest.New()
	f.Room(1, -1)
	f.Creep(0, 1, 10, 10, 0, 0, true, false)
	f.Source(0, 20, 40, 40, 0, 5)
	f.Source(1, 21, 13, 10, 0, 5) // nearest, same room
	f.Source(2, 22, 11, 10, 1, 5) // nearer but other room
	cr := f.C.Creeps()[0]
	best, ok := Closest(cr, f.C.Sources())
	if !ok || best.H() != 21 {
		t.Errorf("Closest = %d ok=%v, want handle 21", best.H(), ok)
	}
	if _, ok := Closest(cr, Filter(f.C.Sources(), func(s Source) bool { return s.H() == 22 })); ok {
		t.Error("a source in another room must not be 'closest'")
	}
	if RangeTo(cr, best) != 3 || RangeTo(cr, f.C.Sources()[2]) != -1 {
		t.Error("RangeTo")
	}
	if n := len(InRangeOf(cr, f.C.Sources(), 3)); n != 1 {
		t.Errorf("InRangeOf = %d", n)
	}
	if Count(f.C.Sources(), func(s Source) bool { return s.Energy() > 0 }) != 3 {
		t.Error("Count")
	}
}

func TestStoreAmountAndQueryWrappers(t *testing.T) {
	f := libtest.New()
	f.Creep(0, 30, 0, 0, 0, 0, true, false)
	b := CreepBase
	f.C.In[b+CreepColSEnergy] = 40
	f.C.In[b+CreepColSOff], f.C.In[b+CreepColSN] = 4, 2
	copy(f.C.StoreArena[4:], []int32{ResH, 7, ResO, 9})
	cr := f.C.Creeps()[0]
	if cr.StoreAmount(ResEnergy) != 40 || cr.StoreAmount(ResO) != 9 || cr.StoreAmount(ResH) != 7 || cr.StoreAmount(ResK) != 0 {
		t.Error("StoreAmount")
	}
	f.Room(0, -1)
	q := f.C.Rooms()[0].QFind(101, Preset(0))
	if q != 0 || f.C.Qry[0] != QRoomFind || f.C.Qry[1] != 1000 {
		t.Errorf("room.QFind wrote %v", f.C.Qry[:3])
	}
}

func TestNameHashMatchesJS(t *testing.T) {
	if NameHash("Bob") != -338976396 { // computed from js/host.js HASH("Bob")
		t.Errorf("NameHash(Bob) = %d", NameHash("Bob"))
	}
	if NameHash("") != int32(-2128831035) { // FNV-1a offset basis as int32
		t.Errorf("NameHash(\"\") = %d", NameHash(""))
	}
}

func TestIDWordsMatchesJSSplit(t *testing.T) {
	a, b, d, ok := IDWords("aaaaaaaabbbbbbbb00000001")
	if !ok || uint32(a) != 0xaaaaaaaa || uint32(b) != 0xbbbbbbbb || d != 1 {
		t.Errorf("IDWords = %x %x %x ok=%v", uint32(a), uint32(b), d, ok)
	}
	if _, _, _, ok := IDWords("zz"); ok {
		t.Error("non-hex id must not parse")
	}
	f := libtest.New()
	f.Source(0, 40, 1, 1, 0, 1)
	f.C.In[SourceBase+SourceColIdA], f.C.In[SourceBase+SourceColIdB], f.C.In[SourceBase+SourceColIdC] = a, b, d
	g := f.C.Game()
	if g.GetObjectById("aaaaaaaabbbbbbbb00000001") != 40 || g.GetObjectById("aaaaaaaabbbbbbbb00000002") != -1 || g.GetObjectById("nothex") != -1 {
		t.Error("Game.GetObjectById")
	}
}

func TestPosMethodsAcrossRooms(t *testing.T) {
	a := MakePos(49, 25, MakeRoomRef(0, 0))
	b := MakePos(1, 25, MakeRoomRef(1, 0))
	if a.GetRangeTo(b) != 2 || !a.InRangeTo(b, 2) || a.InRangeTo(b, 1) || a.IsNearTo(b) {
		t.Error("cross-room range")
	}
	if a.GetDirectionTo(b) != Right || b.GetDirectionTo(a) != Left || a.GetDirectionTo(a) != 0 {
		t.Error("direction")
	}
	if !a.IsEqualTo(a) || a.IsEqualTo(b) {
		t.Error("IsEqualTo")
	}
}

func TestFindClosestByRangeAndInRange(t *testing.T) {
	f := libtest.New()
	f.Room(1, -1) // rooms 0 and 1
	f.C.In[RoomBase+RoomColRef] = int32(MakeRoomRef(0, 0))
	f.C.In[RoomBase+RoomStride+RoomColRef] = int32(MakeRoomRef(1, 0))
	f.Creep(0, 1, 48, 25, 0, 0, true, false)
	f.Source(0, 20, 3, 25, 1, 5)  // other room, 5 away across the border
	f.Source(1, 21, 40, 25, 0, 5) // same room, 8 away
	cr := f.C.Creeps()[0]
	best, ok := FindClosestByRange(cr, f.C.Sources())
	if !ok || best.H() != 20 {
		t.Errorf("closest = %d, want the cross-room source 20", best.H())
	}
	if n := len(FindInRange(cr, f.C.Sources(), 5)); n != 1 {
		t.Errorf("FindInRange = %d", n)
	}
}

func TestStoreView(t *testing.T) {
	f := libtest.New()
	f.Creep(0, 30, 0, 0, 0, 0, true, false)
	b := CreepBase
	// general-purpose store: 100 cap, 50 energy + 7 H used
	f.C.In[b+CreepColSUsed], f.C.In[b+CreepColSFree], f.C.In[b+CreepColSCap] = 57, 43, 100
	f.C.In[b+CreepColSEnergy], f.C.In[b+CreepColSEnergyCap], f.C.In[b+CreepColSEnergyFree] = 50, 100, 43
	f.C.In[b+CreepColSOff], f.C.In[b+CreepColSN] = 0, 1
	f.C.StoreArena[0], f.C.StoreArena[1] = ResH, 7
	s := f.C.Creeps()[0].Store()
	if s.GetUsedCapacity() != 57 || s.GetUsedCapacity(ResEnergy) != 50 || s.GetUsedCapacity(ResH) != 7 || s.Get(ResO) != 0 ||
		s.GetFreeCapacity() != 43 || s.GetFreeCapacity(ResH) != 43 || s.GetCapacity() != 100 || s.GetCapacity(ResEnergy) != 100 {
		t.Errorf("general store: %+v", s)
	}
	// restricted store (e.g. a lab): no general capacity; energy has its own
	f.C.In[b+CreepColSCap], f.C.In[b+CreepColSFree] = 0, 0
	f.C.In[b+CreepColSEnergyCap], f.C.In[b+CreepColSEnergyFree] = 2000, 1500
	r := f.C.Creeps()[0].Store()
	if r.GetCapacity(ResEnergy) != 2000 || r.GetFreeCapacity(ResEnergy) != 1500 || r.GetCapacity(ResH) != 0 || r.GetFreeCapacity(ResH) != 0 {
		t.Errorf("restricted store: %+v", r)
	}
}

func TestGameFacadeAndRoomFinders(t *testing.T) {
	f := libtest.New()
	f.Room(1, -1) // rooms 0 and 1
	f.C.In[HdrTime], f.C.In[HdrCpuBucket], f.C.In[HdrCredits], f.C.In[HdrCpuUnlocked] = 777, 9000, 12345, 1
	f.Spawn(0, 7, true)
	f.C.In[StructureBase+StructureColNameHash] = NameHash("Spawn1")
	f.Creep(0, 1, 5, 5, 0, 0, true, false)
	f.Creep(1, 2, 6, 6, 1, 0, false, false) // hostile, room 1
	f.Creep(2, 3, 7, 7, 1, 0, true, false)
	f.C.In[CreepBase+CreepColNameHash] = NameHash("Bob")

	g := f.C.Game()
	if g.Time != 777 || g.CPU.Bucket != 9000 || !g.CPU.Unlocked || g.Credits != 12.345 {
		t.Errorf("scalars: %+v", g)
	}
	if len(g.Creeps()) != 2 || len(g.Spawns()) != 1 || len(g.Structures()) != 1 || len(g.Rooms()) != 2 {
		t.Errorf("lists: creeps=%d spawns=%d structures=%d rooms=%d", len(g.Creeps()), len(g.Spawns()), len(g.Structures()), len(g.Rooms()))
	}
	if cr, ok := g.Creep("Bob"); !ok || cr.H() != 1 {
		t.Error("Game.Creep(name)")
	}
	if _, ok := g.Creep("Nobody"); ok {
		t.Error("unknown creep name must not match")
	}
	if sp, ok := g.Spawn("Spawn1"); !ok || sp.H() != 7 {
		t.Error("Game.Spawn(name)")
	}
	room1 := g.Rooms()[1]
	if len(room1.Creeps()) != 2 || len(room1.MyCreeps()) != 1 || len(room1.HostileCreeps()) != 1 || len(g.Rooms()[0].Creeps()) != 1 {
		t.Error("room finders")
	}
	if len(g.Rooms()[0].StructuresOfType(StSpawn)) != 1 || len(room1.StructuresOfType(StSpawn)) != 0 {
		// the fixture's spawn has rm 0
		t.Error("StructuresOfType")
	}
}
