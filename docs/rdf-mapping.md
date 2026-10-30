# khub ↔ RDF: the normative mapping

`khub schema rdf` projects a workspace's resolved schema into RDFS + SHACL,
and with `--instances` its entities too, as one Turtle file. This page is the
contract for that file: the vocabulary it uses, how every IRI is minted, what
each khub construct becomes, what survives the trip losslessly and what rides
along as an annotation, and the serialization rules that make the bytes a
function of the schema alone. The firm-ops render the parity runner records
(`parity/cases/projection/schema-rdf/expected/steps/08.stdout`) is the golden
fixture: a change to this page is a change to those bytes, and the other way
round.

## Why a projection, and why SHACL

khub's schema is authored in khub's own vocabulary and never in a description
logic. The reason is well documented: domain experts "are rarely versed in
model or ontology development, and do not know the formal languages or logic
that express ontological concepts," and asking one to work in OWL "may result
in errors or omissions, or in the expert becoming frustrated and losing
interest entirely" (Westerinen & Tauber, *Ontology Development by Domain
Experts (Without Using the "O" Word)*, Applied Ontology, IOS Press). So RDF
here is derived, regenerated like `index.md` and `viz.html`, and never a
second source of truth: the YAML layers are authored, the Turtle is read.

OWL alone cannot say what khub's schema says. khub is closed-world over the
declared subset — a required field is a gate, an enum is a closed list, a
single-valued relation holds one target — and OWL's open-world semantics
reads every one of those as a licence to infer rather than a constraint to
check. SHACL is the W3C constraint language built for exactly this, so every
constraint in the export is a SHACL shape, and the RDFS/OWL layer carries only
vocabulary: classes, properties, labels. Nothing in the file relies on OWL DL
reasoning; `owl:Ontology`, `owl:DatatypeProperty` and `owl:ObjectProperty`
appear because tooling expects them, not because a reasoner is needed.

The projection reads the **resolved** schema: the three authored layers merged
with the embedded base block, every base attribute on every type, every
override applied. That is the contract khub validates against, so it is the
one worth exporting. Since the ontology/policy/storage split the ontology
layer is purely the domain — attributes, relations, `when` — and that layer
maps onto RDFS + SHACL without loss. Policy and storage have no RDF meaning,
so they ride as `khub:` annotations.

## The file

```
# comment header
@prefix rdf: … rdfs: … owl: … xsd: … sh: …          fixed, in this order
@prefix khub:  <urn:khub:vocab/> .                   the annotation vocabulary
@prefix type:  <…/ontology/<key>/type/> .            classes
@prefix field: <…/ontology/<key>/field/> .           predicates
@prefix shape: <…/ontology/<key>/shape/> .           node shapes

<ontology IRI> a owl:Ontology …                      provenance
khub:Entity, khub:layout, …                          the vocabulary, declared
type:<T> a rdfs:Class …                              one per type, declaration order
field:<p> a rdf:Property …                           one per predicate name, first-seen order
shape:<T> a sh:NodeShape …                           one per type, declaration order
<entity IRI> a type:<T> …                            --instances only, scan order
```

## IRI minting

Every IRI hangs off a root, `--base`, default `urn:khub:`. A URN claims no
host, and RFC 8141 lets its namespace-specific string carry the slash-separated
path below, so the same layout works under `urn:khub:` and under
`https://example.org/kb/`. The `khub:` vocabulary alone ignores `--base`: a
consumer reads `khub:acyclic` the same way from every export.

| IRI | Form | Example (firm-ops, workspace `ws`) |
|---|---|---|
| vocabulary | `urn:khub:vocab/<term>` | `urn:khub:vocab/acyclic` |
| ontology | `<base>ontology/<key>/` | `urn:khub:ontology/firm-ops/` |
| class | `<base>ontology/<key>/type/<type>` | `urn:khub:ontology/firm-ops/type/client` |
| predicate | `<base>ontology/<key>/field/<name>` | `urn:khub:ontology/firm-ops/field/stage` |
| base predicate | `urn:khub:vocab/<name>` | `urn:khub:vocab/title` |
| node shape | `<base>ontology/<key>/shape/<type>` | `urn:khub:ontology/firm-ops/shape/client` |
| entity | `<base>workspace/<name>/<type>/<slug>` | `urn:khub:workspace/ws/client/initech` |

