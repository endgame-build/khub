package cli

import (
	"slices"
	"testing"

	"github.com/spf13/pflag"

	"github.com/endgame-build/khub/internal/savedquery"
)

// runOutputFlags are the query flags that shape one run's output rather than
// select its rows. A saved query never holds them; `run` takes each itself.
var runOutputFlags = []string{"format", "receipt"}

// queryOnlyFlags drive the save itself.
var queryOnlyFlags = []string{"save-as", "replace", "help"}

// Every flag `khub query` declares is either saved by --save-as (a key in
// savedquery.Keys) or an output flag `run` accepts too. A flag added to
// query without either fails here, so `run <name>` can never silently drop
// part of the query it was saved from.
func TestSavedQueryCoversEveryQueryFlag(t *testing.T) {
	root := newRoot()
	query, _, err := root.Find([]string{"query"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := root.Find([]string{"run"})
	if err != nil || run.Name() != "run" {
		t.Fatalf("no run command: %v", err)
	}
	query.Flags().VisitAll(func(f *pflag.Flag) {
		switch {
		case slices.Contains(queryOnlyFlags, f.Name):
		case slices.Contains(runOutputFlags, f.Name):
			if run.Flags().Lookup(f.Name) == nil {
				t.Errorf("query --%s shapes the output, but run does not take it", f.Name)
			}
		case !slices.Contains(savedquery.Keys, f.Name):
			t.Errorf("query --%s is neither saved by --save-as (savedquery.Keys) nor an output flag run takes", f.Name)
		}
	})
	for _, key := range savedquery.Keys {
		if query.Flags().Lookup(key) == nil {
			t.Errorf("savedquery.Keys holds %q, which is not a query flag", key)
		}
	}
}
