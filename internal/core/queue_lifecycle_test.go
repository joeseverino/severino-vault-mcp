package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// An agent holds any number of items; a relaunch that files the same goal
// again gets the item it already holds.
func TestAnAgentHoldsManyItemsAndARelaunchFilesNoSecond(t *testing.T) {
	_, v := queueVault(t)
	agent := func(a *core.QueueArgs) { a.Agent, a.Owner = "a1", new("worker") }
	first := add(t, v, "Example Tracker", "delegated", "Build the store", agent)
	again := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "delegated", Text: new("Build the store"), Agent: "a1"}))
	if again.Str("id") != first || !again.Bool("no_op") {
		t.Fatal(jsonx.Compact(again))
	}
	second := add(t, v, "Example Tracker", "delegated", "Build the index", agent)
	queued := add(t, v, "Example Tracker", "next", "Queued")
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: queued, Agent: "a1", Kind: "delegated"}))
	found := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Agent: "a1", Full: true}))
	if want := []string{first, second, queued}; !slices.Equal(ids(found), want) {
		t.Fatal(jsonx.Compact(found))
	}
	r := core.Queue(v, core.QueueArgs{Action: "close", Agent: "a1", Resolution: "x"})
	for _, id := range []string{first, second, queued} {
		if r.Bool("ok") || !strings.Contains(r.Str("error"), id) {
			t.Fatal(jsonx.Compact(r))
		}
	}
}

// A claim cannot take an item another agent holds, nor a done one, and a
// claim leaves the item open.
func TestAClaimNeverOrphansAnotherWorker(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Contested")
	claim := func(agent string) *jsonx.Obj {
		return core.Queue(v, core.QueueArgs{Action: "set", ID: id, Claim: true, Owner: new("worker"), Agent: agent, Kind: "delegated"})
	}
	mustOK(t, claim("a1"))
	r := claim("a2")
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "a1") || r.Str("agent") != "a1" || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
	if got := tk.Get(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", ID: "", Agent: "a1", Full: true})), "items.0.id"); got != id {
		t.Fatal(got)
	}
	// The holder's own claim of a returned item reopens it.
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Status: "returned"}))
	if r := mustOK(t, claim("a1")); tk.Get(r, "item.status") != "open" {
		t.Fatal(jsonx.Compact(r))
	}
	done := add(t, v, "Example Tracker", "next", "Finished")
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: done, Status: "dropped", Resolution: "not needed"}))
	r = core.Queue(v, core.QueueArgs{Action: "set", ID: done, Claim: true, Owner: new("worker"), Agent: "a3"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "dropped") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
}

// release hands a held item back to the queue as unowned next work.
func TestAReleasedItemGoesBackToTheQueue(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Hand back")
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Claim: true, Owner: new("worker"), Agent: "a1", Kind: "delegated"}))
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Status: "returned"}))
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: id, Agent: "a2", Release: true}); r.Bool("ok") || !strings.Contains(r.Str("error"), "a1") {
		t.Fatal("another agent's release", jsonx.Compact(r))
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Agent: "a1", Release: true}))
	if tk.Get(r, "item.kind") != "next" || tk.Get(r, "item.status") != "open" || tk.Has(r, "item.agent") || tk.Get(r, "item.owner") != nil || r.Has("warning") {
		t.Fatal(jsonx.Compact(r))
	}
	if ready := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Ready: true})); !slices.Contains(ids(ready), id) {
		t.Fatal(jsonx.Compact(ready))
	}
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: id, Release: true, Claim: true, Owner: new("x")}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
}

// Changing or closing an item a worker holds, without naming that worker,
// goes through and says who holds it.
func TestTouchingAHeldItemWarnsWhoHoldsIt(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "delegated", "Held", func(a *core.QueueArgs) { a.Agent = "a1" })
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Text: new("Held, retitled")}))
	if !strings.Contains(r.Str("warning"), "a1") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Agent: "a1", Text: new("By its holder")})); r.Has("warning") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Agent: "a2"})); !strings.Contains(r.Str("warning"), "a1") {
		t.Fatal("retargeting says who held it", jsonx.Compact(r))
	}
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: id, Resolution: "example-tracker@abc1234"})); !strings.Contains(r.Str("warning"), "a2") {
		t.Fatal(jsonx.Compact(r))
	}
	free := add(t, v, "Example Tracker", "next", "Nobody holds it")
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: free, Resolution: "done"})); r.Has("warning") {
		t.Fatal(jsonx.Compact(r))
	}
}

