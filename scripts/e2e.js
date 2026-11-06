#!/usr/bin/env node
// Optional end-to-end check (needs Node; not part of the build). Run after
// ./scripts/build.sh:   node scripts/e2e.js
//  1. one real tick through dist/ (loader + host + wasm) against a mocked game
//  2. every generated op/query dispatched against a recording proxy, checking
//     that each calls the API method its name says
"use strict";
const fs = require("fs");
const path = require("path");
const dist = path.join(__dirname, "..", "dist");
let failed = 0;
const check = (ok, msg) => {
  if (!ok) { failed++; console.log("FAIL:", msg); }
};

// ---- constants / globals the Screeps sandbox provides ------------------------
Object.assign(globalThis, {
  WORK: "work", CARRY: "carry", MOVE: "move", OK: 0, ERR_NOT_IN_RANGE: -9,
  RESOURCE_ENERGY: "energy",
  FIND_CREEPS: 101, FIND_POWER_CREEPS: 102, FIND_STRUCTURES: 107, FIND_SOURCES: 105, FIND_MINERALS: 116,
  FIND_DEPOSITS: 117, FIND_CONSTRUCTION_SITES: 111, FIND_DROPPED_RESOURCES: 106, FIND_TOMBSTONES: 118,
  FIND_RUINS: 123, FIND_NUKES: 120, FIND_FLAGS: 109, ORDER_BUY: "buy", ORDER_SELL: "sell",
  RoomPosition: class { constructor(x, y, roomName) { Object.assign(this, { x, y, roomName }); } },
});

function loadModules() {
  const mods = {};
  const req = (name) => {
    if (name === "main.wasm") return new WebAssembly.Module(fs.readFileSync(path.join(dist, "main.wasm")));
    if (mods[name]) return mods[name].exports;
    const m = (mods[name] = { exports: {} });
    const src = fs.readFileSync(path.join(dist, name + ".js"), "utf8");
    new Function("require", "module", "exports", src)(req, m, m.exports);
    return m.exports;
  };
  return req;
}

