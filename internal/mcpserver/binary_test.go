package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vaults"
)

// fakeBinary points the server's executable at a file the test can replace,
// as an install of a newer binary does while a server runs.
func fakeBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(tk.Dir(t), "severino-vault-mcp")
	tk.Write(t, bin, "old build")
	was := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = was })
	return bin
}

// installOver replaces bin the way an install does: a new file renamed over it.
func installOver(t *testing.T, bin, content string) {
	t.Helper()
	next := bin + ".new"
	tk.Write(t, next, content)
	if err := os.Chmod(next, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, bin); err != nil {
		t.Fatal(err)
	}
}

// readyScript is a replacement that answers the handoff probe.
const readyScript = "#!/bin/sh\necho '" + handoffReady + "'\n"

// stubExec records the exec a handoff makes and fails it, so the session
// stays in the test's process.
func stubExec(t *testing.T) *[]handoff {
	t.Helper()
	var execs []handoff
	was := execve
	execve = func(_ string, _ []string, env []string) error {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, handoffEnv+"="); ok {
				var h handoff
				if err := json.Unmarshal([]byte(v), &h); err != nil {
					t.Error(err)
				}
				execs = append(execs, h)
			}
		}
		return errors.New("stubbed exec")
	}
	t.Cleanup(func() { execve = was })
	return &execs
}

// serveFront runs the server behind a front over pipes, as Serve does over
// stdio, and connects a client to it.
func serveFront(t *testing.T, reg *vaults.Registry, opts *mcp.ClientSessionOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	toClient, serverOut := io.Pipe()
	serverIn, fromClient := io.Pipe()
	bin := watchBinary()
	f := newFront(serverIn, serverOut, bin, io.Discard)
	f.takeOver = func(h handoff) error { return bin.takeOver(h, reg.Close) }
	ss, err := newServer(reg, io.Discard, bin).Connect(ctx, f.transport(), nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &mcp.IOTransport{Reader: toClient, Writer: fromClient}, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		session.Close()
		fromClient.Close()
		ss.Close()
	})
	return session
}

// A replacement that can't take the session over (here not a program at all)
// leaves every tool answering from the running code, writes included. The
// first answer after the install says so once, with the fix; the rest answer
// as before.
func TestAReplacementThatCannotTakeOverLeavesTheSessionServing(t *testing.T) {
	reg, root := labsOnly(t)
	bin := fakeBinary(t)
	s := serveFront(t, reg, nil)
	add := func(text string) *jsonx.Obj {
		r, _ := call(t, s, "queue", map[string]any{"action": "add", "project": "backlog", "kind": "next", "text": text})
		return r
	}
	if r := add("Before the install"); !r.Bool("ok") || r.Has("warning") {
		t.Fatalf("before the install: %s", jsonx.Compact(r))
	}
	installOver(t, bin, "not a program")
	if r := add("After the install"); !r.Bool("ok") || !strings.Contains(r.Str("warning"), "could not take over") ||
		!strings.Contains(r.Str("warning"), "/mcp, then Reconnect") {
		t.Fatalf("after the install: %s", jsonx.Compact(r))
	}
	if r := add("Once more"); !r.Bool("ok") || r.Has("warning") {
		t.Fatalf("a second write after the install: %s", jsonx.Compact(r))
	}
	if q := tk.Read(t, filepath.Join(root, "07 Backlog", "Queue.md")); !strings.Contains(q, "Once more") {
		t.Fatal(q)
	}
	if r, isErr := call(t, s, "find", map[string]any{"query": "backup"}); isErr || r.Has("warning") {
		t.Fatalf("a read after the install: %s", jsonx.Compact(r))
	}
}

// initializeVersion makes a client open with the initialize handshake, as
// Claude Code does, instead of the per-request protocol.
const initializeVersion = "2025-11-25"

// A replacement that answers the probe gets the session as the SDK
// negotiated it, at the next message, and nothing before the install.
func TestTheHandoffCarriesTheInitializedSession(t *testing.T) {
	reg, _ := labsOnly(t)
	bin := fakeBinary(t)
	execs := stubExec(t)
	s := serveFront(t, reg, &mcp.ClientSessionOptions{ProtocolVersion: initializeVersion})
	if r, isErr := call(t, s, "find", map[string]any{"query": "backup"}); isErr || len(*execs) != 0 {
		t.Fatalf("before the install: %s, %d execs", jsonx.Compact(r), len(*execs))
	}
	installOver(t, bin, readyScript)
	call(t, s, "find", map[string]any{"query": "backup"})
	if len(*execs) != 1 {
		t.Fatalf("%d execs", len(*execs))
	}
	h := (*execs)[0]
	st := h.State
	if st.InitializeParams == nil || st.InitializeParams.ClientInfo.Name != "test" || st.InitializedParams == nil ||
		st.NegotiatedProtocolVersion != s.InitializeResult().ProtocolVersion || h.Next != "{" {
		t.Fatalf("handed over %+v", h)
	}
}

