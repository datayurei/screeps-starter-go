"use strict";
/**
 * The bot's game I/O. Every tick:
 *   1. pack   - read the game into the shared int32 tables (plain typed-array
 *               writes; no JS<->WASM crossings),
 *   2. decide - call Go's exported `tick(round)`; between rounds, answer the
 *               engine queries Go asked (pathfinding, find*, look*, market...),
 *   3. act    - execute the command records Go emitted (harvest, transfer,
 *               spawnCreep, ...) and store each result code for next tick.
 *
 * Which columns/ops/queries exist is generated from tools/spec_*.go into
 * host_gen.js (see docs/api-coverage.md). This file supplies the helpers that
 * generated code calls, the option PRESETS Go selects by index, and the
 * marshallers for queries whose result isn't a plain number or handle.
 */

const makeGen = require("host_gen");

// ---- state ------------------------------------------------------------------

let exp = null; // the WASM instance's exports
let G = null; // the generated tables (bound to the helpers below)
let V = null; // typed-array views over wasm memory
let viewBuf = null; // the memory.buffer V was built over
let addrs = null;
let respCur = 0; // words used in the response arena this tick
let storeCur = 0; // words used in the store arena this tick

let objs = []; // handle -> game object (this tick only)
let handleOf = new Map(); // game object -> handle
const roomIdx = new Map(); // room name -> row in the room table
const terrainSlot = new Map(); // room name -> slot in the terrain buffer (terrain is immutable)
let matrices = [];
const ARGV = [0, 0, 0, 0, 0];

// Which object tables to pack. Each costs JS CPU per object per tick; turn off
// what the Go brain doesn't read (e.g. PACK.structure = false if unused).
const PACK = {
  creep: true, powercreep: true, structure: true, source: true, mineral: true, deposit: true,
  site: true, resource: true, tombstone: true, ruin: true, nuke: true, flag: true,
};

// Option objects Go selects by index (preset:NAME in the spec). Index 0 is the
// default (undefined). Add entries and reference them from brain by index.
const PRESETS = {
  move: [undefined, { visualizePathStyle: { stroke: "#00ff88" } }],
  spawn: [undefined, { memory: { role: "harvester" } }],
  find: [undefined],
  path: [undefined],
  route: [undefined],
  search: [undefined],
  style: [undefined],
  ofilter: [undefined],
  limits: [undefined],
};

// ---- enums ------------------------------------------------------------------

const RES = []; // wire index -> resource constant (extended with unknown server resources)
const RIDX = Object.create(null);
const ST = Object.create(null); // structureType -> 1-based index
const STRUCT = [undefined];
const PARTIDX = Object.create(null);
const LOOKIDX = Object.create(null);
const LOOKS = [];
const DIRVEC = [null, [0, -1], [1, -1], [1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0], [-1, -1]];
const STATUS = ["normal", "closed", "novice", "respawn", "out of borders"];
const ERR_THROWN = -100;

// ---- registry / identity helpers -------------------------------------------

function HD(o) {
  if (!o) return -1;
  let h = handleOf.get(o);
  if (h === undefined) {
    h = objs.length;
    objs.push(o);
    handleOf.set(o, h);
  }
  return h;
}
function HDS(arr) {
  const r = new Array(arr.length);
  for (let i = 0; i < arr.length; i++) r[i] = HD(arr[i]);
  return r;
}
function H(i) {
  return i >= 0 ? objs[i] : undefined;
}
function RI(name) {
  const i = roomIdx.get(name);
  return i === undefined ? -1 : i;
}
function ID(o, k) {
  const id = o.id;
  if (typeof id !== "string") return 0;
  return parseInt(id.slice(k * 8, k * 8 + 8) || "0", 16) | 0;
}
function OBJ_BY_ID(a, b, c) {
  const hex = (x) => (x >>> 0).toString(16).padStart(8, "0");
  return Game.getObjectById(hex(a) + hex(b) + hex(c));
}
function HASH(s) {
  if (s === undefined || s === null) return 0;
  s = String(s);
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h | 0;
}
function CLIP(n) {
  if (!(n === n)) return 0;
  return n > 2147483647 ? 2147483647 : n < -2147483648 ? -2147483648 : n | 0;
}
function FX(o) {
  let m = 0;
  const e = o.effects;
  if (e) for (let i = 0; i < e.length; i++) if (e[i].effect < 31) m |= 1 << e[i].effect;
  return m;
}

