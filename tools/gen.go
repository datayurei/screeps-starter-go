package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strings"
)

// ---- inventory + coverage ---------------------------------------------------

type invEntry struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Args       string `json:"args"`
	Action     bool   `json:"action"`
	Deprecated bool   `json:"deprecated"`
}

func loadInventory(root string) []invEntry {
	raw, err := os.ReadFile(root + "/api/inventory.json")
	if err != nil {
		fail("gen: " + err.Error())
	}
	var inv []invEntry
	if err := json.Unmarshal(raw, &inv); err != nil {
		fail("gen: inventory: " + err.Error())
	}
	return inv
}

// matchCover expands a cover pattern against the inventory ids. "Structure*.x"
// matches every id whose first segment starts with "Structure" and rest is x.
func matchCover(pat string, ids []string) []string {
	if !strings.Contains(pat, "*") {
		return []string{pat}
	}
	i := strings.Index(pat, "*")
	pre, rest := pat[:i], pat[i+1:] // rest begins with "."
	var out []string
	for _, id := range ids {
		if strings.HasPrefix(id, pre) && strings.HasSuffix(id, rest) {
			seg := strings.SplitN(id, ".", 2)
			if len(seg) == 2 && "."+seg[1] == rest {
				out = append(out, id)
			}
		}
	}
	return out
}

// ---- signature parsing -------------------------------------------------------

type sig struct {
	Cls, Method, Tag string
	Args             []string
	Approach         bool
}

var sigRe = regexp.MustCompile(`^([\w.\-]+?)(?:#(\w+))?\((.*)\)(\s+@approach)?$`)

func parseSig(s string) sig {
	m := sigRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		fail("gen: bad signature: " + s)
	}
	full := m[1]
	dot := strings.LastIndex(full, ".")
	r := sig{Cls: full[:dot], Method: full[dot+1:], Tag: m[2], Approach: m[4] != ""}
	for _, a := range strings.Split(m[3], ",") {
		if a = strings.TrimSpace(a); a != "" {
			r.Args = append(r.Args, a)
		}
	}
	return r
}

func (s sig) id() string { return s.Cls + "." + s.Method }

var staticClasses = map[string]bool{
	"Game": true, "Game.cpu": true, "Game.map": true, "Game.market": true, "Game.shard": true,
	"Game.map-visual": true, "PathFinder": true, "RawMemory": true, "InterShardMemory": true,
	"Memory": true, "PathFinder.CostMatrix": true,
}

// hasRecv: does the record carry a receiver handle in its second word?
func hasRecv(s sig, tpl string) bool {
	if staticClasses[s.Cls] || s.Cls == "RoomPosition" {
		return false
	}
	return tpl == "" || strings.Contains(tpl, "$s")
}

// recvExpr is the JS expression for the receiver object.
func recvExpr(s sig) string {
	switch s.Cls {
	case "Game.map-visual":
		return "Game.map.visual"
	case "RoomVisual":
		return "H(s).visual"
	case "StructureSpawn.Spawning":
		return "H(s).spawning"
	case "Store":
		return "H(s).store"
	case "Room.Terrain":
		return "TERR(H(s))"
	case "RoomPosition":
		return "POS(a[0])"
	}
	if staticClasses[s.Cls] {
		return s.Cls
	}
	return "H(s)"
}

var decoders = map[string]string{
	"h": "H(a[%d])", "i": "a[%d]", "io": "OPT(a[%d])", "b": "a[%d]!==0", "res": "RES[a[%d]]",
	"reso": "RESO(a[%d])", "st": "STRUCT[a[%d]]", "dir": "a[%d]", "str": "S(a[%d])",
	"rn": "RNAME(a[%d])", "arr": "ARR(a[%d])", "parts": "PARTS(a[%d])", "path": "PATH(a[%d])",
	"pos": "POS(a[%d])", "milli": "(a[%d]/1000)", "harr": "HARR(a[%d])", "look": "LOOKS[a[%d]]",
}

func decodeArg(kind string, idx int) string {
	if strings.HasPrefix(kind, "preset:") {
		return fmt.Sprintf("PRESETS.%s[a[%d]]", kind[7:], idx)
	}
	d, ok := decoders[kind]
	if !ok {
		fail("gen: unknown arg kind " + kind)
	}
	return fmt.Sprintf(d, idx)
}

