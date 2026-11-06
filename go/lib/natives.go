package lib

// Native versions of pure RoomPosition methods: no engine call, no query.
// Coordinates are within one room unless the G variants are used.

func abs32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// Range is RoomPosition.getRangeTo for two positions in the same room.
func Range(x1, y1, x2, y2 int32) int32 { return max32(abs32(x1-x2), abs32(y1-y2)) }

// Global converts a room-local position to world coordinates.
func Global(x, y int32, r RoomRef) (gx, gy int32) {
	return int32(r.X())*50 + x, int32(r.Y())*50 + y
}

// RangeG is getRangeTo across rooms, via world coordinates. (Like the engine
// for adjacent rooms; it does not account for room exits.)
func RangeG(x1, y1 int32, r1 RoomRef, x2, y2 int32, r2 RoomRef) int32 {
	ax, ay := Global(x1, y1, r1)
	bx, by := Global(x2, y2, r2)
	return Range(ax, ay, bx, by)
}

// InRange is RoomPosition.inRangeTo.
func InRange(x1, y1, x2, y2, rng int32) bool { return Range(x1, y1, x2, y2) <= rng }

// IsNear is RoomPosition.isNearTo.
func IsNear(x1, y1, x2, y2 int32) bool { return Range(x1, y1, x2, y2) <= 1 }

// IsEqual is RoomPosition.isEqualTo.
func IsEqual(x1, y1, x2, y2 int32) bool { return x1 == x2 && y1 == y2 }

// Direction constants as used by the game.
const (
	Top         = 1
	TopRight    = 2
	Right       = 3
	BottomRight = 4
	Bottom      = 5
	BottomLeft  = 6
	Left        = 7
	TopLeft     = 8
)

// DirectionTo is RoomPosition.getDirectionTo (same room); 0 if the points coincide.
func DirectionTo(x1, y1, x2, y2 int32) int32 {
	dx, dy := x2-x1, y2-y1
	adx, ady := abs32(dx), abs32(dy)
	switch {
	case dx == 0 && dy == 0:
		return 0
	case adx > ady*2:
		if dx > 0 {
			return Right
		}
		return Left
	case ady > adx*2:
		if dy > 0 {
			return Bottom
		}
		return Top
	case dx > 0 && dy > 0:
		return BottomRight
	case dx > 0:
		return TopRight
	case dy > 0:
		return BottomLeft
	}
	return TopLeft
}

// StoreAmount reads one resource's amount from a row's store arena slice
// (sOff, sN columns): the arena holds (resource, amount) pairs for everything
// except energy, which has its own column. It is the packed form of
// Store.getUsedCapacity(resource) for non-energy resources.
func (c *Ctx) StoreAmount(off, n, res int32) int32 {
	if n <= 0 || int(off)+int(n)*2 > StoreArenaCap {
		return 0
	}
	for i := int32(0); i < n; i++ {
		if c.StoreArena[off+i*2] == res {
			return c.StoreArena[off+i*2+1]
		}
	}
	return 0
}