`<key>` is the preset name recorded in `.khub/config.yaml`, falling back to
the workspace name when no preset is recorded. Keying the vocabulary by preset
rather than by workspace is deliberate: two workspaces initialized from
firm-ops mint the same class and predicate IRIs, so a federated consumer joins
them without a mapping table, and an entity IRI stays unique because it carries
the workspace name. A workspace whose schema has drifted far from its preset
can pass its own `--base`. `<name>` is the workspace name from the same file.

A predicate mints **one IRI per name, shared by every type that declares it**
— `field:owner` is the same property on `opportunity`, `project` and
`meeting`. The per-type differences (target class, cardinality, whether it is
required) live in each type's shape, which is where SHACL puts them. A name
that is an attribute on one type and a relation on another (`partner` in
firm-ops: a text field on `partnership`, an edge on `opportunity`) is a plain
`rdf:Property` with a comment saying so; RDFS allows it, and each shape says
which it is there.

A predicate the base block declares (`title`, `created`, `tags`, `related`,
`depends_on`…) mints in the `khub:` vocabulary, not the preset's: the base is
khub's own, identical across presets, so `khub:title` is the same property in
a firm-ops and a build-hub export. The base `type` attribute mints nothing —
it is the discriminator, and in RDF that is `rdf:type`.

Every segment is percent-encoded to RFC 3986 unreserved characters, so a name
with a space or a non-ASCII letter is one segment. A class, predicate or shape
whose name fits a prefixed name's local part (`[A-Za-z_][A-Za-z0-9_-]*`) is
written as one; any other (`team.lead`) is written as a full IRI. Entity IRIs
are always full, so no slug ever has to fit a prefixed name.

## The khub vocabulary

`urn:khub:vocab/` holds one class and the annotation properties the export
needs. Every export declares them, so the file is self-describing. They are
versioned by addition: a term never changes meaning, and a consumer that
ignores a term it does not know reads every later export correctly.

| Term | On | Meaning |
|---|---|---|
| `khub:Entity` | — | `rdfs:Class`; every type is `rdfs:subClassOf` it, and a `to: any` relation targets it |
| `khub:preset`, `khub:presetVersion`, `khub:workspace` | the ontology | provenance from `.khub/config.yaml` |
| `khub:requiredScope` | the ontology | always `"active"`: `sh:minCount` is khub's required gate, which `check` enforces on active entities (`draft: false`) only. A validator that wants khub's exact gate restricts its focus nodes to those without `khub:draft true` |
| `khub:layout`, `khub:format`, `khub:path` | a class | storage: `file`/`folder`/`collection`/`singleton`; `md`/`json`/`yaml`/`jsonl`; the storage path, one value per path template on a templated type |
| `khub:idShape` | a class | the id a new entity is minted with (`ad-YYYY-MM-DD-slug`); absent on a singleton |
| `khub:template` | a class | the declared body-template stem |
| `khub:requiredSingleton`, `khub:orphanExempt` | a class | policy: `required: true` on a singleton; `orphan: true` |
| `khub:when` | a class | the capture cue, in domain language |
| `khub:type` | an attribute shape | the khub scalar kind, always: the XSD mapping loses `list`, and `number` would otherwise read as "decimal only" |
| `khub:default` | an attribute shape | the value an absent attribute reads as |
| `khub:pattern` | an attribute shape | the authoritative RE2 text |
| `khub:kind` | a relation shape | `typed`, `union` or `any` |
| `khub:acyclic` | a relation shape | `check` reports a cycle over this predicate |
| `khub:inverse` | a relation shape | the derived inverse predicate the target answers to; never stored, never emitted as an edge |
| `khub:pathRefs` | a relation shape | the owning type's `path_refs` policy: a value may name a workspace file |

## The mapping

### Types

