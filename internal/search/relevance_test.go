package search

// Relevance regression suite. testdata/relevance is a hand-written build-hub
// corpus seeded with the shapes ranking gets wrong: a term named in one
// entity's title but only mentioned in another's body, a word repeated through
// a long body, accented and non-Latin scripts. Each judged query names the
// entities that answer it and how deep the first one may sit.
//
// The suite scores mean average precision over the top ten, so every relevant
// entity's position counts, not only the first. Run with -v to print each
// query's ranking; that table is what weights are tuned against.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type judged struct {
	query    string
	relevant []string // slugs that answer the query; empty means no hits at all
	topK     int      // the first relevant hit must rank at or above this
}

var judgedQueries = []judged{
	{"encryption", []string{"ad-2025-06-14-envelope-encryption", "req-encrypt-sensitive-payloads", "cmp-kms-gateway"}, 1},
	{`"headless chrome"`, []string{"ad-2025-11-20-chrome-for-pdf", "cmp-pdf-renderer"}, 1},
	{"tracking", []string{"cmp-tracking-api", "cap-shipment-tracking", "api-tracking-v2"}, 1},
	{"invoice OR invoices", []string{"req-invoice-pdf", "uc-pay-invoice", "api-billing"}, 1},
	{"straße", []string{"req-strasse-addresses"}, 1},
	{"café", []string{"cmp-cafe-kiosk"}, 1},
	{"login", []string{"req-login-rate-limit", "ad-2025-09-01-redis-rate-limiter", "cmp-auth-service"}, 1},
	{"erasure", []string{"req-delete-customer-data"}, 2},
	{"NEAR(carrier* webhook*, 5)", []string{"ad-2026-02-05-webhooks-over-polling", "req-shipment-status-latency", "cmp-tracking-api"}, 1},
	{"postgres", []string{"ad-2025-03-02-use-postgres"}, 1},
	{"kiosk", []string{"cmp-cafe-kiosk"}, 1},
	{"offline", []string{"req-offline-scanning", "cmp-scanner-app"}, 1},
	{"currency", []string{"req-multi-currency"}, 1},
	{"zzzqqq", nil, 0},
}

// judgedPlainQueries run with Options.Plain: the text an agent would type or
// paste, punctuation and repeats included.
var judgedPlainQueries = []judged{
	{"encrypt customer sensitive payload", []string{"req-encrypt-sensitive-payloads"}, 1},
	{"how do we handle login rate-limit?", []string{"req-login-rate-limit", "ad-2025-09-01-redis-rate-limiter", "cmp-auth-service"}, 1},
	{"rate rate rate currency quote", []string{"req-multi-currency", "cmp-fx-rates"}, 1},
	{"erase customer data (GDPR)", []string{"req-delete-customer-data"}, 1},
	{"cafe", []string{"cmp-cafe-kiosk"}, 1},
	{"cafe\u0301", []string{"cmp-cafe-kiosk"}, 1},
	{"हिन्दी labels", []string{"req-hindi-labels"}, 1},
	{"Strasse", []string{"req-strasse-addresses"}, 1},
	{longPaste, []string{"req-offline-scanning", "cmp-scanner-app"}, 1},
	{"zzzqqq", nil, 0},
	{"((( ))) -- ^ *", nil, 0},
}

// longPaste is an over-long task description with one real subject in it.
var longPaste = strings.Repeat("please could you look into this for me when you get a chance thanks ", 12) +
	"the depot handheld keeps losing scans while offline"

// mapFloor is the mean average precision the suite must not fall below.
const mapFloor = 0.955

func TestRelevance(t *testing.T) {
	mean := relevanceMAP(t, relevanceWS(t), true)
	if mean < mapFloor {
		t.Errorf("MAP@10 = %.3f, below the floor %.3f", mean, mapFloor)
	}
}

// TestRelevanceWeightSweep prints MAP@10 for a range of title weights, the
// table the pinned weights were chosen from. Set KHUB_RELEVANCE_SWEEP=1 to
// run it.
func TestRelevanceWeightSweep(t *testing.T) {
	if os.Getenv("KHUB_RELEVANCE_SWEEP") == "" {
		t.Skip("set KHUB_RELEVANCE_SWEEP=1")
	}
	ws := relevanceWS(t)
	pinned := titleWeight
	defer func() { titleWeight = pinned }()
	for _, w := range []float64{1, 2, 3, 5, 10, 20} {
		titleWeight = w
		t.Logf("title weight %4.1f: MAP@10 = %.3f", w, relevanceMAP(t, ws, false))
	}
}

// relevanceMAP runs every judged query and returns MAP@10. With strict, it
// logs each ranking and fails a query whose first answer sits below its topK.
func relevanceMAP(t *testing.T, ws string, strict bool) float64 {
	t.Helper()
	var sum float64
	scored := 0
	var all []judged
	var plain []bool
	for _, q := range judgedQueries {
		all, plain = append(all, q), append(plain, false)
	}
	for _, q := range judgedPlainQueries {
		all, plain = append(all, q), append(plain, true)
	}
	for i, q := range all {
		res, err := Search(ws, q.query, Options{Limit: 10, Plain: plain[i]})
		if err != nil {
			t.Fatalf("Search(%q): %v", q.query, err)
		}
		got := hitSlugs(res.Hits)
		if len(q.relevant) == 0 {
			if strict && len(got) != 0 {
				t.Errorf("%q: want no hits, got %v", q.query, got)
			}
			continue
		}
		rank := firstRelevant(got, q.relevant)
		ap := averagePrecision(got, q.relevant)
		scored++
		sum += ap
		if !strict {
			continue
		}
		t.Logf("%-32.32q plain=%-5v AP %.3f  first %d  %v", q.query, plain[i], ap, rank, got)
		if rank == 0 || rank > q.topK {
			t.Errorf("%q: first relevant hit at rank %d, want <= %d (got %v)", q.query, rank, q.topK, got)
		}
	}
	mean := sum / float64(scored)
	if strict {
		t.Logf("MAP@10 = %.3f over %d queries", mean, scored)
	}
	return mean
}

// averagePrecision averages precision at the rank of each relevant hit. A
// relevant entity missing from got contributes zero.
func averagePrecision(got, relevant []string) float64 {
	want := map[string]bool{}
	for _, s := range relevant {
		want[s] = true
	}
	found, sum := 0, 0.0
	for i, s := range got {
		if want[s] {
			found++
			sum += float64(found) / float64(i+1)
		}
	}
	return sum / float64(len(relevant))
}

// firstRelevant is the 1-based rank of the first hit in relevant, or 0.
func firstRelevant(got, relevant []string) int {
	want := map[string]bool{}
	for _, s := range relevant {
		want[s] = true
	}
	for i, s := range got {
		if want[s] {
			return i + 1
		}
	}
	return 0
}

// relevanceWS lays testdata/relevance over a fresh build-hub workspace.
func relevanceWS(t *testing.T) string {
	t.Helper()
	ws := wsFromPreset(t, "build-hub")
	src := filepath.Join("testdata", "relevance")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seedRaw(t, ws, filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}
