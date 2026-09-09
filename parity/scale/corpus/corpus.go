// Package corpus writes a build-hub workspace at scale — a few hundred entity
// files — through the real khub binary, and computes an independent
// ground-truth manifest for it.
//
// Ported from kb 0.14.0 `projects/gen_corpus.py`. The unit suite proves khub's
// rules on corpora of a dozen files and the parity fixtures pin bytes on about
// the same; neither says anything about a corpus the size of a real project's,
// where every command rescans every file and the interesting failures are the
// ones that only appear at n: a walk that never terminates, an index that
// silently drops entities, a scan whose cost is quadratic.
//
// Every file here is authored by `khub add`, driven out of process with its
// relations passed as `--<predicate> <id>`, so nothing bypasses the schema: if
// the generator asks for something the ontology forbids, `add` refuses and the
// build fails. Ids are taken from `add`'s JSON output, never predicted. The
// closing `khub check` is the proof that what came out is a legal corpus.
//
// The manifest is computed from this package's OWN model of the graph — an
// independent implementation of "orphan", "transitive closure", "one hop",
// "stale" — so a disagreement between it and khub is a real finding rather
// than khub agreeing with itself. It reads nothing back out of khub except
// declared configuration (the type list, the per-type `orphan` exemption),
// which is authored input, not a computed answer.
//
// Deterministic: the same seed, scale and today produce byte-identical files.
package corpus

import (
	"bytes"
	// parity/scale is a dev tool, not a khub output path. The jsonio choke
	// point (.claude/rules/go.md) governs the bytes khub itself emits, and
	// .golangci.yml's depguard covers only YAML libraries — the same position
	// parity/yamlgate/emit takes.
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Needle is planted in exactly one requirement body and appears nowhere else
// in the vocabulary below, so `khub search` has a query whose expected result
// set is a single known id.
const Needle = "xylotrope"

// ManifestName is the file written beside the workspace's `.khub/`.
const ManifestName = "smoke-manifest.json"

// Preset is the only preset this generator knows how to fill.
const Preset = "build-hub"

// ------------------------------------------------------------------ vocabulary
//
// Pools rather than prose: titles are drawn from the shuffled cartesian product,
// which is what guarantees they are unique. Two entities of one type with the
// same title mint the same id, and `add` refuses the second — correct behaviour
// that would only look like a generator bug here.

var areas = []string{
	"ledger", "payments", "payouts", "billing", "invoicing", "reconciliation",
	"treasury", "fx", "compliance", "kyc", "fraud", "notifications", "reporting",
	"onboarding", "settlement", "disputes", "webhooks", "identity", "audit",
	"pricing", "tax", "wallet", "cards", "transfers", "statements",
}

var repoKinds = []string{"api", "worker", "ui", "sdk", "cli", "service", "jobs", "gateway", "console", "lib"}

// Wide enough that the product with areas covers the service share past
// scale 2000. `titles` fails rather than truncating when it runs out, so a
// short pool is a loud failure.
var componentRoles = []string{
	"Service", "Gateway", "Worker", "Indexer", "Store", "Cache", "Scheduler",
	"Publisher", "Adapter", "Projector", "Router", "Reconciler", "Ingestor",
	"Exporter", "Validator", "Dispatcher",
}

var libraryRoles = []string{"Client", "Kit", "Schema", "Contracts", "Helpers", "Types"}

type vendor struct{ name, stack string }

var vendors = []vendor{
	{"Stripe", "Stripe"}, {"Adyen", "Adyen"}, {"Plaid", "Plaid"}, {"Twilio", "Twilio"},
	{"SendGrid", "SendGrid"}, {"Datadog", "Datadog"}, {"Snowflake", "Snowflake"},
	{"Auth0", "Auth0"}, {"Okta", "Okta"}, {"PagerDuty", "PagerDuty"},
	{"Cloudflare", "Cloudflare"}, {"Segment", "Segment"}, {"Sentry", "Sentry"},
	{"HashiCorp Vault", "Vault"}, {"Confluent Kafka", "Kafka"}, {"AWS S3", "S3"},
	{"AWS KMS", "KMS"}, {"Onfido", "Onfido"}, {"Sardine", "Sardine"}, {"Modern Treasury", "Modern Treasury"},
	{"Currencycloud", "Currencycloud"}, {"Wise Platform", "Wise"}, {"Marqeta", "Marqeta"},
	{"Unit21", "Unit21"}, {"ComplyAdvantage", "ComplyAdvantage"},
}

var stacks = []string{"Go", "Python", "TypeScript", "Kotlin", "Rust", "Java", "Elixir"}

var teams = []string{"Money Movement", "Core Ledger", "Risk", "Platform", "Growth", "Data", "Trust and Safety"}

var reqVerbs = []string{
	"Post", "Settle", "Reconcile", "Reject", "Retry", "Expire", "Authorise", "Capture",
	"Refund", "Batch", "Sign", "Redact", "Archive", "Replay", "Throttle", "Escalate",
	"Notify", "Reverse",
}

var reqObjects = []string{
	"ledger entries", "card authorisations", "payout batches", "invoice drafts",
	"settlement files", "dispute evidence", "webhook deliveries", "KYC reviews",
	"sanction screenings", "fee schedules", "FX quotes", "wallet transfers",
	"statement exports", "audit records", "refund requests", "onboarding applications",
	"tax filings", "chargeback notices", "balance snapshots", "payment intents",
}

var reqQualifiers = []string{
	"idempotently", "within one business day", "under peak load", "without manual review",
	"in the customer's currency", "before the cutoff", "with a full audit trail",
	"on a single attempt", "across regions", "in strict order", "without data loss",
	"under a signed contract", "at least once", "exactly once",
}

var adrMoves = []string{"Use", "Adopt", "Standardise on", "Retire", "Split out", "Consolidate on", "Defer"}

var adrSubjects = []string{
	"Postgres", "Kafka", "an outbox table", "gRPC", "OpenAPI", "a shared client",
	"per-tenant schemas", "event sourcing", "a read replica", "server-side pagination",
	"signed webhooks", "a feature flag service", "blue-green deploys", "OpenTelemetry",
	"a monorepo", "row-level security", "the saga pattern", "idempotency keys",
}

var adrEnds = []string{
	"for the ledger", "for payouts", "at the API edge", "in the risk pipeline",
	"for reporting", "across services", "for the reconciliation job", "for tenant isolation",
	"in the settlement path", "for the notification fan-out",
}

// Tags is the tag pool; the manifest counts every one of them.
var Tags = []string{
	"ledger", "payments", "risk", "compliance", "platform", "data", "security",
	"performance", "billing", "onboarding",
}

var reqKinds = []string{"functional", "non-functional", "constraint", "business-rule"}

var adrStatuses = []string{"accepted", "accepted", "accepted", "proposed", "rejected"}

// ---------------------------------------------------------------- khub driver

// khub drives one binary against one workspace, out of process. Every call
// pins the clock through KHUB_PARITY_NOW — the same seam the parity recorder
// uses — so a run on any day writes the same bytes.
type khub struct {
	bin  string
	root string
	env  []string
}

func newKhub(bin, root string, today time.Time) *khub {
	env := append(os.Environ(), "KHUB_PARITY_NOW="+today.Format("2006-01-02"))
	return &khub{bin: bin, root: root, env: env}
}

// run executes `khub -C <root> <args>` and returns stdout. A non-zero exit is
// an error carrying the command and everything it printed.
func (k *khub) run(args ...string) ([]byte, error) {
	cmd := exec.Command(k.bin, append([]string{"-C", k.root}, args...)...)
	cmd.Env = k.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		code := -1
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return nil, fmt.Errorf("khub %s exited %d (wanted 0)\n%s%s",
			strings.Join(args, " "), code, stdout.String(), stderr.String())
	}
	return stdout.Bytes(), nil
}