// ---- rooms ------------------------------------------------------------------

const ROOM_RE = /^([WE])(\d+)([NS])(\d+)$/;
function RREF(name) {
  const m = ROOM_RE.exec(name || "");
  if (!m) return 0;
  const x = m[1] === "E" ? +m[2] : -+m[2] - 1;
  const y = m[3] === "S" ? +m[4] : -+m[4] - 1;
  return ((x + 512) << 10) | (y + 512);
}
const RN2 = RREF;
function RNAME(ref) {
  const x = (ref >> 10) - 512;
  const y = (ref & 1023) - 512;
  return (x >= 0 ? "E" + x : "W" + (-x - 1)) + (y >= 0 ? "S" + y : "N" + (-y - 1));
}
function TERRAIN(room) {
  let slot = terrainSlot.get(room.name);
  if (slot !== undefined) return slot;
  if (terrainSlot.size >= G.TERRAIN_SLOTS) return -1;
  slot = terrainSlot.size;
  const cell = G.TERRAIN_CELL;
  room.getTerrain().getRawBuffer(V.terrain.subarray(slot * cell, (slot + 1) * cell));
  terrainSlot.set(room.name, slot);
  return slot;
}
function TERR(obj) {
  return obj && typeof obj.getTerrain === "function" ? obj.getTerrain() : obj;
}

// ---- per-object column helpers ---------------------------------------------

const BP = { t: new Array(8).fill(0), a: new Array(8).fill(0), boost: 0 };
function BODY(o) {
  BP.t.fill(0);
  BP.a.fill(0);
  BP.boost = 0;
  const b = o.body;
  if (!b) return BP;
  for (let i = 0; i < b.length; i++) {
    const k = PARTIDX[b[i].type];
    if (k === undefined) continue;
    BP.t[k]++;
    if (b[i].hits > 0) BP.a[k]++;
    if (b[i].boost) BP.boost |= 1 << k;
  }
  return BP;
}

const SA = { used: 0, free: 0, cap: 0, energy: 0, energyCap: 0, energyFree: 0, off: 0, n: 0 };
function STORE(o) {
  const st = o.store;
  SA.off = storeCur;
  SA.n = 0;
  if (!st) {
    SA.used = SA.free = SA.cap = SA.energy = SA.energyCap = SA.energyFree = 0;
    return SA;
  }
  let used = 0;
  const keys = Object.keys(st);
  for (let i = 0; i < keys.length; i++) {
    const k = keys[i];
    const amt = st[k];
    used += amt;
    if (k !== "energy" && storeCur + 2 <= G.STORE_ARENA_CAP) {
      const ri = RIDX[k];
      V.storeArena[storeCur++] = ri === undefined ? -1 : ri;
      V.storeArena[storeCur++] = amt;
      SA.n++;
    }
  }
  SA.used = used;
  SA.energy = st[RESOURCE_ENERGY] || 0;
  SA.free = st.getFreeCapacity() || 0;
  SA.cap = st.getCapacity() || 0;
  SA.energyCap = st.getCapacity(RESOURCE_ENERGY) || 0;
  SA.energyFree = st.getFreeCapacity(RESOURCE_ENERGY) || 0;
  return SA;
}
function RS(x) {
  if (x === undefined || x === null) return -1;
  const i = RIDX[x];
  return i === undefined ? -2 : i;
}
function PCLASS(n) {
  return n === "operator" ? 1 : 0;
}
function PW(o, id, which) {
  const p = o.powers && o.powers[id];
  return p ? (which ? p.cooldown | 0 : p.level | 0) : 0;
}
function SPAWNING(o, k) {
  const sp = o.structureType === "spawn" ? o.spawning : null;
  if (!sp) return k === 3 ? -1 : 0;
  if (k === 0) return sp.needTime | 0;
  if (k === 1) return sp.remainingTime | 0;
  if (k === 2) {
    let m = 0;
    for (const d of sp.directions || []) m |= 1 << d;
    return m;
  }
  return HD(Game.creeps[sp.name]);
}
function DEST(o, k) {
  const d = o.destination;
  if (!d) return -1;
  if (k === 0) return d.x === undefined ? -1 : d.x;
  if (k === 1) return d.y === undefined ? -1 : d.y;
  const rn = d.roomName || d.room;
  return rn ? RREF(rn) : -1;
}

