package core_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/query"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

func find(v *core.Vault, q string) *jsonx.Obj {
	return core.Find(v, q, core.FindArgs{By: "relevance", Limit: 10, ContextLines: 1})
}

func findBy(v *core.Vault, q, by string) *jsonx.Obj {
	return core.Find(v, q, core.FindArgs{By: by, Limit: 10, ContextLines: 1})
}

// ----- the two-vault fixture from test_core_tools ------------------------------------

type twoVaults struct{ labs, edu *core.Vault }

func newTwoVaults(t *testing.T) twoVaults {
	tmp := tk.Dir(t)
	labs := filepath.Join(tmp, "labs")
	tk.Doc(t, filepath.Join(labs, "03 Runbooks", "Add Proxy Host.md"), `
doc_id: rb-add-proxy-host
title: Add Proxy Host
doc_type: runbook
system: Nginx Proxy Manager
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-01-01
related_projects: [homelab-apps]
tags: [nginx]
`, "## Goal\n\nExpose a service through the proxy.\n")
	_ = os.MkdirAll(filepath.Join(labs, "01 Projects", "homelab-apps"), 0o755)
	tk.Write(t, filepath.Join(labs, "03 Runbooks", "Untagged.md"), "# Untagged\n")

	edu := filepath.Join(tmp, "edu")
	tk.Doc(t, filepath.Join(edu, "01 Projects", "CS6250", "index.md"), `
doc_id: course-cs6250
title: Computer Networks
doc_type: course
system: Georgia Tech
environment: gatech
status: active
sensitivity: internal
last_reviewed: 2026-01-01
`, "## Site\n\n- Routing and BGP.\n")
	tk.Doc(t, filepath.Join(edu, "03 Runbooks", "Exam Answers.md"), `
doc_id: res-exam-answers
title: Exam Answers
doc_type: resource
system: Georgia Tech
environment: gatech
status: active
sensitivity: restricted
last_reviewed: 2026-01-01
`, "## Answers\n\nsecret body\n")
	tk.Write(t, filepath.Join(edu, "03 Runbooks", "Lecture.md"), "# Lecture\n")

	mk := func(name, root string, p *schema.Profile) *core.Vault {
		env := tk.Env(root, "SVMC_RESTRICTED_UNLOCK_AUDIT_LOG", filepath.Join(tmp, name+"-audit.log"))
		return core.NewVault(name, config.Load("", env), p)
	}
	return twoVaults{mk("labs", labs, schema.Labs), mk("edu", edu, schema.Education)}
}

func TestFindRoutesToTheNamedVault(t *testing.T) {
	v := newTwoVaults(t)
	labs := find(v.labs, "proxy host")
	if labs.Str("vault") != "labs" || labs.Str("mode") != "relevance" || tk.Get(labs, "match_count") != 1 {
		t.Fatalf("labs find: %v", jsonx.Compact(labs))
	}
	if got := tk.IDs(tk.Hits(labs, "hits")); !slices.Equal(got, []string{"rb-add-proxy-host"}) {
		t.Fatal(got)
	}
	if len(tk.Hits(find(v.edu, "proxy host"), "hits")) != 0 {
		t.Fatal("edu should not see labs docs")
	}
	if got := tk.IDs(tk.Hits(find(v.edu, "networks"), "hits")); !slices.Equal(got, []string{"course-cs6250"}) {
		t.Fatal(got)
	}
}

func TestFindSystemAndProjectModes(t *testing.T) {
	v := newTwoVaults(t)
	bySystem := findBy(v.labs, "nginx", "system")
	if tk.Get(bySystem, "match_count") != 1 || tk.Get(bySystem, "hits.0.doc_id") != "rb-add-proxy-host" {
		t.Fatal(jsonx.Compact(bySystem))
	}
	if got := tk.IDs(tk.Hits(findBy(v.labs, "homelab-apps", "project"), "hits")); !slices.Equal(got, []string{"rb-add-proxy-host"}) {
		t.Fatal(got)
	}
	if tk.Get(findBy(v.edu, "homelab-apps", "project"), "match_count") != 0 {
		t.Fatal("edu project search crossed vaults")
	}
}

