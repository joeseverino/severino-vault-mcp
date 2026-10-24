// Package queue is the work queue's line grammar: one item per line in a
// project's Queue.md, an Obsidian task whose body is a wikilink, whose kind
// and owner are tags and whose other fields are Dataview inline fields, so
// Obsidian, the Tasks plugin and Dataview read it natively.
// Parse(Format(it)) == it. The grammar is documented in
// docs/ai-tool-contract.md; the store and the tool that write it live in
// internal/core.
//
//   - [<status char>] <text> ([[<body note>|notes]]) #queue/<kind> #owner/<owner>  [agent:: …] …  [id:: …]  [dependsOn:: …]  [created:: …]
//
// The Tasks plugin hides only the fields it owns, so everything a reader
// needs to see reads as text: the body as a short link, kind and owner as
// tags, which a Tasks query filters (tags include #queue/decision) and hides
// (hide tags). The tags follow the text because Tasks keeps tags there when
// it rewrites a line, and moves any it finds among its fields to that spot.
// Tasks reads its fields from the end of the line and stops at the first one
// it does not know, so id and dependsOn come after the queue's own fields, in
// the order Tasks writes them, then the created date, and the done and
// cancelled dates Tasks or Dataview append when a box is ticked follow them.
package queue

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kinds are an item's kinds; Statuses its statuses.
var (
	Kinds    = []string{"next", "delegated", "decision", "follow-up"}
	Statuses = []string{"open", "returned", "closed", "dropped"}
)

// marks are the checkbox characters of the statuses, as the Tasks plugin
// types them: TODO, IN_PROGRESS, DONE and CANCELLED.
var marks = map[string]rune{"open": ' ', "returned": '/', "closed": 'x', "dropped": '-'}

// retiredMarks are checkbox characters of statuses the queue no longer has,
// each read as the status that replaced it, so an older note still parses
// and its next write gives the line the current mark.
var retiredMarks = map[rune]string{'>': "open"}

// Done reports whether status ends an item: it sits under ## Closed, may carry
// a resolution, and no longer blocks another (as Tasks treats DONE and
// CANCELLED).
func Done(status string) bool { return status == "closed" || status == "dropped" }

// LinkKeys are the typed links a hook can match an item by. Format writes the
// body link and the kind and owner tags after the text, then agent, origin,
// repos, the links, base, owns, alias and resolution in this order, then id,
// dependsOn and created.
var LinkKeys = []string{"pr", "branch", "commit", "slug"}

// Unowned is the owner argument meaning nobody; the line simply omits owner.
const Unowned = "-"

// TokenRule says what a one-word value (owner, agent, link, body, alias) may
// hold.
const TokenRule = "one word without spaces, brackets, parentheses or a backslash"

// OwnerRule says what an owner may hold: it is written as the tag
// #owner/<owner>, which any other character would end.
const OwnerRule = "one word of letters, digits, _ or -"

// PathRule says what an owns entry may hold: a path inside the field's
// brackets, in a comma list, so neither may appear in it.
const PathRule = "a path without a comma, brackets, a backslash or a line break"

// BodyLinkRule says what a body link may name: an Obsidian link target.
const BodyLinkRule = "a note name without [ ] | # ^ or a line break"

// Field keys of the line; idKey and dependsOnKey are the Tasks plugin's.
const (
	idKey        = "id"
	dependsOnKey = "dependsOn"
	createdKey   = "created"
	// createdEmoji starts the Tasks emoji form of the created date, which a
	// person may type; it reads as the created field.
	createdEmoji = "➕"
	sep          = "  " // Tasks' own separator before an inline field
	// bodyLabel is the text the body link shows.
	bodyLabel = "notes"
)

// tagKeys are the tag namespaces the queue owns, each with the field it
// carries: #queue/<kind> and #owner/<owner>. A tag in the text in either
// namespace is escaped, so it never reads as the item's kind or owner nor
// matches a query for them.
var tagKeys = map[string]string{"queue": "kind", "owner": "owner"}