// Numeric memory slots: Memory.<coll>[name]._g = [m0, m1, m2, m3]. This is the
// packed form of `.memory`; arbitrary JS-structured memory stays in JS.
function memKey(o) {
  if (!o) return null;
  if (o.body !== undefined) return ["creeps", o.name];
  if (o.className !== undefined) return ["powerCreeps", o.name];
  if (o.structureType === "spawn") return ["spawns", o.name];
  if (o.energyAvailable !== undefined) return ["rooms", o.name];
  if (o.color !== undefined && o.secondaryColor !== undefined) return ["flags", o.name];
  return null;
}
function MEM(o, k) {
  const m = memKey(o);
  if (!m) return 0;
  const c = Memory[m[0]];
  const e = c && c[m[1]];
  return e && e._g ? e._g[k] | 0 : 0;
}
function MEMSET(o, slot, val) {
  const m = memKey(o);
  if (!m || slot < 0 || slot > 3) return -10; // ERR_INVALID_ARGS
  const c = (Memory[m[0]] = Memory[m[0]] || {});
  const e = (c[m[1]] = c[m[1]] || {});
  (e._g = e._g || [0, 0, 0, 0])[slot] = val | 0;
  return 0;
}

// ---- argument decoders (command/query words -> JS values) -------------------

function S(ref) {
  const len = ref & 4095;
  if (!len) return undefined;
  const off = ref >>> 12;
  let s = "";
  let ascii = true;
  for (let i = 0; i < len; i++) {
    const c = V.bytes[off + i];
    if (c >= 128) ascii = false;
    s += String.fromCharCode(c);
  }
  if (!ascii) {
    try {
      return decodeURIComponent(escape(s));
    } catch (e) {
      /* fall through to latin1 */
    }
  }
  return s;
}
function ARR(ref) {
  const len = ref & 4095;
  if (!len) return [];
  const off = ref >>> 12;
  return Array.from(V.arena.subarray(off, off + len));
}
function PARTS(ref) {
  return ARR(ref).map((i) => G.BODYPART_LIST[i]);
}
function PATH(ref) {
  const a = ARR(ref);
  const r = [];
  for (let i = 0; i + 2 < a.length; i += 3) {
    const v = DIRVEC[a[i + 2]] || [0, 0];
    r.push({ x: a[i], y: a[i + 1], dx: v[0], dy: v[1], direction: a[i + 2] });
  }
  return r;
}
function POINTS(ref) {
  const a = ARR(ref);
  const r = [];
  for (let i = 0; i + 1 < a.length; i += 2) r.push({ x: a[i], y: a[i + 1] });
  return r;
}
function POS(v) {
  const u = v >>> 0;
  return new RoomPosition(u & 63, (u >>> 6) & 63, RNAME(u >>> 12));
}
function HARR(ref) {
  return ARR(ref).map(H);
}
function OPT(v) {
  return v < 0 ? undefined : v;
}
function RESO(v) {
  return v < 0 ? undefined : RES[v];
}
function MATRIX(i) {
  return matrices[i];
}
function MATRIX_ADD(m) {
  matrices.push(m);
  return matrices.length - 1;
}

// ---- query result marshallers ----------------------------------------------

const terrCode = (t) => (t === "wall" ? 1 : t === "swamp" ? 2 : 0);

function STEPS(v) {
  const r = [];
  if (!Array.isArray(v)) return r;
  for (const p of v) r.push(p.x, p.y, p.direction | 0);
  return r;
}

const lookPayload = (type, item) => (type === "terrain" ? terrCode(item) : HD(item));