```turtle
type:opportunity a rdfs:Class ;
    rdfs:subClassOf khub:Entity ;
    rdfs:label "opportunity" ;
    khub:when "a possible piece of work with a named client is first mentioned — …" ;
    khub:layout "folder" ;
    khub:format "md" ;
    khub:path "opportunities" ;
    khub:idShape "slug" .
```

A type is a class and a node shape. `khub:idShape`, `khub:template`,
`khub:requiredSingleton` and `khub:orphanExempt` appear only when they say
something.

### Attributes

Each resolved attribute, base ones included, is one `sh:property` on the
type's shape, in declaration order (`sh:order` counts from 0, `type`
excluded). The order is what `khub schema show` prints: base attributes
first, a type's override keeping the base slot, then the type's own.

```turtle
sh:property [
    sh:path field:stage ;
    sh:name "stage" ;
    sh:order 10 ;
    sh:datatype xsd:string ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    sh:in ( "prospect" "proposal-sent" "won" "signed" "lost" ) ;
    khub:type "text"
] ;
```

| khub | SHACL | Notes |
|---|---|---|
| `type: text` | `sh:datatype xsd:string` | the default kind; an enum without `type` is text |
| `type: number` | `sh:datatype xsd:decimal` | khub does not split integers from floats; instances are typed `xsd:decimal` to match |
| `type: date` | `sh:datatype xsd:date` | |
| `type: datetime` | `sh:datatype xsd:dateTime` | khub writes `2026-01-15 10:00:00`; the literal takes XSD's `T` |
| `type: bool` | `sh:datatype xsd:boolean` | |
| `type: list` | no `sh:datatype`, no `sh:maxCount` | items are whatever the author wrote; `khub:type "list"` says so |
| `required: true` | `sh:minCount 1` | khub's gate for active entities; see `khub:requiredScope` |
| single-valued (every non-list) | `sh:maxCount 1` | |
| `enum: [...]` | `sh:in ( ... )` | in declared order |
| `pattern:` | `sh:pattern` when portable, `khub:pattern` always | below |
| `default:` | `khub:default <literal>` | annotation; SHACL has no default |

**Patterns.** khub matches a `pattern` with RE2 over the whole value.
`sh:pattern` is matched with an XPath/SPARQL regex and is a search, not a full
match. So the export writes `sh:pattern` only when the pattern passes a
conservative portability test (no `(?…)` groups or flags, no `\A \z \Z \b \B`,
no `\x` or octal escapes, no `\Q…\E`, no POSIX `[:class:]`, no one-letter `\pL`,
no `-[` class subtraction, no `\a \f \v \e \h`), and anchors it: a pattern that
already reads `^…$` with no top-level alternation goes out as written, any
other is wrapped as `^(…)$`. `khub:pattern` always carries the RE2 text and is
authoritative.

### Relations

```turtle
sh:property [
    sh:path field:engagement ;
    sh:name "engagement" ;
    sh:order 18 ;
    sh:nodeKind sh:IRI ;
    sh:or (
        [ sh:class type:opportunity ]
        [ sh:class type:project ]
        [ sh:class type:partnership ]
    ) ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    khub:kind "union"
] ;
```

| khub | SHACL | Notes |
|---|---|---|
| `to: <type>` | `sh:class type:<type>` | `khub:kind "typed"` |
| `to: [a, b]` | `sh:or ( [sh:class type:a] [sh:class type:b] )` | `khub:kind "union"`. `sh:or`, not `owl:unionOf`: a union class expression has no instances to type, and SHACL is the layer that constrains |
| `to: any` | `sh:class khub:Entity` | `khub:kind "any"` |
| `required: true` | `sh:minCount 1` | the same gate as an attribute's |
| `many: true` | no `sh:maxCount` | otherwise `sh:maxCount 1` |
| `acyclic: true` | `khub:acyclic true` | annotation; SHACL core cannot express acyclicity |
| `inverse: <name>` | `khub:inverse "<name>"` | annotation, never `owl:inverseOf`: khub stores one side and derives the other at read time, and an OWL inverse would license a reasoner to assert triples khub never wrote |
| policy `path_refs` | `khub:pathRefs true` | annotation |

