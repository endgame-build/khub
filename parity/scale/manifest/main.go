// manifest is the one helper parity/scale/smoke.sh needs beyond the Go
// toolchain: it prints a field out of smoke-manifest.json, reduces a khub JSON
// payload on stdin to the number or set an assertion compares, and tells the
// time in milliseconds (macOS ships bash 3.2, which has no EPOCHREALTIME).
//
//	manifest <file> <dotted.path>       one field: scalars bare, arrays space-joined, objects canonical
//	manifest -json <op> [arg] < payload  len | slugs | reach <slug> | field <key> | count <key> | canon
//	manifest -utf8 <dir>                 the number of *.md files under dir that are not valid UTF-8
//	manifest -now                        milliseconds since the epoch
package main

import (
	// A dev tool, not a khub output path — see parity/scale/corpus/corpus.go.
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "-now":
		fmt.Println(time.Now().UnixMilli())
	case "-utf8":
		if len(args) != 2 {
			usage()
		}
		fmt.Println(badUTF8(args[1]))
	case "-json":
		if len(args) < 2 {
			usage()
		}
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			die(err)
		}
		var payload any
		if err := json.Unmarshal(raw, &payload); err != nil {
			die(fmt.Errorf("stdin is not JSON: %v", err))
		}
		fmt.Println(reduce(payload, args[1], args[2:]))
	default:
		if len(args) != 2 {
			usage()
		}
		raw, err := os.ReadFile(args[0])
		if err != nil {
			die(err)
		}
		var payload any
		if err := json.Unmarshal(raw, &payload); err != nil {
			die(fmt.Errorf("%s: %v", args[0], err))
		}
		v, err := walk(payload, args[1])
		if err != nil {
			die(err)
		}
		fmt.Println(render(v))
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: manifest <file> <dotted.path> | -json <op> [arg] | -utf8 <dir> | -now")
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "manifest: %v\n", err)
	os.Exit(1)
}

// walk follows a dotted path through objects and arrays.
func walk(v any, path string) (any, error) {
	for _, step := range strings.Split(path, ".") {
		switch node := v.(type) {
		case map[string]any:
			next, ok := node[step]
			if !ok {
				return nil, fmt.Errorf("no key %q in %s", step, path)
			}
			v = next
		case []any:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("no index %q in %s", step, path)
			}
			v = node[i]
		default:
			return nil, fmt.Errorf("%s: %q is a scalar", path, step)
		}
	}
	return v, nil
}

// render prints a scalar bare, an array of scalars space-joined, and anything
// else as canonical JSON.
func render(v any) string {
	switch node := v.(type) {
	case string:
		return node
	case float64:
		return strconv.FormatFloat(node, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(node)
	case nil:
		return "null"
	case []any:
		parts := make([]string, 0, len(node))
		for _, item := range node {
			if _, nested := item.(map[string]any); nested {
				return canon(v)
			}
			if _, nested := item.([]any); nested {
				return canon(v)
			}
			parts = append(parts, render(item))
		}
		return strings.Join(parts, " ")
	default:
		return canon(v)
	}
}

// canon is JSON with sorted keys and no insignificant whitespace, so two
// payloads compare as data.
func canon(v any) string {
	b, err := json.Marshal(v) // encoding/json sorts map keys
	if err != nil {
		die(err)
	}
	return string(b)
}

func reduce(payload any, op string, args []string) string {
	arg := func() string {
		if len(args) != 1 {
			usage()
		}
		return args[0]
	}
	switch op {
	case "len":
		return strconv.Itoa(len(array(payload)))
	case "slugs":
		return strings.Join(slugs(payload, ""), " ")
	case "reach":
		return strconv.Itoa(len(slugs(payload, arg())))
	case "field":
		obj, ok := payload.(map[string]any)
		if !ok {
			die(fmt.Errorf("field: payload is not an object"))
		}
		return render(obj[arg()])
	case "count":
		obj, ok := payload.(map[string]any)
		if !ok {
			die(fmt.Errorf("count: payload is not an object"))
		}
		return strconv.Itoa(len(array(obj[arg()])))
	case "canon":
		return canon(payload)
	}
	usage()
	return ""
}

func array(v any) []any {
	items, ok := v.([]any)
	if !ok {
		die(fmt.Errorf("expected a JSON array, got %T", v))
	}
	return items
}

// slugs is the distinct `slug` values of an array of records, sorted, minus
// one to exclude — `impact` reports its own start at depth 0 and a deep
// `neighbors` walk can re-reach it, and neither is a hop.
func slugs(payload any, exclude string) []string {
	var out []string
	for _, item := range array(payload) {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		slug, _ := rec["slug"].(string)
		if slug != "" && slug != exclude {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func badUTF8(dir string) int {
	bad := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(raw) {
			bad++
		}
		return nil
	})
	if err != nil {
		die(err)
	}
	return bad
}
