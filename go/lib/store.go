package lib

// A small persistent key-value store owned by Go.
//
// The store lives in Go memory and is read and written with plain Go calls (no
// JS crossings). js/host.js persists it: after a tick in which it changed
// (Dirty), the host asks Go to Encode it, and writes the bytes to RawMemory
// segments; after a global reset or re-instantiation the host reads them back
// and calls Decode before the first tick. So treat Mem as durable, but only up
// to the end of the last tick that completed: a tick that throws still persists
// the writes made before the throw, because the snapshot is taken from Go memory.
//
// Keys are slash-separated strings by convention ("creep/<id>/role"). Values
// are ints, floats, strings or byte slices. A key holds one kind at a time:
// writing a different kind replaces the value, and Get* of the wrong kind
// returns the zero value and false.
//
// The encoded form is versioned (StoreVersion) and checksummed. Decode rejects
// anything it cannot read, leaving the store empty, so a format change on
// deploy drops the old data instead of misreading it.
//
// This file imports nothing from syscall/js, so the store is testable on the host.

import (
	"math"
	"sort"
	"strconv"
)

// StoreVersion is bumped whenever the encoding changes; blobs of another
// version are discarded on load.
const StoreVersion = 1

var storeMagic = [4]byte{'G', 'S', 'M', StoreVersion}

// Value kinds, also the on-wire kind byte.
const (
	kindInt   = 1
	kindFloat = 2
	kindStr   = 3
	kindBytes = 4
)

type entry struct {
	kind uint8
	i    int64
	f    float64
	b    []byte // string bytes for kindStr
}

// KV is the key-value store. The zero value is ready to use.
type KV struct {
	m     map[string]entry
	dirty bool
}

// Mem is the bot's store. It is a package-level singleton because the host
// persists exactly one.
var Mem KV

func (s *KV) put(k string, e entry) {
	if s.m == nil {
		s.m = map[string]entry{}
	}
	s.m[k] = e
	s.dirty = true
}

// SetInt stores an integer.
func (s *KV) SetInt(k string, v int) { s.put(k, entry{kind: kindInt, i: int64(v)}) }

// SetFloat stores a float.
func (s *KV) SetFloat(k string, v float64) { s.put(k, entry{kind: kindFloat, f: v}) }

// SetStr stores a string.
func (s *KV) SetStr(k, v string) { s.put(k, entry{kind: kindStr, b: []byte(v)}) }

// SetBytes stores a copy of v.
func (s *KV) SetBytes(k string, v []byte) {
	s.put(k, entry{kind: kindBytes, b: append([]byte(nil), v...)})
}

// Int returns the integer at k.
func (s *KV) Int(k string) (int, bool) {
	e, ok := s.m[k]
	if !ok || e.kind != kindInt {
		return 0, false
	}
	return int(e.i), true
}

// Float returns the float at k.
func (s *KV) Float(k string) (float64, bool) {
	e, ok := s.m[k]
	if !ok || e.kind != kindFloat {
		return 0, false
	}
	return e.f, true
}

// Str returns the string at k.
func (s *KV) Str(k string) (string, bool) {
	e, ok := s.m[k]
	if !ok || e.kind != kindStr {
		return "", false
	}
	return string(e.b), true
}

// Bytes returns a copy of the bytes at k.
func (s *KV) Bytes(k string) ([]byte, bool) {
	e, ok := s.m[k]
	if !ok || e.kind != kindBytes {
		return nil, false
	}
	return append([]byte(nil), e.b...), true
}

// IntOr is Int with a default.
func (s *KV) IntOr(k string, def int) int {
	if v, ok := s.Int(k); ok {
		return v
	}
	return def
}

// Has reports whether k holds any value.
func (s *KV) Has(k string) bool { _, ok := s.m[k]; return ok }

// Len is the number of keys.
func (s *KV) Len() int { return len(s.m) }

// Delete removes k.
func (s *KV) Delete(k string) {
	if _, ok := s.m[k]; ok {
		delete(s.m, k)
		s.dirty = true
	}
}

// DeletePrefix removes every key starting with prefix and returns how many.
// An empty prefix clears the store.
func (s *KV) DeletePrefix(prefix string) int {
	n := 0
	for k := range s.m {
		if hasPrefix(k, prefix) {
			delete(s.m, k)
			n++
		}
	}
	if n > 0 {
		s.dirty = true
	}
	return n
}

