# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A [Screeps](https://screeps.com) bot written in **Go**, compiled to **WebAssembly** with
**TinyGo**, driven by a thin JS loader. The bot lives in `go/` and `js/`. Build and deploy are
**pure Go + TinyGo — no Node/npm, no bundler**: `tools/` is a small stdlib-only Go program that
patches `wasm_exec.js` and uploads to the Screeps code API, and the JS is hand-written CommonJS
that Screeps runs directly. Deploy scaffolding is adapted from `screeps-typescript-starter`.

## Commands

```sh
./scripts/build.sh                            # TinyGo compile + assemble dist/ (no upload)
./scripts/push.sh main                        # build + upload to "main" in screeps.json (also sim/pserver/season)
( cd go && GOOS=js GOARCH=wasm go vet ./... )  # vet the bot (syscall/js needs the wasm target)
( cd tools && go vet ./... )                   # the build/upload tool
```

- **Tests:** `( cd go && go test ./lib/... ./brain/ )` runs on the host (`go/lib` and `go/brain` have no `syscall/js`
  import). Plain `go test ./...` **fails** ("build constraints exclude all Go files in
  .../syscall/js") because `package main` and `go/screeps` import `syscall/js`, so always name the
  package. Single test: `cd go && go test -run TestName ./brain/`. Keep decision logic in
  `brain` so it stays testable; running tests under `GOOS=js` would need `go_js_wasm_exec` + Node.
- The Go module is named `screeps` (`go/go.mod`), which is why the wrapper imports as
  `"screeps/screeps"`.
- The deploy artifact is built by **TinyGo**, which must be on PATH (`tinygo version`); plain
  `GOOS=js` Go is intentionally not used (too big).
- `go vet` on the bot **must** set `GOOS=js GOARCH=wasm`, or `syscall/js` is excluded and vet
  errors ("build constraints exclude all Go files in .../syscall/js").
- Uploading needs a `screeps.json` (gitignored) created from `screeps.sample.json` with a real
  token (official) or email/password (private server).
- `tools/` is a separate Go module; `scripts/*` invoke it with `(cd tools && go run . …)`.

## Architecture

Screeps calls the loader's exported `loop()` once per game tick. These files map to separately
uploaded Screeps modules, assembled into `dist/` by the build:

| Source        | dist artifact     | Uploaded module name | Role                          |
|---------------|-------------------|----------------------|-------------------------------|
| `js/loader.js`| `dist/main.js`    | `main`               | entry; engine runs this       |
| `js/host.js`   | `dist/host.js`     | `host`                | all game I/O: pack → Go → act |
| `js/host_gen.js` | `dist/host_gen.js` | `host_gen`          | GENERATED tables (columns, ops, queries) |
| `go/` (`main.go` + `brain/`) | `dist/main.wasm` | `main.wasm` (binary) | the bot's decisions |
| (TinyGo glue) | `dist/wasm_exec.js`| `wasm_exec`         | TinyGo runtime, copied in     |

Key mechanics that span these files:

- **The JS is plain CommonJS; the build just copies it.** `scripts/build.sh` runs
  `tinygo build` → `dist/main.wasm`, copies+patches TinyGo's version-matched `wasm_exec.js`,
  and `cp`s `js/loader.js`→`dist/main.js`, and `js/host.js`→`dist/host.js`
  (a new JS module must also be added to the module map in `tools/main.go` `upload`). No bundler/transpile
  — Screeps runs CommonJS directly. `loader.js` `require`s `host`, `wasm_exec`, and `main.wasm`
  by their uploaded module names; the `require(...)` calls go straight to the server.
  `scripts/push.sh` then uploads `dist/` via the Go tool in `tools/`.

- **The WASM instance is created once, then reused across ticks.** `loader.js` guards
  instantiation on `Game.cpu.bucket >= 500` (instantiation is CPU-heavy). A thrown error in
  any tick nulls the instance to force clean re-instantiation next tick. `go/main.go`'s
  `main()` runs once at instantiation (one-time setup); the `//export tick` function runs
  every tick. Screeps calls the loader's `loop()`, which calls `host.run()`, which calls Go's `tick`.

- **Game I/O is in JS; Go only decides, through shared memory.** Each tick `js/host.js` (1) **packs**
  the game into int32 tables inside Go's linear memory (one table per object kind, one row per
  object, one column per property, plus a header of scalars), (2) calls Go's exported
  `tick(round)`: `go/brain`'s `Tick(c *Ctx, round)` (types from `go/lib`) reads columns (`c.CreepHits(i)`) and appends
  **command records** (`c.CreepHarvest(creep, source)`) and **queries** (`c.QRoomFind(...)`); if it
  asked queries, JS answers them (the engine does the work) and calls `tick(round+1)` (max 4
  rounds); `Ctx.Answer(q)` returns the result words, (3) JS **executes** the commands and writes each
  result code to `Res`. No `syscall/js` is involved, so a tick has no per-field crossings.
  Objects are named by **handles** (index into a per-tick JS registry, so they are valid for one tick
  only); use the `idA/idB/idC` columns and `Ctx.FindByID` for identity across ticks. Positions are
  `Pos` (x|y<<6|roomRef<<12), rooms are `RoomRef`. `Approach` command variants moveTo the target
  when the action returns ERR_NOT_IN_RANGE.