// tasksKeys are the Tasks plugin's own Dataview-format fields besides id and
// dependsOn. An extra field with one of these keys (or an emoji date) goes
// after id, where Tasks reads it; any other extra goes before id, so it never
// stops Tasks' parse short of the id.
var tasksKeys = []string{"start", "scheduled", "due", "completion", "cancelled", "priority", "repeat", "onCompletion"}

// doneKeys are the fields Tasks and Dataview add on a tick, dropped again when
// the item reopens.
var doneKeys = []string{"completion", "cancelled"}

// Item is one queue line. Owner "" is unowned; Links holds only the keys
// given. Created is the date the item was filed (YYYY-MM-DD). Base is the
// branch its work lands on, Owns the paths its worker owns and Transcript
// the absolute path of the worker transcript holding its brief and report.
// Origin is the repository the session that filed the item ran in;
// Repos are the repositories one item's work spans. Resolution is set on a
// done item, and on an open one with Repos as the repo@sha landings so far.
// Extra holds the fields the queue does not own (a done date Tasks appended,
// say), each as written. Body is the doc_id of the note holding the detail
// and BodyLink that note as the line links it (its file name); the line
// carries the link when BodyLink is set and a body field otherwise, and the
// store resolves one from the other.
type Item struct {
	ID, Kind, Status, Owner, Text string
	Links                         map[string]string
	BlockedBy                     []string
	Body, BodyLink                string
	Alias, Agent, Origin          string
	Repos                         []string
	Base                          string
	Owns                          []string
	Transcript                    string
	Created                       string
	Resolution                    string
	Extra                         []string
}

var (
	idRe     = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)-([0-9]+)$`)
	prefixRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	tokenRe  = regexp.MustCompile(`^[^\s\[\]()\\]+$`)
	keyRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	ownerRe  = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)
	linkRe   = regexp.MustCompile(`^[^\[\]|#^\r\n]+$`)
	pathRe   = regexp.MustCompile(`^[^,\[\]\\\r\n]+$`)
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	// tagRe is a tag in one of the queue's namespaces at the end of a line.
	tagRe = regexp.MustCompile(`(?:^|\s)(#((?i:queue|owner))/(\S*))$`)
	// bodyLinkRe is the body link at the end of the text; bodyLinkTail the
	// same after its opening parenthesis, as Escape sees it.
	bodyLinkRe   = regexp.MustCompile(`(?:^|\s)(\(\[\[([^\[\]|#^\r\n]+)\|` + bodyLabel + `\]\]\))$`)
	bodyLinkTail = regexp.MustCompile(`^\[\[[^\[\]|#^\r\n]+\|` + bodyLabel + `\]\]\)$`)
	// emojiDateRe is a Tasks emoji-format date at the end of the line: done,
	// cancelled, due, scheduled, start, created.
	emojiDateRe = regexp.MustCompile(`(?:^|\s)((?:✅|❌|📅|⏳|🛫|➕) \d{4}-\d{2}-\d{2})$`)
)

// Version is the item line format this binary writes and reads in full. A
// queue note records the format it was written in as queue_format; a higher
// one was written by a newer binary, whose lines this one may misread.
const Version = 3

// CheckVersion refuses a note's queue_format that this binary cannot write
// under: one above Version, or anything but a whole number. "" is a note
// written before the field existed.
func CheckVersion(v string) error {
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	switch {
	case err != nil || n < 1:
		return fmt.Errorf("queue_format %q is not a format version", v)
	case n > Version:
		return fmt.Errorf("queue_format is %d and this binary knows format %d", n, Version)
	}
	return nil
}

// ItemShaped reports whether line has an item's shape, a task checkbox at the
// start of the line ("- [<char>] ..."), whether or not it parses.
func ItemShaped(line string) bool {
	rest, ok := strings.CutPrefix(strings.TrimRight(line, " \t\r"), "- [")
	if !ok {
		return false
	}
	r, size := utf8.DecodeRuneInString(rest)
	if size == 0 || r == ']' {
		return false
	}
	rest = rest[size:]
	return rest == "]" || strings.HasPrefix(rest, "] ")
}

// rawIDRe is an id written as a field, whatever surrounds it: [id:: x],
// (id:: x), or the Tasks emoji form 🆔 x.
var rawIDRe = regexp.MustCompile(`(?:[\[(]\s*id\s*::\s*|🆔\s*)([a-z0-9]+(?:-[a-z0-9]+)*-[0-9]+)`)

