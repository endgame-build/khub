// Package omap provides the insertion-ordered map that backs every dynamic
// record in khub — frontmatter, collection rows, JSON payloads. Key order is
// observable contract; map[string]any never crosses an API
// boundary.
package omap

// Map is an insertion-ordered map with string keys. Set on an existing key
// keeps its position; Delete closes the gap and is O(n) in the key count,
// which no khub record is large enough to notice.
type Map struct {
	keys []string
	vals map[string]any
}

// New returns an empty Map.
func New() *Map { return &Map{vals: map[string]any{}} }

// Get returns the value for k and whether k is present.
func (m *Map) Get(k string) (any, bool) { v, ok := m.vals[k]; return v, ok }

// Set stores v under k, appending k to the order if it is new.
func (m *Map) Set(k string, v any) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

// Delete removes k; a missing key is a no-op.
func (m *Map) Delete(k string) {
	if _, ok := m.vals[k]; !ok {
		return
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the keys in insertion order. It aliases the backing slice:
// callers that mutate it mutate the map's order.
func (m *Map) Keys() []string { return m.keys }

// Len is the number of keys.
func (m *Map) Len() int { return len(m.keys) }
