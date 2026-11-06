"use strict";
/*
 * Screeps entry module (uploaded as `main`).
 *
 * Responsibilities:
 *   1. Make sure the few globals TinyGo's wasm_exec.js expects exist in the
 *      Screeps sandbox (globalThis, TextEncoder/TextDecoder).
 *   2. Compile + instantiate the Go WASM module ONCE, then reuse the instance
 *      across ticks (instantiation is expensive, so we guard on CPU bucket).
 *   3. Run host.run() every tick (packs the game into shared memory, calls Go's `tick`), recovering from errors by
 *      forcing a clean re-instantiation on the next tick.
 *
 * The .wasm binary is uploaded as a separate module named `main.wasm`;
 * `require("main.wasm")` returns an already-compiled WebAssembly.Module in the
 * Screeps runtime. `wasm_exec` is TinyGo's runtime glue, uploaded as its own
 * module (copied into dist/ at build time so it always matches your TinyGo).
 */

// ---- 1. Minimal polyfills for the Screeps sandbox ---------------------------

if (typeof globalThis === "undefined") {
  // Older isolates expose `global` but not `globalThis`.
  // eslint-disable-next-line no-global-assign
  global.globalThis = global;
}

if (typeof globalThis.TextEncoder === "undefined") {
  globalThis.TextEncoder = class TextEncoder {
    encode(str) {
      str = String(str);
      const bytes = [];
      for (let i = 0; i < str.length; i++) {
        let c = str.charCodeAt(i);
        if (c < 0x80) {
          bytes.push(c);
        } else if (c < 0x800) {
          bytes.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
        } else if (c >= 0xd800 && c <= 0xdbff && i + 1 < str.length) {
          const c2 = str.charCodeAt(++i);
          c = 0x10000 + ((c & 0x3ff) << 10) + (c2 & 0x3ff);
          bytes.push(
            0xf0 | (c >> 18),
            0x80 | ((c >> 12) & 0x3f),
            0x80 | ((c >> 6) & 0x3f),
            0x80 | (c & 0x3f)
          );
        } else {
          bytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
        }
      }
      return new Uint8Array(bytes);
    }
  };
}

if (typeof globalThis.TextDecoder === "undefined") {
  globalThis.TextDecoder = class TextDecoder {
    decode(buf) {
      // Handle every shape wasm_exec passes: Uint8Array (fs.writeSync) AND
      // DataView (loadString). `new Uint8Array(dataView)` yields an EMPTY array
      // (DataView has no length), which silently decoded property names to ""
      // and made every Go js.Value.Get() return undefined. View the underlying
      // bytes instead for any ArrayBuffer view; wrap a raw ArrayBuffer directly.
      let bytes;
      if (buf instanceof Uint8Array) {
        bytes = buf;
      } else if (ArrayBuffer.isView(buf)) {
        bytes = new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength);
      } else {
        bytes = new Uint8Array(buf);
      }
      let out = "";
      let i = 0;
      while (i < bytes.length) {
        let c = bytes[i++];
        if (c > 0x7f) {
          if (c > 0xef) {
            // 4-byte UTF-8 (astral plane); emitted as a surrogate pair below.
            c =
              ((c & 0x07) << 18) |
              ((bytes[i++] & 0x3f) << 12) |
              ((bytes[i++] & 0x3f) << 6) |
              (bytes[i++] & 0x3f);
          } else if (c > 0xdf) {
            c = ((c & 0x0f) << 12) | ((bytes[i++] & 0x3f) << 6) | (bytes[i++] & 0x3f);
          } else if (c > 0xbf) {
            c = ((c & 0x1f) << 6) | (bytes[i++] & 0x3f);
          }
        }
        if (c <= 0xffff) {
          out += String.fromCharCode(c);
        } else {
          c -= 0x10000;
          out += String.fromCharCode(0xd800 + (c >> 10), 0xdc00 + (c & 0x3ff));
        }
      }
      return out;
    }
  };
}