// The handoff waits until every call either way has its answer, but for the
// listen streams the client holds open, which it carries as they were sent.
func TestTheHandoffWaitsForEveryAnswer(t *testing.T) {
	bin := fakeBinary(t)
	w := watchBinary()
	installOver(t, bin, readyScript)
	tries := 0
	var last handoff
	f := newFront(nil, io.Discard, w, io.Discard)
	f.takeOver = func(h handoff) error { tries++; last = h; return errors.New("stay") }
	in := func(line string) { f.inbound([]byte(line + "\n")) }
	out := func(line string) {
		if _, err := f.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	try := func(want int, when string) {
		t.Helper()
		f.maybeHandOver('{')
		if tries != want {
			t.Fatalf("%s: %d tries, want %d", when, tries, want)
		}
		w.refused = nil
	}
	in(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`)
	try(0, "initialize unanswered")
	out(`{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}}`)
	try(0, "before initialized")
	in(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	try(1, "initialized and idle")
	in(`{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{}}`)
	try(1, "a call in flight")
	out(`{"jsonrpc":"2.0","method":"notifications/message","params":{}}`)
	try(1, "a notification is no answer")
	out(`{"jsonrpc":"2.0","id":"a","result":{}}`)
	try(2, "the call answered")
	out(`{"jsonrpc":"2.0","id":1,"method":"roots/list"}`)
	try(2, "a call to the client in flight")
	in(`{"jsonrpc":"2.0","id":1,"result":{"roots":[]}}`)
	try(3, "the client answered")
	in(`{"jsonrpc":"2.0", truncated`)
	try(3, "a line the front can't follow")

	f = newFront(nil, io.Discard, w, io.Discard)
	f.takeOver = func(h handoff) error { tries++; last = h; return errors.New("stay") }
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
	in(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + meta + `}}`)
	out(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	try(4, "a per-request client between calls")
	listen := `{"jsonrpc":"2.0","id":2,"method":"subscriptions/listen","params":{` + meta + `,"notifications":{"toolsListChanged":true}}}`
	in(listen)
	try(5, "a listen stream open")
	if len(last.Listens) != 1 || string(last.Listens[0]) != listen {
		t.Fatalf("carried listens %q", last.Listens)
	}
	in(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{` + meta + `}}`)
	try(5, "a call in flight beside the listen stream")
	out(`{"jsonrpc":"2.0","id":3,"result":{}}`)
	out(`{"jsonrpc":"2.0","id":2,"error":{"code":-32800,"message":"cancelled"}}`)
	try(6, "the listen stream ended")
	if len(last.Listens) != 0 {
		t.Fatalf("an ended listen stream carried: %q", last.Listens)
	}
}