func requireRG(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
}

func TestFindTextNeverSearchesRestrictedBodies(t *testing.T) {
	requireRG(t)
	v := newTwoVaults(t)
	if tk.Get(findBy(v.edu, "secret body", "text"), "match_count") != 0 {
		t.Fatal("restricted body searched")
	}
	hits := findBy(v.edu, "routing and bgp", "text")
	if tk.Get(hits, "match_count") != 1 || tk.Get(hits, "hits.0.doc_id") != "course-cs6250" {
		t.Fatal(jsonx.Compact(hits))
	}
	if snips, _ := tk.Get(hits, "hits.0.snippets").([]*jsonx.Obj); len(snips) == 0 {
		t.Fatal("no snippets")
	}
}

func TestReadDocIsScopedToItsVault(t *testing.T) {
	v := newTwoVaults(t)
	found := core.ReadDoc(v.labs, "rb-add-proxy-host", "", false)
	if !found.Bool("found") || !found.Bool("body_released") || !strings.Contains(found.Str("body"), "Expose a service") {
		t.Fatal(jsonx.Compact(found))
	}
	if core.ReadDoc(v.edu, "rb-add-proxy-host", "", false).Bool("found") {
		t.Fatal("edu found a labs doc")
	}
}

func TestRestrictedBodyStaysGatedPerVault(t *testing.T) {
	v := newTwoVaults(t)
	withheld := core.ReadDoc(v.edu, "res-exam-answers", "", false)
	if withheld.Bool("body_released") || withheld.Has("body") || tk.Get(withheld, "unlock.result") != "not_requested" {
		t.Fatal(jsonx.Compact(withheld))
	}
	requested := core.ReadDoc(v.edu, "res-exam-answers", "", true)
	if requested.Bool("body_released") || requested.Has("body") || tk.Get(requested, "unlock.result") != "disabled" {
		t.Fatal(jsonx.Compact(requested))
	}
}

func TestResourcesRenderPerVault(t *testing.T) {
	v := newTwoVaults(t)
	if !strings.Contains(core.RenderDoc(v.edu, "course-cs6250"), "Routing and BGP") {
		t.Fatal("edu resource")
	}
	if !strings.Contains(core.RenderDoc(v.labs, "course-cs6250"), "Not Found") {
		t.Fatal("labs resource found an edu doc")
	}
	if strings.Contains(core.RenderDoc(v.edu, "res-exam-answers"), "secret body") {
		t.Fatal("restricted resource leaked")
	}
}

func TestSetFrontmatterValidatesAgainstEachVaultsProfile(t *testing.T) {
	v := newTwoVaults(t)
	fields := write.Set{DocID: new("course-cs6200"), Title: new("Lecture"), DocType: new("course"), System: new("Georgia Tech"), Environment: new("gatech")}
	rejected := core.SetFrontmatter(v.labs, "03 Runbooks/Untagged.md", fields)
	if rejected.Bool("ok") || !strings.Contains(rejected.Str("error"), "doc_type") {
		t.Fatal(jsonx.Compact(rejected))
	}
	created := core.SetFrontmatter(v.edu, "03 Runbooks/Lecture.md", fields)
	if !created.Bool("ok") {
		t.Fatal(jsonx.Compact(created))
	}
	if !core.ReadDoc(v.edu, "course-cs6200", "", false).Bool("found") {
		t.Fatal("created doc not found")
	}
}

func TestSetFrontmatterCreateNeedsIdentityFields(t *testing.T) {
	v := newTwoVaults(t)
	r := core.SetFrontmatter(v.labs, "03 Runbooks/Untagged.md", write.Set{Title: new("Untagged")})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "doc_id") || !strings.Contains(r.Str("error"), "doc_type") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestSetFrontmatterCreateMergesAddLists(t *testing.T) {
	v := newTwoVaults(t)
	r := core.SetFrontmatter(v.labs, "03 Runbooks/Untagged.md", write.Set{
		DocID: new("rb-untagged"), Title: new("Untagged"), DocType: new("runbook"), System: new("misc"),
		Tags: write.ListOp{Set: []string{"one"}, HasSet: true, Add: []string{"two"}},
	})
	if !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	hit := tk.Hits(findBy(v.labs, "untagged", "system"), "hits")[0]
	if tags, _ := hit.Get("tags"); !slices.Equal(tags.([]string), []string{"one", "two"}) {
		t.Fatal(tags)
	}
}

