package migrate

// Doc and Rewrite expose the per-document rewrite to the fuzz target in
// package migrate_test.
var Doc = rewriteDoc

type Rewrite = rewrite
