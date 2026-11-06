// Package main is the entry point for the Screeps bot, compiled to WebAssembly
// with TinyGo.
//
// Architecture: the JS side (js/host.js) owns ALL game I/O. Each tick it packs
// the game state into the shared tables of `ctx`, then calls the exported
// `tick(round)` one or more times: Go decides (package brain, built on package lib) and writes
// command and query records back. JS answers queries between rounds and
// finally executes the commands. Go never touches syscall/js on this path, so
// a tick costs zero JS<->WASM crossings beyond the tick calls themselves
// (each syscall/js call would cost ~0.5 us; see README "Performance").
//
// Build: see scripts/build.sh  (tinygo build -o dist/main.wasm -target=wasm ...)
package main

import (
	"unsafe"

	"screeps/brain"
	"screeps/lib"
)

// ctx is the whole shared state. It lives in Go's linear memory; JS lays typed
// arrays over its fields using the addresses from addr(). Static data never
// moves, but the memory's ArrayBuffer can be replaced if memory grows, so JS
// rebuilds its views when memory.buffer changes.
var ctx lib.Ctx

// main runs once when the WASM instance is created (via go.run in the loader).
// It must return; the exported functions stay callable for the life of the
// instance because we build with -scheduler=none.
func main() {
	println("[go] WASM instance initialized")
}

// addr returns the address of a shared buffer. The indices are a contract with
// js/host.js (checked indirectly by layoutHash, which covers the generated layout).
//
//export addr
func addr(buf int) int {
	switch buf {
	case 0:
		return int(uintptr(unsafe.Pointer(&ctx.In[0])))
	case 1:
		return int(uintptr(unsafe.Pointer(&ctx.Out[0])))
	case 2:
		return int(uintptr(unsafe.Pointer(&ctx.Cmd[0])))
	case 3:
		return int(uintptr(unsafe.Pointer(&ctx.Qry[0])))
	case 4:
		return int(uintptr(unsafe.Pointer(&ctx.Arena[0])))
	case 5:
		return int(uintptr(unsafe.Pointer(&ctx.Bytes[0])))
	case 6:
		return int(uintptr(unsafe.Pointer(&ctx.Resp[0])))
	case 7:
		return int(uintptr(unsafe.Pointer(&ctx.RespIdx[0])))
	case 8:
		return int(uintptr(unsafe.Pointer(&ctx.Res[0])))
	case 9:
		return int(uintptr(unsafe.Pointer(&ctx.Terrain[0])))
	case 10:
		return int(uintptr(unsafe.Pointer(&ctx.StoreArena[0])))
	}
	return 0
}

// layoutHash identifies the generated wire layout; the loader refuses a
// js/host_gen.js whose hash differs from this build's.
//
//export layoutHash
func layoutHash() int { return lib.LayoutHash }

// tick runs one decision round (0 .. lib.MaxRounds-1). Round 0 clears what
// Go emitted last tick. It returns 0 on success, 1 if the round panicked, in
// which case JS must not act on the buffers.
//
//export tick
func tick(round int) (status int) {
	defer func() {
		if r := recover(); r != nil {
			println("[go] panic in tick")
			status = 1
		}
	}()
	if round == 0 {
		ctx.Begin()
	}
	brain.Tick(&ctx, round)
	return 0
}