// jsBody renders the JS call for a signature. a[] indices start after the
// receiver pos for RoomPosition methods.
func jsBody(s sig, tpl string) string {
	off := 0
	recvIsArg := s.Cls == "RoomPosition"
	if recvIsArg {
		off = 1
	}
	args := s.Args
	if recvIsArg {
		args = args[1:]
	}
	dec := make([]string, len(args))
	for i, k := range args {
		dec[i] = decodeArg(k, i+off)
	}
	if tpl == "" {
		tpl = "$s." + s.Method + "(" + strings.Join(placeholders(len(args)), ",") + ")"
	}
	tpl = strings.ReplaceAll(tpl, "$s", recvExpr(s))
	for i := len(dec) - 1; i >= 0; i-- { // high to low so $1 doesn't clobber $10
		tpl = strings.ReplaceAll(tpl, fmt.Sprintf("$%d", i), dec[i])
	}
	return tpl
}

func placeholders(n int) []string {
	r := make([]string, n)
	for i := range r {
		r[i] = fmt.Sprintf("$%d", i)
	}
	return r
}

// ---- naming -------------------------------------------------------------------

var nonWord = regexp.MustCompile(`[^A-Za-z0-9]+`)

func camel(s string) string {
	var b strings.Builder
	for _, p := range nonWord.Split(s, -1) {
		if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

func goName(s sig) string { return camel(s.Cls + "." + s.Method + "." + s.Tag) }

func itoa(n int) string { return fmt.Sprint(n) }

var goTypes = map[string]string{
	"h": "Handle", "i": "int32", "io": "int32", "b": "bool", "res": "int32", "reso": "int32",
	"st": "int32", "dir": "int32", "str": "string", "rn": "RoomRef", "arr": "[]int32",
	"parts": "[]int32", "path": "[]int32", "pos": "Pos", "milli": "float64", "harr": "[]Handle", "look": "int32",
}

func goType(k string) string {
	if strings.HasPrefix(k, "preset:") {
		return "Preset"
	}
	return goTypes[k]
}

func goEnc(k, v string) string {
	switch {
	case k == "b":
		return "c.b2i(" + v + ")"
	case k == "str":
		return "c.putStr(" + v + ")"
	case k == "arr", k == "parts", k == "path":
		return "c.putInts(" + v + ")"
	case k == "harr":
		return "c.putHandles(" + v + ")"
	case k == "milli":
		return "milli(" + v + ")"
	default:
		return "int32(" + v + ")"
	}
}

// ---- layout -------------------------------------------------------------------

const (
	cmdWords   = 7
	cmdCap     = 2048
	qryCap     = 512
	arenaCap   = 65536
	bytesCap   = 32768
	respCap    = 65536
	roomCap    = 64
	terrSlots  = 16
	maxRounds  = 4
	resTerrain = 2500
	storeArena = 32768
)

type kindLayout struct {
	k      kindSpec
	base   int
	stride int
	nIdx   int
}

type layout struct {
	hdrLen   int
	room     kindLayout
	kinds    []kindLayout
	inLen    int
	hdrIndex map[string]int
}

func buildLayout() layout {
	var l layout
	l.hdrIndex = map[string]int{}
	n := 0
	for _, h := range headerCols {
		l.hdrIndex[h.Name] = n
		n++
	}
	l.hdrIndex["nRoom"] = n
	n++
	for _, k := range kinds {
		l.hdrIndex["n_"+k.Name] = n
		n++
	}
	l.hdrLen = n
	roomK := kindSpec{Name: "room", Cap: roomCap, Cols: roomCols}
	l.room = kindLayout{k: roomK, base: n, stride: 1 + len(roomCols), nIdx: l.hdrIndex["nRoom"]}
	n += roomCap * l.room.stride
	for _, k := range kinds {
		kl := kindLayout{k: k, base: n, stride: 1 + len(k.Cols), nIdx: l.hdrIndex["n_"+k.Name]}
		l.kinds = append(l.kinds, kl)
		n += k.Cap * kl.stride
	}
	l.inLen = n
	return l
}

func (l layout) all() []kindLayout { return append([]kindLayout{l.room}, l.kinds...) }

// ---- main entry ---------------------------------------------------------------

// extensions are ops/queries this repo adds that are not part of the official API.
var extensions = map[string]bool{"Memory.set": true, "PathFinder.CostMatrix.new": true}

type how struct{ Kind, Where string }

func gen(root string, checkOnly bool) {
	inv := loadInventory(root)
	ids := make([]string, len(inv))
	known := map[string]bool{}
	for i, e := range inv {
		ids[i] = e.ID
		known[e.ID] = true
	}
	cov := map[string][]how{}
	add := func(id, kind, where string) {
		if extensions[id] {
			return
		}
		if !known[id] {
			fail("gen: spec references unknown API id: " + id + " (" + kind + " " + where + ")")
		}
		cov[id] = append(cov[id], how{kind, where})
	}
	addPat := func(pat, kind, where string) {
		m := matchCover(pat, ids)
		if len(m) == 0 {
			fail("gen: cover pattern matches nothing: " + pat)
		}
		for _, id := range m {
			add(id, kind, where)
		}
	}

	l := buildLayout()

	// columns
	for _, h := range headerCols {
		for _, p := range h.Covers {
			addPat(p, "pack", "header."+h.Name)
		}
	}
	for _, kl := range l.all() {
		classes := map[string]bool{}
		for _, cl := range kl.k.Cols {
			for _, p := range cl.Covers {
				addPat(p, "pack", kl.k.Name+"."+cl.Name)
				classes[p] = true
			}
		}
	}
	// ops
	var oprecs []opRec
	seenName := map[string]bool{}
	for i, o := range ops {
		s := parseSig(o.Sig)
		nm := goName(s)
		if seenName[nm] {
			fail("gen: duplicate op name " + nm)
		}
		seenName[nm] = true
		oprecs = append(oprecs, opRec{s, o, i, hasRecv(s, o.Tpl)})
		where := "op " + nm
		if s.Approach {
			where += " (+Approach)"
		}
		add(s.id(), "op", where)
		for _, cv := range o.Covers {
			add(cv, "op", where)
		}
		if s.Approach && (len(s.Args) == 0 || s.Args[0] != "h") {
			fail("gen: @approach needs a leading handle arg: " + o.Sig)
		}
		if len(s.Args) > 5 {
			fail("gen: too many args: " + o.Sig)
		}
	}
	var qrecs []qRec
	for i, qs := range queries {
		s := parseSig(qs.Sig)
		nm := "Q" + goName(s)
		if seenName[nm] {
			fail("gen: duplicate query name " + nm)
		}
		seenName[nm] = true
		qrecs = append(qrecs, qRec{s, qs, i, hasRecv(s, qs.Tpl)})
		add(s.id(), "query", nm)
		for _, cv := range qs.Covers {
			add(cv, "query", nm)
		}
		if len(s.Args) > 5 {
			fail("gen: too many args: " + qs.Sig)
		}
	}
	for _, n := range natives {
		add(n.ID, "native", n.Go)
	}
	for id, why := range collections {
		add(id, "table", why)
	}
	for id, why := range jsOnly {
		if strings.TrimSpace(why) == "" {
			fail("gen: jsOnly entry without a reason: " + id)
		}
		add(id, "js", why)
	}

	// coverage check: every inventory entry must be accounted for
	var missing []string
	for _, e := range inv {
		if len(cov[e.ID]) == 0 {
			missing = append(missing, e.ID)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "gen: %d of %d API entries have no disposition:\n", len(missing), len(inv))
		for _, m := range missing {
			fmt.Fprintln(os.Stderr, "  "+m)
		}
		os.Exit(1)
	}
	fmt.Printf("gen: coverage ok: %d/%d API entries accounted for\n", len(inv), len(inv))
	if checkOnly {
		return
	}

	write(root+"/docs/api-coverage.md", docOut(inv, cov))
	write(root+"/go/lib/gen.go", goOut(l, oprecs, qrecs))
	write(root+"/go/lib/obj_gen.go", goObjOut(l, oprecs, qrecs))
	write(root+"/js/host_gen.js", jsOut(l, oprecs, qrecs))
}

type opRec struct {
	s    sig
	spec opSpec
	code int
	recv bool
}

type qRec struct {
	s    sig
	spec querySpec
	code int
	recv bool
}

func write(path, content string) {
	if strings.HasSuffix(path, ".go") {
		src, err := format.Source([]byte(content))
		if err != nil {
			os.WriteFile(path+".bad", []byte(content), 0o644)
			fail("gen: gofmt " + path + ": " + err.Error() + " (raw output in " + path + ".bad)")
		}
		content = string(src)
	}
	if err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o755); err != nil {
		fail("gen: " + err.Error())
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fail("gen: " + err.Error())
	}
	fmt.Println("gen: wrote", path)
}

func layoutHash(parts ...string) int32 {
	h := fnv.New32a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return int32(h.Sum32() & 0x7fffffff)
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
