// Package errs ports core/errors.py: the located error taxonomy with
// byte-exact message templates. Factories build code + message together so
// the two can never drift.
package errs

import (
	"fmt"
	"path/filepath"
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

// FacetMisplaced is `invalid_schema`: an attribute declares `fields` (or
// `items`) without the type that reads it, at loc.
func FacetMisplaced(loc, facet, wantType string) *Located {
	return &Located{
		Code: "invalid_schema",
		Message: fmt.Sprintf("Invalid schema at %s.%s: Value error, '%s' applies only to type: %s; "+
			"declare type: %s or drop the key", loc, facet, facet, wantType, wantType),
	}
}

// AttributeIsRelation is `invalid_schema`: one name declared on a type as both
// an attribute and a relation, which a key on disk cannot be at once.
func AttributeIsRelation(type_, name string) *Located {
	return &Located{
		Code: "invalid_schema",
		Message: fmt.Sprintf("Invalid schema at ontology.entities.%s: '%s' is declared as both an attribute "+
			"and a relation; rename one", type_, name),
		Type: type_,
	}
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
	path = filepath.ToSlash(path) // printed: khub shows every path with '/'
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

// UpgradeUnreadable is `os_error`: upgrade copies the whole workspace into a
// candidate before changing anything, and one file could not be read.
func UpgradeUnreadable(path string) *Located {
	return &Located{
		Code: "os_error",
		Message: fmt.Sprintf("Upgrade cannot read %s, and it copies the whole workspace before changing anything; "+
			"nothing was written. Make the file readable (or move it out of the workspace) and run khub upgrade again", path),
	}
}

// ConfigMissing is a workspace whose .khub/config.yaml is gone: khub cannot
// read its preset or thresholds, and re-running init recreates only missing
// files.
func ConfigMissing() *Located {
	return &Located{
		Code: "config_missing",
		Message: ".khub/config.yaml is missing, so khub cannot read this workspace's preset or settings. " +
			"Run khub init <preset> . --force here: it recreates only missing files and keeps the schema and entities",
	}
}

// NoPreset is the upgrade guard: a workspace whose config.yaml records no
// preset has nothing to refresh from. Without it an upgrade would have to
// guess which shipped preset to lay over the workspace's own schema.
func NoPreset(path string) *Located {
	path = filepath.ToSlash(path) // printed: khub shows every path with '/'
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

// ValueShape is `value_shape`: a command-line value for a field whose value is
// an object (a mapping) or a list of them. One string cannot spell a mapping,
// and storing it would replace the object with text.
func ValueShape(value, field string, list bool) *Located {
	shape := "an object"
	if list {
		shape = "a list of objects"
	}
	return &Located{
		Code: "value_shape",
		Message: fmt.Sprintf("Field '%s' holds %s, which a command-line value cannot express; "+
			"write it in the entity file", field, shape),
		Relation: field,
		Target:   value,
	}
}

// DraftRetired is `draft_retired`: a write naming the `draft` field, which
// `status` replaced as the publish flag.
func DraftRetired() *Located {
	return &Located{
		Code: "draft_retired",
		Message: "The 'draft' field is retired; status carries the publish state. " +
			"Run `khub edit <id> status draft|stable`, or pass --draft to add",
		Target: "draft",
	}
}

// ClockPinInvalid is `clock_pin_invalid`: KHUB_PARITY_NOW holds neither form
// the clock reads, so no command runs rather than one that reads the real
// clock while the caller believes it pinned.
func ClockPinInvalid(value string) *Located {
	return &Located{
		Code: "clock_pin_invalid",
		Message: fmt.Sprintf("KHUB_PARITY_NOW '%s' is neither a date (2026-01-15) nor an offset "+
			"datetime (2026-01-15T10:00:00Z); fix or unset it", value),
		Target: value,
	}
}

// FieldShadowsFlag is `flag_shadowed`: the caller passed --<field> to a write
// verb, and the type being written declares a field of that name. The verb
// reads its own flag first, so the value would silently miss the field. Only
// the flag form is ambiguous: `khub edit <id> <field> <value>` sets the field.
// hint says how to do without the flag until the field is renamed.
func FieldShadowsFlag(type_, field, command, hint string) *Located {
	return &Located{
		Code: "flag_shadowed",
		Message: fmt.Sprintf("--%s is ambiguous for type '%s': it is both a `khub %s` flag and the "+
			"type's field '%s'. Rename the field in the schema; until then, leave --%s out (%s) "+
			"and set the field with `khub edit <id> %s <value>`",
			field, type_, command, field, field, hint, field),
		Type:   type_,
		Target: field,
	}
}

// CommitRefused is `commit_refused`: `verify --commit` could not make the
// signed commit it promises. khub never commits unsigned; the reason says what
// to fix.
func CommitRefused(reason string) *Located {
	return &Located{Code: "commit_refused", Message: "Refusing to commit: " + reason}
}

// UnknownTier is `filter_error`: a review tier khub does not know, with the
// tiers the option takes.
func UnknownTier(value string, tiers []string) *Located {
	return &Located{
		Code:    "filter_error",
		Message: fmt.Sprintf("No trust tier '%s'. Tiers: %s", value, strings.Join(tiers, ", ")),
		Target:  value,
	}
}

// NoReviewList is `invalid_schema`: `verify` on a type whose schema does not
// declare `verified` as a list of objects, so it has nowhere to record one.
func NoReviewList(type_ string) *Located {
	return &Located{
		Code:    "invalid_schema",
		Message: fmt.Sprintf("Type '%s' does not declare verified as a list of objects, so it records no review", type_),
		Type:    type_,
	}
}

// DateViolation is `date_violation`: a value that is not a valid date or
// datetime (kind) for field.
func DateViolation(value, kind, field string) *Located {
	return &Located{
		Code:     "date_violation",
		Message:  fmt.Sprintf("'%s' is not a valid %s for %s", value, kind, field),
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
// type does not declare. It lists the declared names, so the caller can
// correct the call without a second lookup.
func UnknownFilterField(field, type_ string, declared []string) *Located {
	return &Located{
		Code: "filter_error",
		Message: fmt.Sprintf("No field '%s' on type '%s'. Declared fields: %s",
			field, type_, strings.Join(declared, ", ")),
		Type:   type_,
		Target: field,
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

// AmbiguousAlias is `ambiguity_error` for an alias two entities claim.
func AmbiguousAlias(alias string, candidates []string) *Located {
	return &Located{
		Code: "ambiguity_error",
		Message: fmt.Sprintf("Alias '%s' is ambiguous: %s. Qualify as type/slug",
			alias, strings.Join(candidates, ", ")),
		Target: alias,
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

// PortNotPermitted is the bind refusal for a port this process may not have.
// The cause differs by OS (a privileged port, a range Windows reserves, a port
// another program holds exclusively), so one message names them all.
func PortNotPermitted(port int) *Located {
	return &Located{
		Code: "port_not_permitted",
		Message: fmt.Sprintf("Not permitted to bind port %d; it needs privileges, the system "+
			"reserves it, or another program holds it. Pass --port <n> to serve on another port", port),
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

// PathFieldMissing is `path_field_missing`: a templated type cannot place a
// new entity until the fields its path names are given. The message names the
// flags that supply them.
func PathFieldMissing(type_, template string, fields []string) *Located {
	flags := make([]string, len(fields))
	for i, f := range fields {
		flags[i] = "--" + f
	}
	return &Located{
		Code: "path_field_missing",
		Message: fmt.Sprintf("'%s' is stored at %s; pass %s to say where this one goes",
			type_, template, strings.Join(flags, " and ")),
		Type: type_,
	}
}

// InvalidPathValue is `invalid_path_value`: a field a templated path names
// holds a value that cannot be one directory name.
func InvalidPathValue(type_, field, value string) *Located {
	return &Located{
		Code: "invalid_path_value",
		Message: fmt.Sprintf("'%s' cannot place %s in a directory: '%s' is not a usable directory name",
			field, type_, value),
		Type:   type_,
		Target: value,
	}
}

// PathIgnored is `path_ignored`: the file a new entity would be written to is
// excluded by .khub/ignore, so no scan would ever read it back.
func PathIgnored(type_, path string) *Located {
	return &Located{
		Code: "path_ignored",
		Message: fmt.Sprintf("Refusing to add %s at %s: .khub/ignore excludes that path; "+
			"remove the pattern or store the type elsewhere", type_, path),
		Type:   type_,
		Target: path,
	}
}

// TextTooLarge is `malformed_entity`: a text document past khub's size cap.
// path is empty when the caller holds only the text.
func TextTooLarge(path string, size, limit int) *Located {
	where := "The document"
	if path != "" {
		where = path
	}
	return &Located{
		Code:    "malformed_entity",
		Message: fmt.Sprintf("%s is %d bytes, past khub's %d-byte limit for a text document", where, size, limit),
	}
}

// --- OKF export and import ---------------------------------------------------

// ExportTargetOverlaps is `export_target_overlaps`: the bundle directory is
// the workspace, inside it, or holds it, so a scan would read one as the other.
func ExportTargetOverlaps(target string) *Located {
	target = filepath.ToSlash(target)
	return &Located{
		Code: "export_target_overlaps",
		Message: fmt.Sprintf("Refusing to export into %s: it overlaps the workspace, so one would "+
			"be read as part of the other. Pass a directory outside the workspace that does not hold it", target),
		Target: target,
	}
}

// ExportTargetNotDirectory is `export_target_not_directory`: the target is a
// file.
func ExportTargetNotDirectory(target string) *Located {
	target = filepath.ToSlash(target)
	return &Located{
		Code:    "export_target_not_directory",
		Message: fmt.Sprintf("Refusing to export into %s: it is a file. Pass a directory", target),
		Target:  target,
	}
}

// ExportTargetNotEmpty is `export_target_not_empty`: the target holds files
// and --force was not passed.
func ExportTargetNotEmpty(target string) *Located {
	target = filepath.ToSlash(target)
	return &Located{
		Code: "export_target_not_empty",
		Message: fmt.Sprintf("Target %s is not empty. Pass --force to replace it, which khub does only "+
			"for a bundle it exported before", target),
		Target: target,
	}
}

// ExportTargetNotBundle is `export_target_not_bundle`: --force names a
// non-empty directory that is not a previous export, so replacing it would
// destroy files khub did not write.
func ExportTargetNotBundle(target string) *Located {
	target = filepath.ToSlash(target)
	return &Located{
		Code: "export_target_not_bundle",
		Message: fmt.Sprintf("Refusing to replace %s: it is not a bundle khub exported (no .khub-export "+
			"at its root). Export into an empty or new directory", target),
		Target: target,
	}
}

// ExportTargetUnsafe is `export_target_unsafe`: --force names a previous
// export that now holds something replacing it would delete — a workspace's
// .khub/, a repository's .git/, or a file the export did not write.
func ExportTargetUnsafe(target, path string) *Located {
	target = filepath.ToSlash(target)
	return &Located{
		Code: "export_target_unsafe",
		Message: fmt.Sprintf("Refusing to replace %s: it holds %s, which khub's export did not write "+
			"and replacing the bundle would delete. Move it out, or export into an empty or new directory",
			target, path),
		Target: target,
	}
}

// ExportPathCollision is `export_path_collision`: two entities' concepts, or
// a concept and a reserved OKF name, would share one bundle path.
func ExportPathCollision(id, path, holder string) *Located {
	return &Located{
		Code: "export_path_collision",
		Message: fmt.Sprintf("Cannot export %s: its concept path %s is already %s. "+
			"Rename one of the entities", id, path, holder),
		Target: path,
	}
}

// ImportBundleUnreadable is `import_bundle_unreadable`: the bundle is not a
// directory khub can read.
func ImportBundleUnreadable(bundle, why string) *Located {
	bundle = filepath.ToSlash(bundle)
	return &Located{
		Code:    "import_bundle_unreadable",
		Message: fmt.Sprintf("Bundle %s %s. Pass the directory an OKF bundle lives in", bundle, why),
		Target:  bundle,
	}
}

// ImportBundleOverlaps is `import_bundle_overlaps`: the bundle and the
// workspace overlap, so the workspace would read its own files as concepts.
func ImportBundleOverlaps(bundle string) *Located {
	bundle = filepath.ToSlash(bundle)
	return &Located{
		Code: "import_bundle_overlaps",
		Message: fmt.Sprintf("Refusing to import %s: the bundle and the workspace overlap, so the "+
			"workspace would read its own files as concepts. Import a bundle outside the workspace", bundle),
		Target: bundle,
	}
}

// ImportTypeMapInvalid is `import_type_map_invalid`: the --type-map file is
// unreadable, not a mapping of strings, or names a type the workspace lacks.
func ImportTypeMapInvalid(file, why string) *Located {
	file = filepath.ToSlash(file)
	return &Located{
		Code: "import_type_map_invalid",
		Message: fmt.Sprintf("Type map %s %s. It must be a YAML mapping of OKF type to a declared "+
			"khub type, one string each: `Metric: capability`", file, why),
		Target: file,
	}
}

// QuarantineTaken is `quarantine_taken`: an imported concept already carries
// a key import must write (`okf_verified`, `okf_type`, `okf_unresolved`), so
// what import quarantines there (what) would overwrite it.
func QuarantineTaken(key, what string) *Located {
	return &Located{
		Code: "quarantine_taken",
		Message: fmt.Sprintf("The concept already carries `%s`, so %s cannot be quarantined there "+
			"without overwriting it", key, what),
	}
}

// ImportWriteFailed is `import_write_failed`: a write failed after the plan
// was final, and undoing the run's other writes failed too. left lists the
// files it could not remove or restore. A run that undid everything reports
// the write's own OS error instead.
func ImportWriteFailed(path, cause string, left []string) *Located {
	path = filepath.ToSlash(path)
	return &Located{
		Code: "import_write_failed",
		Message: fmt.Sprintf("Import stopped: writing %s failed (%s), and these files it wrote could not be "+
			"removed or restored, so inspect them before retrying: %s", path, cause, strings.Join(left, ", ")),
		Target: path,
	}
}

// StatusDraftConflict is `status_draft_conflict`: `add --draft` with a
// `--status` that is not draft asks for two publish states at once.
func StatusDraftConflict(status string) *Located {
	return &Located{
		Code: "status_draft_conflict",
		Message: fmt.Sprintf("--draft writes status: draft, but --status says '%s'. "+
			"Pass one of them", status),
	}
}

// Naming is how a new slug was chosen. A refusal's advice depends on it:
// what to pass, or what to rename.
type Naming int

const (
	NamedByID Naming = iota // an explicit --id
	Minted                  // minted from the name or title
	Imported                // an imported concept's file stem
)

// advice is the corrective step a slug refusal names.
func (n Naming) advice() string {
	switch n {
	case Minted:
		return "pass --id <slug> to name this one differently"
	case Imported:
		return "rename the concept's file in the bundle, or remove the existing entity"
	default:
		return "choose another --id"
	}
}

// SlugTaken is `slug_taken`: another entity of the type holds the slug.
func SlugTaken(slug, type_ string, naming Naming) *Located {
	message := fmt.Sprintf("Slug '%s' is already taken in %s; %s", slug, type_, naming.advice())
	if naming != NamedByID {
		message = fmt.Sprintf("Slug '%s' already exists in %s; %s", slug, type_, naming.advice())
	}
	return &Located{Code: "slug_taken", Message: message, Type: type_, Target: slug}
}

// ReservedSlug is `invalid_slug`: the slug is a Windows device name.
func ReservedSlug(slug, type_ string, naming Naming) *Located {
	return &Located{Code: "invalid_slug", Message: fmt.Sprintf(
		"Slug '%s' in %s is a reserved device name on Windows; %s", slug, type_, naming.advice()),
		Type: type_, Target: slug}
}

// NavigationSlug is `invalid_slug`: the slug's file would be an OKF
// navigation file (index.md, log.md).
func NavigationSlug(slug, type_, name string, naming Naming) *Located {
	return &Located{Code: "invalid_slug", Message: fmt.Sprintf(
		"Slug '%s' in %s is reserved: %s is an OKF navigation file; %s", slug, type_, name, naming.advice()),
		Type: type_, Target: slug}
}

// AliasTaken is `alias_taken`: the slug is another entity's alias.
func AliasTaken(slug, owner string, naming Naming) *Located {
	return &Located{Code: "alias_taken", Message: fmt.Sprintf(
		"'%s' is an alias of %s; %s", slug, owner, naming.advice()), Target: slug}
}
