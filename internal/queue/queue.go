// Package queue is the work queue's line grammar: one item per line in a
// project's Queue.md, an Obsidian task whose fields are Dataview inline
// fields, so Obsidian, the Tasks plugin and Dataview read it natively.
// Parse(Format(it)) == it. The grammar is documented in
// docs/ai-tool-contract.md; the store and the tool that write it live in
// internal/core.
//
//   - [<status char>] <text>  [kind:: …]  [owner:: …] …  [id:: …]  [dependsOn:: …]
//
// Tasks reads its fields from the end of the line and stops at the first one
// it does not know, so id and dependsOn come after the queue's own fields, in
// the order Tasks writes them, and the done and cancelled dates Tasks or
// Dataview append when a box is ticked follow them.
package queue

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kinds are an item's kinds; Statuses its statuses.
var (
	Kinds    = []string{"next", "delegated", "decision", "follow-up"}
	Statuses = []string{"open", "returned", "parked", "closed", "dropped"}
)

// marks are the checkbox characters of the statuses, as the Tasks plugin
// types them: TODO, IN_PROGRESS, a forwarded TODO (ON_HOLD where configured),
// DONE and CANCELLED.
var marks = map[string]rune{"open": ' ', "returned": '/', "parked": '>', "closed": 'x', "dropped": '-'}

// Done reports whether status ends an item: it sits under ## Closed, may carry
// a resolution, and no longer blocks another (as Tasks treats DONE and
// CANCELLED).
func Done(status string) bool { return status == "closed" || status == "dropped" }

// LinkKeys are the typed links a hook can match an item by. Format writes
// kind, owner, agent, the links, body, alias and resolution in this order,
// then id and dependsOn.
var LinkKeys = []string{"pr", "branch", "commit", "slug"}

// Unowned is the owner argument meaning nobody; the line simply omits owner.
const Unowned = "-"

// TokenRule says what a one-word value (owner, agent, link, body, alias) may
// hold.
const TokenRule = "one word without spaces, brackets, parentheses or a backslash"

// Field keys of the line; idKey and dependsOnKey are the Tasks plugin's.
const (
	idKey        = "id"
	dependsOnKey = "dependsOn"
	sep          = "  " // Tasks' own separator before an inline field
)

// tasksKeys are the Tasks plugin's own Dataview-format fields besides id and
// dependsOn. An extra field with one of these keys (or an emoji date) goes
// after id, where Tasks reads it; any other extra goes before id, so it never
// stops Tasks' parse short of the id.
var tasksKeys = []string{"created", "start", "scheduled", "due", "completion", "cancelled", "priority", "repeat", "onCompletion"}

// doneKeys are the fields Tasks and Dataview add on a tick, dropped again when
// the item reopens.
var doneKeys = []string{"completion", "cancelled"}

// Item is one queue line. Owner "" is unowned; Links holds only the keys
// given; Resolution is set only on a done item. Extra holds the fields the
// queue does not own (a done date Tasks appended, say), each as written.
type Item struct {
	ID, Kind, Status, Owner, Text string
	Links                         map[string]string
	BlockedBy                     []string
	Body, Alias, Agent            string
	Resolution                    string
	Extra                         []string
}

var (
	idRe     = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)-([0-9]+)$`)
	prefixRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	tokenRe  = regexp.MustCompile(`^[^\s\[\]()\\]+$`)
	keyRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	// emojiDateRe is a Tasks emoji-format date at the end of the line: done,
	// cancelled, due, scheduled, start, created.
	emojiDateRe = regexp.MustCompile(`(?:^|\s)((?:✅|❌|📅|⏳|🛫|➕) \d{4}-\d{2}-\d{2})$`)
)

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

// Brackets and parentheses could open an inline field; the emoji could start a
// Tasks date the parser would take off the end of the text.
const (
	textSpecial  = "[]()✅❌📅⏳🛫➕"
	valueSpecial = "[]()"
)

// Escape makes text safe as the task's description.
func Escape(text string) string { return escape(text, textSpecial) }

// Unescape reverses Escape and the escaping of a resolution.
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
	if it.Owner != "" && (!ValidToken(it.Owner) || it.Owner == Unowned) {
		return fmt.Errorf("owner %q must be %s", it.Owner, TokenRule)
	}
	if err := OneLine("text", it.Text); err != nil {
		return err
	}
	if it.Resolution != "" {
		if !Done(it.Status) {
			return fmt.Errorf("only a closed or dropped item carries a resolution; %s is %s", it.ID, it.Status)
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
	for _, f := range [][2]string{{"body", it.Body}, {"alias", it.Alias}, {"agent", it.Agent}} {
		if f[1] != "" && !ValidToken(f[1]) {
			return fmt.Errorf("%s %q must be %s", f[0], f[1], TokenRule)
		}
	}
	for _, x := range it.Extra {
		if f, ok := lastField(x); !ok || f.raw != x || f.key != "" && owned(f.key) {
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
	case "kind", "owner", "agent", "body", "alias", "resolution", idKey, dependsOnKey:
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
	field("kind", it.Kind)
	field("owner", it.Owner)
	field("agent", it.Agent)
	for _, k := range LinkKeys {
		field(k, it.Links[k])
	}
	field("body", it.Body)
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
	if it.Status == "" {
		return Item{}, false
	}
	var fields []inline
	for {
		f, ok := lastField(rest)
		if !ok {
			break
		}
		fields = append(fields, f)
		rest = strings.TrimRight(rest[:len(rest)-len(f.raw)], " \t")
	}
	slices.Reverse(fields)
	seen := map[string]bool{}
	for _, f := range fields {
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
		case "body":
			it.Body = f.value
		case "alias":
			it.Alias = f.value
		case "resolution":
			it.Resolution = Unescape(f.value)
		case idKey:
			it.ID = f.value
		case dependsOnKey:
			for b := range strings.SplitSeq(f.value, ",") {
				it.BlockedBy = append(it.BlockedBy, strings.TrimSpace(b))
			}
		default:
			if it.Links == nil {
				it.Links = map[string]string{}
			}
			it.Links[f.key] = f.value
		}
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
	start := -1
	for i := end - 1; i >= 0; i-- {
		if (s[i] == '[' || s[i] == '(' || s[i] == ']' || s[i] == ')') && !escapedAt(s, i) {
			start = i
			break
		}
	}
	if start < 0 || s[start] != open || (start > 0 && s[start-1] != ' ' && s[start-1] != '\t') {
		return inline{}, false
	}
	key, value, found := strings.Cut(s[start+1:end], "::")
	key = strings.TrimSpace(key)
	if !found || !keyRe.MatchString(key) {
		return inline{}, false
	}
	return inline{raw: s[start:], key: key, value: strings.TrimSpace(value)}, true
}

// escapedAt reports whether an odd run of backslashes precedes s[i].
func escapedAt(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}
