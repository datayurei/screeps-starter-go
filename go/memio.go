package main

// The Go side of the persistent store (lib.Mem), as seen by js/host.js and the
// in-game console. All traffic goes through one scratch buffer in Go memory:
// JS asks memScratch(n) for its address, writes bytes there, then calls the
// export that consumes them. Nothing here is on the per-tick hot path unless the
// store changed (memDirty), so a quiet tick costs one call.

import (
	"unsafe"

	"screeps/lib"
)

var (
	scratch []byte // JS -> Go argument/blob buffer
	snap    []byte // Go -> JS encoded store
)

// memScratch makes the scratch buffer at least n bytes and returns its address.
//
//export memScratch
func memScratch(n int) int {
	if n < 16 {
		n = 16
	}
	if cap(scratch) < n {
		scratch = make([]byte, n)
	}
	scratch = scratch[:cap(scratch)]
	return int(uintptr(unsafe.Pointer(&scratch[0])))
}

// memLoad decodes scratch[:n] into the store. Returns 1 on success; on failure
// the store is left empty.
//
//export memLoad
func memLoad(n int) int {
	if n < 0 || n > len(scratch) {
		lib.Mem.Reset()
		return 0
	}
	if lib.Mem.Decode(scratch[:n]) {
		return 1
	}
	return 0
}

// memDirty reports whether the store changed since it was last persisted.
//
//export memDirty
func memDirty() int {
	if lib.Mem.Dirty() {
		return 1
	}
	return 0
}

// memEncode serializes the store and returns its length; the bytes are at memSnap().
//
//export memEncode
func memEncode() int {
	snap = lib.Mem.Encode()
	return len(snap)
}

// memSnap is the address of the bytes memEncode produced.
//
//export memSnap
func memSnap() int {
	if len(snap) == 0 {
		return 0
	}
	return int(uintptr(unsafe.Pointer(&snap[0])))
}

// memClean marks the store persisted (call after the write succeeded).
//
//export memClean
func memClean() { lib.Mem.MarkClean(); snap = nil }

// memDirtyAgain forces another persist attempt (call when the write failed).
//
//export memDirtyAgain
func memDirtyAgain() { lib.Mem.MarkDirty() }

// argKey is the key (or prefix) in scratch[:n].
func argKey(n int) (string, bool) {
	if n < 0 || n > len(scratch) {
		return "", false
	}
	return string(scratch[:n]), true
}

// memDump prints the entries whose key starts with the prefix in scratch[:n],
// one "key = value" line each (at most maxDumpLines), then a summary.
//
//export memDump
func memDump(n int) {
	prefix, ok := argKey(n)
	if !ok {
		println("[mem] bad prefix")
		return
	}
	const maxDumpLines = 200
	lines := lib.Mem.Format(prefix)
	for i, l := range lines {
		if i == maxDumpLines {
			println("[mem] ... and", len(lines)-maxDumpLines, "more (narrow the prefix)")
			break
		}
		println("[mem]", l)
	}
	println("[mem]", len(lines), "of", lib.Mem.Len(), "entries shown")
}

// memStat prints the entry count and encoded size.
//
//export memStat
func memStat() {
	dirty := ""
	if lib.Mem.Dirty() {
		dirty = ", unsaved changes"
	}
	println("[mem]", lib.Mem.Len(), "entries,", len(lib.Mem.Encode()), "bytes encoded"+dirty)
}

// memSetInt / memSetFloat / memSetStr write the key in scratch[:klen]; for
// memSetStr the value is the vlen bytes that follow the key. Each returns 1 on
// success.
//
//export memSetInt
func memSetInt(klen, v int) int {
	k, ok := argKey(klen)
	if !ok {
		return 0
	}
	lib.Mem.SetInt(k, v)
	return 1
}

//export memSetFloat
func memSetFloat(klen int, v float64) int {
	k, ok := argKey(klen)
	if !ok {
		return 0
	}
	lib.Mem.SetFloat(k, v)
	return 1
}

//export memSetStr
func memSetStr(klen, vlen int) int {
	k, ok := argKey(klen)
	if !ok || vlen < 0 || klen+vlen > len(scratch) {
		return 0
	}
	lib.Mem.SetStr(k, string(scratch[klen:klen+vlen]))
	return 1
}

// memDelete removes the key in scratch[:klen], or with prefix != 0 every key
// starting with it (an empty prefix clears the store). Returns how many.
//
//export memDelete
func memDelete(klen, prefix int) int {
	k, ok := argKey(klen)
	if !ok {
		return 0
	}
	if prefix != 0 {
		return lib.Mem.DeletePrefix(k)
	}
	if lib.Mem.Has(k) {
		lib.Mem.Delete(k)
		return 1
	}
	return 0
}