// Every new item carries the date it was filed; a worker's item carries the
// branch its work lands on and the paths it owns, and list returns them.
func TestAnItemCarriesItsTranscriptPath(t *testing.T) {
	root, v := queueVault(t)
	path := "/Users/example/.claude/projects/p/agent-a1.jsonl"
	id := add(t, v, "Example Tracker", "delegated", "Build it", func(a *core.QueueArgs) { a.Agent, a.Transcript = "a1", new(path) })
	if note := tk.Read(t, filepath.Join(root, trackerQueue)); !strings.Contains(note, "[transcript:: "+path+"]") {
		t.Fatal(note)
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Agent: "a1", Fields: []string{"id", "transcript"}}))
	if got := jsonx.Compact(tk.Get(r, "items")); got != `[{"id":"`+id+`","transcript":"`+path+`"}]` {
		t.Fatal(got)
	}
	r = mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Transcript: new("")}))
	if tk.Has(r, "item.transcript") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: id, Transcript: new("relative.jsonl")}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestAnItemCarriesItsCreatedDateBaseAndOwns(t *testing.T) {
	root, v := queueVault(t)
	id := add(t, v, "Example Tracker", "delegated", "Build it", func(a *core.QueueArgs) {
		a.Agent, a.Base, a.Owns = "a1", new("feat/store"), []string{"internal/queue,docs/Work Queue.md", "internal/core"}
	})
	if note := tk.Read(t, filepath.Join(root, trackerQueue)); !strings.Contains(note,
		"[base:: feat/store]  [owns:: internal/queue,docs/Work Queue.md,internal/core]  [id:: "+id+"]"+filed+"\n") {
		t.Fatal(note)
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Agent: "a1", Fields: []string{"id", "created", "base", "owns"}}))
	if got := jsonx.Compact(tk.Get(r, "items")); got != `[{"id":"`+id+`","base":"feat/store","owns":["internal/queue","docs/Work Queue.md","internal/core"],"created":"2026-10-22"}]` {
		t.Fatal(got)
	}
	r = mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Base: new(""), Owns: []string{"internal/cli"}, HasOwns: true}))
	if tk.Has(r, "item.base") || jsonx.Compact(tk.Get(r, "item.owns")) != `["internal/cli"]` || tk.Get(r, "item.created") != "2026-10-22" {
		t.Fatal(jsonx.Compact(r))
	}
	for _, a := range []core.QueueArgs{
		{Action: "set", ID: id, Owns: []string{"a]b"}, HasOwns: true},
		{Action: "set", ID: id, Base: new("two words")},
	} {
		if r := core.Queue(v, a); r.Bool("ok") || !r.Has("next_steps") {
			t.Errorf("%+v: %s", a, jsonx.Compact(r))
		}
	}
}

// A claim with kind delegated also takes a decision, when the orchestrator
// (main) owns it or nobody does; any other owner keeps it.
func TestAClaimTakesTheOrchestratorsDecision(t *testing.T) {
	root, v := queueVault(t)
	claim := func(id, agent string) *jsonx.Obj {
		return core.Queue(v, core.QueueArgs{Action: "set", ID: id, Claim: true, Owner: new("worker"), Agent: agent, Kind: "delegated"})
	}
	owned := add(t, v, "Example Tracker", "decision", "Pick the store", func(a *core.QueueArgs) { a.Owner = new("main") })
	r := mustOK(t, claim(owned, "a1"))
	if tk.Get(r, "item.kind") != "delegated" || tk.Get(r, "item.owner") != "worker" || tk.Get(r, "item.agent") != "a1" ||
		tk.Get(r, "item.id") != owned || tk.Get(r, "item.text") != "Pick the store" || tk.Get(r, "item.status") != "open" {
		t.Fatal(jsonx.Compact(r))
	}
	unowned := add(t, v, "Example Tracker", "decision", "Pick the index")
	if r := mustOK(t, claim(unowned, "a2")); tk.Get(r, "item.kind") != "delegated" || tk.Get(r, "item.agent") != "a2" {
		t.Fatal(jsonx.Compact(r))
	}
	joes := add(t, v, "Example Tracker", "decision", "Joe decides", func(a *core.QueueArgs) { a.Owner = new("joe") })
	path := filepath.Join(root, trackerQueue)
	before := tk.Read(t, path)
	r = claim(joes, "a3")
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "joe") || r.Str("owner") != "joe" {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused claim changed the note")
	}
	// Without a claim a decision still does not become delegated.
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: joes, Kind: "delegated", Agent: "a3"}); r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", Agent: "a1", Resolution: "example-tracker@abc1234"}))
	if got := tk.Get(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Status: "closed", Full: true})), "items.0.id"); got != owned {
		t.Fatal(got)
	}
}

// A done item is not brought back by a status write or a claim.
func TestADoneItemIsNotReopenedByStatusOrClaim(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Finished")
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: id, Resolution: "example-tracker@abc1234"}))
	for _, a := range []core.QueueArgs{
		{Action: "set", ID: id, Status: "open"},
		{Action: "set", ID: id, Claim: true, Owner: new("worker"), Agent: "a1"},
	} {
		r := core.Queue(v, a)
		if r.Bool("ok") || !strings.Contains(r.Str("error"), "closed") || !r.Has("next_steps") {
			t.Fatal(jsonx.Compact(r))
		}
	}
}