Every relation shape carries `sh:nodeKind sh:IRI`. No `rdfs:domain` or
`rdfs:range` is written on the shared predicates: RDFS reads them as global
intersections, which would be false for a predicate whose targets differ by
type.

### Entities (`--instances`)

```turtle
<urn:khub:workspace/ws/opportunity/initech-pov> a type:opportunity ;
    khub:draft false ;
    khub:created "2026-01-15"^^xsd:date ;
    khub:updated "2026-01-15"^^xsd:date ;
    khub:tags "pov", "q1" ;
    field:name "Initech POV" ;
    field:stage "prospect" ;
    field:client <urn:khub:workspace/ws/client/initech> ;
    field:owner <urn:khub:workspace/ws/person/noor> .
```

One subject per entity, in scan order (schema declaration order, sorted within
a type) — the order every projection walks. Then:

- `a type:<T>` from the type binding.
- Each **present** declared attribute, in declaration order, as a literal typed
  by the attribute's kind: `xsd:string` (plain), `xsd:decimal`, `xsd:date`,
  `xsd:dateTime`, `xsd:boolean` (shorthand). A list emits one triple per item.
  An absent or null attribute emits nothing; what it reads as is the shape's
  `khub:default`. A structured value where a scalar was expected — a mapping,
  a nested list, a list item that is itself a mapping (`attendees`) — is one
  `rdf:JSON` literal holding its JSON, which loses nothing.
- Each **stored forward edge** the graph resolves, in relation declaration
  order, as an object property to the target's IRI. A value naming no entity
  (dangling, or a path reference to a plain file) emits no triple; `check`
  reports it. **Derived inverses are not emitted**: the graph is derived, and a
  consumer with the forward edges derives them the same way khub does.
- Nothing else: undeclared extension keys, the body, and the slug (the IRI
  carries it) stay out.

Drafts are included, as every khub read includes them; `khub:draft true`
marks one. A consumer validating with the shapes should expect `sh:minCount`
violations on drafts, which is what `khub:requiredScope "active"` says.

## What is lossless, what is annotation

**Lossless over the ontology layer.** Every type, attribute and relation of
the resolved schema reaches the file with its name, kind, cardinality,
required flag, enum, pattern, default, targets, `acyclic`, `inverse` and
`when`, each in declaration order (`sh:order`, and the subject order). A
reader can rebuild the resolved ontology layer from the shapes and the `khub:`
annotations alone; `TestEveryDeclarationIsProjected` in `internal/rdf` holds
that for every name.

**SHACL-expressible, and so checkable by any validator:** datatypes, enums,
portable patterns, required (modulo the draft scope), single-valuedness, target
classes and unions. A SHACL validator run over `--instances` output checks
every one of these against the same file; the firm-ops render was checked this
way with pySHACL and conforms.

**Annotation only, khub remaining authoritative:** `acyclic`, `inverse`,
unportable patterns, defaults, the draft scope of `required`, path references,
and everything in the storage and policy layers. None of these has a faithful
SHACL or OWL rendering, and a lossy one would mislead a validator; the
annotation says what khub enforces and the binary enforces it.

**Not projected:** entity bodies, extension keys, the `.khub/ignore` list, and
khub's own version (it would move every export on every release for no
information). Aliases are projected as the `khub:aliases` list, never as
`owl:sameAs`: an alias is a lookup name, not an identity claim.

## Serialization

The bytes are a function of the schema, the entities and the flags:

- The prefix block is fixed and in the order shown above; every IRI is a
  prefixed name over a declared prefix or an absolute `<…>` reference.
  There are no relative IRIs and no `@base`.
- Subjects, pairs, list items and `sh:property` blocks are emitted in
  declaration or scan order. No map is iterated on the way out.
- One pinned layout: a subject block ends with `.`, every pair is on its own
  four-space-indented line ending in `;`, consecutive pairs under one
  predicate fold into an object list (`a rdf:Property, owl:DatatypeProperty`),
  a blank node with one pair is inline (`[ sh:class type:client ]`) and a
  larger one is a block, and a collection is inline unless it holds blank
  nodes. Objects never wrap.
