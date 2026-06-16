// Command gen-release-fixtures regenerates the release fixture set used
// by `gum gain --fixture-replay` and the release-gate composition check
// (spec.md §12.3, bead gum-hvx). The output tree at
// internal/bench/fixtures/release/ holds:
//
//	manifest.json                 — one entry per fixture, with category + op_id
//	<category>/<seq>-<name>/      — one directory per fixture, with:
//	  request.json                — { "op_id":..., "args":... }
//	  response.json               — raw upstream JSON shape (gum gain reads this)
//
// Categories and target ratios are taken verbatim from spec §12.3:
//
//	workspace_toon_read   50%   Gmail/Drive/Calendar/Sheets reads, shape compresses well under TOON
//	gum_parallel_batch    20%   gum_parallel{} envelopes carrying 2-4 sub-results
//	non_workspace_read    15%   Maps / BigQuery / genai / YouTube Data reads
//	write_destructive     15%   send/insert/trash/delete/create across ≥3 Workspace services
//
// Determinism: the generator seeds math/rand with a constant so successive
// runs produce byte-identical output. CI re-runs the generator and asserts
// `git diff --exit-code` is clean, so any drift trips the gate.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
)

const (
	totalFixtures     = 200
	workspaceReadN    = 100
	gumParallelN      = 40
	nonWorkspaceReadN = 30
	writeDestructiveN = 30
)

// Manifest is the on-disk fixture index. Each entry records the
// fixture path (relative to the manifest), the category bucket used by
// the composition gate, and the op_id from request.json so the gate
// can spot-check classification without re-reading every file.
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	GeneratedBy   string          `json:"generated_by"`
	Total         int             `json:"total"`
	Entries       []ManifestEntry `json:"entries"`
}

type ManifestEntry struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	OpID     string `json:"op_id"`
}

func main() {
	out := flag.String("out", filepath.Join("internal", "bench", "fixtures", "release"), "output directory for the release fixture set")
	flag.Parse()

	if err := os.RemoveAll(*out); err != nil {
		fmt.Fprintf(os.Stderr, "gen-release-fixtures: clean %s: %v\n", *out, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "gen-release-fixtures: mkdir %s: %v\n", *out, err)
		os.Exit(1)
	}

	r := rand.New(rand.NewSource(20260524))

	var entries []ManifestEntry
	entries = append(entries, genWorkspaceReads(r, *out)...)
	entries = append(entries, genGumParallel(r, *out)...)
	entries = append(entries, genNonWorkspaceReads(r, *out)...)
	entries = append(entries, genWriteDestructive(r, *out)...)

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	manifest := Manifest{
		SchemaVersion: 1,
		GeneratedBy:   "cmd/gen-release-fixtures (bead gum-hvx)",
		Total:         len(entries),
		Entries:       entries,
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-release-fixtures: marshal manifest: %v\n", err)
		os.Exit(1)
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(*out, "manifest.json"), body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-release-fixtures: write manifest: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("gen-release-fixtures: wrote %d fixtures under %s\n", len(entries), *out)
}

// writeFixture writes one fixture directory and returns its manifest entry.
func writeFixture(outRoot, category, name string, seq int, opID string, args, response any) ManifestEntry {
	dir := filepath.Join(category, fmt.Sprintf("%03d-%s", seq, name))
	abs := filepath.Join(outRoot, dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		panic(fmt.Errorf("mkdir %s: %w", abs, err))
	}

	req := map[string]any{"op_id": opID, "args": args}
	writeJSON(filepath.Join(abs, "request.json"), req)
	writeJSON(filepath.Join(abs, "response.json"), response)

	return ManifestEntry{Path: dir, Category: category, OpID: opID}
}

func writeJSON(path string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("marshal %s: %w", path, err))
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		panic(fmt.Errorf("write %s: %w", path, err))
	}
}

// ── Workspace TOON reads (100) ──────────────────────────────────────────────