// IDsIn are the ids text writes as an id field, in order, read from the
// text alone so a line no grammar parses still counts.
func IDsIn(text string) []string {
	var out []string
	for _, m := range rawIDRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// Top is the largest n among the ids prefix-n that text writes as an id
// field, or 0.
func Top(text, prefix string) int {
	top := 0
	for _, id := range IDsIn(text) {
		if pre, n, ok := SplitID(id); ok && pre == prefix && n > top {
			top = n
		}
	}
	return top
}

// SplitID returns an id's prefix and number.
func SplitID(id string) (prefix string, n int, ok bool) {
	m := idRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// ValidPrefix reports whether p can start an id.
func ValidPrefix(p string) bool { return prefixRe.MatchString(p) }

// ValidToken reports whether s can stand as a one-word field value.
func ValidToken(s string) bool { return tokenRe.MatchString(s) }

// ValidOwner reports whether s can stand as an owner.
func ValidOwner(s string) bool { return ownerRe.MatchString(s) && s != Unowned }

// ValidBodyLink reports whether s can stand as a body link's target.
func ValidBodyLink(s string) bool { return linkRe.MatchString(s) && strings.TrimSpace(s) == s }

// ValidPath reports whether s can stand in an owns list.
func ValidPath(s string) bool { return pathRe.MatchString(s) && strings.TrimSpace(s) == s }

// ValidRepo reports whether s can stand in a comma list of repositories.
func ValidRepo(s string) bool { return ValidToken(s) && !strings.ContainsAny(s, ",@") }

// Landing is one repo@sha entry of a multi-repo item's resolution.
type Landing struct{ Repo, Commit string }

// Landings reads s as comma-separated repo@sha entries; ok is false when any
// entry is something else.
func Landings(s string) (out []Landing, ok bool) {
	for _, e := range SplitList(s) {
		repo, commit, found := strings.Cut(e, "@")
		if !found || !ValidRepo(repo) || !ValidRepo(commit) {
			return nil, false
		}
		out = append(out, Landing{repo, commit})
	}
	return out, true
}

// SplitList reads a comma list, trimming each entry.
func SplitList(s string) []string {
	var out []string
	for e := range strings.SplitSeq(s, ",") {
		out = append(out, strings.TrimSpace(e))
	}
	return out
}

// escape puts a backslash before each rune in special: Markdown's own escape,
// so Obsidian shows the text as typed.
func escape(s, special string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '\\' || strings.ContainsRune(special, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// valueSpecial is escaped inside a [key:: value] field: only a ] ends it, so
// parentheses and [ stay as typed.
const valueSpecial = "]"

// dateEmoji are the Tasks emoji that start a date.
const dateEmoji = "✅❌📅⏳🛫➕"

var emojiDateTail = regexp.MustCompile(`^ *\d{4}-\d{2}-\d{2}`)

// Escape makes text safe as the task's description, escaping only what a
// parser would read as something other than text: a backslash; a [ or ( that
// opens a Dataview field (a "::" follows it before its own closing bracket);
// the "::" of a leading unbracketed "key:: value"; a Tasks date emoji
// followed by a date; the # of a tag in the queue's own namespaces (#queue,
// #owner and their children, in any case); and the ( of a trailing
// "([[note|notes]])" that would read as the body link. Ordinary brackets,
// parentheses, links and other tags stay as typed.
func Escape(text string) string {
	leading := strings.Index(text, "::")
	if leading <= 0 || strings.ContainsAny(text[:leading], "[]()") {
		leading = -1
	}
	var sb strings.Builder
	prev := ' '
	for i, r := range text {
		rest := text[i+utf8.RuneLen(r):]
		switch {
		case r == '\\',
			r == '[' && opensField(rest, ']'),
			r == '(' && opensField(rest, ')'),
			i == leading,
			strings.ContainsRune(dateEmoji, r) && emojiDateTail.MatchString(rest),
			r == '#' && unicode.IsSpace(prev) && reservedTag(rest),
			r == '(' && unicode.IsSpace(prev) && bodyLinkTail.MatchString(rest):
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
		prev = r
	}
	return sb.String()
}

// reservedTag reports whether the tag rest begins (after its #) is in one of
// the queue's namespaces: the namespace word, then the tag's end or a /.
func reservedTag(rest string) bool {
	for ns := range tagKeys {
		if len(rest) < len(ns) || !strings.EqualFold(rest[:len(ns)], ns) {
			continue
		}
		next, size := utf8.DecodeRuneInString(rest[len(ns):])
		if size == 0 || !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' && next != '-' {
			return true
		}
	}
	return false
}

// opensField reports whether a "::" comes in rest before the closing bracket.
func opensField(rest string, closing byte) bool {
	before, _, found := strings.Cut(rest, "::")
	return found && strings.IndexByte(before, closing) < 0
}

// Unescape reverses Escape and the escaping of a resolution, whichever
// characters carry a backslash.
func Unescape(s string) string {
	var sb strings.Builder
	escaped := false
	for _, r := range s {
		if !escaped && r == '\\' {
			escaped = true
			continue
		}
		escaped = false
		sb.WriteRune(r)
	}
	if escaped {
		sb.WriteByte('\\')
	}
	return sb.String()
}

// Validate reports the first way it breaks the grammar or the lifecycle.
func (it Item) Validate() error {
	if _, _, ok := SplitID(it.ID); !ok {
		return fmt.Errorf("id %q is not <prefix>-<number>", it.ID)
	}
	if !slices.Contains(Kinds, it.Kind) {
		return fmt.Errorf("kind %q is not one of %s", it.Kind, strings.Join(Kinds, ", "))
	}
	if !slices.Contains(Statuses, it.Status) {
		return fmt.Errorf("status %q is not one of %s", it.Status, strings.Join(Statuses, ", "))
	}
	if it.Status == "returned" && it.Kind != "delegated" {
		return fmt.Errorf("only a delegated item is returned; %s is %s", it.ID, it.Kind)
	}
	if it.Owner != "" && !ValidOwner(it.Owner) {
		return fmt.Errorf("owner %q must be %s", it.Owner, OwnerRule)
	}
	if it.BodyLink != "" && !ValidBodyLink(it.BodyLink) {
		return fmt.Errorf("body link %q must be %s", it.BodyLink, BodyLinkRule)
	}
	if err := OneLine("text", it.Text); err != nil {
		return err
	}
	if it.Resolution != "" {
		if !Done(it.Status) && len(it.Repos) == 0 {
			return fmt.Errorf("only a closed or dropped item, or one with repos, carries a resolution; %s is %s", it.ID, it.Status)
		}
		if err := OneLine("resolution", it.Resolution); err != nil {
			return err
		}
	}
	for k, v := range it.Links {
		if !slices.Contains(LinkKeys, k) {
			return fmt.Errorf("link %q is not one of %s", k, strings.Join(LinkKeys, ", "))
		}
		if !ValidToken(v) {
			return fmt.Errorf("link %s %q must be %s", k, v, TokenRule)
		}
	}
	for _, b := range it.BlockedBy {
		if _, _, ok := SplitID(b); !ok {
			return fmt.Errorf("blocked_by %q is not an item id", b)
		}
	}
	for _, f := range [][2]string{{"body", it.Body}, {"alias", it.Alias}, {"agent", it.Agent}, {"origin", it.Origin}, {"base", it.Base}} {
		if f[1] != "" && !ValidToken(f[1]) {
			return fmt.Errorf("%s %q must be %s", f[0], f[1], TokenRule)
		}
	}
	if it.Transcript != "" && (!ValidPath(it.Transcript) || !strings.HasPrefix(it.Transcript, "/")) {
		return fmt.Errorf("transcript %q must be an absolute path, %s", it.Transcript, PathRule)
	}
	for _, r := range it.Repos {
		if !ValidRepo(r) {
			return fmt.Errorf("repos %q must each be %s, without a comma or @", r, TokenRule)
		}
	}
	for _, p := range it.Owns {
		if !ValidPath(p) {
			return fmt.Errorf("owns %q must each be %s", p, PathRule)
		}
	}
	if it.Created != "" && !dateRe.MatchString(it.Created) {
		return fmt.Errorf("created %q is not a YYYY-MM-DD date", it.Created)
	}
	for _, x := range it.Extra {
		if f, ok := lastField(x); !ok || f.raw != x || f.key != "" && owned(f.key) || strings.HasPrefix(x, createdEmoji) {
			return fmt.Errorf("extra field %q is not one inline field the queue does not own", x)
		}
	}
	return nil
}

// OneLine refuses an empty value, one with a line break, or one with
// surrounding space.
func OneLine(name, s string) error {
	switch {
	case strings.TrimSpace(s) == "":
		return fmt.Errorf("%s is empty", name)
	case strings.ContainsAny(s, "\r\n"):
		return fmt.Errorf("%s must be one line", name)
	case !utf8.ValidString(s):
		return fmt.Errorf("%s is not valid UTF-8", name)
	case strings.TrimSpace(s) != s:
		return fmt.Errorf("%s has surrounding space", name)
	}
	return nil
}

// Reopen moves a done item back to status, dropping its resolution and the
// done and cancelled dates a tick added, as Tasks does when a box is unticked.
func (it *Item) Reopen(status string) {
	it.Status, it.Resolution = status, ""
	var keep []string
	for _, x := range it.Extra {
		if f, _ := lastField(x); !slices.Contains(doneKeys, f.key) && !strings.HasPrefix(x, "✅") && !strings.HasPrefix(x, "❌") {
			keep = append(keep, x)
		}
	}
	it.Extra = keep
}

// owned reports whether the queue owns field key.
func owned(key string) bool {
	switch key {
	case "kind", "owner", "agent", "origin", "repos", "base", "owns", "transcript", "body", "alias", "resolution", idKey, dependsOnKey, createdKey:
		return true
	}
	return slices.Contains(LinkKeys, key)
}

// Format renders it as its line, without a newline.
func Format(it Item) string {
	var sb strings.Builder
	field := func(k, v string) {
		if v != "" {
			sb.WriteString(sep + "[" + k + ":: " + v + "]")
		}
	}
	sb.WriteString("- [" + string(marks[it.Status]) + "] " + Escape(it.Text))
	if it.BodyLink != "" {
		sb.WriteString(" ([[" + it.BodyLink + "|" + bodyLabel + "]])")
	}
	sb.WriteString(" #queue/" + it.Kind)
	if it.Owner != "" {
		sb.WriteString(" #owner/" + it.Owner)
	}
	field("agent", it.Agent)
	field("origin", it.Origin)
	field("repos", strings.Join(it.Repos, ","))
	for _, k := range LinkKeys {
		field(k, it.Links[k])
	}
	field("base", it.Base)
	field("owns", strings.Join(it.Owns, ","))
	field("transcript", it.Transcript)
	if it.BodyLink == "" {
		field("body", it.Body)
	}
	field("alias", it.Alias)
	field("resolution", escape(it.Resolution, valueSpecial))
	var after []string
	for _, x := range it.Extra {
		if f, _ := lastField(x); f.key == "" || slices.Contains(tasksKeys, f.key) {
			after = append(after, x)
		} else {
			sb.WriteString(sep + x)
		}
	}
	field(idKey, it.ID)
	field(dependsOnKey, strings.Join(it.BlockedBy, ","))
	field(createdKey, it.Created)
	for _, x := range after {
		sb.WriteString(sep + x)
	}
	return sb.String()
}

// Parse reads one line; ok is false for any line that is not an item.
func Parse(line string) (it Item, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimRight(line, " \t\r"), "- [")
	if !found {
		return Item{}, false
	}
	mark, size := utf8.DecodeRuneInString(rest)
	rest, found = strings.CutPrefix(rest[size:], "] ")
	if !found {
		return Item{}, false
	}
	for st, m := range marks {
		if m == mark {
			it.Status = st
		}
	}
	if st, ok := retiredMarks[mark]; ok {
		it.Status = st
	}
	if it.Status == "" {
		return Item{}, false
	}
	var fields []inline
	// A line with a [kind:: …] or [owner:: …] field predates the tags, so a
	// tag in it is its text's.
	older := false
	for {
		f, ok := lastField(rest)
		if ok && (f.key == "kind" || f.key == "owner") {
			older = true
		}
		if !ok && !older {
			f, ok = lastTag(rest)
		}
		if !ok {
			break
		}
		fields = append(fields, f)
		rest = strings.TrimRight(rest[:len(rest)-len(f.raw)], " \t")
	}
	slices.Reverse(fields)
	seen := map[string]bool{}
	for _, f := range fields {
		if f.key == "" && strings.HasPrefix(f.raw, createdEmoji) {
			f.key, f.value = createdKey, strings.TrimSpace(strings.TrimPrefix(f.raw, createdEmoji))
		}
		if f.key == "" || !owned(f.key) {
			it.Extra = append(it.Extra, f.raw)
			continue
		}
		if seen[f.key] || f.value == "" {
			return Item{}, false
		}
		seen[f.key] = true
		switch f.key {
		case "kind":
			it.Kind = f.value
		case "owner":
			it.Owner = f.value
		case "agent":
			it.Agent = f.value
		case "origin":
			it.Origin = f.value
		case "repos":
			it.Repos = SplitList(f.value)
		case "base":
			it.Base = f.value
		case "owns":
			it.Owns = SplitList(f.value)
		case "transcript":
			it.Transcript = f.value
		case createdKey:
			it.Created = f.value
		case "body":
			it.Body = f.value
		case "alias":
			it.Alias = f.value
		case "resolution":
			it.Resolution = Unescape(f.value)
		case idKey:
			it.ID = f.value
		case dependsOnKey:
			it.BlockedBy = SplitList(f.value)
		default:
			if it.Links == nil {
				it.Links = map[string]string{}
			}
			it.Links[f.key] = f.value
		}
	}
	if m := bodyLinkRe.FindStringSubmatch(rest); m != nil {
		if seen["body"] {
			return Item{}, false
		}
		it.BodyLink = m[2]
		rest = strings.TrimRight(rest[:len(rest)-len(m[1])], " \t")
	}
	it.Text = Unescape(rest)
	if it.Validate() != nil {
		return Item{}, false
	}
	return it, true
}

// inline is one field at the end of a line: a Dataview inline field
// ([key:: value] or (key:: value)), or a Tasks emoji date (key "").
type inline struct {
	raw, key, value string
}

// lastTag reads a tag in one of the queue's namespaces that ends s, as the
// field it carries: #queue/<kind> is kind and #owner/<owner> is owner, the
// keys the older [kind:: …] and [owner:: …] fields still read as.
func lastTag(s string) (inline, bool) {
	m := tagRe.FindStringSubmatch(s)
	if m == nil {
		return inline{}, false
	}
	return inline{raw: m[1], key: tagKeys[strings.ToLower(m[2])], value: m[3]}, true
}

// lastField reads the field that ends s, after whitespace or at its start.
func lastField(s string) (inline, bool) {
	if m := emojiDateRe.FindStringSubmatch(s); m != nil && !escapedAt(s, len(s)-len(m[1])) {
		return inline{raw: m[1]}, true
	}
	end := len(s) - 1
	if end < 0 || (s[end] != ']' && s[end] != ')') || escapedAt(s, end) {
		return inline{}, false
	}
	open := byte('[')
	if s[end] == ')' {
		open = '('
	}
	// Only the field's own closing bracket ends it, so its value may hold the
	// other kind, and its opening one, as typed. The field starts at the
	// leftmost opening bracket that reads as "key::" before the previous
	// field's close: any opening bracket further right is inside the value.
	var f inline
	found := false
	for i := end - 1; i >= 0; i-- {
		if escapedAt(s, i) {
			continue
		}
		if s[i] == s[end] {
			break
		}
		if s[i] != open || (i > 0 && s[i-1] != ' ' && s[i-1] != '\t') {
			continue
		}
		key, value, ok := strings.Cut(s[i+1:end], "::")
		if key = strings.TrimSpace(key); ok && keyRe.MatchString(key) {
			f, found = inline{raw: s[i:], key: key, value: strings.TrimSpace(value)}, true
		}
	}
	return f, found
}

// escapedAt reports whether an odd run of backslashes precedes s[i].
func escapedAt(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}