- **The wire layout is GENERATED.** `tools/spec_state.go` (columns) and `tools/spec_api.go` (ops,
  queries, natives, JS-only entries) are the single source of truth. `go run ./tools gen .`
  (run by `scripts/build.sh`) writes `go/lib/gen.go`, `go/lib/obj_gen.go`, `js/host_gen.js` and
  `docs/api-coverage.md`; never edit those. Generation **fails unless every one of the 661 entries
  in `api/inventory.json` (a snapshot of docs.screeps.com/api) has a disposition**, so the coverage
  claim is enforced. `LayoutHash` is embedded on both sides and `host.bind()` throws on mismatch.
  `js/host.js` supplies the helpers the generated code calls, `PRESETS` (options objects Go selects
  by index), and the `MARSHAL` functions that flatten non-trivial query results (row shapes are
  documented above `MARSHAL`).

- **Game/Memory are bridged per tick** (still needed by `go/screeps`; `host.js` uses the real
  per-tick locals directly). Screeps injects `Game`/`Memory`/`RawMemory` as
  per-tick module locals, not on `globalThis`. TinyGo's `js.Global()` *is* `globalThis`, so
  `loader.js` copies them onto `globalThis` each tick before wrapper code runs, or Go sees
  `undefined`. Persistent globals (`console`, `FIND_*`/`OK`/`ERR_*` constants) are already on
  `globalThis` and are read directly from Go via `syscall/js`.

- **`loader.js` polyfills the Screeps sandbox**: `globalThis`, `TextEncoder`/`TextDecoder`,
  and a `Reflect` shim that TinyGo's `syscall/js` glue depends on. Rarely needs changes.

## Writing bot logic

Decisions go in **`go/brain/brain.go`** (`Tick`), written against typed row wrappers that read like
the JS API but are index-based under the hood:

```go
for _, cr := range Filter(c.Creeps(), func(x Creep) bool { return x.IsMine() }) {
    if cr.FreeCapacity() > 0 {
        if src, ok := Closest(cr, sources); ok { cr.HarvestApproach(src, MoveGreenPath) }
    }
}
```

The facade mirrors the original API's shape (`go/lib/game.go` + generated `go/lib/obj_gen.go`):