func genWorkspaceReads(r *rand.Rand, out string) []ManifestEntry {
	const cat = "workspace_toon_read"
	var es []ManifestEntry
	seq := 0
	// 35 gmail messages.list at varied sizes (1, 5, 10, 25, 50) × 7 each.
	sizes := []int{1, 5, 10, 25, 50}
	for _, size := range sizes {
		for i := 0; i < 7; i++ {
			seq++
			args := map[string]any{
				"userId":     "me",
				"q":          fmt.Sprintf("label:inbox newer_than:%dd", 1+i),
				"maxResults": size,
			}
			es = append(es, writeFixture(out, cat, fmt.Sprintf("gmail-list-%d", size), seq,
				"gmail.users.messages.list", args, gmailListResponse(r, size)))
		}
	}
	// 20 gmail messages.get single-message bodies.
	for i := 0; i < 20; i++ {
		seq++
		args := map[string]any{"userId": "me", "id": fakeMsgID(r)}
		es = append(es, writeFixture(out, cat, "gmail-get", seq,
			"gmail.users.messages.get", args, gmailGetResponse(r)))
	}
	// 15 drive files.list at varied sizes.
	dSizes := []int{3, 10, 25, 50, 100}
	for _, size := range dSizes {
		for i := 0; i < 3; i++ {
			seq++
			args := map[string]any{
				"q":        "mimeType != 'application/vnd.google-apps.folder'",
				"pageSize": size,
				"fields":   "files(id,name,mimeType,modifiedTime,size,owners)",
			}
			es = append(es, writeFixture(out, cat, fmt.Sprintf("drive-list-%d", size), seq,
				"drive.files.list", args, driveListResponse(r, size)))
		}
	}
	// 10 drive files.get
	for i := 0; i < 10; i++ {
		seq++
		args := map[string]any{"fileId": fakeDriveID(r), "fields": "id,name,mimeType,modifiedTime,size,owners"}
		es = append(es, writeFixture(out, cat, "drive-get", seq,
			"drive.files.get", args, driveGetResponse(r)))
	}
	// 10 calendar events.list
	for i := 0; i < 10; i++ {
		seq++
		size := 5 + r.Intn(15)
		args := map[string]any{
			"calendarId":   "primary",
			"timeMin":      "2026-05-01T00:00:00Z",
			"timeMax":      "2026-06-01T00:00:00Z",
			"singleEvents": true,
		}
		es = append(es, writeFixture(out, cat, "calendar-list", seq,
			"calendar.events.list", args, calendarListResponse(r, size)))
	}
	// 10 sheets values.get
	for i := 0; i < 10; i++ {
		seq++
		rows := 5 + r.Intn(20)
		args := map[string]any{
			"spreadsheetId": fakeSheetID(r),
			"range":         fmt.Sprintf("Sheet1!A1:D%d", rows),
		}
		es = append(es, writeFixture(out, cat, "sheets-values-get", seq,
			"sheets.spreadsheets.values.get", args, sheetsValuesResponse(r, rows)))
	}
	if len(es) != workspaceReadN {
		panic(fmt.Errorf("workspace_toon_read: produced %d, want %d", len(es), workspaceReadN))
	}
	return es
}

// ── gum_parallel batches (40) ───────────────────────────────────────────────

func genGumParallel(r *rand.Rand, out string) []ManifestEntry {
	const cat = "gum_parallel_batch"
	var es []ManifestEntry
	for i := 0; i < gumParallelN; i++ {
		seq := i + 1
		n := 2 + r.Intn(3) // 2..4
		calls := make([]map[string]any, 0, n)
		results := make([]map[string]any, 0, n)
		for j := 0; j < n; j++ {
			switch r.Intn(3) {
			case 0:
				calls = append(calls, map[string]any{
					"op_id": "gmail.users.messages.list",
					"args":  map[string]any{"userId": "me", "maxResults": 5},
				})
				results = append(results, map[string]any{
					"op_id":  "gmail.users.messages.list",
					"status": "ok",
					"data":   gmailListResponse(r, 5),
				})
			case 1:
				calls = append(calls, map[string]any{
					"op_id": "drive.files.list",
					"args":  map[string]any{"pageSize": 10, "fields": "files(id,name)"},
				})
				results = append(results, map[string]any{
					"op_id":  "drive.files.list",
					"status": "ok",
					"data":   driveListResponse(r, 10),
				})
			case 2:
				calls = append(calls, map[string]any{
					"op_id": "calendar.events.list",
					"args":  map[string]any{"calendarId": "primary"},
				})
				results = append(results, map[string]any{
					"op_id":  "calendar.events.list",
					"status": "ok",
					"data":   calendarListResponse(r, 6),
				})
			}
		}
		args := map[string]any{"calls": calls}
		resp := map[string]any{"batch_id": fmt.Sprintf("bat_%06d", seq), "results": results}
		es = append(es, writeFixture(out, cat, "parallel", seq, "gum_parallel", args, resp))
	}
	return es
}