// Row shapes (Go reads them with Answer()):
//   look:         (lookType, payload)*          payload = handle, or terrain code
//   lookFor:      payload*                      handles, or terrain codes
//   lookArea:     (x, y, lookType, payload)*
//   lookForArea:  (x, y, lookType, payload)*
//   events:       (event, objectHandle, d0, d1)*  d0 = target handle if the event has targetId, else first numeric field
//   exits:        (direction, roomRef)*
//   route:        (exit, roomRef)*  |  [errCode]  (odd length = error)
//   status:       [statusIndex, unixSeconds]    0 normal 1 closed 2 novice 3 respawn 4 out of borders
//   orders:       (handle, buy?1:0, resource, priceMilli, amount, remaining, roomRef)*
//   history:      (resource, dayNumber, transactions, volume, avgMilli, stddevMilli)*
//   search:       [incomplete, cost, ops, pos*]  pos = x | y<<6 | roomRef<<12
//   heap:         10 fields of getHeapStatistics in V8 order
//   terrain:      [0 plain | 1 wall | 2 swamp]
const MARSHAL = {
  look(v) {
    const r = [];
    for (const it of v || []) r.push(LOOKIDX[it.type], lookPayload(it.type, it[it.type]));
    return r;
  },
  lookFor(v) {
    if (!v || !v.length) return [];
    return typeof v[0] === "string" ? v.map(terrCode) : HDS(v);
  },
  lookArea(v) {
    const r = [];
    for (const it of v || []) r.push(it.x, it.y, LOOKIDX[it.type], lookPayload(it.type, it[it.type]));
    return r;
  },
  lookForArea(v, s, a) {
    const t = G.LOOK_LIST[a[0]];
    const r = [];
    for (const it of v || []) r.push(it.x, it.y, a[0], lookPayload(t, it[t]));
    return r;
  },
  events(v) {
    const r = [];
    for (const e of v || []) {
      const d = e.data || {};
      const nums = [];
      for (const k in d) if (typeof d[k] === "number") nums.push(d[k]);
      let d0 = nums[0] | 0;
      let d1 = nums[1] | 0;
      if (d.targetId !== undefined) {
        d0 = HD(Game.getObjectById(d.targetId));
        d1 = nums[0] | 0;
      }
      r.push(e.event, HD(Game.getObjectById(e.objectId)), d0, d1);
    }
    return r;
  },
  heap(v) {
    const k = [
      "total_heap_size", "total_heap_size_executable", "total_physical_size", "total_available_size",
      "used_heap_size", "heap_size_limit", "malloced_memory", "peak_malloced_memory",
      "does_zap_garbage", "externally_allocated_size",
    ];
    return k.map((n) => (v && v[n]) | 0);
  },
  exits(v) {
    const r = [];
    for (const d in v || {}) r.push(+d, RREF(v[d]));
    return r;
  },
  route(v) {
    if (typeof v === "number") return [v];
    const r = [];
    for (const s of v || []) r.push(s.exit, RREF(s.room));
    return r;
  },
  status(v) {
    const i = STATUS.indexOf(v && v.status);
    return [i < 0 ? 0 : i, v && v.timestamp ? Math.floor(v.timestamp / 1000) : 0];
  },
  orders(v) {
    const r = [];
    for (const o of v || []) {
      r.push(HD(o), o.type === "buy" ? 1 : 0, RS(o.resourceType), Math.round(o.price * 1000), o.amount | 0,
        o.remainingAmount | 0, o.roomName ? RREF(o.roomName) : 0);
    }
    return r;
  },
  history(v) {
    const r = [];
    for (const h of v || []) {
      r.push(RS(h.resourceType), Math.floor(Date.parse(h.date) / 86400000), h.transactions | 0, h.volume | 0,
        Math.round(h.avgPrice * 1000), Math.round(h.stddevPrice * 1000));
    }
    return r;
  },
  search(v) {
    const r = [v.incomplete ? 1 : 0, v.cost | 0, v.ops | 0];
    for (const p of v.path || []) r.push(((p.x | (p.y << 6)) | (RREF(p.roomName) << 12)) | 0);
    return r;
  },
  terrain(v) {
    return [terrCode(v)];
  },
};

// ---- generated tables ---------------------------------------------------------

G = makeGen({
  HD, HDS, RI, ID, FX, HASH, ST, RS, BODY, STORE, MEM, PCLASS, PW, SPAWNING, DEST, RN2, RREF, TERRAIN, CLIP,
  H, S, ARR, PARTS, PATH, POS, RNAME, RES, RESO, STRUCT, PRESETS, OPT, HARR, MATRIX, MATRIX_ADD, MEMSET,
  POINTS, OBJ_BY_ID, TERR, STEPS, MARSHAL, LOOKS,
});

