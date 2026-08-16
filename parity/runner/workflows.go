package main

// Workflow-shaped sequences — the "real work" half of differential mode.
//
// Uniform random verbs rarely produce a VALID referential chain: an
// opportunity that actually points at a client that actually exists, converted
// into a project that cites it, with meetings hanging off it. Those chains are
// what khub is for, and they are where ordering, inverse edges and the
// integrity gates interact.
//
// Each workflow is an ordered template. Placeholders bind to entities created
// earlier IN THE SAME RUN:
//
//	{new:TYPE}   mint a fresh entity of TYPE first (the step is prefixed)
//	{last:TYPE}  the most recently created TYPE, else the workflow is skipped
//	{any:TYPE}   any known TYPE, else skipped
//	{today}      the frozen clock date
//
// Skipping rather than substituting a bogus id keeps sequences realistic; the
// random mode already covers dangling-reference behaviour.

import (
	"fmt"
	"strings"
)

type workflow struct {
	name  string
	steps [][]string
}

// firmOpsWorkflows mirrors how the HQ workspace is actually driven: a sales
// arc, a delivery arc, meeting triage, a weekly review, knowledge capture, and
// cleanup.
var firmOpsWorkflows = []workflow{
	{"new-business", [][]string{
		{"add", "client", "--name", "Northwind {n}", "--updated", "{today}", "--format", "json"},
		{"add", "person", "--name", "Rep {n}", "--role", "consultant", "--format", "json"},
		{"add", "opportunity", "--title", "Northwind discovery {n}", "--updated", "{today}",
			"--stage", "prospect", "--client", "{last:client}", "--owner", "{last:person}", "--format", "json"},
		{"get", "{last:opportunity}", "--edges", "--format", "json"},
		{"edit", "{last:opportunity}", "stage", "proposal-sent", "--format", "json"},
		{"neighbors", "{last:client}", "--format", "json"},
		{"edit", "{last:opportunity}", "stage", "won", "--format", "json"},
		{"check", "--format", "json"},
	}},
	{"opportunity-to-project", [][]string{
		{"add", "project", "--title", "Delivery {n}", "--updated", "{today}",
			"--client", "{any:client}", "--origin-opportunity", "{any:opportunity}",
			"--owner", "{any:person}", "--format", "json"},
		{"link", "{last:project}", "team", "{any:person}", "--format", "json"},
		{"impact", "{last:project}", "--format", "json"},
		{"neighbors", "{last:project}", "--in", "--format", "json"},
		{"validate", "--format", "json"},
	}},
	{"meeting-triage", [][]string{
		{"add", "transcript", "--title", "Call {n}", "--source", "recording",
			"--date", "{today}", "--format", "json"},
		{"add", "meeting", "--date", "{today}", "--call-type", "client", "--source", "recording",
			"--transcript", "{last:transcript}", "--engagement", "{any:project}", "--format", "json"},
		{"get", "{last:meeting}", "--edges", "--format", "json"},
		{"query", "--type", "meeting", "--format", "json"},
		{"status", "--format", "json"},
	}},
	{"weekly-review", [][]string{
		{"stale", "--format", "json"},
		{"query", "--stale", "--format", "json"},
		{"edit", "{any:client}", "updated", "{today}", "--format", "json"},
		{"check", "--format", "json"},
		{"reindex", "--dry-run"},
	}},
	{"knowledge-capture", [][]string{
		{"add", "fragment", "--title", "Note {n}", "--stage", "raw", "--format", "json"},
		{"link", "{last:fragment}", "owner", "{any:person}", "--format", "json"},
		{"edit", "{last:fragment}", "stage", "mature", "--format", "json"},
		{"search", "Note", "--format", "json"},
		{"edit", "{last:fragment}", "stage", "promoted", "--format", "json"},
		{"query", "--type", "fragment", "--stage", "promoted", "--format", "json"},
	}},
	{"cleanup", [][]string{
		{"query", "--orphan", "--format", "json"},
		{"remove", "{any:fragment}", "--format", "json"},
		{"remove", "{any:fragment}", "--force", "--format", "json"},
		{"check", "--strict", "--format", "json"},
		{"validate", "--strict", "--format", "json"},
	}},
	{"fragment-raw-read", [][]string{
		{"add", "fragment", "--title", "Scrap {n}", "--stage", "raw", "--format", "json"},
		{"get", "{last:fragment}", "--format", "raw"},
		{"get", "{last:fragment}", "--edges", "--format", "json"},
		{"edit", "{last:fragment}", "stage", "processed", "--format", "json"},
		{"get", "{last:fragment}", "--format", "raw"},
		{"validate", "--format", "json"},
	}},
}

