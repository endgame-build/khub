// Package errs ports core/errors.py: the located error taxonomy with
// byte-exact message templates. Factories build code + message together so
// the two can never drift.
package errs

import (
	"fmt"
	"strings"
)

// Located is core.errors.LocatedError: a schema error that names where it
// occurred. Error() is the message alone, exactly like __str__.
type Located struct {
	Code     string
	Message  string
	Type     string
	Relation string
	Target   string
}

// Error returns the message alone, never the code or the location fields.
func (e *Located) Error() string { return e.Message }

// Usage is the Click usage-error analog: plain text on stderr, exit 2.
type Usage struct{ Message string }

// Error returns the usage message as it is printed to stderr.
func (e *Usage) Error() string { return e.Message }

// New builds a located error from a code and a finished message. The named
// factories below are preferred: they keep code and message together.
func New(code, message string) *Located { return &Located{Code: code, Message: message} }

// --- resolver ----------------------------------------------------------------

// UnknownTarget is `unknown_relation_target`: a relation whose `to` names a
// type the schema does not declare.
func UnknownTarget(type_, relation, target string) *Located {
	return &Located{
		Code:     "unknown_relation_target",
		Message:  fmt.Sprintf("Type '%s' relation '%s' targets unknown type '%s'", type_, relation, target),
		Type:     type_,
		Relation: relation,
		Target:   target,
	}
}

// RawLinkMLSmuggled is `raw_linkml_smuggled`: a construct outside khub's own
// schema vocabulary, refused rather than passed through.
func RawLinkMLSmuggled(type_, construct, location string) *Located {
	where := ""
	if location != "" {
		where = " at " + location
	}
	return &Located{
		Code:    "raw_linkml_smuggled",
		Message: fmt.Sprintf("Unknown construct '%s' (not khub vocabulary)%s", construct, where),
		Type:    type_,
		Target:  construct,
	}
}

// DuplicateType is `duplicate_type`: a type declared more than once.
func DuplicateType(type_ string) *Located {
	return &Located{Code: "duplicate_type", Message: fmt.Sprintf("Duplicate type '%s'", type_), Type: type_}
}

// CollectionPathCollision is two collection types resolving to one inventory
// file. Accepting it broke three things at once: each type's `query` claimed
// the other's rows, `check` saw both, and the write lock is keyed per TYPE
// (.khub/generated/locks/<type>.lock) while the file is shared — so two
// writers took different locks and could interleave on the same bytes. That
// last one is why this is rejected at resolve time rather than reported by
// `check`: by the time a report exists the file may already be torn.
func CollectionPathCollision(relpath string, types []string) *Located {
	return &Located{
		Code: "collection_path_collision",
		Message: fmt.Sprintf(
			"Collection types %s all store at '%s'. Give each collection type its own "+
				"path: rows are claimed by every type pointing at the file, and the write "+
				"lock is keyed per type, so concurrent writes are not serialized.",
			strings.Join(quoteAll(types), ", "), relpath),
		Type: types[0],
	}
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "'" + s + "'"
	}
	return out
}

// --- workspace ---------------------------------------------------------------

// UnknownPreset is `unknown_preset`: `init` named a preset the binary does
// not ship; the message lists the ones it does.
func UnknownPreset(name string, known []string) *Located {
	return &Located{
		Code:    "unknown_preset",
		Message: fmt.Sprintf("Unknown preset '%s'. Known presets: %s", name, strings.Join(known, ", ")),
		Target:  name,
	}
}

// TargetNotEmpty is `target_not_empty`: `init` into a directory that already
// holds files; the message names --force as the way through.
func TargetNotEmpty(path string) *Located {
	return &Located{
		Code: "target_not_empty",
		Message: fmt.Sprintf("Target %s is not empty. Pass --force to scaffold alongside the "+
			"existing files; no file already there is modified", path),
		Target: path,
	}
}

// UnknownType is `unknown_type`: a type the resolved schema does not declare.
func UnknownType(name, preset string, known []string) *Located {
	return &Located{
		Code:    "unknown_type",
		Message: fmt.Sprintf("No type '%s' in the %s schema. Known types: %s", name, preset, strings.Join(known, ", ")),
		Type:    name,
	}
}

// NoWorkspace is `no_workspace`: no .khub directory at or above the working
// directory.
func NoWorkspace() *Located {
	return &Located{Code: "no_workspace", Message: "No .khub workspace found. Run khub init <preset>"}
}

