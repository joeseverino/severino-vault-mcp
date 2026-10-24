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
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A server outlives an install of a newer binary. Between two messages, with
// every call answered, the process execs the new binary in place: same pid,
// same stdin and stdout, so the client keeps its connection. The new process
// restores the session through the SDK's ServerSessionOptions.State, so no
// second initialize is needed.
//
// handoffEnv carries that session: the initialized state and the first byte
// of the next message, which the old process had to read to learn a message
// was waiting. Set to handoffProbe, it asks a binary whether it can take a
// session over at all, so an install that can't (a build without this code,
// a broken file) never gets the session.
const (
	handoffEnv   = "SVMC_SESSION_HANDOFF"
	handoffProbe = "probe"
	handoffReady = "severino-vault-mcp session handoff 1"
	probeTimeout = 10 * time.Second
)

// handoff is what the old process hands the new one.
type handoff struct {
	State mcp.ServerSessionState `json:"state"`
	Next  string                 `json:"next"`
}

// executable is the running binary's resolved path; tests point it at a file
// they replace.
var executable = func() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// execve replaces the process; tests stub it.
var execve = syscall.Exec

// binaryWatch holds the identity of the binary this process started from,
// and the replacement it last failed to hand the session to.
type binaryWatch struct {
	path string
	info os.FileInfo

	mu      sync.Mutex
	refused os.FileInfo
	reason  string
}

// watchBinary records the running binary; nil when it cannot be found, which
// leaves the process serving the code it started with.
func watchBinary() *binaryWatch {
	p, err := executable()
	if err != nil {
		return nil
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil
	}
	return &binaryWatch{path: p, info: info}
}

// sameBinary reports whether two stats are one file as it was: an install
// renames a new file over the path (another inode), a rewrite in place keeps
// the inode but not its size or mtime.
func sameBinary(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// candidate is the replacement binary to hand the session to, unless there is
// none or it already refused.
func (w *binaryWatch) candidate() (os.FileInfo, bool) {
	if w == nil {
		return nil, false
	}
	now, err := os.Stat(w.path)
	if err != nil || sameBinary(w.info, now) {
		return nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return now, w.refused == nil || !sameBinary(w.refused, now)
}

func (w *binaryWatch) refuse(info os.FileInfo, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refused, w.reason = info, err.Error()
}

// warning says, once a replacement could not take the session over, that the
// answers still come from the code this process started with.
func (w *binaryWatch) warning() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refused == nil {
		return ""
	}
	return "the server binary " + w.path + " was replaced, but the new one could not take over this session (" +
		w.reason + "), so this process keeps serving the code it started with; restart the session to run the new binary"
}

// takeOver probes the replacement, closes what the process holds open, and
// execs it with the session. It returns only when the session stays here.
func (w *binaryWatch) takeOver(h handoff, release func()) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	probe := exec.CommandContext(ctx, w.path, os.Args[1:]...) //nolint:gosec // the binary this process was started as
	probe.Env = withHandoff(os.Environ(), handoffProbe)
	out, err := probe.Output()
	if err != nil {
		return fmt.Errorf("its handoff probe failed: %w", err)
	}
	if strings.TrimSpace(string(out)) != handoffReady {
		return errors.New("it does not speak this session handoff")
	}
	release()
	if err := execve(w.path, os.Args, withHandoff(os.Environ(), string(data))); err != nil {
		return fmt.Errorf("exec after closing the providers, so provider tools are down too: %w", err)
	}
	return nil
}

// withHandoff is env with handoffEnv set to v.
func withHandoff(env []string, v string) []string {
	env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, handoffEnv+"=") })
	return append(env, handoffEnv+"="+v)
}

// front sits between the SDK and stdio. It reads stdin one message line at a
// time, so at the start of a line it holds no unread bytes, and it follows
// the wire both ways: the calls still unanswered, and the session state the
// SDK negotiated, which it can hand over. A message shape it can't follow
// stops handoffs for the rest of the process.
type front struct {
	in  io.Reader
	out io.Writer
	bin *binaryWatch
	log io.Writer
	// takeOver is binaryWatch.takeOver with what to release before the exec.
	takeOver func(handoff) error

	pending []byte // read from in, not yet given to the SDK
	line    []byte // the inbound line given so far

	mu     sync.Mutex
	state  mcp.ServerSessionState
	initID string
	// sessionless is a client on the per-request protocol (SEP-2575): each
	// call carries what initialize would have, so there is no state to hand
	// over.
	sessionless bool
	inflight    map[string]bool // calls from the client not yet answered
	listens     map[string]bool // the subscriptions/listen calls among them, parked until the client leaves
	awaiting    map[string]bool // calls to the client not yet answered
	lost        bool
	outLine     []byte
}

func newFront(in io.Reader, out io.Writer, bin *binaryWatch, log io.Writer) *front {
	return &front{in: in, out: out, bin: bin, log: log, inflight: map[string]bool{}, listens: map[string]bool{}, awaiting: map[string]bool{}}
}

// resume takes the session a replaced binary handed over.
func (f *front) resume(h handoff) {
	f.state = h.State
	f.pending = []byte(h.Next)
}