G.RESOURCE_LIST.forEach((r, i) => {
  RES[i] = r;
  RIDX[r] = i;
});
G.STRUCTURE_LIST.forEach((s, i) => {
  ST[s] = i + 1;
  STRUCT.push(s);
});
G.BODYPART_LIST.forEach((p, i) => (PARTIDX[p] = i));
G.LOOK_LIST.forEach((l, i) => {
  LOOKIDX[l] = i;
  LOOKS.push(l);
});

// ---- bind / views --------------------------------------------------------------

function bind(exports) {
  for (const name of ["tick", "addr", "layoutHash", ...MEM_EXPORTS]) {
    if (typeof exports[name] !== "function") {
      throw new Error(`WASM module does not export \`${name}\` - check go/main.go`);
    }
  }
  if (exports.layoutHash() !== G.LAYOUT_HASH) {
    throw new Error(
      `layout mismatch: wasm ${exports.layoutHash()} vs host_gen.js ${G.LAYOUT_HASH} - rerun \`go run ./tools gen\` and rebuild`
    );
  }
  exp = exports;
  registerConsole();
  addrs = [];
  for (let i = 0; i <= 10; i++) addrs.push(exports.addr(i));
  V = null;
  viewBuf = null;
  terrainSlot.clear();
  memReset();
  // Resources the server knows but the spec doesn't: append, usable by number.
  if (typeof RESOURCES_ALL !== "undefined") {
    for (const r of RESOURCES_ALL) {
      if (RIDX[r] === undefined) {
        RIDX[r] = RES.length;
        RES.push(r);
        console.log(`[bot] resource ${r} is not in the spec; assigned index ${RIDX[r]}`);
      }
    }
  }
}

function unbind() {
  exp = null;
  memReset();
  V = null;
  viewBuf = null;
  terrainSlot.clear();
}

function views() {
  const buf = exp.memory ? exp.memory.buffer : null;
  if (buf === null) throw new Error("wasm instance exposes no memory");
  if (buf === viewBuf && V !== null) return;
  viewBuf = buf;
  const a = addrs;
  V = {
    in: new Int32Array(buf, a[0], G.IN_LEN),
    out: new Int32Array(buf, a[1], G.OUT_LEN),
    cmd: new Int32Array(buf, a[2], G.CMD_CAP * G.CMD_WORDS),
    qry: new Int32Array(buf, a[3], G.QRY_CAP * G.CMD_WORDS),
    arena: new Int32Array(buf, a[4], G.ARENA_CAP),
    bytes: new Uint8Array(buf, a[5], G.BYTES_CAP),
    resp: new Int32Array(buf, a[6], G.RESP_CAP),
    respIdx: new Int32Array(buf, a[7], G.QRY_CAP * 2),
    res: new Int32Array(buf, a[8], G.CMD_CAP),
    terrain: new Uint8Array(buf, a[9], G.TERRAIN_SLOTS * G.TERRAIN_CELL),
    storeArena: new Int32Array(buf, a[10], G.STORE_ARENA_CAP),
  };
}

// ---- persistent store (lib.Mem) -------------------------------------------------
//
// Go owns a key-value store (go/lib/store.go). It survives global resets and
// re-instantiation because we copy it into RawMemory segments:
//   - after a tick in which it changed, encode it in Go, base64 it and write the
//     chunks that differ;
//   - on a fresh instance, read the segments back and hand the bytes to Go
//     before the first tick. Until that has happened the bot does not run, so it
//     never decides on an empty store that is only empty because of the reset.
// (RawMemory.get/set is the Memory JSON string itself, so it can't hold this.)
// The segments must be re-requested every tick; we do that at the end of run(),
// which means it overrides a RawMemory.setActiveSegments command from Go.
const MEM_SEG_BASE = 90; // first segment used
const MEM_SEG_COUNT = 4; // segments used (<= 10, the per-tick active limit)
const MEM_SEG_CHARS = 100000; // chars per segment (limit is 100 KB)
const MEM_WAIT_TICKS = 2; // ticks to wait for the segments to become readable
const MEM_EXPORTS = [
  "memScratch", "memLoad", "memDirty", "memEncode", "memSnap", "memClean", "memDirtyAgain",
  "memDump", "memStat", "memSetInt", "memSetFloat", "memSetStr", "memDelete",
];

