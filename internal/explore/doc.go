// Package explore builds the read models behind `khub serve`'s page: the
// workspace's totals, findings and changes for the review home, one type's rows
// with a count column per relation, one entity's page with its body parsed into
// a safe block tree, its history and its reach, search, and saved queries. The
// schema the page draws comes from introspect.
//
// It is to the served page what introspect is to `khub schema`: every record
// is derived from the live tree on each call, nothing is cached or written,
// and nothing here knows a type by name — the columns come from the resolved
// schema, the markers from the same functions `khub query` and `khub check`
// use, and the counts from the same graph. That is what lets the page's
// numbers agree with the CLI's by construction rather than by test alone.
package explore