// TinyGo's syscall/js glue implements property access via Reflect
// (get/set/apply/construct/deleteProperty). The Screeps isolate doesn't provide
// a working Reflect, so polyfill it. Handles three cases: missing entirely,
// partially present, or present-but-frozen (replace the global).
(function () {
  const haveReflect = typeof Reflect !== "undefined" && Reflect;
  const ok =
    haveReflect &&
    typeof Reflect.get === "function" &&
    typeof Reflect.set === "function" &&
    typeof Reflect.apply === "function" &&
    typeof Reflect.construct === "function" &&
    typeof Reflect.deleteProperty === "function";
  if (ok) return;

  const shim = {
    get(target, key) {
      return target[key];
    },
    set(target, key, value) {
      target[key] = value;
      return true;
    },
    has(target, key) {
      return key in target;
    },
    deleteProperty(target, key) {
      delete target[key];
      return true;
    },
    apply(fn, thisArg, args) {
      return fn.apply(thisArg, args);
    },
    construct(fn, args) {
      return new (Function.prototype.bind.apply(fn, [null].concat(Array.prototype.slice.call(args))))();
    },
    ownKeys(target) {
      const names = Object.getOwnPropertyNames(target);
      return typeof Object.getOwnPropertySymbols === "function"
        ? names.concat(Object.getOwnPropertySymbols(target))
        : names;
    }
  };

  if (haveReflect) {
    for (const k in shim) {
      if (typeof Reflect[k] !== "function") {
        try {
          Reflect[k] = shim[k];
        } catch (e) {
          /* frozen - fall through to global replacement below */
        }
      }
    }
    if (typeof Reflect.get !== "function") {
      globalThis.Reflect = shim;
    }
  } else {
    globalThis.Reflect = shim;
  }
})();

// crypto.getRandomValues: TinyGo's wasm_exec.js reads this at EVAL time to seed
// the Go runtime, and the official-server isolate (unlike the Sim) exposes no
// `crypto`. Provide a non-cryptographic PRNG — the Go runtime only needs it for
// things like map-iteration seeding, not security, so Math.random is fine.
if (typeof globalThis.crypto === "undefined" || typeof globalThis.crypto.getRandomValues !== "function") {
  globalThis.crypto = {
    getRandomValues(arr) {
      // TypedArray element assignment masks to the element's width, so a 32-bit
      // value fills Uint8/16/32 arrays correctly.
      for (let i = 0; i < arr.length; i++) arr[i] = (Math.random() * 0x100000000) >>> 0;
      return arr;
    }
  };
}

// performance.now: wasm_exec.js also requires this at EVAL time (right after
// crypto). TinyGo uses it for the Go monotonic clock; with -scheduler=none our
// bot never reads the clock, so this value only needs to exist and advance.
if (typeof globalThis.performance === "undefined" || typeof globalThis.performance.now !== "function") {
  const haveDate = typeof Date !== "undefined" && typeof Date.now === "function";
  const origin = haveDate ? Date.now() : 0;
  let counter = 0;
  globalThis.performance = {
    now() {
      return haveDate ? Date.now() - origin : ++counter;
    }
  };
}

// ---- 2. Load TinyGo runtime glue (defines globalThis.Go) --------------------
//
// wasm_exec is required LAZILY inside instantiate(), NOT at the top of this file.
// It touches crypto/TextEncoder at eval time, so it must load only after the
// polyfills above have run — requiring it inside instantiate() guarantees that.

// The bot's game I/O (pack state -> Go tick -> issue API calls); see js/host.js.
const host = require("host");

// Indirect reference to `require` so the wasm fetch below stays a *runtime* call
// inside the CPU-bucket guard, instead of being hoisted to module load time.
const runtimeRequire =
  typeof require !== "undefined"
    ? require
    : () => {
        throw new Error("require is not available in this runtime");
      };

// ---- 3. Instance lifecycle --------------------------------------------------

let instance = null; // live WebAssembly.Instance
let goTick = null; // exported Go `tick` function