let memRestored = false; // the store has been loaded (or given up on)
let memPersist = false; // writing back is safe (the restore read real data)
let memWaited = 0;
let memLast = []; // last chunk strings known to be in the segments

function memReset() {
  memRestored = false;
  memPersist = false;
  memWaited = 0;
  memLast = [];
}

const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
const B64I = new Int16Array(128).fill(-1);
for (let i = 0; i < 64; i++) B64I[B64.charCodeAt(i)] = i;

function b64encode(u8) {
  const parts = [];
  let s = "";
  const n = u8.length;
  for (let i = 0; i < n; i += 3) {
    const a = u8[i];
    const b = i + 1 < n ? u8[i + 1] : 0;
    const c = i + 2 < n ? u8[i + 2] : 0;
    s += B64[a >> 2] + B64[((a & 3) << 4) | (b >> 4)];
    s += i + 1 < n ? B64[((b & 15) << 2) | (c >> 6)] : "=";
    s += i + 2 < n ? B64[c & 63] : "=";
    if (s.length >= 8192) {
      parts.push(s);
      s = "";
    }
  }
  parts.push(s);
  return parts.join("");
}

// Returns a Uint8Array, or null if the text is not valid base64.
function b64decode(str) {
  const n = str.length;
  if (n % 4 !== 0) return null;
  let pad = 0;
  if (n && str[n - 1] === "=") pad++;
  if (n > 1 && str[n - 2] === "=") pad++;
  const out = new Uint8Array((n / 4) * 3 - pad);
  let o = 0;
  for (let i = 0; i < n; i += 4) {
    let v = 0;
    for (let k = 0; k < 4; k++) {
      const ch = str.charCodeAt(i + k);
      const d = ch === 61 && i + k >= n - pad ? 0 : ch < 128 ? B64I[ch] : -1;
      if (d < 0) return null;
      v = (v << 6) | d;
    }
    if (o < out.length) out[o++] = (v >> 16) & 255;
    if (o < out.length) out[o++] = (v >> 8) & 255;
    if (o < out.length) out[o++] = v & 255;
  }
  return out;
}

function memSegIds() {
  const ids = [];
  for (let i = 0; i < MEM_SEG_COUNT; i++) ids.push(MEM_SEG_BASE + i);
  return ids;
}

// Returns true when the bot may run this tick.
function memEnsureRestored() {
  if (memRestored) return true;
  const segs = RawMemory.segments;
  const chunks = [];
  let ready = true;
  for (let i = 0; i < MEM_SEG_COUNT; i++) {
    const c = segs[MEM_SEG_BASE + i];
    if (typeof c !== "string") ready = false;
    chunks.push(c);
  }
  if (!ready) {
    if (++memWaited <= MEM_WAIT_TICKS) return false; // requested; readable next tick
    console.log("[mem] segments never became readable; running with an empty store that will NOT be saved");
    memRestored = true;
    memPersist = false;
    return true;
  }
  memLast = chunks;
  memRestored = true;
  memPersist = true;
  const text = chunks.join("");
  if (text === "") return true; // first run: nothing saved yet
  const bytes = b64decode(text);
  let ok = 0;
  if (bytes !== null) {
    const ptr = exp.memScratch(bytes.length);
    new Uint8Array(exp.memory.buffer, ptr, bytes.length).set(bytes);
    ok = exp.memLoad(bytes.length);
  }
  if (!ok) console.log("[mem] saved store is unreadable or from another format version; starting empty");
  return true;
}

