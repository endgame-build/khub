// Package omap provides the insertion-ordered map that backs every dynamic
// record in khub — frontmatter, collection rows, JSON payloads. Key order is
// observable contract (go-port-plan R5); map[string]any never crosses an API
// boundary.
package omap

type Map struct {
	keys []string
	vals map[string]any
}

func New() *Map { return &Map{vals: map[string]any{}} }

func (m *Map) Get(k string) (any, bool) { v, ok := m.vals[k]; return v, ok }

func (m *Map) Set(k string, v any) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

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

func (m *Map) Keys() []string { return m.keys }
func (m *Map) Len() int       { return len(m.keys) }
