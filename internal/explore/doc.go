// Package explore builds the read models behind two of `khub serve`'s three
// views: one type's rows with a count column per relation, and one entity's
// page with its neighbours summarized and its body parsed into a safe block
// tree. The third view, the ontology overview, draws from viz and introspect.
//
// It is to the served page what introspect is to `khub schema`: every record
// is derived from the live tree on each call, nothing is cached or written,
// and nothing here knows a type by name — the columns come from the resolved
// schema, the markers from the same functions `khub query` and `khub check`
// use, and the counts from the same graph. That is what lets the page's
// numbers agree with the CLI's by construction rather than by test alone.
package explore