| Original | Go |
|---|---|
| `Game.time`, `Game.cpu.bucket`, `Game.gcl.level` | `g := c.Game(); g.Time, g.CPU.Bucket, g.GCL.Level` |
| `Game.creeps` / `.spawns` / `.structures` / `.constructionSites` | `g.Creeps()` / `g.Spawns()` / `g.Structures()` / `g.ConstructionSites()` (yours only, like the original) |
| `Game.creeps['Bob']`, `Game.spawns['S']`, `Game.flags['f']` | `g.Creep("Bob")`, `g.Spawn("S")`, `g.Flag("f")` (matched by name hash; ASCII names) |
| `Game.getObjectById(id)` | `g.GetObjectById(id)` -> `Handle` (an `Object`), -1 if not among the packed objects |
| `room.find(FIND_...)` | `room.Creeps()`, `.Sources()`, `.Structures()`, ... plus `MyCreeps()`, `HostileCreeps()`, `StructuresOfType(StTower)` |
| `creep.pos`, `pos.getRangeTo(t)`, `.inRangeTo`, `.isNearTo`, `.getDirectionTo` | `cr.Pos()`, `pos.GetRangeTo(t)`, `.InRangeTo(t, r)`, `.IsNearTo(t)`, `.GetDirectionTo(t)` (cross-room via world coordinates) |
| `pos.findClosestByRange(list)`, `.findInRange(list, r)` | `FindClosestByRange(pos, list)`, `FindInRange(pos, list, r)` (functions: Go methods can't be generic) |
| `creep.store.getFreeCapacity(res)` etc. | `cr.Store().GetFreeCapacity(res)`, `GetUsedCapacity`, `GetCapacity`, `Get` |

`Creep`, `Structure`, `Source`, `Room`, ... (generated into `go/lib/obj_gen.go`) have a method per column
(`cr.Hits()`), per action (`cr.Harvest(obj)`, `...Approach`), and per query (`room.QFind(...)`). `Object` is
anything with `Handle()`: any wrapper or a bare `Handle`. Helpers: `Filter`, `Count`, `Closest` (same-room
straight-line range, no pathfinding; use this for "what can this creep reach"), `InRangeOf`, `RangeTo`. The same calls exist flat on `*Ctx`
(`c.CreepHits(i)`, `c.CreepHarvest(h, h)`) if you prefer. Semantics are unchanged by the sugar: reads are
a start-of-tick snapshot, actions are queued, wrappers are valid for one tick only.

- **Read state:** call the generated accessors (`c.StructureHits(i)`, `c.NSource()`, `c.Time()`...).
  Missing a property? Add a column in `tools/spec_state.go` (a JS expression over `o`) and regenerate.
- **Act:** call the generated builder for the API method (`c.CreepTransfer(...)`); its name is
  `Class` + `Method` (+ overload tag). `docs/api-coverage.md` maps every API entry to its Go name.
- **Need an engine answer** (pathfinding, `find`, `look`, market, map): ask in round 0
  (`q := c.QRoomFind(...)`), read `c.Answer(q)` in round 1+. Check `c.Answered(q)`.
- **Pure math** (`Range`, `DirectionTo`, `StoreAmount`, ...) is in `go/lib/natives.go`.
- **Options objects** (moveTo opts, spawn memory, find filters) are `PRESETS` in `js/host.js`
  referenced by index (`brain.MoveGreenPath`); add one there and a constant in brain.
- Rules: `brain` must not import `syscall/js` (host tests depend on it); handles die with the tick;
  numeric memory is 4 slots per object (`Memory.set` op, `mem0..mem3` columns); for anything
  richer use the persistent store `Mem` (below). Arbitrary JS-structured `Memory` stays in JS.
  Other strings/blobs (RawMemory, foreign segments, market ids) are JS-side by design;
  see the `js` rows in the coverage doc. Logging from Go is `println`.
- Tests: `cd go && go test ./brain/` (fixtures set table cells via the generated `<Kind>Col<Name>`
  constants). `node scripts/e2e.js` (optional, needs Node, run after a build) executes a real tick
  through `dist/` with a mocked game and smoke-tests every generated op/query against a recording
  proxy.
- CPU: packing cost is JS-side and scales with objects x columns. Turn off tables the brain doesn't
  read with `PACK.<kind> = false` in `js/host.js` (the structure table is the biggest).

`go/screeps` (typed `syscall/js` wrapper) is no longer used by the bot; each call is a ~0.5 µs
crossing. Keep it for experiments only.

The bot spawns one `[WORK,CARRY,MOVE]` harvester when it has no creeps, sends each creep to the
nearest same-room active source (Chebyshev range, not a path search) until full, then to its
room's controller.

## Persistent store (`lib.Mem`)

`Mem` (`go/lib/store.go`, type `KV`) is a key-value store that lives in Go memory and survives global
resets and re-instantiation. Keys are slash paths; values are int, float, string or bytes:
`Mem.SetInt("creep/"+id+"/role", 3)`, `Mem.Int(k)`, `Mem.IntOr(k, def)`, `Mem.Delete`, `Mem.DeletePrefix`,
`Mem.Keys(prefix)`, and `Mem.Sweep("creep/", alive)` to drop dead objects' entries. `ObjKey(kind, IdA, IdB, IdC, field)`
builds the conventional per-object key (creep names are not visible to Go; the id words are).

How it persists (`js/host.js`, "persistent store" section; Go side in `go/memio.go`):
- After a tick in which the store changed (`memDirty`), Go encodes it (`memEncode`: versioned, sorted, FNV
  checksum), JS base64s it into `RawMemory.segments[90..93]` (`MEM_SEG_*` constants) and rewrites only the
  chunks that differ. It does this in a `finally`, so writes made before a throwing tick are kept.
- `RawMemory.get/set` is the Memory JSON string itself, so it can't carry the blob; segments are used instead.
  They must be re-requested every tick: `run()` calls `setActiveSegments` last, which **overrides** a
  `RawMemory.setActiveSegments` command emitted by Go. Segments 90-93 are reserved.
- On a fresh instance JS feeds the saved bytes to `memLoad` before the first `tick`. If the segments are not
  readable yet (only the very first tick ever) the bot skips up to 2 ticks; if they never become readable it
  runs with an empty store that is **not** saved, so real data is never overwritten by a failed read.
- A blob that fails the checksum or has another `StoreVersion` is discarded (store starts empty, overwritten
  on the next change). **Bump `StoreVersion` whenever the encoding changes.** Capacity is 4 x 100000 base64
  chars (about 300 KB); over that, the write is refused with a log line.
- Console (registered on bind, so not available on the very first console command after a reset):
  `mem(prefix?)` dumps entries, `memsize()`, `memset(key, numberOrString)`, `memdel(key)`, `memclear(prefix)`
  (`memclear("")` clears all). Output goes through Go `println`.
- Test: `go test ./lib/` (round trip, damage rejection) and `node scripts/e2e.js` (console write, save, restore
  in a fresh instance, damaged blob).

## Performance model

Measured in-game on shard3: Go compute is ~10× faster than JS (~0.5 ns vs ~5 ns/iter), but each
JS↔WASM `syscall/js` crossing costs a flat ~500–600 ns (vs ~2 ns for a native JS property read). A
Go bot that reads the game object by object is dominated by that cost; the naive "pull" port in
the prototype was ~27× slower than plain JS, while the shared-memory path was ~23× cheaper than
pull and on par with pure JS at light compute. **The real bot uses the shared-memory design**
(see Architecture), so a tick costs the packing writes plus one `tick()` call. Those figures were
measured on a synthetic benchmark (since removed), not on `host.js`; measure real CPU before quoting
them for the bot.

## Gotchas

- **`-scheduler=none`** (in `scripts/build.sh`) keeps the binary small and exported functions
  callable after `main()` returns, but assumes a synchronous loop. If you introduce
  goroutines, channels, `time.Sleep`, or `js.FuncOf` callbacks, you must remove that flag.
- The build **must** run before upload; `scripts/push.sh` chains `build.sh` then the Go uploader.
- Keep `wasm_exec.js` version-matched to the installed TinyGo — `build.sh` always re-copies it
  from `$(tinygo env TINYGOROOT)`, so never hand-edit `dist/wasm_exec.js`. It then runs the
  `tools/` `patch` command on the copy (see below) — that patch is part of the build. Edit the
  patch rules in `tools/main.go`.

## Screeps + TinyGo sandbox gotchas (official server)

The **official server isolate is much stricter than the Sim**. The Sim (a Chrome extension)
provides a browser-like global; the official isolate does not, so code that runs in the Sim can
fail only on a live server. All of the following are already handled in `js/loader.js` and the
`tools/` patch — this records *why*, since they're easy to reintroduce.

- **`wasm_exec.js` reads `crypto.getRandomValues` and `performance.now` at eval time.** The
  isolate exposes neither. The loader polyfills both *at module-load time* (a `Math.random`
  PRNG and a `Date.now`-based clock) **before** `wasm_exec` is required.
- **`wasm_exec` is required lazily inside `instantiate()`**, not at the top of `loader.js`, so
  the polyfills above run first. A top-level `require("wasm_exec")` would execute before them and
  crash on load. Any new load-time global a future `wasm_exec` needs must go in that same
  top-of-loader polyfill block.
- **`Game`/`Memory`/`console` are not plain writable globals.** `Memory` is a read-only global
  getter (assigning it throws); `console` is a per-module lexical binding, not on `globalThis`.
  `bridgeGlobal()` assigns what it can and swallows the read-only throw. Go logs via **stdout**
  (`println` → `wasm_exec`'s `fs.writeSync` → `console.log`), never via `js.Global().Get("console")`.
- **The one that looks like a deep ABI bug but isn't:** the isolate has **no native
  `TextDecoder`**, so the loader's polyfill is used — and `wasm_exec`'s `loadString` passes a
  **`DataView`**. `new Uint8Array(aDataView)` yields an *empty* array (a DataView has no
  `length`), so every decoded property name became `""` and **every `js.Value.Get()`/`.Call()`
  returned `undefined`** — including `js.Global().Get("Game")`. The symptom mimics a broken
  global or an i64/BigInt return problem; it is neither. The polyfill must view a `DataView`'s
  bytes (`new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)`). Logging worked throughout
  only because that path decodes a `Uint8Array`, not a `DataView`.
- **The `tools/` patch rewrites `Reflect.*` in the syscall/js glue to direct property access**
  (`v[prop]`), because the `Reflect` visible to the `wasm_exec` module misbehaves in the isolate.
  `build.sh` applies it after copying `wasm_exec.js`; it fails the build if any `Reflect.*` call
  survives, so keep the patterns in `tools/main.go` in sync if you upgrade TinyGo. The same patch
  also makes two performance changes: it caches the memory `DataView`, and it gives `loadString` a
  cached `Uint8Array` and an ASCII fast path. Each rule is an exact-string match, and the build fails
  if neither the original nor the replacement text is found. That means a new TinyGo whose
  `wasm_exec.js` is formatted differently breaks the build loudly instead of shipping unpatched glue.
- **`go.run()` is async**, so a panic in Go `main()` becomes a rejected promise, not a throw the
  loader's `try/catch` sees. `instantiate()` attaches a `.catch` to surface it and reset the
  instance.
- **Debug on the official server, not the Sim**, for anything touching the JS↔WASM boundary:
  `Game.cpu.getUsed()` is only real metered CPU there, and these sandbox differences only appear
  there. Upload with `./scripts/push.sh main` (sim/season/pserver as appropriate).

### Package layout: `go/lib` vs `go/brain`

`go/brain` is your bot (`brain.go` plus any files you add); it dot-imports `screeps/lib`, so names like
`Creep`, `Filter`, `StSpawn` and `c.Game()` stay unqualified (change the import line to `"screeps/lib"` if you
prefer `lib.Creep`). `go/lib` is the plumbing you normally never edit: `ctx.go` (command/query emission and
encodings), `natives.go`, `obj.go` (helpers), `game.go` (Pos/Store/Game facade), and the generated
`gen.go` / `obj_gen.go`. `go/lib/libtest` builds fixtures for tests of both packages. Bot-specific constants
(preset indices like `MoveGreenPath`) belong in `brain`, not `lib`. Both packages must stay free of
`syscall/js` so host tests work; `go/main.go` is the only glue (`tick`, buffer addresses).