// Keys returns the keys starting with prefix, sorted.
func (s *KV) Keys(prefix string) []string {
	var ks []string
	for k := range s.m {
		if hasPrefix(k, prefix) {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

// Sweep deletes every key under prefix whose next path segment fails keep.
// With prefix "creep/" and key "creep/ab12/role" the segment is "ab12". Use it to
// drop the entries of objects that no longer exist. Returns the count removed.
func (s *KV) Sweep(prefix string, keep func(segment string) bool) int {
	n := 0
	for k := range s.m {
		if !hasPrefix(k, prefix) {
			continue
		}
		seg := k[len(prefix):]
		for i := 0; i < len(seg); i++ {
			if seg[i] == '/' {
				seg = seg[:i]
				break
			}
		}
		if !keep(seg) {
			delete(s.m, k)
			n++
		}
	}
	if n > 0 {
		s.dirty = true
	}
	return n
}

// Dirty reports whether the store changed since it was last persisted
// (MarkClean) or loaded (Decode).
func (s *KV) Dirty() bool { return s.dirty }

// MarkClean records that the current contents were persisted. The host calls it
// only after the write succeeded, so a failed write is retried next tick.
func (s *KV) MarkClean() { s.dirty = false }

// MarkDirty forces the next persist; useful after a write that did not fit.
func (s *KV) MarkDirty() { s.dirty = true }

// Reset empties the store. It marks it dirty only if it held data.
func (s *KV) Reset() {
	if len(s.m) > 0 {
		s.dirty = true
	}
	s.m = nil
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// ObjKey builds the conventional key for a per-object field from the object's
// id words (the IdA/IdB/IdC columns, stable for the object's lifetime):
// "creep/1a2b3c4d.5e6f7a8b.9c0d1e2f/role".
func ObjKey(kind string, a, b, c int32, field string) string {
	h := func(x int32) string {
		s := strconv.FormatUint(uint64(uint32(x)), 16)
		for len(s) < 8 {
			s = "0" + s
		}
		return s
	}
	return kind + "/" + h(a) + h(b) + h(c) + "/" + field
}

// ---- encoding -----------------------------------------------------------------
//
//	magic[4] ("GSM"+version) | count uvarint | entries... | fnv32 of all prior bytes (LE)
//	entry: keyLen uvarint | key | kind byte | payload
//	  int:   zigzag varint        float: 8 bytes LE bits
//	  str/bytes: len uvarint | data
//
// Entries are written in sorted key order so equal stores encode identically.

func putUvarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func fnv32(b []byte) uint32 {
	h := uint32(2166136261)
	for _, c := range b {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// Encode serializes the store. It does not touch the dirty flag.
func (s *KV) Encode() []byte {
	b := append([]byte(nil), storeMagic[:]...)
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b = putUvarint(b, uint64(len(keys)))
	for _, k := range keys {
		e := s.m[k]
		b = putUvarint(b, uint64(len(k)))
		b = append(b, k...)
		b = append(b, e.kind)
		switch e.kind {
		case kindInt:
			b = putUvarint(b, uint64(e.i<<1)^uint64(e.i>>63))
		case kindFloat:
			bits := math.Float64bits(e.f)
			for i := 0; i < 8; i++ {
				b = append(b, byte(bits>>(8*i)))
			}
		default:
			b = putUvarint(b, uint64(len(e.b)))
			b = append(b, e.b...)
		}
	}
	h := fnv32(b)
	b = append(b, byte(h), byte(h>>8), byte(h>>16), byte(h>>24))
	return b
}

type reader struct {
	b   []byte
	pos int
	bad bool
}

func (r *reader) uvarint() uint64 {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.pos >= len(r.b) {
			r.bad = true
			return 0
		}
		c := r.b[r.pos]
		r.pos++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v
		}
	}
	r.bad = true
	return 0
}

func (r *reader) take(n uint64) []byte {
	if r.bad || n > uint64(len(r.b)-r.pos) {
		r.bad = true
		return nil
	}
	p := r.b[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return p
}

// Decode replaces the store with the encoded one. On any error (wrong magic or
// version, bad checksum, truncation) it leaves the store EMPTY and returns false,
// so a corrupt or old-format blob never yields half-read data. On success the
// store is not dirty.
func (s *KV) Decode(b []byte) bool {
	s.m, s.dirty = nil, false
	if len(b) < len(storeMagic)+1+4 {
		return false
	}
	body, sum := b[:len(b)-4], b[len(b)-4:]
	if fnv32(body) != uint32(sum[0])|uint32(sum[1])<<8|uint32(sum[2])<<16|uint32(sum[3])<<24 {
		return false
	}
	for i := range storeMagic {
		if body[i] != storeMagic[i] {
			return false
		}
	}
	r := &reader{b: body, pos: len(storeMagic)}
	n := r.uvarint()
	m := map[string]entry{}
	for ; n > 0 && !r.bad; n-- {
		k := string(r.take(r.uvarint()))
		kind := r.take(1)
		if r.bad {
			break
		}
		e := entry{kind: kind[0]}
		switch e.kind {
		case kindInt:
			z := r.uvarint()
			e.i = int64(z>>1) ^ -int64(z&1)
		case kindFloat:
			p := r.take(8)
			if r.bad {
				break
			}
			var bits uint64
			for i := 0; i < 8; i++ {
				bits |= uint64(p[i]) << (8 * i)
			}
			e.f = math.Float64frombits(bits)
		case kindStr, kindBytes:
			e.b = append([]byte{}, r.take(r.uvarint())...)
		default:
			r.bad = true
		}
		m[k] = e
	}
	if r.bad || r.pos != len(body) {
		return false
	}
	s.m = m
	return true
}

// ---- text dump -----------------------------------------------------------------

// Format returns one "key = value" line per key starting with prefix, sorted,
// for the console dump. Strings are quoted; bytes show length and a hex prefix.
func (s *KV) Format(prefix string) []string {
	var lines []string
	for _, k := range s.Keys(prefix) {
		e := s.m[k]
		var v string
		switch e.kind {
		case kindInt:
			v = strconv.FormatInt(e.i, 10)
		case kindFloat:
			v = strconv.FormatFloat(e.f, 'g', -1, 64)
		case kindStr:
			v = strconv.Quote(string(e.b))
		default:
			const hexd = "0123456789abcdef"
			n := len(e.b)
			if n > 16 {
				n = 16
			}
			h := make([]byte, 0, 2*n)
			for _, c := range e.b[:n] {
				h = append(h, hexd[c>>4], hexd[c&15])
			}
			v = "bytes(" + strconv.Itoa(len(e.b)) + ") " + string(h)
			if len(e.b) > n {
				v += "..."
			}
		}
		lines = append(lines, k+" = "+v)
	}
	return lines
}
