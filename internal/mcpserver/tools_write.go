package mcpserver

import (
	"slices"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

func (h *handlers) setFrontmatter(fields []schema.Field) coreTool {
	fargs := write.Args(fields, "", false)
	input := object(jsonx.New(
		"relative_path", str("Path from the vault root."),
		"vault", h.vaultProp("Which vault."),
		write.UnsetKey, strList(write.UnsetHelp),
	).Merge(frontmatterProps(fargs)), []string{"relative_path"})
	return coreTool{"set_frontmatter", descSet, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		rel := p.required("relative_path")
		c := readChanges(p, fargs)
		if p.err != nil {
			return nil, p.err
		}
		return core.SetFrontmatter(v, rel, c), nil
	}}
}

func (h *handlers) writeDoc(fields []schema.Field) coreTool {
	fargs := write.Args(slices.DeleteFunc(slices.Clone(fields), func(f schema.Field) bool { return f.Name == "doc_id" }), "", true)
	input := object(jsonx.New(
		"mode", enum(core.WriteDocModes, "create", "See the tool description."),
		"body", optStr("Markdown without frontmatter: the whole doc, or the section's content. Omit for delete_section and replace_text."),
		"vault", h.vaultProp("Which vault."),
		"relative_path", optStr("Path from the vault root; required to create."),
		"doc_id", optStr("The new doc's id, or the doc to edit (or give relative_path)."),
		"heading", optStr(`The section to edit: a heading_path or section slug from read_doc's outline, or a unique heading text without the #s; "" for the text before the first heading. Section modes, and replace_text to search only that section.`),
		"old_text", optStr("replace_text: the exact text to replace, copied from read_doc; it must occur exactly once in the body, or in the section heading names."),
		"new_text", optStr(`replace_text: what old_text becomes; "" removes it.`),
		"include_restricted", boolean("Edit a restricted doc."),
	).Merge(frontmatterProps(fargs)), nil)
	return coreTool{"write_doc", descWriteDoc, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		mode := p.choice("mode", core.WriteDocModes, "create")
		body := p.str("body")
		if mode != "delete_section" && mode != "replace_text" {
			body = p.required("body")
		}
		w := core.WriteDocArgs{
			Mode:              mode,
			Body:              body,
			RelativePath:      p.str("relative_path"),
			DocID:             p.str("doc_id"),
			Heading:           p.opt("heading"),
			OldText:           p.str("old_text"),
			NewText:           p.opt("new_text"),
			New:               readChanges(p, fargs),
			IncludeRestricted: p.boolean("include_restricted"),
		}
		if p.err != nil {
			return nil, p.err
		}
		return core.WriteDoc(v, w), nil
	}}
}

// frontmatterProps are the input properties of frontmatter arguments.
func frontmatterProps(fargs []write.Arg) *jsonx.Obj {
	o := jsonx.New()
	for _, a := range fargs {
		switch {
		case a.Op == write.OpTouch:
			o.Set(a.Key, boolean(a.Help()))
		case a.Op != write.OpSet:
			o.Set(a.Key, strList(a.Help()))
		case a.Field.Kind == schema.KindInteger:
			o.Set(a.Key, jsonx.New("type", []string{"integer", "null"}, "description", a.Help()))
		case a.Field.Kind == schema.KindBoolean:
			o.Set(a.Key, jsonx.New("type", []string{"boolean", "null"}, "description", a.Help()))
		default:
			o.Set(a.Key, optStr(a.Help()))
		}
	}
	return o
}

// readChanges reads the frontmatter arguments a call gives.
func readChanges(p *params, fargs []write.Arg) write.Changes {
	var c write.Changes
	for _, fa := range fargs {
		if !p.given(fa.Key) {
			continue
		}
		var value any
		switch {
		case fa.Op == write.OpTouch, fa.Field.Kind == schema.KindBoolean:
			value = p.boolean(fa.Key)
		case fa.Op != write.OpSet:
			value = p.list(fa.Key)
		case fa.Field.Kind == schema.KindInteger:
			value = p.integer(fa.Key, 0)
		default:
			value = p.str(fa.Key)
		}
		if p.err != nil {
			return c
		}
		c.Apply(fa, value)
	}
	if unset := p.list(write.UnsetKey); len(unset) > 0 {
		c = c.Unsetting(unset...)
	}
	return c
}

const descSet = `Create or update a doc's frontmatter, validated against the vault's schema.

A file without frontmatter gets a new block (doc_id, title, doc_type and
system required; environment, status and sensitivity default to other,
active, internal). With frontmatter, omitted fields stay and doc_id never
changes once set. A list field is replaced whole, or edited with add_<field>
and remove_<field>. unset removes fields from the block by name, with any
list items, even a leftover key the profile no longer declares; a field the
doc requires is refused. Every field the vault's profile declares is an argument.`

const descWriteDoc = `Create a registered doc, or edit a section or a passage of its body, validated against the vault's schema.

create (default): write relative_path with body and a new frontmatter block
    (doc_id, title, doc_type and system required; environment, status and
    sensitivity default to other, active, internal). Refuses an existing
    file and a body that carries its own frontmatter; adds ` + "`# <title>`" + `
    when the body has no H1.
replace_section: replace the section heading names (doc_id or
    relative_path names the doc). A heading, by its text, heading_path or
    section slug, names its whole block: everything up to the next heading
    of its level or higher, subsections included, which is what read_doc
    returns for it; a part slug (-part-N) names just that part. The heading
    line stays. Refuses a body that drops a subsection heading the block
    has; keep them in the body or target the subsection. "" is the text
    before the first heading.
append_section: add body at the end of that whole block.
delete_section: remove that whole block, heading line and subsections
    included; takes no body. Refuses "", a part slug, and a section that is
    the doc's last heading.
replace_text: replace old_text with new_text ("" removes it) where it
    occurs exactly once in the body, or in the section heading names when
    given; zero or several matches is an error with the count. Use it to
    change a sentence or a bullet. Include the line break in old_text to
    remove a whole line. Frontmatter is never searched; set_frontmatter
    changes it.
Section modes and replace_text keep the frontmatter as it is except last_reviewed, which
becomes today. Restricted docs need include_restricted and Touch ID.`