// transport is the SDK's view of the front.
func (f *front) transport() mcp.Transport {
	return &mcp.IOTransport{Reader: io.NopCloser(f), Writer: nopWriteCloser{f}}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// Read gives the SDK the next bytes of the current line. At a line's start it
// reads one byte, which says a message is waiting; when the binary has been
// replaced and nothing is in flight, that is the moment to hand over.
func (f *front) Read(p []byte) (int, error) {
	if len(f.pending) == 0 {
		if len(f.line) == 0 {
			var one [1]byte
			n, err := f.in.Read(one[:])
			if n == 0 {
				return 0, err
			}
			if !isSpace(one[0]) {
				f.maybeHandOver(one[0])
			}
			f.pending = one[:]
		} else {
			buf := make([]byte, 64<<10)
			n, err := f.in.Read(buf)
			if n == 0 {
				return 0, err
			}
			f.pending = buf[:n]
		}
	}
	end := len(f.pending)
	if i := bytes.IndexByte(f.pending, '\n'); i >= 0 {
		end = i + 1
	}
	n := copy(p, f.pending[:end])
	f.line = append(f.line, f.pending[:n]...)
	f.pending = f.pending[n:]
	if f.line[len(f.line)-1] == '\n' {
		f.inbound(f.line)
		f.line = f.line[:0]
	}
	return n, nil
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

// Write passes the SDK's output on, following each complete line.
func (f *front) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := f.out.Write(p)
	f.outLine = append(f.outLine, p[:n]...)
	for {
		i := bytes.IndexByte(f.outLine, '\n')
		if i < 0 {
			break
		}
		f.outbound(f.outLine[:i])
		f.outLine = f.outLine[i+1:]
	}
	return n, err
}

// maybeHandOver execs the replacement binary when the session is between
// messages and fully initialized. It returns only when the session stays.
func (f *front) maybeHandOver(next byte) {
	f.mu.Lock()
	st := f.state
	initialized := st.InitializeParams != nil && st.InitializedParams != nil && st.NegotiatedProtocolVersion != ""
	open := !f.lost && (initialized || f.sessionless && st.InitializeParams == nil)
	listening := len(f.inflight) > 0 && len(f.inflight) == len(f.listens)
	idle := open && len(f.inflight) == 0 && len(f.awaiting) == 0
	h := handoff{State: f.state, Next: string(next)}
	f.mu.Unlock()
	if !idle && (!open || !listening) {
		return
	}
	info, ok := f.bin.candidate()
	if !ok {
		return
	}
	err := errors.New("the client holds a subscriptions/listen stream open, which a new process can't resume")
	if idle {
		err = f.takeOver(h)
	}
	fmt.Fprintf(f.log, "severino-vault-mcp: %s was replaced, but the new binary could not take over the session: %v\n", f.bin.path, err)
	f.bin.refuse(info, err)
}

// wireMessage is the part of a JSON-RPC message the front follows.
type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

// perRequest reports whether a call names its protocol version in its own
// _meta, as the per-request protocol does.
func (m wireMessage) perRequest() bool {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	_ = json.Unmarshal(m.Params, &p)
	_, ok := p.Meta[mcp.MetaKeyProtocolVersion]
	return ok
}

// messages parses one line as a message or a batch; ok is false for a line
// the front can't follow.
func messages(line []byte) ([]wireMessage, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, true
	}
	var batch []wireMessage
	if line[0] == '[' {
		return batch, json.Unmarshal(line, &batch) == nil
	}
	var m wireMessage
	if json.Unmarshal(line, &m) != nil {
		return nil, false
	}
	return []wireMessage{m}, true
}

// idKey is a request id as one comparable string, the same for the id the
// client sent and the one the SDK echoes; "" when there is none.
func idKey(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if len(raw) == 0 || dec.Decode(&v) != nil {
		return ""
	}
	switch v := v.(type) {
	case string:
		return "s" + v
	case json.Number:
		return "n" + v.String()
	}
	return ""
}

// inbound follows a line from the client.
func (f *front) inbound(line []byte) {
	msgs, ok := messages(line)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !ok {
		f.lost = true
		return
	}
	for _, m := range msgs {
		id := idKey(m.ID)
		switch {
		case m.Method != "" && id != "":
			f.inflight[id] = true
			f.sessionless = f.sessionless || m.perRequest()
			switch m.Method {
			case "initialize":
				var params mcp.InitializeParams
				if json.Unmarshal(m.Params, &params) != nil {
					f.lost = true
				}
				f.state.InitializeParams, f.initID = &params, id
			case "subscriptions/listen":
				f.listens[id] = true
			}
		case m.Method == "notifications/initialized":
			params := &mcp.InitializedParams{}
			if len(m.Params) > 0 && json.Unmarshal(m.Params, params) != nil {
				f.lost = true
			}
			f.state.InitializedParams = params
		case m.Method == "" && id != "":
			delete(f.awaiting, id)
		}
	}
}

// outbound follows a line the server wrote; the caller holds f.mu.
func (f *front) outbound(line []byte) {
	msgs, ok := messages(line)
	if !ok {
		f.lost = true
		return
	}
	for _, m := range msgs {
		id := idKey(m.ID)
		switch {
		case m.Method == "" && id != "":
			delete(f.inflight, id)
			delete(f.listens, id)
			if id == f.initID {
				var r mcp.InitializeResult
				if json.Unmarshal(m.Result, &r) == nil {
					f.state.NegotiatedProtocolVersion = r.ProtocolVersion
				}
				f.initID = ""
			}
		case m.Method != "" && id != "":
			f.awaiting[id] = true
		}
	}
}

// toolsChanged tells an initialized client to list the tools again, which
// the new binary may have changed. A per-request client hears of changes only
// on a listen stream, which keeps a call in flight and so never hands over.
func (f *front) toolsChanged() error {
	if f.state.InitializedParams == nil {
		return nil
	}
	_, err := f.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}` + "\n"))
	return err
}