function memPersistNow() {
  if (!memPersist || !exp.memDirty()) return;
  const n = exp.memEncode();
  const b64 = b64encode(new Uint8Array(exp.memory.buffer, exp.memSnap(), n));
  if (b64.length > MEM_SEG_COUNT * MEM_SEG_CHARS) {
    console.log(`[mem] store is ${b64.length} chars encoded, over the ${MEM_SEG_COUNT * MEM_SEG_CHARS} limit; NOT saved`);
    exp.memClean(); // retry only when it changes again
    return;
  }
  try {
    for (let i = 0; i < MEM_SEG_COUNT; i++) {
      const chunk = b64.slice(i * MEM_SEG_CHARS, (i + 1) * MEM_SEG_CHARS);
      if (chunk !== memLast[i]) {
        RawMemory.segments[MEM_SEG_BASE + i] = chunk;
        memLast[i] = chunk;
      }
    }
    exp.memClean();
  } catch (e) {
    console.log(`[mem] saving failed: ${e && e.message ? e.message : e}`);
    exp.memDirtyAgain();
  }
}

// Console helpers: type e.g. mem("creep/") in the game console.
function memArg(str, extra) {
  const enc = unescape(encodeURIComponent(String(str)));
  const ex = extra === undefined ? "" : unescape(encodeURIComponent(String(extra)));
  const ptr = exp.memScratch(enc.length + ex.length);
  const u8 = new Uint8Array(exp.memory.buffer, ptr, enc.length + ex.length);
  for (let i = 0; i < enc.length; i++) u8[i] = enc.charCodeAt(i);
  for (let i = 0; i < ex.length; i++) u8[enc.length + i] = ex.charCodeAt(i);
  return [enc.length, ex.length];
}
function memLive() {
  if (exp === null) return "the bot is not running (wasm instance not created)";
  if (!memRestored) return "the store is not loaded yet; try again next tick";
  return null;
}
const MEM_CONSOLE = {
  // mem(prefix?) - print entries whose key starts with prefix (all if omitted)
  mem(prefix) {
    const bad = memLive();
    if (bad) return bad;
    exp.memDump(memArg(prefix === undefined ? "" : prefix)[0]);
    return "ok";
  },
  // memsize() - entry count and encoded size
  memsize() {
    const bad = memLive();
    if (bad) return bad;
    exp.memStat();
    return "ok";
  },
  // memset(key, value) - write a number or string (for debugging)
  memset(key, value) {
    const bad = memLive();
    if (bad) return bad;
    if (typeof value === "number") {
      const [kl] = memArg(key);
      const ok = Number.isInteger(value) && Math.abs(value) < 2147483648 ? exp.memSetInt(kl, value) : exp.memSetFloat(kl, value);
      return ok ? "ok" : "failed";
    }
    if (typeof value === "string") {
      const [kl, vl] = memArg(key, value);
      return exp.memSetStr(kl, vl) ? "ok" : "failed";
    }
    return "value must be a number or a string";
  },
  // memdel(key) - delete one key
  memdel(key) {
    const bad = memLive();
    if (bad) return bad;
    return exp.memDelete(memArg(key)[0], 0) ? "deleted" : "no such key";
  },
  // memclear(prefix) - delete every key under prefix; memclear("") clears everything
  memclear(prefix) {
    const bad = memLive();
    if (bad) return bad;
    if (prefix === undefined) return 'pass a prefix; memclear("") clears everything';
    return `deleted ${exp.memDelete(memArg(prefix)[0], 1)} entries`;
  },
};
function registerConsole() {
  for (const name in MEM_CONSOLE) globalThis[name] = MEM_CONSOLE[name];
}

// ---- the tick -------------------------------------------------------------------

function cleanDeadCreepMemory() {
  for (const name in Memory.creeps) {
    if (!(name in Game.creeps)) delete Memory.creeps[name];
  }
}

function pack() {
  const v = V.in;
  const rooms = Object.values(Game.rooms).slice(0, G.TABLES[0].cap);
  roomIdx.clear();
  for (let i = 0; i < rooms.length; i++) roomIdx.set(rooms[i].name, i);

  const room = G.TABLES[0];
  for (let i = 0; i < rooms.length; i++) room.pack(rooms[i], v, room.base + i * room.stride, HD(rooms[i]));
  v[room.nIdx] = rooms.length;

  for (let t = 1; t < G.TABLES.length; t++) {
    const tb = G.TABLES[t];
    let n = 0;
    if (PACK[tb.name]) {
      for (let r = 0; r < rooms.length && n < tb.cap; r++) {
        const rows = tb.find(rooms[r]);
        for (let i = 0; i < rows.length && n < tb.cap; i++, n++) {
          tb.pack(rows[i], v, tb.base + n * tb.stride, HD(rows[i]));
        }
      }
    }
    v[tb.nIdx] = n;
  }
  G.packHeader(v);
}

