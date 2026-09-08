// Package errs ports core/errors.py: the located error taxonomy with
// byte-exact message templates. Factories build code + message together so
// the two can never drift (go-port-plan A.3).
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

func (e *Located) Error() string { return e.Message }

// Usage is the Click usage-error analog: plain text on stderr, exit 2.
type Usage struct{ Message string }

func (e *Usage) Error() string { return e.Message }

func New(code, message string) *Located { return &Located{Code: code, Message: message} }

// --- resolver ----------------------------------------------------------------

func UnknownTarget(type_, relation, target string) *Located {
	return &Located{
		Code:     "unknown_relation_target",
		Message:  fmt.Sprintf("Type '%s' relation '%s' targets unknown type '%s'", type_, relation, target),
		Type:     type_,
		Relation: relation,
		Target:   target,
	}
}

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

func UnknownPreset(name string, known []string) *Located {
	return &Located{
		Code:    "unknown_preset",
		Message: fmt.Sprintf("Unknown preset '%s'. Known presets: %s", name, strings.Join(known, ", ")),
		Target:  name,
	}
}

func TargetNotEmpty(path string) *Located {
	return &Located{
		Code: "target_not_empty",
		Message: fmt.Sprintf("Target %s is not empty. Pass --force to scaffold alongside the "+
			"existing files; no file already there is modified", path),
		Target: path,
	}
}

func UnknownType(name, preset string, known []string) *Located {
	return &Located{
		Code:    "unknown_type",
		Message: fmt.Sprintf("No type '%s' in the %s schema. Known types: %s", name, preset, strings.Join(known, ", ")),
		Type:    name,
	}
}

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

func BadTarget(value string) *Located {
	return &Located{
		Code:    "bad_target",
		Message: fmt.Sprintf("Unknown --target '%s'. Choose claude, agents, or both", value),
		Target:  value,
	}
}

// --- authoring ---------------------------------------------------------------

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

// IdPrefixUndecided is a by-value id_prefix whose deciding attribute is unset
// at mint. Capture is never blocked, but an id has to come from something, and
// the bare `NNN-slug` that used to stand in for the missing prefix is gone.
func IdPrefixUndecided(type_, by string, members []string) *Located {
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

func StrictUnknownField(field string) *Located {
	return &Located{
		Code:    "strict_unknown_field",
		Message: fmt.Sprintf("Unknown field '%s' rejected under --strict", field),
		Target:  field,
	}
}

func EnumViolation(value, field string, allowed []string) *Located {
	return &Located{
		Code:     "enum_violation",
		Message:  fmt.Sprintf("'%s' is not a valid %s (%s)", value, field, strings.Join(allowed, ", ")),
		Relation: field,
		Target:   value,
	}
}

func PatternViolation(value, field, pattern string) *Located {
	return &Located{
		Code:     "pattern_violation",
		Message:  fmt.Sprintf("'%s' does not match the pattern for %s (%s)", value, field, pattern),
		Relation: field,
		Target:   value,
	}
}

func NumberViolation(value, field string) *Located {
	return &Located{
		Code:     "number_violation",
		Message:  fmt.Sprintf("'%s' is not a valid number for %s", value, field),
		Relation: field,
		Target:   value,
	}
}

func LookupError(id string) *Located {
	return &Located{Code: "lookup_error", Message: fmt.Sprintf("No entity '%s' found", id), Target: id}
}

// --- query -------------------------------------------------------------------

func UnknownFilterField(field, type_ string) *Located {
	return &Located{
		Code:    "filter_error",
		Message: fmt.Sprintf("No field '%s' on type '%s'", field, type_),
		Type:    type_,
		Target:  field,
	}
}

func FTSUnavailable(detail string) *Located {
	return &Located{
		Code:    "fts_unavailable",
		Message: fmt.Sprintf("SQLite FTS5 is unavailable in this build (%s)", detail),
	}
}

func BadSearchQuery(text, detail string) *Located {
	return &Located{
		Code:    "bad_search_query",
		Message: fmt.Sprintf("Invalid search query '%s' (%s). Quote phrases: '\"exact phrase\"'", text, detail),
		Target:  text,
	}
}

func AmbiguousSlug(slug string, candidates []string) *Located {
	return &Located{
		Code:    "ambiguity_error",
		Message: fmt.Sprintf("Slug '%s' is ambiguous: %s. Qualify as type/slug", slug, strings.Join(candidates, ", ")),
		Target:  slug,
	}
}

func IllegalPredicate(predicate, type_ string) *Located {
	return &Located{
		Code:     "illegal_predicate",
		Message:  fmt.Sprintf("Predicate '%s' is not legal for type '%s'", predicate, type_),
		Type:     type_,
		Relation: predicate,
	}
}

func CardinalityViolation(predicate string) *Located {
	return &Located{
		Code: "cardinality_violation",
		Message: fmt.Sprintf("Predicate '%s' is single-valued; "+
			"`khub edit <id> %s <target>` replaces the current value", predicate, predicate),
		Relation: predicate,
	}
}

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
