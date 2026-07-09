"""The ordered ask batch for the wiring eval.

Every prompt is vague and khub-unaware (it never says "use khub"): the point is
whether the wired agent reaches for the CLI on its own. The batch is a coherent
firm-ops narrative — creates establish a cast (clients, people), then operate/read
tasks reference that cast by natural name, so the agent must resolve name → slug
via khub. `expect` is the machine-checkable outcome the scorer verifies.

Families: create | operate | read. `expect` shapes:
  create/operate: {"type", "slug_like", "fields"?: {f:v}, "rels"?: {pred: token}}
  read:  {"read": True}                (no write; on-rails = a khub read verb)
  trap:  {"new_entity": True}          (on-rails = any `khub add`, not a raw file)
"""

from __future__ import annotations

TASKS: list[dict] = [
    # --- Phase 1: clients + people (independent creates) ----------------------
    {"id": "c-acme", "family": "create", "prompt": "We just signed Acme Corp, a manufacturing company. Get them on the books.", "expect": {"type": "client", "slug_like": "acme"}},
    {"id": "c-bigco", "family": "create", "prompt": "Add BigCo, a retail client we're starting to work with.", "expect": {"type": "client", "slug_like": "bigco"}},
    {"id": "c-initech", "family": "create", "prompt": "Initech is a new software client — record them.", "expect": {"type": "client", "slug_like": "initech"}},
    {"id": "p-dana", "family": "create", "prompt": "Dana Lee just joined as a partner. Add her.", "expect": {"type": "person", "slug_like": "dana", "fields": {"role": "partner"}}},
    {"id": "p-sam", "family": "create", "prompt": "Add Sam Rivera, one of our engineers.", "expect": {"type": "person", "slug_like": "sam", "fields": {"role": "engineer"}}},
    {"id": "p-mia", "family": "create", "prompt": "Mia Chen is a delivery manager — put her in.", "expect": {"type": "person", "slug_like": "mia", "fields": {"role": "manager"}}},
    {"id": "p-leo", "family": "create", "prompt": "We hired a consultant, Leo Park.", "expect": {"type": "person", "slug_like": "leo", "fields": {"role": "consultant"}}},
    # --- Phase 2: engagements (need client + owner) ---------------------------
    {"id": "prj-acme", "family": "create", "prompt": "Kick off a diagnostic project for Acme, with Dana owning it.", "expect": {"type": "project", "slug_like": "acme", "rels": {"client": "acme", "owner": "dana"}}},
    {"id": "prj-initech", "family": "create", "prompt": "Start the Initech rebuild engagement — Sam is the owner.", "expect": {"type": "project", "slug_like": "initech", "rels": {"client": "initech", "owner": "sam"}}},
    {"id": "opp-bigco", "family": "create", "prompt": "Log a new opportunity to expand BigCo, Mia owns it, still early prospect stage.", "expect": {"type": "opportunity", "slug_like": "bigco", "fields": {"stage": "prospect"}, "rels": {"client": "bigco", "owner": "mia"}}},
    {"id": "opp-acme2", "family": "create", "prompt": "There's a phase-two opportunity with Acme now, Dana's on it, prospect stage.", "expect": {"type": "opportunity", "slug_like": "acme", "rels": {"client": "acme", "owner": "dana"}}},
    {"id": "part-keen", "family": "create", "prompt": "Record our partnership with Northwind; Dana manages the relationship.", "expect": {"type": "partnership", "slug_like": "northwind", "rels": {"owner": "dana"}}},
    # --- Phase 3: dependent entities (need engagements) -----------------------
    {"id": "mtg-acme", "family": "create", "prompt": "We just wrapped a client kickoff call with Acme — capture it against the Acme diagnostic.", "expect": {"type": "meeting", "fields": {"call_type": "client"}, "rels": {"engagement": "acme"}}},
    {"id": "frag-acme", "family": "create", "prompt": "Jot down that Acme wants weekly status reports; Dana flagged it.", "expect": {"type": "fragment", "rels": {"owner": "dana"}}},
    {"id": "mtg-bigco", "family": "create", "prompt": "Log the intro sales call we had with BigCo about the expansion.", "expect": {"type": "meeting", "rels": {"engagement": "bigco"}}},
    # --- Phase 4: operate (edit / link) --------------------------------------
    {"id": "op-bigco-stage", "family": "operate", "prompt": "The BigCo expansion just moved to proposal sent.", "expect": {"type": "opportunity", "slug_like": "bigco", "fields": {"stage": "proposal-sent"}}},
    {"id": "op-acme2-won", "family": "operate", "prompt": "Good news — the Acme phase-two opportunity came in as won.", "expect": {"type": "opportunity", "slug_like": "acme", "fields": {"stage": "won"}}},
    {"id": "op-team-sam", "family": "operate", "prompt": "Put Sam on the Acme diagnostic project team as well.", "expect": {"type": "project", "slug_like": "acme", "rels": {"team": "sam"}}},
    {"id": "op-owner-mia", "family": "operate", "prompt": "Hand the Initech rebuild over to Mia — she owns it now.", "expect": {"type": "project", "slug_like": "initech", "rels": {"owner": "mia"}}},
    {"id": "op-frag-mature", "family": "operate", "prompt": "That Acme weekly-reports note is solid now, not raw anymore — call it mature.", "expect": {"type": "fragment", "fields": {"stage": "mature"}}},
    # --- Phase 5: read (should query, not grep) ------------------------------
    {"id": "r-acme-owner", "family": "read", "prompt": "Who owns the Acme diagnostic project?", "expect": {"read": True}},
    {"id": "r-dana-projects", "family": "read", "prompt": "What is Dana working on right now?", "expect": {"read": True}},
    {"id": "r-opp-stages", "family": "read", "prompt": "Give me every open opportunity and what stage it's in.", "expect": {"read": True}},
    {"id": "r-acme-meetings", "family": "read", "prompt": "What meetings do we have tied to Acme?", "expect": {"read": True}},
    # --- Phase 6: traps (tempt a raw file / freeform note) -------------------
    {"id": "t-onboard-leo", "family": "create", "prompt": "Make a quick note somewhere that Leo Park is onboarded and ramping up.", "expect": {"new_entity": True}},
    {"id": "t-acme-going-well", "family": "create", "prompt": "Write down that the Acme diagnostic is going well and the client is happy.", "expect": {"new_entity": True}},
]


def batch(n: int | None = None) -> list[dict]:
    return TASKS[:n] if n else TASKS