function answerQueries(from, to) {
  const q = V.qry;
  for (let i = from; i < to; i++) {
    const b = i * G.CMD_WORDS;
    for (let k = 0; k < 5; k++) ARGV[k] = q[b + 2 + k];
    let ans = [];
    try {
      ans = G.QOPS[q[b]](q[b + 1], ARGV);
      if (!Array.isArray(ans)) ans = [];
    } catch (e) {
      console.log(`[bot] query ${G.QOP_NAMES[q[b]]} threw: ${e && e.message ? e.message : e}`);
    }
    let n = ans.length;
    if (respCur + n > G.RESP_CAP) n = 0;
    for (let k = 0; k < n; k++) V.resp[respCur + k] = ans[k];
    V.respIdx[i * 2] = respCur;
    V.respIdx[i * 2 + 1] = n;
    respCur += n;
  }
}

// decide runs Go rounds until no new queries are outstanding; returns the number
// of commands, or -1 if Go failed (the buffers must not be acted on).
function decide() {
  let answered = 0;
  respCur = 0;
  for (let round = 0; round < G.MAX_ROUNDS; round++) {
    if (exp.tick(round) !== 0) {
      console.log("[bot] go tick failed; skipping this tick's commands");
      return -1;
    }
    views(); // Go may have grown memory
    const nq = V.out[1];
    if (nq <= answered) break;
    if (round === G.MAX_ROUNDS - 1) {
      console.log(`[bot] ${nq - answered} queries unanswered after ${G.MAX_ROUNDS} rounds`);
      break;
    }
    answerQueries(answered, nq);
    answered = nq;
    V.out[5] = nq;
  }
  return V.out[0];
}

function execute(n) {
  const c = V.cmd;
  let failures = 0;
  for (let i = 0; i < n; i++) {
    const b = i * G.CMD_WORDS;
    const w0 = c[b];
    const op = w0 & 0x7fff;
    const recv = c[b + 1];
    for (let k = 0; k < 5; k++) ARGV[k] = c[b + 2 + k];
    let code = 0;
    try {
      code = G.OPS[op](recv, ARGV);
    } catch (e) {
      code = ERR_THROWN;
      if (failures++ < 5) console.log(`[bot] ${G.OP_NAMES[op]} threw: ${e && e.message ? e.message : e}`);
    }
    if (typeof code !== "number") code = 0;
    if ((w0 & 0x8000) !== 0 && code === ERR_NOT_IN_RANGE) {
      try {
        H(recv).moveTo(H(ARGV[0]), PRESETS.move[(w0 >>> 16) & 0x7fff]);
      } catch (e) {
        /* the creep may be unable to move; ignore */
      }
    } else if (code < 0 && code !== ERR_THROWN && failures++ < 5) {
      console.log(`[bot] ${G.OP_NAMES[op]} -> ${code}`);
    } else if (code === 0 && G.OP_NAMES[op] === "StructureSpawn.spawnCreep") {
      console.log(`[bot] spawning ${S(ARGV[1])}`);
    }
    V.res[i] = code;
  }
  if (V.out[4] > 0) console.log(`[bot] ${V.out[4]} commands/arguments dropped (buffer overflow)`);
}

function run() {
  cleanDeadCreepMemory();
  views();
  try {
    if (!memEnsureRestored()) return; // segments requested below; the bot starts next tick
    views(); // restoring may have grown Go's memory
    objs = [];
    handleOf = new Map();
    matrices = [];
    storeCur = 0;
    pack();
    const n = decide();
    if (n >= 0) execute(n);
  } finally {
    // Persist even if the tick threw: the store lives in Go memory, which the
    // loader discards after an error.
    try {
      if (memRestored) memPersistNow();
    } catch (e) {
      console.log(`[mem] persist threw: ${e && e.message ? e.message : e}`);
    }
    RawMemory.setActiveSegments(memSegIds());
  }
}

module.exports = { bind, unbind, run, PACK, PRESETS, _test: { gen: () => G, HD, H, MATRIX_ADD } };