- `rdf:type` is written `a`. Booleans and integers use Turtle's shorthand;
  every other literal is a quoted string, with a datatype when one applies.
- Strings escape `\` `"` and the five named controls, every other control
  character as `\uXXXX`, and nothing else: non-ASCII text is written as the
  UTF-8 it is.
- Each export opens with the same two comment lines. No timestamp, no
  binary version.

The emitter is `internal/rdf/turtle.go`. It has no Turtle dependency: emission
is a few escaping rules and one layout, less code than a library plus the shim
that would keep these bytes pinned. The same property that keeps entity files
diff-stable in git keeps this file diff-stable: a schema change is a readable
diff of `schema.ttl`.

## Worked example

From the firm-ops preset with one person, one client, one opportunity and one
meeting (the parity case). Abridged — the recorded fixture is the whole file.

```turtle
@prefix khub: <urn:khub:vocab/> .
@prefix type: <urn:khub:ontology/firm-ops/type/> .
@prefix field: <urn:khub:ontology/firm-ops/field/> .
@prefix shape: <urn:khub:ontology/firm-ops/shape/> .

<urn:khub:ontology/firm-ops/> a owl:Ontology ;
    rdfs:label "firm-ops" ;
    khub:preset "firm-ops" ;
    khub:presetVersion "0.3.0" ;
    khub:workspace "ws" ;
    khub:requiredScope "active" .

type:meeting a rdfs:Class ;
    rdfs:subClassOf khub:Entity ;
    rdfs:label "meeting" ;
    khub:when "a call happens that produced a decision, a commitment, or a next step" ;
    khub:layout "file" ;
    khub:format "md" ;
    khub:path "meetings" ;
    khub:idShape "slug" .

field:engagement a rdf:Property, owl:ObjectProperty ;
    rdfs:label "engagement" .

field:partner a rdf:Property ;
    rdfs:label "partner" ;
    rdfs:comment "An attribute on some types and a relation on others; each shape says which." .

shape:meeting a sh:NodeShape ;
    sh:targetClass type:meeting ;
    sh:property [
        sh:path khub:created ;
        sh:name "created" ;
        sh:order 2 ;
        sh:datatype xsd:date ;
        sh:maxCount 1 ;
        khub:type "date"
    ] ;
    sh:property [
        sh:path field:date ;
        sh:name "date" ;
        sh:order 9 ;
        sh:datatype xsd:dateTime ;
        sh:minCount 1 ;
        sh:maxCount 1 ;
        khub:type "datetime"
    ] ;
    sh:property [
        sh:path field:engagement ;
        sh:name "engagement" ;
        sh:order 18 ;
        sh:nodeKind sh:IRI ;
        sh:or (
            [ sh:class type:opportunity ]
            [ sh:class type:project ]
            [ sh:class type:partnership ]
        ) ;
        sh:minCount 1 ;
        sh:maxCount 1 ;
        khub:kind "union"
    ] .

<urn:khub:workspace/ws/meeting/kickoff-call> a type:meeting ;
    khub:draft false ;
    khub:created "2026-01-15"^^xsd:date ;
    khub:updated "2026-01-15"^^xsd:date ;
    khub:title "Kickoff \"call\"" ;
    field:date "2026-01-15T10:00:00"^^xsd:dateTime ;
    field:call_type "client" ;
    field:source "manual" ;
    field:engagement <urn:khub:workspace/ws/opportunity/initech-pov> ;
    field:owner <urn:khub:workspace/ws/person/noor> .
```

`meeting` overrides the base `created` to optional, so its shape carries no
`sh:minCount` there while `opportunity`'s does — the resolved schema, not the
authored delta. `partner` is text on `partnership` and an edge elsewhere, so
its property is untyped and its two shapes disagree, legitimately.

## Reading the file elsewhere

Point any RDF tool at `.khub/generated/schema.ttl`; regenerate it after a
schema change (it is gitignored, and nothing regenerates it automatically).
Validate entities with the same file as data and shapes graph:
`pyshacl -s schema.ttl schema.ttl` after `khub schema rdf --instances`.
Join two workspaces of one preset by loading both `--instances` exports: the
classes and predicates coincide, the entities do not.