// field is one `--name value` pair; an empty value is not passed at all.
type field struct{ name, value string }

// add creates one entity and returns the slug khub minted for it — never one
// this package guessed.
func (k *khub) add(typ string, fields ...field) (string, error) {
	args := []string{"add", typ, "--format", "json"}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		if f.name == "draft" {
			args = append(args, "--draft")
			continue
		}
		args = append(args, "--"+f.name, f.value)
	}
	out, err := k.run(args...)
	if err != nil {
		return "", err
	}
	var rec struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out, &rec); err != nil {
		return "", fmt.Errorf("khub add %s: unreadable record %q: %w", typ, out, err)
	}
	if rec.Slug == "" {
		return "", fmt.Errorf("khub add %s: record carries no slug: %s", typ, out)
	}
	return rec.Slug, nil
}

// ------------------------------------------------------------------ the world

type edge struct{ predicate, target string }

type dates struct{ created, updated time.Time }

// world is the corpus under construction plus this package's own model of its
// graph. `edges` is the only thing the manifest is computed from; it is
// deliberately never read back out of khub.
//
// Every map is iterated through `order`, never ranged directly: the manifest
// is a file format and a map walk leaking into it is exactly the bug the
// determinism assertion exists to catch.
type world struct {
	r      *rand.Rand
	today  time.Time
	order  []string
	types  map[string]string
	edges  map[string][]edge
	dates  map[string]dates
	tags   map[string][]string
	titles map[string]string
	drafts map[string]bool
	// inbound is built once by index(); nil until then.
	inbound map[string][]edge
}

