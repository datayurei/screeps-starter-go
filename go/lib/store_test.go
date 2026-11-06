package lib

import (
	"math"
	"reflect"
	"testing"
)

func filled() *KV {
	var s KV
	s.SetInt("a/int", -123456)
	s.SetInt("a/zero", 0)
	s.SetInt("a/big", math.MaxInt32)
	s.SetFloat("a/pi", math.Pi)
	s.SetFloat("a/neg", -0.5)
	s.SetStr("b/name", "héllo \"world\"")
	s.SetStr("b/empty", "")
	s.SetBytes("c/blob", []byte{0, 1, 2, 255, 254})
	return &s
}

func TestKVRoundTrip(t *testing.T) {
	a := filled()
	enc := a.Encode()
	if !a.Dirty() {
		t.Error("Encode must not clear the dirty flag; MarkClean does")
	}
	a.MarkClean()
	var b KV
	if !b.Decode(enc) {
		t.Fatal("Decode failed on a valid blob")
	}
	if b.Dirty() {
		t.Error("a freshly decoded store must not be dirty")
	}
	if !reflect.DeepEqual(a.m, b.m) {
		t.Errorf("round trip changed the store:\n%v\n%v", a.m, b.m)
	}
	if v, ok := b.Int("a/int"); !ok || v != -123456 {
		t.Errorf("Int = %d %v", v, ok)
	}
	if v, ok := b.Str("b/name"); !ok || v != "héllo \"world\"" {
		t.Errorf("Str = %q %v", v, ok)
	}
	if v, ok := b.Bytes("c/blob"); !ok || !reflect.DeepEqual(v, []byte{0, 1, 2, 255, 254}) {
		t.Errorf("Bytes = %v %v", v, ok)
	}
}

func TestKVEncodingIsDeterministic(t *testing.T) {
	if !reflect.DeepEqual(filled().Encode(), filled().Encode()) {
		t.Error("equal stores must encode to equal bytes")
	}
}

func TestKVEmptyRoundTrip(t *testing.T) {
	var a, b KV
	if !b.Decode(a.Encode()) || b.Len() != 0 {
		t.Error("empty store must round-trip")
	}
}

func TestKVDecodeRejectsDamage(t *testing.T) {
	enc := filled().Encode()
	cases := map[string][]byte{
		"nil":       nil,
		"short":     enc[:3],
		"truncated": enc[:len(enc)-5],
		"extra":     append(append([]byte(nil), enc...), 0),
	}
	flip := append([]byte(nil), enc...)
	flip[10] ^= 0x40
	cases["bit flip"] = flip
	ver := append([]byte(nil), enc...)
	ver[3]++ // other version, checksum no longer matches either
	cases["version"] = ver
	for name, blob := range cases {
		s := filled()
		if s.Decode(blob) {
			t.Errorf("%s: Decode accepted a bad blob", name)
		}
		if s.Len() != 0 {
			t.Errorf("%s: a failed Decode must leave the store empty, has %d keys", name, s.Len())
		}
	}
}

func TestKVDecodeRejectsOtherVersionWithValidChecksum(t *testing.T) {
	enc := filled().Encode()
	body := append([]byte(nil), enc[:len(enc)-4]...)
	body[3]++
	h := fnv32(body)
	blob := append(body, byte(h), byte(h>>8), byte(h>>16), byte(h>>24))
	var s KV
	if s.Decode(blob) {
		t.Error("a blob of another StoreVersion must be rejected")
	}
}

func TestKVKindMismatch(t *testing.T) {
	var s KV
	s.SetInt("k", 5)
	if _, ok := s.Str("k"); ok {
		t.Error("Str on an int key must report false")
	}
	s.SetStr("k", "x")
	if _, ok := s.Int("k"); ok {
		t.Error("overwriting with another kind must replace the value")
	}
	if got := s.IntOr("missing", 7); got != 7 {
		t.Errorf("IntOr default = %d", got)
	}
}

func TestKVDirtyTracking(t *testing.T) {
	var s KV
	if s.Dirty() {
		t.Error("new store is clean")
	}
	s.Delete("nope")
	s.DeletePrefix("nope/")
	s.Reset()
	if s.Dirty() {
		t.Error("no-op mutations must not mark dirty")
	}
	s.SetInt("x", 1)
	if !s.Dirty() {
		t.Error("Set must mark dirty")
	}
	s.MarkClean()
	s.Delete("x")
	if !s.Dirty() {
		t.Error("Delete of a present key must mark dirty")
	}
}

func TestKVPrefixOps(t *testing.T) {
	s := filled()
	if got := s.Keys("a/"); !reflect.DeepEqual(got, []string{"a/big", "a/int", "a/neg", "a/pi", "a/zero"}) {
		t.Errorf("Keys = %v", got)
	}
	if n := s.DeletePrefix("a/"); n != 5 {
		t.Errorf("DeletePrefix removed %d", n)
	}
	if s.Len() != 3 {
		t.Errorf("Len = %d", s.Len())
	}
	if n := s.DeletePrefix(""); n != 3 || s.Len() != 0 {
		t.Error("empty prefix must clear the store")
	}
}

func TestKVSweep(t *testing.T) {
	var s KV
	s.SetInt(ObjKey("creep", 1, 2, 3, "role"), 1)
	s.SetInt(ObjKey("creep", 1, 2, 3, "target"), 9)
	s.SetInt(ObjKey("creep", 4, 5, 6, "role"), 2)
	s.SetInt("global/x", 1)
	alive := ObjKey("creep", 4, 5, 6, "")
	alive = alive[len("creep/") : len(alive)-1] // the id segment: drop the prefix and the trailing "/"
	n := s.Sweep("creep/", func(seg string) bool { return seg == alive })
	if n != 2 || !s.Has(ObjKey("creep", 4, 5, 6, "role")) || !s.Has("global/x") {
		t.Errorf("Sweep removed %d; keys now %v", n, s.Keys(""))
	}
}

func TestObjKey(t *testing.T) {
	if got := ObjKey("creep", 0x1a2b, -1, 0, "role"); got != "creep/00001a2bffffffff00000000/role" {
		t.Errorf("ObjKey = %q", got)
	}
}

func TestKVFormat(t *testing.T) {
	var s KV
	s.SetInt("n", -4)
	s.SetStr("s", "hi")
	s.SetFloat("f", 1.5)
	s.SetBytes("b", []byte{0xde, 0xad})
	want := []string{`b = bytes(2) dead`, `f = 1.5`, `n = -4`, `s = "hi"`}
	if got := s.Format(""); !reflect.DeepEqual(got, want) {
		t.Errorf("Format = %q", got)
	}
}
