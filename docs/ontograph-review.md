# OntoGraph: reviewed, not adopted

**Status:** comparative review, 2026-08. Verdict up front: **do not adopt, port, or
vendor.** Three things are worth taking — a citation, a visualization idea (#58), and
a caveated test corpus. The code is not one of them.

Reviewed at [`NinePts/OntoGraph`](https://github.com/NinePts/OntoGraph) commit
`c35aec2` — the tip, dated **10 January 2019**.

## What it is

A Java / Spring Boot REST service plus a small GUI for graphing OWL ontologies.
It loads an ontology (or a zip of them) into [Stardog](http://www.stardog.com/),
runs SPARQL to pull out classes, properties, individuals and restrictions, and
emits **GraphML** in four notations:

- a **custom** format (configurable node shape, node color, property line color,
  source and target arrowheads, varied independently for classes vs individuals
  and object vs data properties),
- [**Graffoo**](http://www.essepuntato.it/graffoo/),
- [**VOWL**](http://vowl.visualdataweb.org/v2/#notation),
- a **UML-style** class diagram.

Layout is then performed by hand in [yEd](http://www.yworks.com/products/yed).
Licensed Apache-2.0.

## Why it cannot be used as a tool

| | |
|---|---|
| Last commit | `c35aec2`, 2019-01-10 — 7.5 years stale |
| Spring Boot | 1.5.6 (EOL August 2019) |
| Java / Gradle | 8 / 4.2 |
| Artifact repos | `jcenter()` (sunset 2021); `http://maven.stardog.com` (gone, and plain HTTP) |
| Triple store | Stardog 5.2.1 — commercial, current major is 10.x |
| Shape | 41 Java files, but `GraphDBAccess.java` 78KB, `GraphMLUtils.java` 53KB, `GraphMLOutputDetails.java` 51KB |

Three independent blockers, any one of them sufficient:

1. **It will not build.** Both artifact repositories it declares are gone.
2. **It requires a commercial database.** khub's premise is that no database is
   ever the source of truth; standing up Stardog to draw a diagram inverts that.
3. **Its output is not a picture.** GraphML needs yEd — a proprietary desktop
   application — to lay out, which puts a manual step in the middle of what
   should be one command.

Porting the SPARQL-to-GraphML logic to Python would be a rewrite rather than a
port, and it would target OWL constructs khub does not emit. `khub viz` already
produces a self-contained Cytoscape HTML with no network and no external editor,
which is the better end state on every axis except notation richness.

## What is worth taking

### 1. The paper

`OntologyDevelopmentByDomainExperts.pdf` in the repo root is Westerinen &
Tauber, *Ontology Development by Domain Experts (Without Using the "O" Word)*,
accepted in **Applied Ontology** (IOS Press). It argues khub's premise, in print
and citable:

> The individuals with the domain knowledge are rarely versed in model or
> ontology development, and do not know the formal languages or logic that
> express ontological concepts. What is needed is to create renderings of the
> ontologies that fit how the experts work.

> Asking a domain expert to use an ontology-authoring tool or to understand the
> complexities of a description logic language (such as OWL) may result in
> errors or omissions, or in the expert becoming frustrated and losing interest
> entirely.

This is the support for two positions the design memo currently asserts on its
own: that the schema is authored in khub's own vocabulary rather than OWL, and
— more precisely — that the RDF work (#44–46) is a *derived projection* and must
never become the authoring surface.

The paper also surfaces **UPON Lite** (De Nicola & Missikoff, 2016), a
methodology built explicitly to reduce "the role of and dependence on ontology
engineers," where every step produces "a self-contained artifact readily
available to end users":

| UPON Lite step | khub analogue |
|---|---|
| domain lexicon — the terms that matter | the type and predicate names |
| natural-language definitions per term | `description` on types and fields |
| generalization/specialization (IS-A) hierarchy | *(khub has no inheritance — #9 bundles are the deliberate substitute)* |
| predicate scope: domains and ranges | typed relations with `to:` targets |
| meronymy (whole-part) | containment predicates; #14 owned children |
| an ontologist formalizes it in OWL | **the step khub deletes** |

khub has no documented preset-authoring methodology. This is a citable spine for
one, and the mapping above is most of the outline.

### 2. Aspect-split visualization → candidate #58

OntoGraph's central design decision, and the one idea worth implementing:

> OntoGraph is architected to create separate graphs for the classes, object and
> data properties, and individuals of an ontology. Separate graphs are generated
> to reduce the number of nodes and edges on any single graph, and thereby
> reduce crowding and help focus the semantics.

`khub viz` renders one **instance** graph today (`core/viz.py`: Cytoscape
elements, per-type coloring, predicate-labeled edges, `--type` filter, the
library inlined so the HTML opens with no network). There is no way to draw the
**schema** — the types, their legal predicates, which relations are required,
cardinality, union targets. A reader can see the entities and not the ontology.

Filed as **#58** in [`feature-candidates.md`](feature-candidates.md).

Supporting findings from the paper, worth keeping in view when designing it:
VOWL was built for "casual ontology users with only little training"; a study
(Fu, Noy & Storey, 2013) found graphs "held their attention better" than
indented trees and suited overviews and multiple inheritance; and Katifori et
al. (2003) note that *all* aspects — classes, hierarchies, instances,
relationships, properties — must be visualizable to understand an ontology,
which is the argument for aspects rather than one crowded picture.

### 3. The test corpus — with a caveat that matters more than the corpus

43 `.ttl` test ontologies and 36 control `.graphml` files under
`src/test/resources/`, covering blank nodes, `unionOf` / `intersectionOf` /
`complementOf`, `oneOf`, disjoint unions, datatype restrictions, and nested
unions. If #45 (the `.khub/generated/schema.ttl` projection) ships, these are
ready-made adversarial inputs.

**The caveat:** its SPARQL reaches for `owl:oneOf`, `owl:complementOf`,
`owl:withRestrictions`, `owl:onDatatype`, `TransitiveProperty`,
`AsymmetricProperty` and nested unions — most of which khub's profile
deliberately excludes. Treating the testcases as a coverage target would drag
khub toward full OWL, which #44 explicitly rejects. Use them to check that what
khub *emits* survives a real OWL consumer; never as a bar to clear.

The same applies to the notations. Borrow the visual grammar for subclassing,
domain/range and cardinality; ignore the constructs khub does not have.

## Sources

- [NinePts/OntoGraph](https://github.com/NinePts/OntoGraph) — source at `c35aec2`
- Westerinen, A. & Tauber, T., *Ontology Development by Domain Experts (Without
  Using the "O" Word)*, Applied Ontology, IOS Press —
  [PDF in the repo](https://github.com/NinePts/OntoGraph/blob/master/OntologyDevelopmentByDomainExperts.pdf)
- De Nicola, A. & Missikoff, M. (2016), *A Lightweight Methodology for Rapid
  Ontology Engineering* (UPON Lite) — via the paper
- [Graffoo](http://www.essepuntato.it/graffoo/) · [VOWL](http://vowl.visualdataweb.org/v2/#notation)