func newWorld(r *rand.Rand, today time.Time) *world {
	return &world{
		r: r, today: today,
		types: map[string]string{}, edges: map[string][]edge{}, dates: map[string]dates{},
		tags: map[string][]string{}, titles: map[string]string{}, drafts: map[string]bool{},
	}
}

func (w *world) born(slug, typ string, out []edge, d dates, tags []string, draft bool, title string) {
	w.order = append(w.order, slug)
	w.types[slug] = typ
	w.edges[slug] = out
	w.dates[slug] = d
	w.tags[slug] = tags
	w.titles[slug] = title
	if draft {
		w.drafts[slug] = true
	}
}

// age is a plausible created/updated pair: a quarter recent, the rest
// trailing back up to about three years. Skewed on purpose — a corpus where
// everything was written today makes `stale` and `status` answer nothing.
func (w *world) age() dates {
	roll := w.r.Float64()
	var days int
	switch {
	case roll < 0.25:
		days = w.r.IntN(31)
	case roll < 0.60:
		days = 31 + w.r.IntN(150)
	default:
		days = 181 + w.r.IntN(820)
	}
	created := w.today.AddDate(0, 0, -days)
	updated := created.AddDate(0, 0, w.r.IntN(min(days, 150)+1))
	return dates{created: created, updated: updated}
}

// sample draws n distinct elements of pool, sorted.
func (w *world) sample(pool []string, n int) []string {
	idx := w.r.Perm(len(pool))[:n]
	out := make([]string, n)
	for i, j := range idx {
		out[i] = pool[j]
	}
	slices.Sort(out)
	return out
}

func (w *world) pick(pool []string) string { return pool[w.r.IntN(len(pool))] }

// -- the model the manifest is computed from --------------------------------

func (w *world) index() {
	w.inbound = map[string][]edge{}
	for _, src := range w.order {
		for _, e := range w.edges[src] {
			w.inbound[e.target] = append(w.inbound[e.target], edge{predicate: e.predicate, target: src})
		}
	}
}

// outOf is the distinct targets of slug's out-edges, optionally on one predicate.
func (w *world) outOf(slug, predicate string) []string {
	return distinct(w.edges[slug], predicate)
}

// into is the distinct sources of edges into slug, optionally on one predicate.
func (w *world) into(slug, predicate string) []string {
	return distinct(w.inbound[slug], predicate)
}

