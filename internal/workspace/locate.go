// Package workspace ports the workspace-resolution half of khub's core:
// locate.go carries src/khub/core/locate.py (find_workspace, provenance) plus
// stale_days from src/khub/core/project.py and its DEFAULT_STALE_DAYS anchor
// from src/khub/core/workspace.py. Workspace scaffolding (the init port) lands in a
// sibling file.
package workspace

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/omap"
)

// DefaultStaleDays is workspace.py DEFAULT_STALE_DAYS: the staleness threshold
// when config.yaml carries none. The init port writes it into fresh configs;
// StaleDays falls back to it.
const DefaultStaleDays = 90

// FindWorkspace returns the workspace root (the dir holding ".khub/") at or
// above start — locate.find_workspace. The start path is resolved like
// Python's Path.resolve() (absolute, symlinks followed when the path exists);
// a miss is the shared no_workspace resolution error.
func FindWorkspace(start string) (string, error) {
	cur, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(cur); rerr == nil {
		cur = resolved
	}
	for d := cur; ; {
		if fi, serr := os.Stat(filepath.Join(d, ".khub")); serr == nil && fi.IsDir() {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", errs.NoWorkspace()
}

// Provenance is locate.provenance: the workspace's preset name and version
// from .khub/config.yaml, as the two-key ordered record the schema/status
// views embed verbatim ({"preset": ..., "version": ...}; key order is the
// JSON contract). Missing or null values read as "". A missing config.yaml
// errors like Python's FileNotFoundError (the CLI os_error boundary).
func Provenance(root string) (*omap.Map, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	out := omap.New()
	out.Set("preset", configString(cfg, "preset"))
	out.Set("version", configString(cfg, "version"))
	return out, nil
}

// PresetSource is the `source` init recorded in .khub/config.yaml: the
// directory the preset was resolved from, or "" for the packaged presets
// (Python's None). A relative source is resolved against the workspace root,
// so an upgrade run from any directory reads the same tree init did. Missing
// and null both read as "".
func PresetSource(root string) (string, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return "", err
	}
	source := configString(cfg, "source")
	if source == "" || filepath.IsAbs(source) {
		return source, nil
	}
	return filepath.Join(root, source), nil
}

// StaleDays is project.stale_days: the workspace's threshold from
// .khub/config.yaml. Tolerates a null defaults: block, a null/blank
// stale_days: value, and a quoted number; any absent/empty value falls back
// to DefaultStaleDays.
func StaleDays(root string) (int, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return 0, err
	}
	dv, _ := cfg.Get("defaults")
	defaults, err := asMapOrEmpty(dv, "defaults")
	if err != nil {
		return 0, err
	}
	raw, ok := defaults.Get("stale_days")
	if !ok || raw == nil || raw == "" {
		return DefaultStaleDays, nil
	}
	return staleDaysInt(raw)
}

// loadConfig reads and parses .khub/config.yaml — resolve.load_yaml applied
// to the config path, including the `data or {}` falsy-to-empty collapse.
func loadConfig(root string) (*omap.Map, error) {
	data, err := fsio.ReadFile(root, filepath.Join(root, ".khub", "config.yaml"))
	if err != nil {
		return nil, err
	}
	v, err := canon.LoadDoc(string(data))
	if err != nil {
		return nil, err
	}
	return asMapOrEmpty(v, "config.yaml")
}

// asMapOrEmpty mirrors `value or {}` followed by mapping use: falsy values
// become an empty map; a truthy non-mapping is an error (Python would crash
// on .get with AttributeError — surfaced here with a description instead).
func asMapOrEmpty(v any, where string) (*omap.Map, error) {
	if m, ok := v.(*omap.Map); ok {
		return m, nil
	}
	if pyFalsy(v) {
		return omap.New(), nil
	}
	return nil, fmt.Errorf("%s is not a mapping (got %T)", where, v)
}

// pyFalsy reports whether a loaded YAML value is falsy under Python rules.
func pyFalsy(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case int64:
		return x == 0
	case float64:
		return x == 0
	case canon.BigInt:
		return x.Literal == "0"
	case []any:
		return len(x) == 0
	}
	return false
}

// configString is cfg.get(key, "") narrowed to the str the annotation
// promises: missing and null read as ""; non-string scalars render as their
// Python str() form.
func configString(cfg *omap.Map, key string) string {
	v, ok := cfg.Get(key)
	if !ok || v == nil {
		return ""
	}
	if s, isStr := v.(string); isStr {
		return s
	}
	return pyScalarString(v)
}

// pyScalarString renders a loaded YAML scalar the way Python str() would.
func pyScalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(x, 10)
	case canon.BigInt:
		return x.Literal
	case float64:
		return canon.PyFloatRepr(x)
	case canon.Date:
		return x.ISO
	case canon.DateTime:
		return x.ISO
	default:
		return fmt.Sprintf("%v", v)
	}
}

// staleDaysInt is int(raw) over the value shapes YAML loading produces.
func staleDaysInt(raw any) (int, error) {
	switch x := raw.(type) {
	case int64:
		return int(x), nil
	case int:
		return x, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, fmt.Errorf("stale_days is not a finite number: %v", x)
		}
		return int(x), nil // int() truncates toward zero
	case canon.BigInt:
		n, err := strconv.Atoi(x.Literal)
		if err != nil {
			return 0, fmt.Errorf("stale_days out of range: %s", x.Literal)
		}
		return n, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("invalid stale_days %q", x)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("invalid stale_days value of type %T", raw)
	}
}
