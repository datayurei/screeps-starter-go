package lib

// Hand-written helpers for the object-style layer (the typed wrappers are in
// the generated obj_gen.go).

// Object is anything that names a game object: every wrapper (Creep, Source,
// Structure, ...) and a bare Handle.
type Object interface{ Handle() Handle }

// Handle makes a bare Handle usable as an Object.
func (h Handle) Handle() Handle { return h }

// Positioned is an object with a position inside a packed room.
type Positioned interface {
	X() int32
	Y() int32
	Rm() int32
}

// Filter returns the elements for which keep is true.
func Filter[T any](xs []T, keep func(T) bool) []T {
	var r []T
	for _, x := range xs {
		if keep(x) {
			r = append(r, x)
		}
	}
	return r
}

// Count returns how many elements satisfy keep.
func Count[T any](xs []T, keep func(T) bool) int {
	n := 0
	for _, x := range xs {
		if keep(x) {
			n++
		}
	}
	return n
}

// RangeTo is RoomPosition.getRangeTo for two objects in the same room, or -1 if
// they are in different rooms (use RangeG with room refs for that).
func RangeTo(a, b Positioned) int32 {
	if a.Rm() != b.Rm() {
		return -1
	}
	return Range(a.X(), a.Y(), b.X(), b.Y())
}

// Closest returns the element nearest to from by range, among those in the same
// room. Ties go to the earlier element. ok is false if there is none. This is a
// straight-line range, like findClosestByRange; it does no pathfinding.
func Closest[T Positioned](from Positioned, xs []T) (best T, ok bool) {
	bestD := int32(1 << 30)
	for _, x := range xs {
		if d := RangeTo(from, x); d >= 0 && d < bestD {
			best, bestD, ok = x, d, true
		}
	}
	return
}

// InRangeOf returns the elements within rng of from (same room).
func InRangeOf[T Positioned](from Positioned, xs []T, rng int32) []T {
	return Filter(xs, func(x T) bool {
		d := RangeTo(from, x)
		return d >= 0 && d <= rng
	})
}