func distinct(edges []edge, predicate string) []string {
	var out []string
	for _, e := range edges {
		if predicate == "" || e.predicate == predicate {
			out = append(out, e.target)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// closure is the transitive set reachable from slug over one predicate,
// forward (what it points at) or reverse (what points at it), excluding slug.
func (w *world) closure(slug, predicate string, reverse bool) []string {
	seen := map[string]bool{}
	var found []string
	frontier := []string{slug}
	for len(frontier) > 0 {
		var next []string
		for _, node := range frontier {
			var step []string
			if reverse {
				step = w.into(node, predicate)
			} else {
				step = w.outOf(node, predicate)
			}
			for _, other := range step {
				if other != slug && !seen[other] {
					seen[other] = true
					found = append(found, other)
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	slices.Sort(found)
	return found
}

// oneHop is the distinct union of in- and out-neighbours.
func (w *world) oneHop(slug string) []string {
	both := append(w.outOf(slug, ""), w.into(slug, "")...)
	slices.Sort(both)
	return slices.Compact(both)
}

// orphans is khub's rule, reimplemented: no edge out and none in, and the type
// is not declared exempt. Every edge this package writes resolves — `add`
// refuses a dangling target — so "resolved" needs no special case.
func (w *world) orphans(exempt map[string]bool) []string {
	out := []string{}
	for _, slug := range w.order {
		if exempt[w.types[slug]] {
			continue
		}
		if len(w.edges[slug]) == 0 && len(w.inbound[slug]) == 0 {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return out
}

// stale counts entities whose `updated` is more than days old — khub's
// `age > threshold` (internal/gitlog/stale.go).
func (w *world) stale(days int) int {
	n := 0
	for _, slug := range w.order {
		if daysBetween(w.dates[slug].updated, w.today) > days {
			n++
		}
	}
	return n
}

func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

// ------------------------------------------------------------------ generation

// titles draws count unique titles from the shuffled product of the pools.
func titles(r *rand.Rand, count int, join func([]string) string, pools ...[]string) ([]string, error) {
	parts := [][]string{nil}
	for _, pool := range pools {
		var next [][]string
		for _, p := range parts {
			for _, item := range pool {
				next = append(next, append(slices.Clone(p), item))
			}
		}
		parts = next
	}
	if len(parts) < count {
		return nil, fmt.Errorf("vocabulary holds %d titles, need %d", len(parts), count)
	}
	combos := make([]string, len(parts))
	for i, p := range parts {
		combos[i] = join(p)
	}
	r.Shuffle(len(combos), func(i, j int) { combos[i], combos[j] = combos[j], combos[i] })
	return combos[:count], nil
}

// Counts is the per-type mix from one knob; requirements take the remainder
// so the total is exact. Exported so the bench can name a corpus before it
// builds one.
type Counts struct{ Repo, Component, Adr, Requirement int }

// Plan returns the mix for a scale, or an error when the scale cannot hold a
// corpus.
func Plan(scale int) (Counts, error) {
	c := Counts{
		Repo:      max(6, roundInt(float64(scale)*0.08)),
		Component: max(12, roundInt(float64(scale)*0.20)),
		Adr:       max(8, roundInt(float64(scale)*0.24)),
	}
	c.Requirement = scale - 2 - c.Repo - c.Component - c.Adr // 2 = the prd/arc42 singletons
	if c.Requirement < 10 {
		return c, fmt.Errorf("scale %d is too small to hold a corpus", scale)
	}
	return c, nil
}

func roundInt(f float64) int { return int(f + 0.5) }

func titleCase(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func (w *world) makeRepos(k *khub, count int) ([]string, error) {
	names, err := titles(w.r, count, func(p []string) string { return p[0] + "-" + p[1] }, areas, repoKinds)
	if err != nil {
		return nil, err
	}
	var out []string
	for i, name := range names {
		// A tenth are archived — a real registry has entries nothing moves in.
		archived := i%10 == 9
		d := w.age()
		tags := w.sample(Tags, 2)
		status := "active"
		if archived {
			status = "archived"
		}
		slug, err := k.add("repo",
			field{"title", titleCase(name)},
			field{"repo", "acme/" + name},
			field{"status", status},
			field{"tags", strings.Join(tags, ",")},
			field{"created", iso(d.created)},
			field{"updated", iso(d.updated)},
			field{"body", repoBody(name, archived)},
		)
		if err != nil {
			return nil, err
		}
		w.born(slug, "repo", nil, d, tags, false, name)
		out = append(out, slug)
	}
	return out, nil
}

// makeComponents writes externals first, then libraries, then services.
// `depends_on` is declared acyclic, and the cheapest way to guarantee a DAG is
// to only ever point backwards in this list. It is also the truth: a service
// depends on a library depends on a vendor, not the other way round.
func (w *world) makeComponents(k *khub, count int, repos []string) ([]string, error) {
	// Capped at the vendor list, with the remainder handed to services: every
	// external carries a real vendor's name and there are only so many.
	externals := min(len(vendors), max(2, roundInt(float64(count)*0.18)))
	libraries := max(2, roundInt(float64(count)*0.30))
	services := count - externals - libraries
	ours := repos[:max(0, len(repos)-4)] // the last four are left unreferenced on purpose
	if len(ours) == 0 {
		ours = repos
	}
	joinTitle := func(p []string) string { return titleCase(p[0]) + " " + p[1] }
	libTitles, err := titles(w.r, libraries, joinTitle, areas, libraryRoles)
	if err != nil {
		return nil, err
	}
	svcTitles, err := titles(w.r, services, joinTitle, areas, componentRoles)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, v := range vendors[:externals] {
		d := w.age()
		tags := w.sample(Tags, 2)
		slug, err := k.add("component",
			field{"title", v.name}, field{"kind", "external"}, field{"stack", v.stack},
			field{"tags", strings.Join(tags, ",")},
			field{"created", iso(d.created)}, field{"updated", iso(d.updated)},
			field{"body", componentBody(v.name, "external", v.stack, "")},
		)
		if err != nil {
			return nil, err
		}
		w.born(slug, "component", nil, d, tags, false, v.name)
		out = append(out, slug)
	}

	type tier struct {
		kind  string
		names []string
		fan   int
	}
	for _, t := range []tier{{"library", libTitles, 2}, {"service", svcTitles, 3}} {
		for _, name := range t.names {
			d := w.age()
			tags := w.sample(Tags, 2)
			stack := w.pick(stacks)
			team := w.pick(teams)
			repo := w.pick(ours)
			var deps []string
			if len(out) > 0 {
				deps = w.sample(out, min(len(out), w.r.IntN(t.fan+1)))
			}
			draft := w.r.Float64() < 0.06
			slug, err := k.add("component",
				field{"title", name}, field{"kind", t.kind}, field{"stack", stack}, field{"repo", repo},
				field{"depends_on", strings.Join(deps, ",")},
				field{"tags", strings.Join(tags, ",")},
				field{"draft", boolFlag(draft)},
				field{"created", iso(d.created)}, field{"updated", iso(d.updated)},
				field{"body", componentBody(name, t.kind, stack, team)},
			)
			if err != nil {
				return nil, err
			}
			edges := []edge{{"repo", repo}}
			for _, dep := range deps {
				edges = append(edges, edge{"depends_on", dep})
			}
			w.born(slug, "component", edges, d, tags, draft, name)
			out = append(out, slug)
		}
	}
	return out, nil
}

// makeRequirements writes requirements with one in eight deliberately
// unlinked. An unlinked requirement is an orphan gap and that is the design —
// `realized_in` is the only edge tying a rule to the code that satisfies it.
// The unlinked set is returned so makeAdrs can avoid pointing at them, which
// would resolve the orphan and quietly delete the gap this corpus exists to
// exhibit.
func (w *world) makeRequirements(k *khub, count int, components []string) (ids []string, unlinked map[string]bool, needleID string, err error) {
	names, err := titles(w.r, count, func(p []string) string { return p[0] + " " + p[1] + " " + p[2] },
		reqVerbs, reqObjects, reqQualifiers)
	if err != nil {
		return nil, nil, "", err
	}
	unlinked = map[string]bool{}
	needleAt := w.r.IntN(count)
	for i, name := range names {
		d := w.age()
		tags := w.sample(Tags, 1+w.r.IntN(3))
		kind := reqKinds[i%len(reqKinds)]
		bare := i%8 == 5
		var realized []string
		if !bare {
			realized = w.sample(components, 1+w.r.IntN(3))
		}
		draft := w.r.Float64() < 0.08
		body := w.requirementBody(name, kind, i == needleAt)
		slug, err := k.add("requirement",
			field{"title", name}, field{"kind", kind},
			field{"realized_in", strings.Join(realized, ",")},
			field{"tags", strings.Join(tags, ",")},
			field{"draft", boolFlag(draft)},
			field{"created", iso(d.created)}, field{"updated", iso(d.updated)},
			field{"body", body},
		)
		if err != nil {
			return nil, nil, "", err
		}
		var edges []edge
		for _, c := range realized {
			edges = append(edges, edge{"realized_in", c})
		}
		w.born(slug, "requirement", edges, d, tags, draft, name)
		ids = append(ids, slug)
		if bare {
			unlinked[slug] = true
		}
		if i == needleAt {
			needleID = slug
		}
	}
	return ids, unlinked, needleID, nil
}

// makeAdrs writes the dated type. `--created` is passed so the date in the
// minted id is the date the decision was taken rather than the day the
// generator ran — which is what keeps the corpus reproducible on any day.
// One deliberate supersession chain of known length comes first; scattered
// pairs after it are kept off the chain so its expected length stays knowable.
func (w *world) makeAdrs(k *khub, count int, targets []string) (ids, chain []string, err error) {
	names, err := titles(w.r, count, func(p []string) string { return p[0] + " " + p[1] + " " + p[2] },
		adrMoves, adrSubjects, adrEnds)
	if err != nil {
		return nil, nil, err
	}
	chainLen := min(7, max(3, count/12))
	for i, name := range names {
		d := w.age()
		tags := w.sample(Tags, 2)
		status := adrStatuses[i%len(adrStatuses)]
		affects := w.sample(targets, 1+w.r.IntN(4))
		supersedes := ""
		switch {
		case i < chainLen && len(chain) > 0:
			supersedes = chain[len(chain)-1]
		case i >= chainLen && len(ids) > chainLen && w.r.Float64() < 0.25:
			supersedes = w.pick(ids[chainLen:])
		}
		slug, err := k.add("adr",
			field{"title", name}, field{"status", status},
			field{"affects", strings.Join(affects, ",")},
			field{"supersedes", supersedes},
			field{"tags", strings.Join(tags, ",")},
			field{"created", iso(d.created)}, field{"updated", iso(d.updated)},
			field{"body", w.adrBody(name, status)},
		)
		if err != nil {
			return nil, nil, err
		}
		var edges []edge
		for _, t := range affects {
			edges = append(edges, edge{"affects", t})
		}
		if supersedes != "" {
			edges = append(edges, edge{"supersedes", supersedes})
		}
		w.born(slug, "adr", edges, d, tags, false, name)
		ids = append(ids, slug)
		if i < chainLen {
			chain = append(chain, slug)
		}
	}
	return ids, chain, nil
}

// -------------------------------------------------------------------- bodies
//
// Prose that satisfies the build-hub templates for the six types kb 0.14.0 ships
// (requirement: no headings; component and repo: optional headings only; adr:
// Context/Decision/Consequences with word-count floors of 25/15/20) so a
// generated corpus is legal and not thin. None of it contains the needle.

func repoBody(name string, archived bool) string {
	state := "Active."
	if archived {
		state = "Archived — read-only, kept for audit."
	}
	return fmt.Sprintf("`acme/%s` on the internal GitLab. %s\n", name, state)
}

func componentBody(title, kind, stack, team string) string {
	if kind == "external" {
		return fmt.Sprintf("%s is a vendor we sit on: it is not ours to change, only to "+
			"react to. Integration surface is %s's public API; the contract "+
			"lives behind their versioning, not ours.\n", title, stack)
	}
	return fmt.Sprintf("%s is a %s written in %s, owned by %s. It is the unit "+
		"that gets paged, deployed and rolled back on its own.\n", title, kind, stack, team)
}

func (w *world) requirementBody(title, kind string, needle bool) string {
	subject := w.pick([]string{"the ledger", "the payments API", "the payout worker",
		"the reconciliation job", "the risk pipeline"})
	trigger := w.pick([]string{"a request arrives", "the cutoff passes", "a webhook is received",
		"a batch is opened", "a review is queued"})
	lines := []string{fmt.Sprintf("When %s, %s shall %s.", trigger, subject, lowerFirst(title))}
	switch kind {
	case "non-functional":
		p99 := w.pick([]string{"120", "250", "400", "800"})
		lines = append(lines, "", fmt.Sprintf("Target: p99 under %sms, measured at the "+
			"edge over a rolling hour. Enforced by the SLO alert, not by review.", p99))
	case "constraint":
		lines = append(lines, "", "This is an invariant: no path may violate it, including manual "+
			"operator action and backfills.")
	default:
		lines = append(lines, "", w.pick([]string{
			"Stated by the payments lead during the settlement review.",
			"Carried over from the incumbent system; the auditors read it every quarter.",
			"Agreed with the risk team when the vendor contract was signed.",
		}))
	}
	if needle {
		lines = append(lines, "", "Codename for the pilot: "+Needle+".")
	}
	return strings.Join(lines, "\n") + "\n"
}

func (w *world) adrBody(title, status string) string {
	rejected := w.pick([]string{"a queue per tenant", "a nightly batch", "a third-party SaaS",
		"doing nothing", "a shared database"})
	return "## Context\n\n" +
		fmt.Sprintf("The team hit the same question twice in a quarter, and %s "+
			"was the shape the answer kept taking. The alternative on the table was "+
			"%s, and it lost on cost: cheaper to start with and dearer to keep, "+
			"once the second team needed the same thing.\n\n", strings.ToLower(title), rejected) +
		"## Decision\n\n" +
		fmt.Sprintf("Status is %s. We %s, and every new service "+
			"in this area follows it without re-litigating the choice.\n\n", status, lowerFirst(title)) +
		"## Consequences\n\n" +
		fmt.Sprintf("One more thing to operate, and one less thing to argue about. Revisit if the "+
			"volume grows past the point where %s would have been cheaper, or when the "+
			"vendor contract comes up for renewal.\n", rejected)
}

func iso(t time.Time) string { return t.Format("2006-01-02") }

func boolFlag(b bool) string {
	if b {
		return "true"
	}
	return ""
}

// ------------------------------------------------------------------- manifest

// Manifest is the ground truth `smoke.sh` and the bench read. Field order is
// the JSON key order; every map is emitted with sorted keys by encoding/json.
type Manifest struct {
	Generated Generated      `json:"generated"`
	Total     int            `json:"total"`
	PerType   map[string]int `json:"per_type"`
	Orphans   []string       `json:"orphans"`
	Drafts    int            `json:"drafts"`
	// Requirements written without `realized_in` — the orphan gap by design.
	MissingRealizedIn int            `json:"missing_realized_in"`
	Stale             map[string]int `json:"stale"`
	Tags              map[string]int `json:"tags"`
	Closures          []Closure      `json:"closures"`
	Degrees           []Degree       `json:"degrees"`
	History           History        `json:"history"`
	Search            Search         `json:"search"`
	Probes            Probes         `json:"probes"`
}

type Generated struct {
	Scale       int    `json:"scale"`
	Seed        uint64 `json:"seed"`
	Today       string `json:"today"`
	Preset      string `json:"preset"`
	KhubVersion string `json:"khub_version"`
}

// Closure is one root's transitive `depends_on` reach in both directions:
// `impact <id>` should reach Forward others, `impact <id> --reverse` Reverse.
type Closure struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Predicate string `json:"predicate"`
	Forward   int    `json:"forward"`
	Reverse   int    `json:"reverse"`
}

// Degree is one node's one-hop neighbourhood: distinct sources in, distinct
// targets out, and the distinct union.
type Degree struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	In     int    `json:"in"`
	Out    int    `json:"out"`
	OneHop int    `json:"one_hop"`
}

// History describes the deliberate supersession chain. `history <newest>`
// walks `supersedes` forward and should reach Chain-1 others; from Middle it
// reaches MiddleReaches.
type History struct {
	Newest        string `json:"newest"`
	Middle        string `json:"middle"`
	Chain         int    `json:"chain"`
	MiddleReaches int    `json:"middle_reaches"`
}

type Search struct {
	Needle string `json:"needle"`
	ID     string `json:"id"`
}

// Probes are handles for the mutation cases, so the smoke script never spells
// an id or a title of its own.
type Probes struct {
	// CycleFrom already depends on CycleTo; linking the reverse closes a loop
	// over an acyclic predicate.
	CycleFrom string `json:"cycle_from"`
	CycleTo   string `json:"cycle_to"`
	// Something points at HeldComponent: `remove` must refuse.
	HeldComponent string `json:"held_component"`
	// Minting TakenTitle again in TakenType must be a refusal.
	TakenTitle string `json:"taken_title"`
	TakenType  string `json:"taken_type"`
	// A component, so `realized_in` accepts it.
	LinkTarget string `json:"link_target"`
}

func (w *world) manifest(scale int, seed uint64, version string, typeNames []string, exempt map[string]bool,
	chain []string, unlinked map[string]bool, needleID string) *Manifest {
	w.index()

	perType := map[string]int{}
	for _, t := range typeNames {
		perType[t] = 0
	}
	for _, slug := range w.order {
		perType[w.types[slug]]++
	}

	tagCounts := map[string]int{}
	for _, tag := range Tags {
		tagCounts[tag] = 0
	}
	for _, slug := range w.order {
		for _, tag := range w.tags[slug] {
			tagCounts[tag]++
		}
	}

	// Roots for the closures: the hub (most ancestors), the deepest (most
	// descendants), and a node with both. First in creation order wins a tie.
	hub, deepest, mid := "", "", ""
	hubN, deepN := -1, -1
	for _, slug := range w.order {
		rev := len(w.closure(slug, "depends_on", true))
		fwd := len(w.closure(slug, "depends_on", false))
		if rev > hubN {
			hub, hubN = slug, rev
		}
		if fwd > deepN {
			deepest, deepN = slug, fwd
		}
		if mid == "" && rev > 0 && fwd > 0 {
			mid = slug
		}
	}
	if mid == "" {
		mid = hub
	}
	closures := []Closure{}
	for _, root := range []struct{ id, role string }{{hub, "hub"}, {deepest, "deepest"}, {mid, "middle"}} {
		closures = append(closures, Closure{
			ID: root.id, Role: root.role, Predicate: "depends_on",
			Forward: len(w.closure(root.id, "depends_on", false)),
			Reverse: len(w.closure(root.id, "depends_on", true)),
		})
	}

	busiest, busiestN := "", -1
	held := ""
	for _, slug := range w.order {
		if n := len(w.oneHop(slug)); n > busiestN {
			busiest, busiestN = slug, n
		}
		if held == "" && w.types[slug] == "component" && len(w.inbound[slug]) > 0 {
			held = slug
		}
	}
	degrees := []Degree{}
	for _, node := range []struct{ id, role string }{{busiest, "busiest"}, {held, "held component"}, {chain[len(chain)-1], "newest adr"}} {
		degrees = append(degrees, Degree{
			ID: node.id, Role: node.role,
			In: len(w.into(node.id, "")), Out: len(w.outOf(node.id, "")), OneHop: len(w.oneHop(node.id)),
		})
	}

	middle := chain[len(chain)/2]
	cycleFrom, cycleTo := "", ""
	for _, slug := range w.order {
		if deps := w.outOf(slug, "depends_on"); len(deps) > 0 {
			cycleFrom, cycleTo = slug, deps[0]
			break
		}
	}
	taken := ""
	for _, slug := range w.order {
		if w.types[slug] == "requirement" {
			taken = slug
			break
		}
	}

	return &Manifest{
		Generated:         Generated{Scale: scale, Seed: seed, Today: iso(w.today), Preset: Preset, KhubVersion: version},
		Total:             len(w.order),
		PerType:           perType,
		Orphans:           w.orphans(exempt),
		Drafts:            len(w.drafts),
		MissingRealizedIn: len(unlinked),
		Stale: map[string]int{
			"30": w.stale(30), "90": w.stale(90), "365": w.stale(365), "3650": w.stale(3650),
		},
		Tags:     tagCounts,
		Closures: closures,
		Degrees:  degrees,
		History: History{
			Newest: chain[len(chain)-1], Middle: middle, Chain: len(chain),
			MiddleReaches: len(w.closure(middle, "supersedes", false)),
		},
		Search: Search{Needle: Needle, ID: needleID},
		Probes: Probes{
			CycleFrom: cycleFrom, CycleTo: cycleTo,
			HeldComponent: held,
			TakenTitle:    w.titles[taken], TakenType: "requirement",
			LinkTarget: held,
		},
	}
}

// ------------------------------------------------------------------------ build

// Options is one generation request.
type Options struct {
	Bin   string    // the khub binary to drive
	Work  string    // the workspace to write; replaced if it holds a manifest or is empty
	Scale int       // total entities, singletons included
	Seed  uint64    // the PRNG seed
	Today time.Time // pins every date and the clock khub sees
	Log   func(format string, args ...any)
}

// looksGenerated is the guard on deleting: only ever remove something this
// package wrote, or an empty directory — never an arbitrary path somebody
// passed by mistake.
func looksGenerated(path string) (bool, error) {
	if _, err := os.Stat(filepath.Join(path, ManifestName)); err == nil {
		return true, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// Build writes the corpus and its manifest, then proves it legal with a
// closing `khub check`.
func Build(o Options) (*Manifest, error) {
	say := o.Log
	if say == nil {
		say = func(string, ...any) {}
	}
	work, err := filepath.Abs(o.Work)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(work); err == nil {
		ok, err := looksGenerated(work)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s exists and was not written by this generator — refusing to delete it (no %s inside)", work, ManifestName)
		}
		if err := os.RemoveAll(work); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	plan, err := Plan(o.Scale)
	if err != nil {
		return nil, err
	}

	r := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
	k := newKhub(o.Bin, work, o.Today)
	w := newWorld(r, o.Today)

	versionOut, err := exec.Command(o.Bin, "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("%s --version: %w", o.Bin, err)
	}
	version := strings.TrimSpace(string(versionOut))
	say("khub %s -> %s", version, work)

	if _, err := k.run("init", Preset, work, "--format", "json"); err != nil {
		return nil, err
	}
	// The singletons `init` scaffolded are entities like any other: they carry
	// dates, they count towards the total, and they are exempt from the orphan
	// sweep by schema, not by omission. Their scaffolded bodies stay — a
	// template's own scaffold satisfies every rule it declares — and only their
	// dates are backdated, through the same `edit` an author would use.
	for _, single := range []string{"prd", "arc42"} {
		d := w.age()
		if _, err := k.run("edit", single, "--created", iso(d.created), "--updated", iso(d.updated), "--format", "json"); err != nil {
			return nil, err
		}
		w.born(single, single, nil, d, nil, false, "")
	}

	say("  repos        %d", plan.Repo)
	repos, err := w.makeRepos(k, plan.Repo)
	if err != nil {
		return nil, err
	}
	say("  components   %d", plan.Component)
	components, err := w.makeComponents(k, plan.Component, repos)
	if err != nil {
		return nil, err
	}
	say("  requirements %d", plan.Requirement)
	requirements, unlinked, needleID, err := w.makeRequirements(k, plan.Requirement, components)
	if err != nil {
		return nil, err
	}
	say("  decisions    %d", plan.Adr)
	linkable := slices.Concat(components, repos)
	for _, req := range requirements {
		if !unlinked[req] {
			linkable = append(linkable, req)
		}
	}
	_, chain, err := w.makeAdrs(k, plan.Adr, linkable)
	if err != nil {
		return nil, err
	}

	if _, err := k.run("reindex"); err != nil {
		return nil, err
	}

	// Declared configuration, read from khub because it is authored input the
	// generator must agree with, not an answer it is checking.
	typeNames, exempt, err := schemaTypes(k)
	if err != nil {
		return nil, err
	}
	m := w.manifest(o.Scale, o.Seed, version, typeNames, exempt, chain, unlinked, needleID)
	payload, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(work, ManifestName), append(payload, '\n'), 0o644); err != nil {
		return nil, err
	}

	// The proof that everything above wrote a legal corpus. Orphan gaps are
	// legal and this corpus has them by design, so the strict gate is the
	// smoke script's business.
	if _, err := k.run("check"); err != nil {
		return nil, err
	}
	say("  %d entities, %d orphan gaps, %d stale past 90 days — check passed",
		m.Total, len(m.Orphans), m.Stale["90"])
	return m, nil
}

// schemaTypes reads the declared type list and each type's `orphan` exemption.
func schemaTypes(k *khub) ([]string, map[string]bool, error) {
	out, err := k.run("schema", "types", "--format", "json")
	if err != nil {
		return nil, nil, err
	}
	var names []string
	if err := json.Unmarshal(out, &names); err != nil {
		return nil, nil, fmt.Errorf("khub schema types: unreadable payload %q: %w", out, err)
	}
	exempt := map[string]bool{}
	for _, name := range names {
		out, err := k.run("schema", "show", name, "--format", "json")
		if err != nil {
			return nil, nil, err
		}
		var view struct {
			Orphan bool `json:"orphan"`
		}
		if err := json.Unmarshal(out, &view); err != nil {
			return nil, nil, fmt.Errorf("khub schema show %s: unreadable payload: %w", name, err)
		}
		if view.Orphan {
			exempt[name] = true
		}
	}
	return names, exempt, nil
}

// Read loads a manifest written beside a workspace.
func Read(work string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(work, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(work, ManifestName), err)
	}
	return &m, nil
}