func TestSetFrontmatterUpdatesInPlaceAndKeepsDocID(t *testing.T) {
	v := newTwoVaults(t)
	renamed := core.SetFrontmatter(v.labs, "03 Runbooks/Add Proxy Host.md", write.Set{DocID: new("rb-other")})
	if renamed.Bool("ok") || !strings.Contains(renamed.Str("error"), "immutable") {
		t.Fatal(jsonx.Compact(renamed))
	}
	updated := core.SetFrontmatter(v.labs, "03 Runbooks/Add Proxy Host.md", write.Set{Tags: write.ListOp{Add: []string{"proxy"}}, Status: new("deprecated")})
	if !updated.Bool("ok") {
		t.Fatal(jsonx.Compact(updated))
	}
	if cf, _ := updated.Get("changed_fields"); !slices.Equal(cf.([]string), []string{"status", "tags"}) {
		t.Fatal(cf)
	}
}

func TestTaskWriteValidatesPerAction(t *testing.T) {
	v := newTwoVaults(t)
	cases := map[string]core.TaskWriteArgs{
		"requires title":          {Action: "add"},
		"requires doc_id, status": {Action: "status"},
		"requires note_path":      {Action: "promote", Title: "x"},
		"unknown action":          {Action: "archive"},
	}
	for want, a := range cases {
		a.Effort, a.Priority = "S", "med"
		if got := core.TaskWrite(v.labs, a).Str("error"); !strings.Contains(got, want) {
			t.Errorf("%s: %q", want, got)
		}
	}
}

func TestTasksStayInTheirVault(t *testing.T) {
	v := newTwoVaults(t)
	added := core.TaskWrite(v.edu, core.TaskWriteArgs{Action: "add", Title: "Finish the BGP lab", Effort: "S", Priority: "med"})
	if !added.Bool("ok") {
		t.Fatal(jsonx.Compact(added))
	}
	id := added.Str("doc_id")
	if got := tk.IDs(tk.Hits(core.TaskBoard(v.edu, tasks.Filter{}), "tasks")); !slices.Equal(got, []string{id}) {
		t.Fatal(got)
	}
	if len(tk.Hits(core.TaskBoard(v.labs, tasks.Filter{}), "tasks")) != 0 {
		t.Fatal("labs board saw an edu task")
	}
	if !core.TaskWrite(v.edu, core.TaskWriteArgs{Action: "status", DocID: id, Status: "done"}).Bool("ok") {
		t.Fatal("status")
	}
	if len(tk.Hits(core.TaskBoard(v.edu, tasks.Filter{}), "tasks")) != 0 {
		t.Fatal("done task still on the board")
	}
}

func TestTaskBoardListsTheProjectsTasksCanGoIn(t *testing.T) {
	v := newTwoVaults(t)
	var slugs []string
	for _, p := range tk.Hits(core.TaskBoard(v.labs, tasks.Filter{}), "projects") {
		slugs = append(slugs, p.Str("slug"))
	}
	if !slices.Equal(slugs, []string{"homelab-apps"}) {
		t.Fatal(slugs)
	}
}

// ----- the fake_vault cases from test_search ------------------------------------

func encodedUnlockHash(phrase string) string {
	encoded, err := gate.HashPhrase(phrase, gate.Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32})
	if err != nil {
		panic(err)
	}
	return encoded
}

func TestDailyProgressResolvesFridayFromAnchor(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	r := core.DailyProgress(v, "what progress did i make on friday?", "2026-06-20")
	if !r.Bool("found") || r.Str("resolved_date") != "2026-06-19" || r.Str("date_resolution") != "weekday" ||
		r.Str("doc_id") != "daily-20260619" || r.Str("obsidian_path") != "00 Inbox/Daily Note/2026-06-19.md" ||
		!strings.Contains(r.Str("body"), "Daily Note template") {
		t.Fatal(jsonx.Compact(r))
	}
	items, _ := r.Get("progress_items")
	if !slices.ContainsFunc(items.([]string), func(s string) bool { return strings.Contains(s, "archive command") }) {
		t.Fatal(items)
	}
}