// NoPreset is the upgrade guard: a workspace whose config.yaml records no
// preset has nothing to refresh from. Without it an upgrade would have to
// guess which shipped preset to lay over the workspace's own schema.
func NoPreset(path string) *Located {
	return &Located{
		Code: "no_preset",
		Message: fmt.Sprintf("Workspace %s records no preset in .khub/config.yaml; "+
			"run khub init <preset> first", path),
		Target: path,
	}
}

// BadTarget is `bad_target`: a `wire --target` other than claude, agents or
// both.
func BadTarget(value string) *Located {
	return &Located{
		Code:    "bad_target",
		Message: fmt.Sprintf("Unknown --target '%s'. Choose claude, agents, or both", value),
		Target:  value,
	}
}

// --- authoring ---------------------------------------------------------------

// ReferentialIntegrity is `referential_integrity`: a relation names a target
// entity that does not exist. noun defaults to "relation".
func ReferentialIntegrity(targetType, target, predicate, noun string) *Located {
	if noun == "" {
		noun = "relation"
	}
	return &Located{
		Code:     "referential_integrity",
		Message:  fmt.Sprintf("No %s '%s' to satisfy %s '%s'", targetType, target, noun, predicate),
		Type:     targetType,
		Relation: predicate,
		Target:   target,
	}
}

// InvalidSlug is `invalid_slug`: the slug source leaves nothing to mint from
// once slugified.
func InvalidSlug(source string) *Located {
	return &Located{
		Code:    "invalid_slug",
		Message: fmt.Sprintf("Cannot mint a slug from '%s'", source),
		Target:  source,
	}
}

// NoSlugSource is a type with neither `name` nor `title` set and no --id: there
// is nothing to mint from. The type-name fallback went with the ordinal —
// without one it would mint a single id per type.
func NoSlugSource(type_ string) *Located {
	return &Located{
		Code: "no_slug_source",
		Message: fmt.Sprintf("Type '%s' has no name or title to mint an id from; "+
			"pass --name or --title, or name it with --id <slug>", type_),
		Type: type_,
	}
}

// IDPrefixUndecided is a by-value id_prefix whose deciding attribute is unset
// at mint. Capture is never blocked, but an id has to come from something, and
// the bare `NNN-slug` that used to stand in for the missing prefix is gone.
func IDPrefixUndecided(type_, by string, members []string) *Located {
	return &Located{
		Code: "id_prefix_undecided",
		Message: fmt.Sprintf("Type '%s' needs --%s <%s> to mint an id; "+
			"pass it, or name the entity with --id <slug>", type_, by, strings.Join(members, "|")),
		Type:     type_,
		Relation: by,
	}
}

// SlugTaken is a slug that already names an entity of the type. An explicit
// --id is the caller's own collision; a minted one is the design working
// (minting reads no siblings, so the same title mints the same id), and the
// message names the way out.
func SlugTaken(slug, type_ string, minted bool) *Located {
	message := fmt.Sprintf("Slug '%s' is already taken in %s; choose another --id", slug, type_)
	if minted {
		message = fmt.Sprintf("Slug '%s' already exists in %s; "+
			"pass --id <slug> to name this one differently", slug, type_)
	}
	return &Located{Code: "slug_taken", Message: message, Type: type_, Target: slug}
}

// StrictUnknownField is `strict_unknown_field`: a field the type does not
// declare, refused because --strict was passed.
func StrictUnknownField(field string) *Located {
	return &Located{
		Code:    "strict_unknown_field",
		Message: fmt.Sprintf("Unknown field '%s' rejected under --strict", field),
		Target:  field,
	}
}

// EnumViolation is `enum_violation`: a value outside the attribute's enum.
func EnumViolation(value, field string, allowed []string) *Located {
	return &Located{
		Code:     "enum_violation",
		Message:  fmt.Sprintf("'%s' is not a valid %s (%s)", value, field, strings.Join(allowed, ", ")),
		Relation: field,
		Target:   value,
	}
}

// PatternViolation is `pattern_violation`: a value the attribute's pattern
// does not match.
func PatternViolation(value, field, pattern string) *Located {
	return &Located{
		Code:     "pattern_violation",
		Message:  fmt.Sprintf("'%s' does not match the pattern for %s (%s)", value, field, pattern),
		Relation: field,
		Target:   value,
	}
}

// NumberViolation is `number_violation`: a value that does not parse as a
// number.
func NumberViolation(value, field string) *Located {
	return &Located{
		Code:     "number_violation",
		Message:  fmt.Sprintf("'%s' is not a valid number for %s", value, field),
		Relation: field,
		Target:   value,
	}
}