// ── Non-Workspace reads (30) ────────────────────────────────────────────────

func genNonWorkspaceReads(r *rand.Rand, out string) []ManifestEntry {
	const cat = "non_workspace_read"
	var es []ManifestEntry
	seq := 0
	// 10 maps places.search
	for i := 0; i < 10; i++ {
		seq++
		args := map[string]any{"query": "coffee near me", "radius": 1500}
		es = append(es, writeFixture(out, cat, "maps-places", seq,
			"maps.places.searchText", args, mapsPlacesResponse(r, 5+r.Intn(8))))
	}
	// 8 youtube search
	for i := 0; i < 8; i++ {
		seq++
		args := map[string]any{"part": "snippet", "q": "golang testing", "maxResults": 10}
		es = append(es, writeFixture(out, cat, "youtube-search", seq,
			"youtube.search.list", args, youtubeSearchResponse(r, 10)))
	}
	// 7 bigquery jobs.query
	for i := 0; i < 7; i++ {
		seq++
		args := map[string]any{
			"query": "SELECT user_id, event, COUNT(*) c FROM `proj.ds.events` WHERE day=CURRENT_DATE() GROUP BY 1,2 LIMIT 50",
		}
		es = append(es, writeFixture(out, cat, "bigquery-query", seq,
			"bigquery.jobs.query", args, bigqueryQueryResponse(r, 10+r.Intn(20))))
	}
	// 5 genai generateContent
	for i := 0; i < 5; i++ {
		seq++
		args := map[string]any{
			"model":    "gemini-1.5-pro",
			"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "Summarize this PR in one sentence."}}}},
		}
		es = append(es, writeFixture(out, cat, "genai-generate", seq,
			"genai.models.generateContent", args, genaiResponse(r)))
	}
	if len(es) != nonWorkspaceReadN {
		panic(fmt.Errorf("non_workspace_read: produced %d, want %d", len(es), nonWorkspaceReadN))
	}
	return es
}

// ── Writes / destructives (30) ──────────────────────────────────────────────

func genWriteDestructive(r *rand.Rand, out string) []ManifestEntry {
	const cat = "write_destructive"
	var es []ManifestEntry
	seq := 0
	// 8 gmail send
	for i := 0; i < 8; i++ {
		seq++
		args := map[string]any{
			"userId": "me",
			"raw":    "VG86IHJlY2lwaWVudEBleGFtcGxlLmNvbQ0KU3ViamVjdDogcGluZw0KDQpoaQ==",
		}
		resp := map[string]any{
			"id":       fakeMsgID(r),
			"threadId": fakeMsgID(r),
			"labelIds": []string{"SENT"},
		}
		es = append(es, writeFixture(out, cat, "gmail-send", seq,
			"gmail.users.messages.send", args, resp))
	}
	// 5 gmail trash
	for i := 0; i < 5; i++ {
		seq++
		args := map[string]any{"userId": "me", "id": fakeMsgID(r)}
		resp := map[string]any{"id": args["id"], "labelIds": []string{"TRASH"}}
		es = append(es, writeFixture(out, cat, "gmail-trash", seq,
			"gmail.users.messages.trash", args, resp))
	}
	// 6 drive create
	for i := 0; i < 6; i++ {
		seq++
		args := map[string]any{"name": fmt.Sprintf("notes-%d.md", seq), "mimeType": "text/markdown"}
		resp := map[string]any{"id": fakeDriveID(r), "name": args["name"], "mimeType": args["mimeType"]}
		es = append(es, writeFixture(out, cat, "drive-create", seq,
			"drive.files.create", args, resp))
	}
	// 6 drive delete
	for i := 0; i < 6; i++ {
		seq++
		args := map[string]any{"fileId": fakeDriveID(r)}
		resp := map[string]any{}
		es = append(es, writeFixture(out, cat, "drive-delete", seq,
			"drive.files.delete", args, resp))
	}
	// 5 calendar insert
	for i := 0; i < 5; i++ {
		seq++
		args := map[string]any{
			"calendarId": "primary",
			"resource": map[string]any{
				"summary": fmt.Sprintf("Sync #%d", seq),
				"start":   map[string]any{"dateTime": "2026-06-01T15:00:00Z"},
				"end":     map[string]any{"dateTime": "2026-06-01T15:30:00Z"},
			},
		}
		resp := map[string]any{
			"id":      fakeCalEventID(r),
			"status":  "confirmed",
			"summary": args["resource"].(map[string]any)["summary"],
			"start":   args["resource"].(map[string]any)["start"],
			"end":     args["resource"].(map[string]any)["end"],
		}
		es = append(es, writeFixture(out, cat, "calendar-insert", seq,
			"calendar.events.insert", args, resp))
	}
	if len(es) != writeDestructiveN {
		panic(fmt.Errorf("write_destructive: produced %d, want %d", len(es), writeDestructiveN))
	}
	return es
}