func TestDailyProgressReportsMissingNote(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	r := core.DailyProgress(v, "what happened yesterday?", "2026-06-19")
	if r.Bool("found") || r.Str("resolved_date") != "2026-06-18" || r.Str("expected_path") != "00 Inbox/Daily Note/2026-06-18.md" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestDuplicateDocIDIsExcludedAndReportedAtRuntime(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Duplicate.md"), `
doc_id: rb-add-nginx-proxy-host
title: Duplicate Nginx Runbook
doc_type: runbook
system: Duplicate
environment: other
status: active
sensitivity: internal
`, "# Duplicate\n")
	v := tk.Vault(root)
	r := core.ReadDoc(v, "rb-add-nginx-proxy-host", "", false)
	paths, _ := r.Get("paths")
	sorted := slices.Sorted(slices.Values(paths.([]string)))
	if r.Bool("found") || !r.Bool("ambiguous") || !slices.Equal(sorted, []string{"03 Runbooks/Add Nginx Proxy Host.md", "03 Runbooks/Duplicate.md"}) {
		t.Fatal(jsonx.Compact(r))
	}
	for _, h := range tk.Hits(find(v, "nginx proxy"), "hits") {
		if h.Str("doc_id") == "rb-add-nginx-proxy-host" {
			t.Fatal("duplicate ranked")
		}
	}
	res := core.RenderDoc(v, "rb-add-nginx-proxy-host")
	if !strings.Contains(res, "# Duplicate Vault Doc ID") || !strings.Contains(res, "03 Runbooks/Duplicate.md") {
		t.Fatal(res)
	}
}

func TestFindRanksNginxQuery(t *testing.T) {
	r := find(tk.Vault(tk.FakeVault(t)), "nginx proxy")
	if tk.Get(r, "hits.0.doc_id") != "rb-add-nginx-proxy-host" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindMatchesBodyOnlyTerm(t *testing.T) {
	r := find(tk.Vault(tk.FakeVault(t)), "expose a service")
	if tk.Get(r, "hits.0.doc_id") != "rb-add-nginx-proxy-host" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindBodySignalDoesNotOutrankADirectTagHit(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "02 Infrastructure", "Mentions HTTPS.md"), `
doc_id: infra-mentions-https
title: Edge Notes
doc_type: architecture_note
system: Misc
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [notes]
`, "We terminate HTTPS at the edge. HTTPS HTTPS HTTPS everywhere, lots of HTTPS.\n")
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "HTTPS Runbook.md"), `
doc_id: rb-https-setup
title: HTTPS Setup
doc_type: runbook
system: TLS
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [https, tls]
`, "Steps.\n")
	if r := find(tk.Vault(root), "https"); tk.Get(r, "hits.0.doc_id") != "rb-https-setup" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindIgnoresPureStopwordQuery(t *testing.T) {
	if r := find(tk.Vault(tk.FakeVault(t)), "a the of and to"); len(tk.Hits(r, "hits")) != 0 {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindRanksNormalSSHAboveRecovery(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "SSH Into VPS.md"), `
doc_id: rb-ssh-into-vps
title: SSH Into VPS
doc_type: runbook
system: sl-cloud-edge-01
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-05-17
tags: [vps, ssh, access, cloud-edge]
`, "## Connect\n\n```bash\nssh edge\n```\n")
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Recover SSH Access.md"), `
doc_id: rb-recover-ssh-cloud-edge
title: Recover SSH Access
doc_type: recovery_procedure
system: sl-cloud-edge-01
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-05-16
tags: [vps]
`, "Use only when `ssh edge` is refused or times out.\n")
	if r := find(tk.Vault(root), "how do i ssh into the VPS"); tk.Get(r, "hits.0.doc_id") != "rb-ssh-into-vps" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindIncludesQuickIndexRecommendation(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "02 Infrastructure", "AdGuard Home Setup.md"), `
doc_id: infra-adguard-home
title: AdGuard Home Setup
doc_type: architecture_note
system: AdGuard Home
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-05-17
tags: [homelab, adguard, home]
`, "# AdGuard Home Setup\n\nAdGuard Home runs as a Docker container.\n")
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Quick Index.md"), `
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [index, mcp, navigation]
`, "# Example Operations Vault Quick Index\n\n| Intent | Command | Doc |\n|---|---|---|\n"+
		"| Check AdGuard Home container status | `ssh homelab-server 'cd /opt/apps/adguard && docker compose ps'` | [[AdGuard Home Setup]] |\n")
	r := find(tk.Vault(root), "how do i check the status of the adguard home container?")
	if tk.Get(r, "hits.0.doc_id") != "infra-adguard-home" || tk.Get(r, "recommended.source") != "vault://labs/quick-index" ||
		tk.Get(r, "recommended.target_doc_id") != "infra-adguard-home" || !strings.Contains(tk.Get(r, "recommended.command").(string), "docker compose ps") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindDoesNotRecommendConflictingQuickIndexDoc(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Quick Index.md"), `
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [index, mcp, navigation]
`, "# Example Operations Vault Quick Index\n\n| Intent | Command | Doc |\n|---|---|---|\n"+
		"| Run Vault MCP tests | `cd repo && scripts/check.sh` | [[Generate Internal Service Certificate]] |\n"+
		"| Check Vault MCP config index | `severino-vault-mcp doctor` | [[Generate Internal Service Certificate]] |\n")
	r := find(tk.Vault(root), "restart vault mcp")
	if tk.Get(r, "hits.0.doc_id") == "rb-generate-internal-cert" || !r.Has("quick_index_matches") || r.Has("recommended") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocWithholdsRestrictedBody(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	if tk.Get(find(v, "local pki"), "hits.0.doc_id") != "infra-local-pki" {
		t.Fatal("hit")
	}
	r := core.ReadDoc(v, "infra-local-pki", "", false)
	if !r.Bool("found") || r.Bool("body_released") || r.Has("body") || !strings.Contains(strings.ToLower(r.Str("advisory")), "restricted") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocRestrictedRequestRequiresLocalUnlock(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "infra-local-pki", "", true)
	if r.Bool("body_released") || r.Has("body") || tk.Get(r, "unlock.result") != "disabled" ||
		!strings.Contains(strings.ToLower(tk.Get(r, "unlock.message").(string)), "interactive unlock is disabled") {
		t.Fatal(jsonx.Compact(r))
	}
}

func withPrompt(t *testing.T, phrase string) {
	prev := gate.PromptPhrase
	gate.PromptPhrase = func(string, string) (string, bool) { return phrase, true }
	t.Cleanup(func() { gate.PromptPhrase = prev })
}

func TestReadDocReleasesRestrictedAfterLocalUnlock(t *testing.T) {
	root := tk.FakeVault(t)
	audit := filepath.Join(root, "audit.log")
	v := tk.Vault(root, "SVMC_ALLOW_RESTRICTED_UNLOCK", "1", "SVMC_RESTRICTED_UNLOCK_HASH", encodedUnlockHash("open sesame"),
		"SVMC_RESTRICTED_UNLOCK_AUDIT_LOG", audit)
	withPrompt(t, "open sesame")
	r := core.ReadDoc(v, "infra-local-pki", "", true)
	if !r.Bool("body_released") || !strings.Contains(r.Str("body"), "CA private key") || !r.Bool("override_used") || tk.Get(r, "unlock.result") != "released" {
		t.Fatal(jsonx.Compact(r))
	}
	log := tk.Read(t, audit)
	if !strings.Contains(log, "doc_id=infra-local-pki") || !strings.Contains(log, "result=released") {
		t.Fatal(log)
	}
}

func TestReadDocKeepsRestrictedLockedAfterBadUnlock(t *testing.T) {
	root := tk.FakeVault(t)
	audit := filepath.Join(root, "audit.log")
	v := tk.Vault(root, "SVMC_ALLOW_RESTRICTED_UNLOCK", "1", "SVMC_RESTRICTED_UNLOCK_HASH", encodedUnlockHash("open sesame"),
		"SVMC_RESTRICTED_UNLOCK_AUDIT_LOG", audit)
	withPrompt(t, "wrong")
	r := core.ReadDoc(v, "infra-local-pki", "", true)
	if r.Bool("body_released") || r.Has("body") || tk.Get(r, "unlock.result") != "failed" {
		t.Fatal(jsonx.Compact(r))
	}
	if log := tk.Read(t, audit); !strings.Contains(log, "result=failed") {
		t.Fatal(log)
	}
}

func TestReadDocReturnsBodyForInternal(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "rb-add-nginx-proxy-host", "", false)
	if !r.Bool("body_released") || !strings.Contains(r.Str("body"), "## Goal") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocResolvesLocalAliases(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "https proxy", "", false)
	if !r.Bool("found") || r.Str("doc_id") != "rb-add-nginx-proxy-host" || tk.Get(r, "resolved_from_alias.matched_alias") != "https proxy" || !r.Bool("body_released") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocAliasPreservesRestrictedGate(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "offline ca", "", false)
	if !r.Bool("found") || r.Str("doc_id") != "infra-local-pki" || r.Bool("body_released") || r.Has("body") ||
		tk.Get(r, "resolved_from_alias.target_doc_id") != "infra-local-pki" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocMissingDocGuidesDiscovery(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "not a doc", "", false)
	if r.Bool("found") || !strings.Contains(r.Str("guidance"), "`find`") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestQuickIndexAndDocResources(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	if qi := core.RenderDoc(v, core.QuickIndexDocID); !strings.Contains(qi, "# Example Operations Vault Quick Index") || !strings.Contains(qi, "rb-add-nginx-proxy-host") {
		t.Fatal(qi)
	}
	if d := core.RenderDoc(v, "rb-add-nginx-proxy-host"); !strings.Contains(d, "## Goal") || !strings.Contains(d, "Expose an internal service") {
		t.Fatal(d)
	}
	if d := core.RenderDoc(v, "infra-local-pki"); !strings.Contains(d, "restricted") || !strings.Contains(d, "infra-local-pki") || strings.Contains(d, "CA private key lives offline") {
		t.Fatal(d)
	}
	if d := core.RenderDoc(v, "rb-does-not-exist"); !strings.Contains(d, "# Vault Doc Not Found") || !strings.Contains(d, "rb-does-not-exist") {
		t.Fatal(d)
	}
}

func TestReadDocReleasesSensitiveWithAdvisory(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	core.SetFrontmatter(v, "03 Runbooks/Add Nginx Proxy Host.md", write.Set{Sensitivity: new("sensitive")})
	r := core.ReadDoc(v, "rb-add-nginx-proxy-host", "", false)
	if !r.Bool("body_released") || !strings.Contains(r.Str("body"), "## Goal") || !strings.Contains(strings.ToLower(r.Str("advisory")), "sensitive") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestSetFrontmatterValidatesEnums(t *testing.T) {
	r := core.SetFrontmatter(tk.Vault(tk.FakeVault(t)), "01 Projects/untagged.md",
		write.Set{DocID: new("bad-prefix-foo"), Title: new("Foo"), DocType: new("runbook"), System: new("Foo")})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "doc_id") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestSetFrontmatterAcceptsHomelabEnvironmentAndCreates(t *testing.T) {
	root := tk.FakeVault(t)
	r := core.SetFrontmatter(tk.Vault(root), "01 Projects/untagged.md", write.Set{
		DocID: new("project-homelab-untagged"), Title: new("Homelab Untagged"), DocType: new("architecture_note"),
		System: new("Homelab"), Environment: new("homelab"),
	})
	if !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	body := tk.Read(t, filepath.Join(root, "01 Projects", "untagged.md"))
	if !strings.HasPrefix(body, "---") || !strings.Contains(body, "environment: homelab") || !strings.Contains(body, "doc_id: project-homelab-untagged") {
		t.Fatal(body)
	}
}

func TestSetFrontmatterRefusesDocIDChange(t *testing.T) {
	r := core.SetFrontmatter(tk.Vault(tk.FakeVault(t)), "03 Runbooks/Add Nginx Proxy Host.md",
		write.Set{DocID: new("rb-something-else"), Title: new("X"), DocType: new("runbook"), System: new("X")})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "immutable") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestSetFrontmatterTouchesLastReviewed(t *testing.T) {
	root := tk.FakeVault(t)
	r := core.SetFrontmatter(tk.Vault(root), "03 Runbooks/Add Nginx Proxy Host.md",
		write.Set{TouchLastReviewed: true, Tags: write.ListOp{Add: []string{"proxy"}}})
	cf, _ := r.Get("changed_fields")
	if !r.Bool("ok") || !slices.Contains(cf.([]string), "last_reviewed") || !slices.Contains(cf.([]string), "tags") {
		t.Fatal(jsonx.Compact(r))
	}
	body := tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md"))
	if !strings.Contains(body, "proxy") || !strings.Contains(body, "doc_id: rb-add-nginx-proxy-host") {
		t.Fatal(body)
	}
}

func TestSetFrontmatterPreservesMultilineScalar(t *testing.T) {
	root := tk.FakeVault(t)
	p := filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")
	text := tk.Read(t, p)
	tk.Write(t, p, strings.Replace(text, "tags:\n  - nginx\n  - network-operations\n",
		"tags:\n  - nginx\n  - network-operations\nnotes: >-\n  First line of context.\n  Second line of context.\n", 1))
	if r := core.SetFrontmatter(tk.Vault(root), "03 Runbooks/Add Nginx Proxy Host.md", write.Set{Title: new("Add an Nginx Proxy Host")}); !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.Contains(tk.Read(t, p), "notes: First line of context. Second line of context.") {
		t.Fatal(tk.Read(t, p))
	}
}

func TestSetFrontmatterKeepsOriginalOnAtomicWriteFailure(t *testing.T) {
	root := tk.FakeVault(t)
	p := filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")
	original := tk.Read(t, p)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Dir(p)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	r := core.SetFrontmatter(tk.Vault(root), "03 Runbooks/Add Nginx Proxy Host.md", write.Set{Title: new("Should Not Persist")})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "write failed") || tk.Read(t, p) != original {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestSetFrontmatterCreateNeedsTheRequiredFields(t *testing.T) {
	r := core.SetFrontmatter(tk.Vault(tk.FakeVault(t)), "01 Projects/untagged.md", write.Set{Status: new("active")})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "creating one needs") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindTextFindsTextInBody(t *testing.T) {
	requireRG(t)
	r := findBy(tk.Vault(tk.FakeVault(t)), "HTTPS via NPM", "text")
	if !slices.Contains(tk.IDs(tk.Hits(r, "hits")), "rb-add-nginx-proxy-host") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindTextAlwaysExcludesRestricted(t *testing.T) {
	requireRG(t)
	r := findBy(tk.Vault(tk.FakeVault(t)), "CA private key", "text")
	if tk.Get(r, "match_count") != 0 || tk.Get(r, "excluded.restricted_skipped").(int) < 1 {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindTextSkipsFrontmatterHits(t *testing.T) {
	requireRG(t)
	r := findBy(tk.Vault(tk.FakeVault(t)), "NPM", "text")
	for _, h := range tk.Hits(r, "hits") {
		if h.Str("doc_id") != "rb-add-nginx-proxy-host" {
			continue
		}
		snips, _ := h.Get("snippets")
		for _, s := range snips.([]*jsonx.Obj) {
			if n, _ := s.Get("line_number"); n.(int) < 17 {
				t.Fatal(jsonx.Compact(s))
			}
		}
		return
	}
	t.Fatal("nginx hit missing")
}

func TestFindProjectFiltersBySlug(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	core.SetFrontmatter(v, "03 Runbooks/Add Nginx Proxy Host.md", write.Set{RelatedProjects: write.ListOp{Add: []string{"client-edge-dns"}}})
	r := findBy(v, "client-edge-dns", "project")
	if tk.Get(r, "match_count") != 1 || tk.Get(r, "hits.0.doc_type") != "runbook" {
		t.Fatal(jsonx.Compact(r))
	}
}

func repoRoot(t *testing.T) string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..")
}

func TestSampleVaultIsReproducible(t *testing.T) {
	v := tk.Vault(filepath.Join(repoRoot(t), "examples", "sample-vault"))
	if qi := core.RenderDoc(v, core.QuickIndexDocID); !strings.Contains(qi, "Example Operations Vault Quick Index") || !strings.Contains(qi, "rb-generate-internal-cert") {
		t.Fatal(qi)
	}
	if d := core.RenderDoc(v, "rb-generate-internal-cert"); !strings.Contains(d, "## Commands") || !strings.Contains(d, "./cert-gen <service>.internal.example") {
		t.Fatal(d)
	}
	if tk.Get(find(v, "generate internal certificate"), "hits.0.doc_id") != "rb-generate-internal-cert" {
		t.Fatal("cert search")
	}
	for _, id := range []string{"infra-offline-ca", "Offline CA", "offline ca"} {
		r := core.ReadDoc(v, id, "", false)
		if !r.Bool("found") || r.Str("doc_id") != "infra-offline-ca" || r.Bool("body_released") || r.Has("body") {
			t.Fatalf("%s: %s", id, jsonx.Compact(r))
		}
	}
	if !slices.Contains(tk.IDs(tk.Hits(findBy(v, "Offline CA", "system"), "hits")), "infra-offline-ca") {
		t.Fatal("system search")
	}
}

func TestReadDocDefaultReturnsWholeBodyUnchanged(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "rb-add-nginx-proxy-host", "", false)
	if r.Has("body_scope") || r.Has("section") || !strings.HasPrefix(r.Str("body"), "## Goal") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocSectionReturnsOnlyThatSpan(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	r := core.ReadDoc(tk.Vault(root), "rb-backup-ops", "troubleshooting", false)
	if r.Str("body_scope") != "section" || r.Str("section") != "troubleshooting" || !strings.Contains(r.Str("body"), "resolver logs") ||
		strings.Contains(r.Str("body"), "Routine operations") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocUnknownSectionListsAvailable(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	r := core.ReadDoc(tk.Vault(root), "rb-backup-ops", "nope", false)
	var slugs []string
	for _, s := range tk.Hits(r, "available_sections") {
		slugs = append(slugs, s.Str("section"))
	}
	if r.Bool("body_released") || r.Has("body") || !slices.Contains(slugs, "routine-operations") || !slices.Contains(slugs, "troubleshooting") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindHitCarriesSectionMenuAndReadReturnsIt(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	v := tk.Vault(root)
	top := tk.Hits(find(v, "resolver latency troubleshooting"), "hits")[0]
	if top.Str("doc_id") != "rb-backup-ops" || top.Str("section") != "troubleshooting" || top.Has("body") || top.Str("section_summary") == "" {
		t.Fatal(jsonx.Compact(top))
	}
	hit := tk.Hits(find(v, "resolver latency"), "hits")[0]
	r := core.ReadDoc(v, hit.Str("doc_id"), hit.Str("section"), false)
	if !strings.Contains(r.Str("body"), "resolver logs") || strings.Contains(r.Str("body"), "Routine operations") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindSectionsMatchesTheFindTool(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	v := tk.Vault(root)
	q := "resolver latency troubleshooting"
	service := query.FindSections(v.Loader, q, 10)
	tool := find(v, q)
	sh, _ := service.Get("hits")
	th, _ := tool.Get("hits")
	if jsonx.Compact(sh) != jsonx.Compact(th) || tk.Get(service, "indexed_doc_count") != tk.Get(tool, "indexed_doc_count") {
		t.Fatal("service and tool disagree")
	}
}

func TestUnknownModeIsRefused(t *testing.T) {
	if r := findBy(tk.Vault(tk.FakeVault(t)), "x", "fuzzy"); !strings.Contains(r.Str("error"), "unknown mode") {
		t.Fatal(jsonx.Compact(r))
	}
}
