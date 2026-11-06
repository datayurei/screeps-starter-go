// Package screeps is a thin, typed wrapper over the Screeps game API for Go
// bots compiled to WebAssembly with TinyGo. It exists so your editor can
// autocomplete and type-check game calls instead of stringly-typed
// syscall/js (creep.Call("harvest", source)).
//
// Every type here is a zero-cost struct around a js.Value; methods are a
// direct translation of the official API (https://docs.screeps.com/api/).
//
// COST MODEL — read this: each method that touches the game (a property read
// or a call) is one or more JS<->WASM crossings, ~0.5 µs of in-game CPU each
// on the official server. The wrapper does not add
// crossings over hand-written syscall/js, but it makes them easy to write in
// loops. Read a value once into a Go variable instead of calling the getter
// repeatedly, and for heavy per-tick math over many objects prefer packing
// data into shared memory from JS (see js/host.js).
//
// Constants (ErrorCode, FindConstant, BodyPart, ...) are hard-coded Go
// values, so using them costs nothing.
//
// Not wrapped yet? Every type exposes JSValue(), so you can always drop down
// to raw syscall/js for the parts of the API this package doesn't cover.
package screeps

import "syscall/js"

// Target is anything that can be passed to the game as a target: every
// wrapped object and RoomPosition implements it.
type Target interface {
	JSValue() js.Value
}

// Persistent JS globals are cached once; they live for the life of the
// isolate. (Game/Memory are NOT cached: the loader re-bridges them every tick.)
var (
	jsObject = js.Global().Get("Object")
	jsArray  = js.Global().Get("Array")
)

// Keys returns Object.keys(v). Returns nil for null/undefined.
func Keys(v js.Value) []string {
	if !v.Truthy() {
		return nil
	}
	ks := jsObject.Call("keys", v)
	n := ks.Length()
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = ks.Index(i).String()
	}
	return out
}

// NewObject returns a fresh empty JS object ({}).
func NewObject() js.Value { return jsObject.New() }

// intOr reads v as an int, returning def for null/undefined (the API returns
// null for e.g. Store.getFreeCapacity of an unsupported resource).
func intOr(v js.Value, def int) int {
	if v.Type() != js.TypeNumber {
		return def
	}
	return v.Int()
}

func stringOr(v js.Value, def string) string {
	if v.Type() != js.TypeString {
		return def
	}
	return v.String()
}

func errCode(v js.Value) ErrorCode { return ErrorCode(intOr(v, int(ErrInvalidArgs))) }

// wrapArray converts a JS array into a Go slice using wrap for each element.
func wrapArray[T any](arr js.Value, wrap func(js.Value) T) []T {
	if !arr.Truthy() {
		return nil
	}
	n := arr.Length()
	out := make([]T, n)
	for i := 0; i < n; i++ {
		out[i] = wrap(arr.Index(i))
	}
	return out
}

// toArray builds a JS array from wrapped targets.
func toArray[T Target](items []T) js.Value {
	arr := jsArray.New(len(items))
	for i, it := range items {
		arr.SetIndex(i, it.JSValue())
	}
	return arr
}

// pick maps a JS object returned by the game (e.g. from findClosestByPath)
// back to the matching Go element. js.Value.Equal compares refs in Go, and
// the same JS object always has the same ref, so this costs no crossings.
func pick[T Target](items []T, res js.Value) (T, bool) {
	var zero T
	if !res.Truthy() {
		return zero, false
	}
	for _, it := range items {
		if it.JSValue().Equal(res) {
			return it, true
		}
	}
	return zero, false
}

// Log writes a line to the Screeps console. It goes through stdout (println →
// wasm_exec's fs.writeSync → console.log) because `console` is not reachable
// from js.Global() in the Screeps isolate. Supports string, error, int,
// ErrorCode, bool, js.Value and fmt.Stringer-like values without pulling in fmt.
func Log(args ...any) {
	var b []byte
	for i, a := range args {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, toString(a)...)
	}
	println(string(b))
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	case interface{ String() string }: // ErrorCode, js.Value, ...
		return x.String()
	case int:
		return itoa(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return "<value>"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
