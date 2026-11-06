package lib

// Hand-written runtime for the generated Ctx (see gen.go): command and
// query emission, the argument arenas, and position/room encodings.

func clampN(n int32, max int) int {
	if n < 0 {
		return 0
	}
	if int(n) > max {
		return max
	}
	return int(n)
}

func (c *Ctx) b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// milli converts a float to thousandths, rounding half away from zero.
func milli(f float64) int32 {
	if f < 0 {
		return int32(f*1000 - 0.5)
	}
	return int32(f*1000 + 0.5)
}

// Begin starts a tick: it clears everything Go emitted last tick. Call it once,
// before round 0.
func (c *Ctx) Begin() {
	for i := range c.Out {
		c.Out[i] = 0
	}
}

func (c *Ctx) overflow() { c.Out[OutOverflow]++ }

// A reference into the arenas is off<<12 | len (len < 4096).
func ref(off, n int) int32 { return int32(off<<12 | n) }

func (c *Ctx) putStr(s string) int32 {
	n := len(s)
	if n == 0 {
		return 0
	}
	if n > 4095 {
		n = 4095
	}
	off := int(c.Out[OutNBytes])
	if off+n > BytesCap {
		c.overflow()
		return 0
	}
	copy(c.Bytes[off:], s[:n])
	c.Out[OutNBytes] += int32(n)
	return ref(off, n)
}

func (c *Ctx) putInts(a []int32) int32 {
	n := len(a)
	if n == 0 {
		return 0
	}
	if n > 4095 {
		n = 4095
	}
	off := int(c.Out[OutNArena])
	if off+n > ArenaCap {
		c.overflow()
		return 0
	}
	copy(c.Arena[off:], a[:n])
	c.Out[OutNArena] += int32(n)
	return ref(off, n)
}

func (c *Ctx) putHandles(a []Handle) int32 {
	n := len(a)
	if n == 0 {
		return 0
	}
	if n > 4095 {
		n = 4095
	}
	off := int(c.Out[OutNArena])
	if off+n > ArenaCap {
		c.overflow()
		return 0
	}
	for i := 0; i < n; i++ {
		c.Arena[off+i] = int32(a[i])
	}
	c.Out[OutNArena] += int32(n)
	return ref(off, n)
}

// emit appends one command record: [op|flags, recv, a0..a4].
func (c *Ctx) emit(op, flags, recv, a0, a1, a2, a3, a4 int32) {
	n := int(c.Out[OutNCmd])
	if n >= CmdCap {
		c.overflow()
		return
	}
	b := n * CmdWords
	c.Cmd[b], c.Cmd[b+1], c.Cmd[b+2], c.Cmd[b+3] = op|flags, recv, a0, a1
	c.Cmd[b+4], c.Cmd[b+5], c.Cmd[b+6] = a2, a3, a4
	c.Out[OutNCmd]++
}

// ask appends one query record and returns its id.
func (c *Ctx) ask(op, recv, a0, a1, a2, a3, a4 int32) Query {
	n := int(c.Out[OutNQry])
	if n >= QryCap {
		c.overflow()
		return -1
	}
	b := n * CmdWords
	c.Qry[b], c.Qry[b+1], c.Qry[b+2], c.Qry[b+3] = op, recv, a0, a1
	c.Qry[b+4], c.Qry[b+5], c.Qry[b+6] = a2, a3, a4
	c.Out[OutNQry]++
	return Query(n)
}

// Answered reports whether the query has been answered (true from the round
// after it was asked).
func (c *Ctx) Answered(q Query) bool { return q >= 0 && int32(q) < c.Out[OutNAnswered] }

// Answer is the query's result words, or nil if not yet answered. The meaning
// of the words is the query's return kind in tools/spec_api.go: int/bool/handle
// are one word, handles is a list, steps is (x, y, direction) triples, and
// custom:* kinds are documented on their marshaller in js/host.js.
func (c *Ctx) Answer(q Query) []int32 {
	if !c.Answered(q) {
		return nil
	}
	off, n := c.RespIdx[q*2], c.RespIdx[q*2+1]
	return c.Resp[off : off+n]
}

// PrevResult is the ERR_* / OK code of command i from the previous tick.
// Valid at the start of a tick, before Begin clears the counts; commands and
// their results are index-aligned.
func (c *Ctx) PrevResult(i int) int32 { return c.Res[i] }

// ---- positions and rooms --------------------------------------------------------

// MakeRoomRef encodes world room coordinates (E0S0 is 0,0; W0 is x=-1, N0 is y=-1).
func MakeRoomRef(rx, ry int) RoomRef { return RoomRef((rx+512)<<10 | (ry + 512)) }

func (r RoomRef) X() int { return int(r>>10) - 512 }
func (r RoomRef) Y() int { return int(r&1023) - 512 }

// MakePos packs a position: x | y<<6 | room<<12 (32 bits).
func MakePos(x, y int, r RoomRef) Pos { return Pos(uint32(x) | uint32(y)<<6 | uint32(r)<<12) }

func (p Pos) X() int        { return int(uint32(p) & 63) }
func (p Pos) Y() int        { return int(uint32(p) >> 6 & 63) }
func (p Pos) Room() RoomRef { return RoomRef(uint32(p) >> 12) }
