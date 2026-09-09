package schema

// Ordered is the typed insertion-ordered map backing every dict[str, X] of the
// Python ontology layer (schema_model / model). Iteration order = declaration
// order is contract; a
// Set on an existing key keeps its original position, exactly like a Python
// dict assignment. Keys returns the backing slice — callers must not mutate it.
type Ordered[V any] struct {
	keys []string
	vals map[string]V
}

// NewOrdered returns an empty Ordered map.
func NewOrdered[V any]() *Ordered[V] { return &Ordered[V]{vals: map[string]V{}} }

// Get returns the value for k and whether k is present.
func (o *Ordered[V]) Get(k string) (V, bool) { v, ok := o.vals[k]; return v, ok }

// Has reports whether k is present.
func (o *Ordered[V]) Has(k string) bool { _, ok := o.vals[k]; return ok }

// Set stores v under k, appending k to the order if it is new.
func (o *Ordered[V]) Set(k string, v V) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Keys returns the keys in declaration order; it aliases the backing slice.
func (o *Ordered[V]) Keys() []string { return o.keys }

// Len is the number of keys.
func (o *Ordered[V]) Len() int { return len(o.keys) }
