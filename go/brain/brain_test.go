package brain

import (
	"testing"

	. "screeps/lib"
	"screeps/lib/libtest"
)

func TestSpawnsHarvesterWhenNoOwnCreeps(t *testing.T) {
	f := libtest.New()
	f.Room(0, -1)
	f.Spawn(0, 7, true)
	f.C.In[HdrTime] = 123
	Tick(f.C, 0)
	if f.C.Out[OutNCmd] != 1 {
		t.Fatalf("want 1 command, got %d", f.C.Out[OutNCmd])
	}
	op, _, w := libtest.Cmd(f.C, 0)
	if op != OpStructureSpawnSpawnCreep || w[1] != 7 {
		t.Errorf("want spawnCreep on handle 7, got op=%d recv=%d", op, w[1])
	}
}

func TestNoSpawnOrBusySpawnMeansNoSpawnCommand(t *testing.T) {
	f := libtest.New()
	Tick(f.C, 0)
	if f.C.Out[OutNCmd] != 0 {
		t.Error("no spawn: must not emit commands")
	}
	f = libtest.New()
	f.Spawn(0, 7, false)
	Tick(f.C, 0)
	if f.C.Out[OutNCmd] != 0 {
		t.Error("busy spawn and no creeps: must not spawn")
	}
}

func TestHarvestsNearestActiveSourceInSameRoom(t *testing.T) {
	f := libtest.New()
	f.Room(0, 50)
	f.Spawn(0, 7, true)
	f.Source(0, 20, 40, 40, 0, 3000) // far
	f.Source(1, 21, 12, 10, 0, 3000) // nearest in room 0
	f.Source(2, 22, 11, 10, 0, 0)    // nearer but empty
	f.Source(3, 23, 10, 11, 1, 3000) // nearer but other room
	f.Creep(0, 30, 10, 10, 0, 50, true, false)
	Tick(f.C, 0)
	op, flags, w := libtest.Cmd(f.C, 0)
	if op != OpCreepHarvest || flags&0x8000 == 0 {
		t.Fatalf("want harvest with approach flag, got op=%d flags=%#x", op, flags)
	}
	if w[1] != 30 || w[2] != 21 {
		t.Errorf("want creep 30 harvesting source 21, got recv=%d target=%d", w[1], w[2])
	}
	if preset := f.C.Cmd[0] >> 16 & 0x7fff; preset != int32(MoveGreenPath) {
		t.Errorf("approach move preset = %d, want %d", preset, MoveGreenPath)
	}
}

func TestFullCreepUpgradesRoomController(t *testing.T) {
	f := libtest.New()
	f.Room(0, 50)
	f.Spawn(0, 7, true)
	f.Creep(0, 30, 10, 10, 0, 0, true, false)
	Tick(f.C, 0)
	op, _, w := libtest.Cmd(f.C, 0)
	if op != OpCreepUpgradeController || w[1] != 30 || w[2] != 50 {
		t.Errorf("got op=%d recv=%d target=%d", op, w[1], w[2])
	}
}

func TestIgnoresHostileAndSpawningCreeps(t *testing.T) {
	f := libtest.New()
	f.Room(0, 50)
	f.Spawn(0, 7, true)
	f.Creep(0, 30, 10, 10, 0, 50, false, false) // hostile
	f.Creep(1, 31, 10, 10, 0, 50, true, true)   // ours but spawning
	Tick(f.C, 0)
	// one own creep exists (the spawning one), so no spawn either
	if f.C.Out[OutNCmd] != 0 {
		t.Errorf("want no commands, got %d", f.C.Out[OutNCmd])
	}
}