// ---- 1. a real tick ------------------------------------------------------------
{
  const mkStore = (energy, free) => {
    const s = { energy };
    for (const [k, v] of Object.entries({ getFreeCapacity: () => free, getCapacity: () => 50, getUsedCapacity: () => energy }))
      Object.defineProperty(s, k, { value: v, enumerable: false });
    return s;
  };
  const calls = [];
  const room = { name: "W1N1", energyAvailable: 300, energyCapacityAvailable: 300, storage: undefined, terminal: undefined,
    controller: { id: "c0ffee00c0ffee00c0ffee00", my: true, pos: { x: 25, y: 25, roomName: "W1N1" }, effects: [], structureType: "controller" },
    getTerrain: () => ({ getRawBuffer: (a) => { a[0] = 1; a[1] = 2; return a; } }) };
  const pos = (x, y) => ({ x, y, roomName: "W1N1" });
  const mkCreep = (name, x, y, free, spawning) => ({ id: "aaaaaaaabbbbbbbbcccc" + name.padEnd(4, "0"), name, my: true, owner: { username: "me" }, pos: pos(x, y),
    effects: [], spawning, fatigue: 0, hits: 100, hitsMax: 100, ticksToLive: 1000, body: [{ type: "work", hits: 100 }, { type: "carry", hits: 100 }, { type: "move", hits: 0 }],
    store: mkStore(50 - free, free), room,
    harvest(s) { calls.push([name, "harvest", s.id]); return s.far ? -9 : 0; },
    upgradeController() { calls.push([name, "upgrade"]); return 0; },
    moveTo() { calls.push([name, "moveTo"]); return 0; } });
  const near = { id: "1111111122222222333333", pos: pos(12, 10), effects: [], energy: 3000, energyCapacity: 3000, ticksToRegeneration: 0, far: true };
  const far = { id: "4444444455555555666666", pos: pos(40, 40), effects: [], energy: 3000, energyCapacity: 3000, ticksToRegeneration: 0 };
  const spawnCalls = [];
  const spawn = { id: "5p4wn5p4wn5p4wn5p4wn", name: "S", structureType: "spawn", my: true, owner: { username: "me" }, pos: pos(20, 20), effects: [], hits: 5000, hitsMax: 5000,
    store: mkStore(300, 0), spawning: null, spawnCreep: (...a) => (spawnCalls.push(a), 0) };
  const a = mkCreep("a", 10, 10, 50, false), b = mkCreep("b", 10, 10, 0, false), c = mkCreep("c", 1, 1, 50, true);
  const finds = { 101: [a, b, c], 107: [spawn, room.controller], 105: [near, far] };
  room.find = (t) => finds[t] || [];
  globalThis.Memory = { creeps: { dead: {}, a: {} } };
  globalThis.Game = { time: 100, cpu: { limit: 20, tickLimit: 20, bucket: 10000, unlocked: false, unlockedTime: 0, getUsed: () => 0 },
    gcl: { level: 1, progress: 0, progressTotal: 1000 }, gpl: { level: 0, progress: 0, progressTotal: 1000 }, market: { credits: 0 },
    shard: { name: "shard3", ptr: false }, resources: {}, rooms: { W1N1: room }, creeps: { a, b, c }, spawns: { S: spawn }, getObjectById: () => null };

  // RawMemory with the segment rules of the real game: only segments requested by
  // setActiveSegments in an earlier tick are readable, and they survive a reset.
  const saved = {};
  let active = [];
  globalThis.RawMemory = { segments: {}, setActiveSegments: (ids) => { active = ids.slice(); } };
  const beginTick = () => {
    const seg = {};
    for (const id of active) seg[id] = saved[id] === undefined ? "" : saved[id];
    RawMemory.segments = new Proxy(seg, {
      set(t, k, v) { if (!(k in t)) throw new Error("segment " + k + " is not active"); saved[k] = t[k] = v; return true; },
    });
  };

  const req = loadModules();
  const loader = req("main");
  const log = [];
  const orig = console.log;
  console.log = (...x) => log.push(x.join(" "));
  beginTick();
  loader.loop(); // first ever tick: the segments are not readable yet, so the bot waits
  check(calls.length === 0 && active.length > 0, "tick 1 must wait for the memory segments, calls=" + JSON.stringify(calls));
  beginTick();
  loader.loop();
  console.log = orig;
  check(!log.some((l) => /error|mismatch|failed/i.test(l)), "loader logged an error: " + log.join(" | "));
  check(JSON.stringify(calls) === JSON.stringify([["a", "harvest", "1111111122222222333333"], ["a", "moveTo"], ["b", "upgrade"]]),
    "creep calls: " + JSON.stringify(calls));
  check(spawnCalls.length === 0 && !("dead" in Memory.creeps), "spawned although creeps exist, or dead memory kept");

  Game.creeps = {}; finds[101] = []; Game.time = 101; calls.length = 0;
  console.log = (...x) => log.push(x.join(" "));
  beginTick();
  loader.loop();
  console.log = orig;
  check(spawnCalls.length === 1 && spawnCalls[0][1] === "harvester_101" && spawnCalls[0][0].join() === "work,work,carry,move" &&
    spawnCalls[0][2].memory.role === "harvester", "spawn call: " + JSON.stringify(spawnCalls));
  // lite packing: the 2nd+ road/wall/rampart is a template copy plus a few columns;
  // every row must equal what the full packer writes for the same object.
  {
    const mk = (i, type, extra) => Object.assign({ id: "0000000" + i + "aaaaaaaa0000000" + i, structureType: type, pos: pos(i, 2 * i),
      effects: i === 3 ? [{ effect: 5 }] : [], hits: 100 * i, hitsMax: 5000 }, extra);
    const lites = [mk(1, "road", { ticksToDecay: 10 }), mk(2, "rampart", { my: true, owner: { username: "me" }, isPublic: false, ticksToDecay: 7 }),
      mk(3, "road", { ticksToDecay: 99 }), mk(4, "constructedWall", {}), mk(5, "rampart", { my: false, owner: { username: "foe" }, isPublic: true, ticksToDecay: 1 }),
      mk(6, "constructedWall", { hits: undefined }), mk(7, "road", { ticksToDecay: 3 })];
    // a store with a non-energy resource, packed first: it advances the store arena, which
    // must not leak into the template rows (their store columns are those of an empty store)
    const box = mk(8, "container", { store: Object.assign(mkStore(0, 1900), { H: 100 }) });
    const structures107 = finds[107];
    finds[107] = [box, lites[0], spawn, ...lites.slice(1), room.controller];
    Game.time = 102;
    beginTick();
    loader.loop();
    const host = req("host");
    const tb = host._test.gen().TABLES.find((t) => t.name === "structure");
    const v = host._test.input();
    check(v[tb.nIdx] === finds[107].length, "structure count " + v[tb.nIdx]);
    const full = new Int32Array(tb.stride);
    finds[107].forEach((o, i) => {
      if (o === box) return; // re-packing it would append to the arena again, so its offset differs
      tb.pack(o, full, 0, host._test.HD(o));
      const got = Array.from(v.subarray(tb.base + i * tb.stride, tb.base + (i + 1) * tb.stride));
      check(got.join() === Array.from(full).join(), `structure row ${i} (${o.structureType}) differs from the full pack:\n  ${got}\n  ${Array.from(full)}`);
    });
    finds[107] = structures107; // the scenarios below run with the original room
  }
  console.log("tick scenario:", failed ? "FAILED" : "ok");

  // ---- persistent store: console write -> saved to segments -> restored in a new instance
  const before = failed;
  log.length = 0;
  console.log = (...x) => log.push(x.join(" "));
  check(memset("creep/x/role", 3) === "ok" && memset("note", "héllo") === "ok" && memset("ratio", 0.25) === "ok", "memset");
  Game.time = 102;
  beginTick();
  loader.loop();
  console.log = orig;
  check(typeof saved[90] === "string" && saved[90].length > 0, "a dirty store must be written to segment 90");
  const written = saved[90];
  Game.time = 103;
  beginTick();
  loader.loop(); // nothing changed: no rewrite
  check(saved[90] === written, "unchanged store rewritten");

  // fresh isolate: new modules, new wasm instance; only `saved` and `active` survive
  const loader2 = loadModules()("main");
  log.length = 0;
  console.log = (...x) => log.push(x.join(" "));
  beginTick();
  loader2.loop();
  check(!log.some((l) => /unreadable|never became/.test(l)), "restore logged a problem: " + log.join(" | "));
  mem();
  console.log = orig;
  const lines = log.filter((l) => l.startsWith("[mem]"));
  check(lines.includes('[mem] creep/x/role = 3') && lines.includes('[mem] note = "héllo"') && lines.includes("[mem] ratio = 0.25"),
    "restored dump: " + JSON.stringify(lines));

  // damaged blob: the bot starts with an empty store instead of failing
  saved[90] = saved[90].slice(0, -8) + "AAAAAAAA";
  const loader3 = loadModules()("main");
  log.length = 0;
  console.log = (...x) => log.push(x.join(" "));
  beginTick();
  loader3.loop();
  mem();
  console.log = orig;
  check(log.some((l) => /unreadable/.test(l)) && log.some((l) => /0 of 0 entries/.test(l)), "damaged blob: " + log.join(" | "));
  console.log("persistent store:", failed === before ? "ok" : "FAILED");
}