// LookupError is `lookup_error`: no entity resolves to the id.
func LookupError(id string) *Located {
	return &Located{Code: "lookup_error", Message: fmt.Sprintf("No entity '%s' found", id), Target: id}
}

// --- query -------------------------------------------------------------------

// UnknownFilterField is `filter_error`: a `query` filter names a field the
// type does not declare.
func UnknownFilterField(field, type_ string) *Located {
	return &Located{
		Code:    "filter_error",
		Message: fmt.Sprintf("No field '%s' on type '%s'", field, type_),
		Type:    type_,
		Target:  field,
	}
}

// FTSUnavailable is `fts_unavailable`: the SQLite build has no FTS5.
func FTSUnavailable(detail string) *Located {
	return &Located{
		Code:    "fts_unavailable",
		Message: fmt.Sprintf("SQLite FTS5 is unavailable in this build (%s)", detail),
	}
}

// BadSearchQuery is `bad_search_query`: FTS5 rejected the MATCH expression.
func BadSearchQuery(text, detail string) *Located {
	return &Located{
		Code:    "bad_search_query",
		Message: fmt.Sprintf("Invalid search query '%s' (%s). Quote phrases: '\"exact phrase\"'", text, detail),
		Target:  text,
	}
}

// AmbiguousSlug is `ambiguity_error`: a bare slug names entities of more than
// one type.
func AmbiguousSlug(slug string, candidates []string) *Located {
	return &Located{
		Code:    "ambiguity_error",
		Message: fmt.Sprintf("Slug '%s' is ambiguous: %s. Qualify as type/slug", slug, strings.Join(candidates, ", ")),
		Target:  slug,
	}
}

// IllegalPredicate is `illegal_predicate`: a relation the source type does
// not declare.
func IllegalPredicate(predicate, type_ string) *Located {
	return &Located{
		Code:     "illegal_predicate",
		Message:  fmt.Sprintf("Predicate '%s' is not legal for type '%s'", predicate, type_),
		Type:     type_,
		Relation: predicate,
	}
}

// CardinalityViolation is `cardinality_violation`: `link` on a single-valued
// relation that already has a target.
func CardinalityViolation(predicate string) *Located {
	return &Located{
		Code: "cardinality_violation",
		Message: fmt.Sprintf("Predicate '%s' is single-valued; "+
			"`khub edit <id> %s <target>` replaces the current value", predicate, predicate),
		Relation: predicate,
	}
}

// InboundEdgeRefusal is `inbound_edge_refusal`: `remove` on an entity other
// entities still point at, without --force.
func InboundEdgeRefusal(type_, slug string, count int) *Located {
	phrase := "edges resolve"
	if count == 1 {
		phrase = "edge resolves"
	}
	return &Located{
		Code: "inbound_edge_refusal",
		Message: fmt.Sprintf("Refusing to remove %s '%s': %d inbound %s "+
			"to it. Pass --force to override", type_, slug, count, phrase),
		Type:   type_,
		Target: slug,
	}
}

// --- serve --------------------------------------------------------------------

// PortInUse is the `khub serve` bind refusal. Naming the corrective flag is
// the point: serve is the one command whose failure an agent cannot diagnose
// from the workspace, because the cause is another process on the machine.
func PortInUse(port int) *Located {
	return &Located{
		Code: "port_in_use",
		Message: fmt.Sprintf("Port %d is already in use. Pass --port <n> to serve "+
			"on another port", port),
	}
}

// PortNotPermitted is the bind refusal for a port this process may not have:
// ports below 1024 need privileges khub does not ask for.
func PortNotPermitted(port int) *Located {
	return &Located{
		Code: "port_not_permitted",
		Message: fmt.Sprintf("Not permitted to bind port %d; ports below 1024 need "+
			"privileges. Pass --port <n> to serve on an unprivileged port", port),
	}
}

// ServeNeedsTTY refuses `khub serve` on a non-terminal stdout.
//
// serve blocks until interrupted, so an agent that runs it in a foreground
// call hangs until its own timeout — the failure the no-prompts rule exists to
// prevent. The escape reuses the existing IsTTY precedence rather than adding
// a flag: TTY_COMPATIBLE=1 says "this caller knows the command blocks".
func ServeNeedsTTY() *Located {
	return &Located{
		Code: "serve_needs_tty",
		Message: "khub serve runs until interrupted and is a human command; " +
			"stdout is not a terminal. Set TTY_COMPATIBLE=1 to run it anyway " +
			"(background it, or an agent call will block)",
	}
}
