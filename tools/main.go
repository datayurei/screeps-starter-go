// Command tools replaces the Node/npm build helpers with pure Go:
//
//	go run . patch <path/to/wasm_exec.js>   # rewrite the syscall/js glue
//	go run . upload <repoRoot> <destination> # POST dist/ to the Screeps code API
//
// It has no dependencies beyond the standard library. See scripts/build.sh and
// scripts/push.sh for how it is invoked.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: go run . <patch|upload|gen> ...")
	}
	switch os.Args[1] {
	case "patch":
		if len(os.Args) != 3 {
			fail("usage: go run . patch <path/to/wasm_exec.js>")
		}
		patch(os.Args[2])
	case "upload":
		if len(os.Args) != 4 {
			fail("usage: go run . upload <repoRoot> <destination>")
		}
		upload(os.Args[2], os.Args[3])
	case "gen":
		if len(os.Args) < 3 {
			fail("usage: go run . gen <repoRoot> [--check]")
		}
		gen(os.Args[2], len(os.Args) > 3 && os.Args[3] == "--check")
	default:
		fail("unknown subcommand: " + os.Args[1])
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// --- patch: rewrite Reflect.* in wasm_exec.js to direct access, and add the
// memory-view caching / ASCII fast-path optimizations. See CLAUDE.md for why.
func patch(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail("patch: read: " + err.Error())
	}
	s := string(raw)

	reps := [][2]string{
		// Correctness: the isolate's Reflect misbehaves for wasm_exec; use direct ops.
		{"let result = Reflect.get(v, prop);", "let result = v[prop];"},
		{"Reflect.set(v, p, x);", "v[p] = x;"},
		{"Reflect.deleteProperty(v, p);", "delete v[p];"},
		{"return boxValue(Reflect.get(unboxValue(v_ref), i));", "return boxValue(unboxValue(v_ref)[i]);"},
		{"Reflect.set(unboxValue(v_ref), i, unboxValue(x_ref));", "unboxValue(v_ref)[i] = unboxValue(x_ref);"},
		{"const m = Reflect.get(v, name);", "const m = v[name];"},
		{"storeValue(ret_addr, Reflect.apply(m, v, args));", "storeValue(ret_addr, m.apply(v, args));"},
		{"storeValue(ret_addr, Reflect.apply(v, undefined, args));", "storeValue(ret_addr, v.apply(undefined, args));"},
		{"storeValue(ret_addr, Reflect.construct(v, args));", "storeValue(ret_addr, new v(...args));"},
		// Performance: cache the memory DataView instead of allocating per call.
		{
			"\t\t\t\treturn new DataView(this._inst.exports.memory.buffer);",
			"\t\t\t\tconst b = this._inst.exports.memory.buffer;\n" +
				"\t\t\t\tif (this.__dvBuf !== b) { this.__dvBuf = b; this.__dv = new DataView(b); }\n" +
				"\t\t\t\treturn this.__dv;",
		},
		// Performance: cached Uint8Array + ASCII fast path in loadString.
		{
			"\t\t\t\treturn decoder.decode(new DataView(this._inst.exports.memory.buffer, ptr, len));",
			"\t\t\t\tconst b = this._inst.exports.memory.buffer;\n" +
				"\t\t\t\tif (this.__u8Buf !== b) { this.__u8Buf = b; this.__u8 = new Uint8Array(b); }\n" +
				"\t\t\t\tconst u8 = this.__u8;\n" +
				"\t\t\t\tlet ascii = true;\n" +
				"\t\t\t\tfor (let i = 0; i < len; i++) { if (u8[ptr + i] >= 0x80) { ascii = false; break; } }\n" +
				"\t\t\t\tif (ascii && len <= 1024) return String.fromCharCode.apply(null, u8.subarray(ptr, ptr + len));\n" +
				"\t\t\t\treturn decoder.decode(u8.subarray(ptr, ptr + len));",
		},
	}

	applied := 0
	for _, r := range reps {
		if strings.Contains(s, r[0]) {
			s = strings.ReplaceAll(s, r[0], r[1])
			applied++
		} else if !strings.Contains(s, r[1]) {
			fail("patch: pattern not found (wasm_exec.js changed shape?): " + r[0])
		}
	}

	if regexp.MustCompile(`Reflect\.(get|set|apply|construct|deleteProperty)\s*\(`).MatchString(s) {
		fail("patch: Reflect.* calls still remain after patching")
	}

	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		fail("patch: write: " + err.Error())
	}
	fmt.Printf("patch-wasm-exec: ok (%d replacements, no Reflect.* remaining)\n", applied)
}

// --- upload: POST the built dist/ modules to the Screeps code API.

type destConfig struct {
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func upload(root, dest string) {
	raw, err := os.ReadFile(root + "/screeps.json")
	if err != nil {
		fail("upload: read screeps.json (copy screeps.sample.json first): " + err.Error())
	}
	var all map[string]destConfig
	if err := json.Unmarshal(raw, &all); err != nil {
		fail("upload: parse screeps.json: " + err.Error())
	}
	cfg, ok := all[dest]
	if !ok {
		fail("upload: destination not found in screeps.json: " + dest)
	}

	proto := cfg.Protocol
	if proto == "" {
		proto = "https"
	}
	port := cfg.Port
	if port == 0 {
		if proto == "https" {
			port = 443
		} else {
			port = 21025
		}
	}
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	base := fmt.Sprintf("%s://%s:%d%s", proto, cfg.Hostname, port, path)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	client := &http.Client{}

	// Auth: a token is used directly; otherwise sign in with email/password.
	token := cfg.Token
	if token == "" && cfg.Password != "" {
		user := cfg.Email
		if user == "" {
			user = cfg.Username
		}
		body, _ := json.Marshal(map[string]string{"email": user, "password": cfg.Password})
		resp, err := client.Post(base+"api/auth/signin", "application/json", bytes.NewReader(body))
		if err != nil {
			fail("upload: signin: " + err.Error())
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var sr struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(rb, &sr); err != nil {
			fail("upload: signin: bad response: " + err.Error() + ": " + string(rb))
		}
		if sr.Token == "" {
			fail("upload: signin failed: " + string(rb))
		}
		token = sr.Token
	}
	if token == "" {
		fail("upload: no token or email/password in screeps.json for destination " + dest)
	}

	// Gather modules: JS as strings, the wasm as a base64 "binary" module.
	modules := map[string]any{}
	for name, file := range map[string]string{
		"main":      "dist/main.js",
		"host":      "dist/host.js",
		"host_gen":  "dist/host_gen.js",
		"wasm_exec": "dist/wasm_exec.js",
	} {
		b, err := os.ReadFile(root + "/" + file)
		if err != nil {
			fail("upload: read " + file + ": " + err.Error())
		}
		modules[name] = string(b)
	}
	wasm, err := os.ReadFile(root + "/dist/main.wasm")
	if err != nil {
		fail("upload: read dist/main.wasm: " + err.Error())
	}
	modules["main.wasm"] = map[string]string{"binary": base64.StdEncoding.EncodeToString(wasm)}

	branch := cfg.Branch
	if branch == "" {
		branch = "default"
	}
	payload, _ := json.Marshal(map[string]any{"branch": branch, "modules": modules})

	req, _ := http.NewRequest(http.MethodPost, base+"api/user/code", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		fail("upload: POST: " + err.Error())
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Sprintf("upload: server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(rb))))
	}
	fmt.Printf("uploaded to %q (branch %q): %s\n", dest, branch, strings.TrimSpace(string(rb)))
}