// ── Shape builders ──────────────────────────────────────────────────────────

func gmailListResponse(r *rand.Rand, n int) map[string]any {
	msgs := make([]map[string]string, n)
	for i := range msgs {
		id := fakeMsgID(r)
		msgs[i] = map[string]string{"id": id, "threadId": id}
	}
	return map[string]any{"messages": msgs, "resultSizeEstimate": n}
}

func gmailGetResponse(r *rand.Rand) map[string]any {
	id := fakeMsgID(r)
	return map[string]any{
		"id":           id,
		"threadId":     id,
		"labelIds":     []string{"INBOX", "UNREAD"},
		"snippet":      "Hey, can you look at this draft when you get a moment? Thanks.",
		"sizeEstimate": 4321 + r.Intn(2000),
		"payload": map[string]any{
			"mimeType": "text/plain",
			"headers": []map[string]string{
				{"name": "From", "value": "alex@example.com"},
				{"name": "To", "value": "you@example.com"},
				{"name": "Subject", "value": "Draft review"},
				{"name": "Date", "value": "Sun, 24 May 2026 12:00:00 +0000"},
			},
			"body": map[string]any{"size": 1234 + r.Intn(800), "data": "SGV5LCBjYW4geW91IGxvb2sgYXQgdGhpcyBkcmFmdC4="},
		},
	}
}

func driveListResponse(r *rand.Rand, n int) map[string]any {
	files := make([]map[string]any, n)
	for i := range files {
		files[i] = map[string]any{
			"id":           fakeDriveID(r),
			"name":         fmt.Sprintf("doc-%03d.gdoc", i+1),
			"mimeType":     "application/vnd.google-apps.document",
			"modifiedTime": "2026-05-2" + fmt.Sprintf("%dT12:00:00Z", r.Intn(4)),
			"size":         fmt.Sprintf("%d", 2048+r.Intn(8192)),
			"owners":       []map[string]string{{"displayName": "Alex Owner", "emailAddress": "alex@example.com"}},
		}
	}
	return map[string]any{"files": files}
}

func driveGetResponse(r *rand.Rand) map[string]any {
	return map[string]any{
		"id":           fakeDriveID(r),
		"name":         "Roadmap Q3 2026.gdoc",
		"mimeType":     "application/vnd.google-apps.document",
		"modifiedTime": "2026-05-23T18:42:00Z",
		"size":         fmt.Sprintf("%d", 4096+r.Intn(20000)),
		"owners":       []map[string]string{{"displayName": "Pat Owner", "emailAddress": "pat@example.com"}},
	}
}

func calendarListResponse(r *rand.Rand, n int) map[string]any {
	items := make([]map[string]any, n)
	for i := range items {
		items[i] = map[string]any{
			"id":       fakeCalEventID(r),
			"status":   "confirmed",
			"summary":  fmt.Sprintf("Standup #%d", i+1),
			"start":    map[string]any{"dateTime": "2026-05-25T15:00:00Z"},
			"end":      map[string]any{"dateTime": "2026-05-25T15:15:00Z"},
			"attendees": []map[string]any{
				{"email": "a@example.com", "responseStatus": "accepted"},
				{"email": "b@example.com", "responseStatus": "needsAction"},
			},
		}
	}
	return map[string]any{"kind": "calendar#events", "items": items}
}