// The process that takes a session over answers the client's open listen
// stream under its id without a second acknowledgement, says on that stream
// that the lists may have changed, and ends the stream when the client
// cancels it, so a later handoff doesn't carry it.
func TestAResumedSessionKeepsTheListenStream(t *testing.T) {
	reg, _ := labsOnly(t)
	serverIn, toServer := io.Pipe()
	fromServer, serverOut := io.Pipe()
	f := newFront(serverIn, serverOut, nil, io.Discard)
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"c","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
	f.resume(handoff{
		Listens: []json.RawMessage{json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":"subscriptions/listen","params":{` + meta + `,"notifications":{"toolsListChanged":true,"resourcesListChanged":true}}}`)},
		Next:    "{",
	})
	ss, err := newServer(reg, io.Discard, nil).Connect(context.Background(), f.transport(), &mcp.ServerSessionOptions{State: &f.state})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { toServer.Close(); ss.Close() })
	lines := make(chan string, 16)
	go func() {
		buf := make([]byte, 0, 1<<20)
		for {
			var one [1 << 16]byte
			n, err := fromServer.Read(one[:])
			buf = append(buf, one[:n]...)
			for {
				i := bytes.IndexByte(buf, '\n')
				if i < 0 {
					break
				}
				lines <- string(buf[:i])
				buf = buf[i+1:]
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	next := func() map[string]any {
		t.Helper()
		select {
		case l := <-lines:
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				t.Fatalf("%v: %s", err, l)
			}
			return m
		case <-time.After(10 * time.Second):
			t.Fatal("no message from the server")
		}
		return nil
	}
	send := func(s string) {
		if _, err := io.WriteString(toServer, s+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	// The rest of the message whose first byte the old process read.
	send(`"jsonrpc":"2.0","id":8,"method":"tools/list","params":{` + meta + `}}`)
	var changed []string
	for range 3 {
		m := next()
		switch {
		case m["method"] == "notifications/subscriptions/acknowledged":
			t.Fatalf("a second acknowledgement: %v", m)
		case m["id"] == float64(8):
			if m["result"] == nil {
				t.Fatalf("tools/list: %v", m)
			}
		default:
			params, _ := m["params"].(map[string]any)
			sub, _ := params["_meta"].(map[string]any)
			if sub[mcp.MetaKeySubscriptionID] != float64(7) {
				t.Fatalf("a notification off the stream: %v", m)
			}
			changed = append(changed, m["method"].(string))
		}
	}
	slices.Sort(changed)
	if !slices.Equal(changed, []string{"notifications/resources/list_changed", "notifications/tools/list_changed"}) {
		t.Fatal(changed)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`)
	if m := next(); m["id"] != float64(7) {
		t.Fatalf("the cancelled stream's answer: %v", m)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.listens) != 0 || len(f.inflight) != 0 {
		t.Fatalf("still open: %v %v", f.listens, f.inflight)
	}
}

// A binary rewritten in place keeps its inode; its size or mtime still tells.
func TestABinaryRewrittenInPlaceCountsAsReplaced(t *testing.T) {
	bin := fakeBinary(t)
	w := watchBinary()
	if _, replaced := w.candidate(); w == nil || replaced {
		t.Fatal("the binary as started reads as replaced")
	}
	tk.Write(t, bin, "a rebuilt binary of another size")
	if _, replaced := w.candidate(); !replaced {
		t.Fatal("a rewrite in place reads as the same binary")
	}
}

// lockedBuffer is a bytes.Buffer a process and the test share.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// build compiles the server with label as its version.
func build(t *testing.T, dir, label string) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, label)
	cmd := exec.Command(gobin, "build", "-o", out,
		"-ldflags=-X github.com/joeseverino/severino-vault-mcp/v3/internal/mcpserver.version="+label,
		"github.com/joeseverino/severino-vault-mcp/v3/cmd/severino-vault-mcp")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return out
}

func envList(env config.Env) []string {
	var out []string
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out
}

// A real client keeps its stdio session across installs, opened either way:
// with initialize, or as Claude Code does, with server/discover and a
// subscriptions/listen stream it holds open for the tool list. The call after
// each new binary lands is answered by that binary in the same process, and
// the client is told to list the tools again, on its listen stream when it
// has one.
func TestAnInstalledBuildTakesOverARunningSession(t *testing.T) {
	dir := tk.Dir(t)
	old, next := build(t, dir, "e2e-old"), build(t, dir, "e2e-new")
	for name, version := range map[string]string{"initialize": initializeVersion, "per-request": ""} {
		t.Run(name, func(t *testing.T) {
			bin := filepath.Join(tk.Dir(t), "severino-vault-mcp")
			installOver(t, bin, tk.Read(t, old))
			root := tk.FakeVault(t)
			tk.MultisectionDoc(t, root)

			cmd := exec.Command(bin)
			cmd.Env = envList(tk.Env(root))
			stderr := &lockedBuffer{}
			cmd.Stderr = stderr
			relisted := make(chan struct{}, 1)
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ClientOptions{
				ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
					select {
					case relisted <- struct{}{}:
					default:
					}
				},
			})
			s, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: cmd},
				&mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			add := func(text string) {
				t.Helper()
				r, isErr := call(t, s, "queue", map[string]any{"action": "add", "project": "backlog", "kind": "next", "text": text})
				if isErr || !r.Bool("ok") || r.Has("warning") {
					t.Fatalf("%s: %s\n%s", text, jsonx.Compact(r), stderr)
				}
			}
			add("Before the install")
			if strings.Contains(stderr.String(), "took over") {
				t.Fatalf("a takeover before the install:\n%s", stderr)
			}

			for i, build := range []string{next, old} {
				label := filepath.Base(build)
				installOver(t, bin, tk.Read(t, build))
				add(fmt.Sprintf("After install %d", i+1))
				if !strings.Contains(stderr.String(), "build "+label+" took over the session") {
					t.Fatalf("%s did not take over:\n%s", label, stderr)
				}
				select {
				case <-relisted:
				case <-time.After(10 * time.Second):
					t.Fatalf("after %s the client was not told to list the tools again", label)
				}
				if got := names(tools(t, s)); !slices.Equal(got, coreTools) {
					t.Fatal(got)
				}
				if r, _ := call(t, s, "read_doc", map[string]any{"doc_id": "rb-backup-ops"}); r.Str("doc_id") != "rb-backup-ops" {
					t.Fatalf("read_doc after %s took over: %s", label, jsonx.Compact(r))
				}
			}
			if strings.Contains(stderr.String(), "could not take over") {
				t.Fatalf("a refused takeover:\n%s", stderr)
			}
			q := tk.Read(t, filepath.Join(root, "07 Backlog", "Queue.md"))
			for _, text := range []string{"Before the install", "After install 1", "After install 2"} {
				if !strings.Contains(q, text) {
					t.Fatal(q)
				}
			}
		})
	}
}