// buildLiteWorkflows drive the other shipped preset: a spec/decision arc.
var buildLiteWorkflows = []workflow{
	{"requirement-to-spec", [][]string{
		{"add", "requirement", "--title", "Rule {n}", "--kind", "functional", "--format", "json"},
		{"add", "component", "--title", "Service {n}", "--kind", "service", "--format", "json"},
		{"link", "{last:requirement}", "realized_in", "{last:component}", "--format", "json"},
		{"add", "feature-spec", "--title", "Work {n}", "--status", "planned",
			"--requirements", "{last:requirement}", "--format", "json"},
		{"edit", "{last:feature-spec}", "status", "active", "--format", "json"},
		{"neighbors", "{last:requirement}", "--format", "json"},
		{"check", "--format", "json"},
	}},
	{"decision-supersession", [][]string{
		{"add", "adr", "--title", "Choice {n}", "--status", "proposed", "--format", "json"},
		{"edit", "{last:adr}", "status", "accepted", "--format", "json"},
		{"add", "adr", "--title", "Rethink {n}", "--status", "accepted",
			"--supersedes", "{last:adr}", "--format", "json"},
		{"get", "{last:adr}", "--format", "raw"},
		{"history", "{last:adr}", "--format", "json"},
		{"query", "--missing", "superseded", "--format", "json"},
		{"check", "--format", "json"},
	}},
	{"dependency-chain", [][]string{
		{"add", "component", "--title", "Edge {n}", "--kind", "service", "--format", "json"},
		{"link", "{last:component}", "depends_on", "{any:component}", "--format", "json"},
		{"impact", "{any:component}", "--format", "json"},
		{"impact", "{any:component}", "--reverse", "--format", "json"},
		{"check", "--format", "json"},
		{"reindex"},
	}},
}

// buildHubWorkflows extend the build-lite arcs with the types only build-hub
// declares. `repo` is the one COLLECTION-layout type any preset ships, so this
// list is the only place the lock-serialized write path is exercised in
// workflow shape — it used to sit in firmOpsWorkflows, where `repo` does not
// exist and `{any:project}` could never bind, so the arc silently never ran.
var buildHubWorkflows = append(append([]workflow{}, buildLiteWorkflows...),
	workflow{"collection-churn", [][]string{
		{"add", "repo", "--repo", "org/svc-{n}", "--status", "active", "--format", "json"},
		{"get", "{last:repo}", "--format", "raw"},
		{"edit", "{last:repo}", "status", "archived", "--format", "json"},
		{"query", "--type", "repo", "--format", "json"},
		{"check", "--format", "json"},
	}},
	workflow{"domain-to-work", [][]string{
		{"add", "domain", "--title", "Area {n}", "--tier", "core", "--format", "json"},
		{"add", "capability", "--title", "Ability {n}", "--format", "json"},
		{"add", "feature-spec", "--title", "Slice {n}", "--status", "planned", "--format", "json"},
		{"add", "work-package", "--title", "Batch {n}", "--status", "planned",
			"--feature", "{last:feature-spec}", "--format", "json"},
		{"add", "test-spec", "--title", "Check {n}",
			"--verifies", "{last:feature-spec}", "--format", "json"},
		{"edit", "{last:work-package}", "status", "active", "--format", "json"},
		{"neighbors", "{last:feature-spec}", "--depth", "2", "--format", "json"},
		{"stale", "--format", "json"},
		{"check", "--format", "json"},
	}},
)

// bind resolves placeholders; ok=false means the workflow cannot run yet.
func (s *fuzzState) bind(argv []string, n int) (out []string, ok bool) {
	for _, a := range argv {
		switch {
		case strings.Contains(a, "{n}"):
			a = strings.ReplaceAll(a, "{n}", fmt.Sprintf("%03d", n))
		case a == "{today}":
			a = "2026-01-15"
		case strings.HasPrefix(a, "{last:"), strings.HasPrefix(a, "{any:"):
			typ := strings.TrimSuffix(a[strings.Index(a, ":")+1:], "}")
			sl := s.created[typ]
			if len(sl) == 0 {
				return nil, false
			}
			if strings.HasPrefix(a, "{last:") {
				a = typ + "/" + sl[len(sl)-1]
			} else {
				a = typ + "/" + s.pick(sl)
			}
		}
		out = append(out, a)
	}
	return out, true
}

// workflowsFor picks the template set matching the workspace's preset.
func workflowsFor(preset string) []workflow {
	switch preset {
	case "build-hub":
		return buildHubWorkflows
	case "build-lite":
		return buildLiteWorkflows
	default:
		return firmOpsWorkflows
	}
}

// nextWorkflowStep drains the current workflow, choosing a new one when empty.
// A workflow whose placeholders cannot bind is skipped and the next is tried,
// so early steps (nothing created yet) still make progress.
func (s *fuzzState) nextWorkflowStep(flows []workflow, counter *int) []string {
	for attempt := 0; attempt < len(flows)+1; attempt++ {
		if len(s.queue) == 0 {
			wf := flows[s.rng.Intn(len(flows))]
			s.queue = append(s.queue, wf.steps...)
			s.flowName = wf.name
			*counter++
			s.flowN = *counter
			s.fired[wf.name]++
		}
		next := s.queue[0]
		s.queue = s.queue[1:]
		if bound, ok := s.bind(next, s.flowN); ok {
			return bound
		}
		// unbindable: drop the rest of this workflow and try another
		s.skipped[s.flowName]++
		s.queue = nil
	}
	return []string{"status", "--format", "json"}
}

// flowNames lists every arc declared for a preset, fired or not — the summary
// has to name a dead arc, and a map of counts alone cannot.
func flowNames(preset string) []string {
	var names []string
	for _, wf := range workflowsFor(preset) {
		names = append(names, wf.name)
	}
	return names
}

// arcNamesInOrder returns the declared arcs in DECLARATION order (not sorted —
// declaration order is how the workflow file reads), then any arc seen at
// runtime that the declared list somehow missed.
func arcNamesInOrder(declared []string, fired, skipped map[string]int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(declared))
	for _, n := range declared {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, m := range []map[string]int{fired, skipped} {
		for n := range m {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}