func sheetsValuesResponse(r *rand.Rand, rows int) map[string]any {
	values := make([][]any, rows)
	for i := range values {
		values[i] = []any{
			fmt.Sprintf("ROW-%03d", i+1),
			r.Intn(100),
			float64(r.Intn(10000)) / 100,
			[]string{"OK", "WARN", "FAIL"}[r.Intn(3)],
		}
	}
	return map[string]any{
		"range":          fmt.Sprintf("Sheet1!A1:D%d", rows),
		"majorDimension": "ROWS",
		"values":         values,
	}
}

func mapsPlacesResponse(r *rand.Rand, n int) map[string]any {
	places := make([]map[string]any, n)
	for i := range places {
		places[i] = map[string]any{
			"id":          fmt.Sprintf("ChIJ%010d", r.Intn(1<<30)),
			"displayName": map[string]string{"text": fmt.Sprintf("Cafe %d", i+1)},
			"location":    map[string]float64{"latitude": 37.42 + float64(r.Intn(100))/1000, "longitude": -122.08 + float64(r.Intn(100))/1000},
			"rating":      3.5 + float64(r.Intn(15))/10,
		}
	}
	return map[string]any{"places": places}
}

func youtubeSearchResponse(r *rand.Rand, n int) map[string]any {
	items := make([]map[string]any, n)
	for i := range items {
		items[i] = map[string]any{
			"kind": "youtube#searchResult",
			"id":   map[string]string{"kind": "youtube#video", "videoId": fmt.Sprintf("yt_%010d", r.Intn(1<<30))},
			"snippet": map[string]any{
				"publishedAt": "2026-05-20T10:00:00Z",
				"channelId":   "UCabc123",
				"title":       fmt.Sprintf("Go testing patterns episode %d", i+1),
				"description": "Hands-on look at table-driven tests in Go 1.23+",
			},
		}
	}
	return map[string]any{"kind": "youtube#searchListResponse", "items": items}
}

func bigqueryQueryResponse(r *rand.Rand, rows int) map[string]any {
	out := make([]map[string]any, rows)
	for i := range out {
		out[i] = map[string]any{
			"f": []map[string]any{
				{"v": fmt.Sprintf("u_%07d", r.Intn(1<<24))},
				{"v": []string{"open", "click", "share", "convert"}[r.Intn(4)]},
				{"v": fmt.Sprintf("%d", 1+r.Intn(50))},
			},
		}
	}
	return map[string]any{
		"jobReference":  map[string]string{"projectId": "proj", "jobId": fmt.Sprintf("job_%012d", r.Intn(1<<32))},
		"totalRows":     fmt.Sprintf("%d", rows),
		"schema":        map[string]any{"fields": []map[string]string{{"name": "user_id", "type": "STRING"}, {"name": "event", "type": "STRING"}, {"name": "c", "type": "INTEGER"}}},
		"rows":          out,
		"jobComplete":   true,
		"cacheHit":      false,
	}
}

func genaiResponse(r *rand.Rand) map[string]any {
	return map[string]any{
		"candidates": []map[string]any{{
			"content":      map[string]any{"role": "model", "parts": []map[string]string{{"text": "This PR replaces the BoltDB cache with a SQLite WAL cache for HTTP responses, adds an idempotent migration, and gates on a coverage floor."}}},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]int{"promptTokenCount": 412, "candidatesTokenCount": 64, "totalTokenCount": 476},
	}
}

func fakeMsgID(r *rand.Rand) string     { return fmt.Sprintf("18a%07x", r.Intn(1<<28)) }
func fakeDriveID(r *rand.Rand) string   { return fmt.Sprintf("1%027x", r.Int63n(1<<60)) }
func fakeCalEventID(r *rand.Rand) string { return fmt.Sprintf("ev_%010x", r.Int63n(1<<40)) }
func fakeSheetID(r *rand.Rand) string   { return fmt.Sprintf("ss_%030x", r.Int63n(1<<60)) }