function instantiate() {
  // Load TinyGo's runtime glue now that the polyfills above exist (it reads
  // crypto/TextEncoder at eval time). Side effect: defines globalThis.Go.
  runtimeRequire("wasm_exec");

  const Go = globalThis.Go;
  if (typeof Go !== "function") {
    throw new Error("wasm_exec did not define Go - check the wasm_exec module");
  }

  const go = new Go();
  const required = runtimeRequire("main.wasm");
  const mod =
    required instanceof WebAssembly.Module
      ? required
      : new WebAssembly.Module(required); // fallback if bytes are returned

  const inst = new WebAssembly.Instance(mod, go.importObject);

  // Runs package init + main() once. With -scheduler=none main() returns and
  // the exported functions stay callable for the life of the instance.
  // go.run is async: a panic in Go main() becomes a REJECTED promise, not a
  // synchronous throw, so it would otherwise escape loop()'s try/catch as an
  // unhandled rejection. Surface it with our prefix and reset so the next tick
  // re-instantiates cleanly.
  const runResult = go.run(inst);
  if (runResult && typeof runResult.catch === "function") {
    runResult.catch((e) => {
      console.log(`[loader] go.run rejected (Go panic in main?): ${e && e.stack ? e.stack : e}`);
      instance = null;
      goTick = null;
      host.unbind();
    });
  }

  if (typeof inst.exports.tick !== "function") {
    throw new Error("WASM module does not export `tick` - check //export tick");
  }

  instance = inst;
  goTick = inst.exports.tick;
  host.bind(inst.exports); // verifies the shared-buffer layout matches this build
}

// ensureInstance lazily instantiates the WASM module, guarding on CPU bucket so
// the (expensive) one-time instantiation can't overrun a tick's CPU limit.
// Returns true once the instance is live.
function ensureInstance() {
  if (goTick !== null) return true;
  if (Game.cpu.bucket < 500) {
    console.log(`[loader] waiting for CPU bucket before instantiating WASM (bucket=${Game.cpu.bucket})`);
    return false;
  }
  console.log("[loader] instantiating Go WASM module");
  instantiate();
  return true;
}

// ---- 4. Game loop -----------------------------------------------------------

// bridgeGlobal copies a per-tick value onto globalThis for Go to read, tolerating
// servers where the name is a read-only global getter (e.g. Memory on the
// official server) — there the value is already present, so the throw is benign.
function bridgeGlobal(name, value) {
  if (typeof value === "undefined") return;
  try {
    globalThis[name] = value;
  } catch (e) {
    // Read-only global on this server; Go reads it off globalThis directly.
  }
}

function loop() {
  try {
    // Make Game/Memory visible to Go (TinyGo's js.Global() IS globalThis). On
    // some sandboxes these are per-tick module locals that must be copied onto
    // globalThis or Go sees `undefined`; on the official server they are already
    // read-only global getters the engine manages (assigning Memory throws), in
    // which case Go reads them directly and bridgeGlobal is a no-op. Done before
    // any WASM call. (console and the FIND_*/OK/ERR_* constants are already on
    // globalThis, so they don't need bridging.)
    // `console` is injected per-context on the official server too (not on
    // globalThis), so Go's js.Global().Get("console") sees undefined and .Call
    // traps. Bridge it alongside Game/Memory. (FIND_*/OK/ERR_* constants used
    // from Go have numeric fallbacks, so they don't need bridging.)
    bridgeGlobal("console", console);
    bridgeGlobal("Game", Game);
    bridgeGlobal("Memory", Memory);
    if (typeof RawMemory !== "undefined") bridgeGlobal("RawMemory", RawMemory);


    // Normal bot: instantiate once (bucket-guarded), then run the bot.
    if (!ensureInstance()) return;
    host.run();
  } catch (err) {
    console.log(`[loader] error: ${err && err.stack ? err.stack : err}`);
    // Force a clean re-instantiation next tick.
    instance = null;
    goTick = null;
    host.unbind();
  }
}

module.exports.loop = loop;
