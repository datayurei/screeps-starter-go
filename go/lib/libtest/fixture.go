// Package libtest builds Ctx snapshots the way js/host.js would, for tests of
// the lib and brain packages. It imports no syscall/js, so `go test` runs on the host.
package libtest

import . "screeps/lib"

// Fixture fills a Ctx's tables by hand.
type Fixture struct{ C *Ctx }

// New returns an empty fixture.
func New() *Fixture { return &Fixture{C: new(Ctx)} }

// Room sets room row i (and the room count to i+1) with a controller handle.
func (f *Fixture) Room(i int, controller Handle) {
	f.C.In[HdrNRoom] = int32(i + 1)
	b := RoomBase + i*RoomStride
	f.C.In[b] = int32(1000 + i)
	f.C.In[b+RoomColController] = int32(controller)
}

// Spawn sets structure row i to one of your spawns.
func (f *Fixture) Spawn(i int, h Handle, idle bool) {
	f.C.In[HdrNStructure] = int32(i + 1)
	b := StructureBase + i*StructureStride
	f.C.In[b] = int32(h)
	f.C.In[b+StructureColType], f.C.In[b+StructureColMine] = StSpawn, 1
	if !idle {
		f.C.In[b+StructureColSpawning] = 1
	}
}

// Source sets source row i.
func (f *Fixture) Source(i int, h Handle, x, y, room, energy int32) {
	f.C.In[HdrNSource] = int32(i + 1)
	b := SourceBase + i*SourceStride
	f.C.In[b] = int32(h)
	f.C.In[b+SourceColX], f.C.In[b+SourceColY], f.C.In[b+SourceColRm], f.C.In[b+SourceColEnergy] = x, y, room, energy
}

// Creep sets creep row i.
func (f *Fixture) Creep(i int, h Handle, x, y, room, free int32, mine, spawning bool) {
	f.C.In[HdrNCreep] = int32(i + 1)
	b := CreepBase + i*CreepStride
	f.C.In[b] = int32(h)
	f.C.In[b+CreepColX], f.C.In[b+CreepColY], f.C.In[b+CreepColRm], f.C.In[b+CreepColSFree] = x, y, room, free
	if mine {
		f.C.In[b+CreepColMine] = 1
	}
	if spawning {
		f.C.In[b+CreepColSpawning] = 1
	}
}

// Cmd returns emitted command i as (op, flags, words); words[1] is the receiver.
func Cmd(c *Ctx, i int) (op, flags int32, w []int32) {
	b := i * CmdWords
	return c.Cmd[b] & 0x7fff, c.Cmd[b] &^ 0x7fff, c.Cmd[b : b+CmdWords]
}