// ---- 2. every op and query through the generated tables -----------------------------
{
  let rec = [];
  // A recording stand-in for any game object: property reads nest, calls are logged.
  // getTerrain() returns another recorder (the real one returns a Terrain object).
  const mk = (p) => new Proxy(function () {}, {
    get: (_, k) => (typeof k === "symbol" ? undefined : mk(p + "." + k)),
    apply: () => (rec.push(p), p.endsWith(".getTerrain") ? mk(p + "()") : 0),
  });
  globalThis.RoomPosition = class { constructor() { return mk("RoomPosition"); } };
  const host = loadModules()("host");
  const T = host._test, G = T.gen();
  for (const g of ["Game", "PathFinder", "RawMemory", "InterShardMemory", "PowerCreep"]) globalThis[g] = mk(g);
  globalThis.Memory = {};
  const recvH = T.HD(mk("obj")); // handle 0 doubles as every receiver / handle argument
  T.MATRIX_ADD(mk("matrix"));
  const NOCHECK = new Set(["Memory.set", "PathFinder.CostMatrix.set", "PathFinder.CostMatrix.get", "PathFinder.CostMatrix.clone",
    "PathFinder.CostMatrix.new", "Game.getObjectById", "Game.map-visual.getSize"]);
  const expectName = (full) => {
    const id = full.split("#")[0];
    const cls = id.slice(0, id.lastIndexOf(".")), m = id.slice(id.lastIndexOf(".") + 1);
    return { id, cls, m };
  };
  const run = (table, names, kind) => table.forEach((fn, i) => {
    const { id, cls, m } = expectName(names[i]);
    rec = [];
    try { fn(recvH, [0, 0, 0, 0, 0]); } catch (e) { check(false, `${kind} ${names[i]} threw: ${e.message}`); return; }
    if (NOCHECK.has(id)) return;
    const hit = rec.some((p) => p.endsWith("." + m) || p === id || p === cls + "." + m);
    check(hit, `${kind} ${names[i]} called [${rec.join(", ")}] but expected *.${m}`);
  });
  run(G.OPS, G.OP_NAMES, "op");
  run(G.QOPS, G.QOP_NAMES, "query");
  console.log(`dispatch smoke: ${G.OPS.length} ops, ${G.QOPS.length} queries:`, failed ? "FAILED" : "ok");
}

process.exit(failed ? 1 : 0);
