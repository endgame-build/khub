// Package canon is the serialization choke point: the only package in khub
// that reads or writes YAML and JSON bytes. It holds the dual-mode YAML loader
// (1.1 at the scan altitude, 1.2 at the edit altitude), the token-splice
// writer that rewrites only the values that moved, the ruamel-shaped emitter
// in both profiles, the three JSON dialects khub emits, and the per-format
// entity and collection serializers built on them. Everything else goes
// through canon's loaders and dumpers: a second YAML or JSON library on a
// write path would quietly reshape on-disk bytes that are a contract.
package canon
