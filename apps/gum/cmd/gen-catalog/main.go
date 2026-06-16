// Command gen-catalog generates the build-time Google capability catalog.
//
// Phase 0 skeleton: trivial entry point. Real generation pipeline lands in Phase 1
// per spec.md §5.4.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// opSpec is used internally to declare the fixed Phase-3 ops to generate.
type opSpec struct {
	opID          string
	title         string
	summary       string
	service       string
	serviceFamily string
	variantID     string
	httpMethod    string
	httpPath      string
	goPkg         string
	goCall        string
}

func httpGet(url string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	// A bounded timeout so a slow/hung Discovery endpoint can't stall the
	// generator (and CI) indefinitely.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("discovery doc HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

const (
	gmailDiscoveryURL    = "https://www.googleapis.com/discovery/v1/apis/gmail/v1/rest"
	calendarDiscoveryURL = "https://www.googleapis.com/discovery/v1/apis/calendar/v3/rest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-catalog: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	outPath := flag.String("out", "internal/embedded/catalog.json", "output path for catalog.json (overwritten)")
	gmailOnly := flag.Bool("gmail-only", false, "fetch only the gmail discovery doc (legacy behaviour)")
	stubsDir := flag.String("stubs-out", "gen/dispatch", "output directory for per-variant dispatch stubs (spec §5.7)")
	skipStubs := flag.Bool("no-stubs", false, "skip emitting gen/dispatch/*.go stubs")
	offlineStubsOnly := flag.Bool("offline-stubs-only", false, "skip network; load embedded catalog and only re-emit gen/dispatch/*.go stubs")
	injectMetaOffline := flag.Bool("inject-meta-offline", false, "skip network; load the existing catalog, append any missing meta ops (gum.code), and rewrite catalog.json + .sha256 in lockstep")
	injectGoogleAdsOffline := flag.Bool("inject-googleads-offline", false, "skip network; load the existing catalog, add/replace the Google Ads Keyword Planner ops, and rewrite catalog.json + .sha256 in lockstep")
	refreshSourceOpsFlag := flag.Bool("refresh-source-ops", false, "skip network; rebuild in-source hand-authored ops (Search Console) and replace matching ops in catalog.json by op_id, then rewrite catalog.json + .sha256 in lockstep")
	applyRequestFieldsFlag := flag.Bool("apply-request-fields", false, "skip network; set Op.RequestFields from the central Tier A map (request_fields_data.go) on matching ops in catalog.json, then rewrite catalog.json + .sha256 in lockstep")
	enrichRequestFieldsFlag := flag.Bool("enrich-request-fields", false, "fetch Discovery docs; apply the hand-map then derive RequestFields for every REST op still missing them, and rewrite catalog.json + .sha256 in lockstep")
	flag.Parse()

	if *offlineStubsOnly {
		return emitStubsOffline(*outPath, *stubsDir)
	}

	if *injectMetaOffline {
		return injectMeta(*outPath)
	}

	if *injectGoogleAdsOffline {
		return injectGoogleAds(*outPath)
	}

	if *refreshSourceOpsFlag {
		return refreshSourceOps(*outPath)
	}

	if *applyRequestFieldsFlag {
		return applyRequestFields(*outPath)
	}

	if *enrichRequestFieldsFlag {
		return enrichRequestFields(*outPath)
	}

	gmailResp, err := httpGet(gmailDiscoveryURL)
	if err != nil {
		return fmt.Errorf("fetch gmail discovery doc: %w", err)
	}
	defer func() { _ = gmailResp.Close() }()

	var cat *catalog.Catalog
	if *gmailOnly {
		cat, err = GenerateFromDiscovery(gmailResp)
		if err != nil {
			return err
		}
	} else {
		calResp, calErr := httpGet(calendarDiscoveryURL)
		if calErr != nil {
			return fmt.Errorf("fetch calendar discovery doc: %w", calErr)
		}
		defer func() { _ = calResp.Close() }()
		cat, err = GenerateFromDiscoveries(gmailResp, calResp)
		if err != nil {
			return err
		}
		// Append the Search Console family. These ops are hardcoded rather than
		// parsed from the searchconsole discovery doc because the v1 URL
		// Inspection endpoint and the v3 webmasters endpoints live on different
		// path prefixes — a single discovery walk would misroute one of them.
		cat.Ops = append(cat.Ops, BuildSearchConsoleOps()...)
		cat.Ops = append(cat.Ops, BuildCalendarWriteOps()...)
		cat.Ops = append(cat.Ops, BuildTasksOps()...)
		cat.Ops = append(cat.Ops, BuildFlightsOp())
		cat.Ops = append(cat.Ops, BuildDocsOps()...)
		cat.Ops = append(cat.Ops, BuildSheetsOps()...)
		cat.Ops = append(cat.Ops, BuildSlidesOps()...)
		cat.Ops = append(cat.Ops, BuildDriveOps()...)
		cat.Ops = append(cat.Ops, BuildGmailTierBOps()...)
		cat.Ops = append(cat.Ops, BuildCalendarTierBOps()...)
		cat.Ops = append(cat.Ops, BuildAdminDirectoryOps()...)
		cat.Ops = append(cat.Ops, BuildPeopleOps()...)
		cat.Ops = append(cat.Ops, BuildYouTubeDataOps()...)
		cat.Ops = append(cat.Ops, BuildFormsOps()...)
		cat.Ops = append(cat.Ops, BuildChatOps()...)
		cat.Ops = append(cat.Ops, BuildClassroomOps()...)
		cat.Ops = append(cat.Ops, BuildPhotosOps()...)
		cat.Ops = append(cat.Ops, BuildCloudIdentityOps()...)
		cat.Ops = append(cat.Ops, BuildAppsScriptOps()...)
		cat.Ops = append(cat.Ops, BuildVaultOps()...)
		cat.Ops = append(cat.Ops, BuildMeetOps()...)
		cat.Ops = append(cat.Ops, BuildGroupsSettingsOps()...)
		cat.Ops = append(cat.Ops, BuildIndexingOps()...)
		cat.Ops = append(cat.Ops, BuildAdminReportsOps()...)
		cat.Ops = append(cat.Ops, BuildCustomSearchOps()...)
		cat.Ops = append(cat.Ops, BuildMapsOps()...)
		cat.Ops = append(cat.Ops, BuildPlacesRoutesOps()...)
		cat.Ops = append(cat.Ops, BuildGoogleAdsOps()...)
		cat.Ops = append(cat.Ops, BuildUnofficialPluginOps()...)
		cat.Ops = append(cat.Ops, BuildMetaOps()...)
		if err := cat.Validate(); err != nil {
			return fmt.Errorf("validate catalog with searchconsole + calendar write + tasks + flights + docs + sheets + slides + drive + gmail-tier-b + calendar-tier-b + admin-directory + unofficial-plugin + meta ops: %w", err)
		}
	}

	// Per spec §5.4 line 676: validate any catalog-embedded expression-profile
	// definitions against docs/expression-profile-dsl.json. v0.1.0 catalogs hold
	// profile name-references only; this loop runs vacuously today but the gate
	// is in place for the day profiles get inlined.
	for _, raw := range catalogEmbeddedProfiles(cat) {
		if err := profile.ValidateRawProfileFile(raw); err != nil {
			return fmt.Errorf("expression-profile validation failed: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(*outPath), err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cat); err != nil {
		return fmt.Errorf("encode catalog: %w", err)
	}

	if err := os.WriteFile(*outPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *outPath, err)
	}

	if !*skipStubs && !*gmailOnly {
		n, err := WriteDispatchStubs(cat, *stubsDir)
		if err != nil {
			return fmt.Errorf("emit dispatch stubs: %w", err)
		}
		fmt.Fprintf(os.Stderr, "gen-catalog: emitted %d dispatch stubs to %s\n", n, *stubsDir)
	}

	// Companion .sha256 file in sha256sum-canonical format.
	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(*outPath))
	sha256Path := *outPath + ".sha256"
	if err := os.WriteFile(sha256Path, []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", sha256Path, err)
	}

	return nil
}

// emitStubsOffline loads the embedded catalog, augments it with the in-source
// Workspace Tier B builders that aren't yet in the embedded snapshot (Gmail
// Tier B + Calendar Tier B — daily CI regen hasn't run since they landed),
// and then re-runs WriteDispatchStubs. No network access is performed so the
// pipeline can ship the stub layer in dev environments and CI lanes without
// reaching out to discovery.googleapis.com.
func emitStubsOffline(catalogPath, stubsDir string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("offline stubs: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("offline stubs: parse %s: %w", catalogPath, err)
	}
	// Augment with in-source builders that may have landed after the last
	// embedded regeneration. Duplicates are filtered by op_id+variant_id so
	// re-running this against a fresh catalog.json stays idempotent.
	seen := map[string]bool{}
	for _, op := range cat.Ops {
		for _, v := range op.Variants {
			seen[op.OpID+"\x00"+v.VariantID] = true
		}
	}
	candidates := []catalog.Op{}
	candidates = append(candidates, BuildGmailTierBOps()...)
	candidates = append(candidates, BuildCalendarTierBOps()...)
	candidates = append(candidates, BuildAdminDirectoryOps()...)
	for _, op := range candidates {
		dup := false
		for _, v := range op.Variants {
			if seen[op.OpID+"\x00"+v.VariantID] {
				dup = true
				break
			}
		}
		if !dup {
			cat.Ops = append(cat.Ops, op)
		}
	}
	// Validate the augmented catalog before emitting stubs — mirrors every other
	// offline path (inject-meta, refresh-source-ops, apply/enrich-request-fields).
	// Without this, a malformed in-source builder op (dup op_id, empty summary,
	// dangling default_variant_id, bad enum) would silently produce stubs for an
	// invalid catalog instead of failing fast here.
	if err := cat.Validate(); err != nil {
		return fmt.Errorf("offline stubs: validate augmented catalog: %w", err)
	}
	n, err := WriteDispatchStubs(&cat, stubsDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "gen-catalog: emitted %d dispatch stubs to %s (offline)\n", n, stubsDir)
	return nil
}

// injectMeta refreshes catalogPath with the meta-service ops (gum.code) without
// touching the network. It parses the existing catalog, appends any meta op not
// already present (idempotent by op_id), re-encodes with the same indented
// encoder the full pipeline uses, and rewrites catalog.json plus its companion
// .sha256 in lockstep. This is the network-free path used to land the gum.code
// op into the embedded snapshot (gum-7ras) and by CI lanes that cannot reach
// discovery.googleapis.com. generated_at is preserved verbatim so the diff is
// limited to the appended op.
func injectMeta(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("inject-meta: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("inject-meta: parse %s: %w", catalogPath, err)
	}

	present := map[string]bool{}
	for _, op := range cat.Ops {
		present[op.OpID] = true
	}
	added := 0
	for _, op := range BuildMetaOps() {
		if present[op.OpID] {
			continue
		}
		cat.Ops = append(cat.Ops, op)
		present[op.OpID] = true
		added++
	}

	if err := cat.Validate(); err != nil {
		return fmt.Errorf("inject-meta: validate catalog with meta ops: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&cat); err != nil {
		return fmt.Errorf("inject-meta: encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("inject-meta: write %s: %w", catalogPath, err)
	}

	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("inject-meta: write %s.sha256: %w", catalogPath, err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: injected %d meta op(s) into %s (offline)\n", added, catalogPath)
	return nil
}

// injectGoogleAds adds (or replaces, by op_id) the Google Ads Keyword Planner
// ops in catalogPath without touching the network, then rewrites catalog.json +
// .sha256 in lockstep. Unlike refreshSourceOps this also APPENDS ops that are
// not yet present, so it lands brand-new googleads ops offline with a minimal
// diff (generated_at preserved). Re-running it updates the ops in place.
func injectGoogleAds(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("inject-googleads: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("inject-googleads: parse %s: %w", catalogPath, err)
	}

	rebuilt := map[string]catalog.Op{}
	order := []string{}
	for _, op := range BuildGoogleAdsOps() {
		rebuilt[op.OpID] = op
		order = append(order, op.OpID)
	}

	replaced := 0
	matched := map[string]bool{}
	for i := range cat.Ops {
		if op, ok := rebuilt[cat.Ops[i].OpID]; ok {
			cat.Ops[i] = op
			matched[cat.Ops[i].OpID] = true
			replaced++
		}
	}
	added := 0
	for _, opID := range order {
		if matched[opID] {
			continue
		}
		cat.Ops = append(cat.Ops, rebuilt[opID])
		added++
	}

	if err := cat.Validate(); err != nil {
		return fmt.Errorf("inject-googleads: validate catalog with googleads ops: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&cat); err != nil {
		return fmt.Errorf("inject-googleads: encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("inject-googleads: write %s: %w", catalogPath, err)
	}

	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("inject-googleads: write %s.sha256: %w", catalogPath, err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: injected googleads ops into %s (offline): %d added, %d replaced\n", catalogPath, added, replaced)
	return nil
}

// refreshSourceOps rebuilds the in-source hand-authored ops and replaces the
// matching ops in catalogPath by op_id (preserving order), then rewrites
// catalog.json + .sha256 in lockstep. It is the network-free way to land
// builder changes (e.g. new RequestFields) into the embedded snapshot with a
// minimal diff: generated_at is preserved and only the rebuilt ops change.
// Currently scoped to manual source builders that need network-free snapshot
// refreshes after code changes.
func refreshSourceOps(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("refresh-source-ops: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("refresh-source-ops: parse %s: %w", catalogPath, err)
	}

	rebuilt := map[string]catalog.Op{}
	for _, op := range BuildSearchConsoleOps() {
		rebuilt[op.OpID] = op
	}
	for _, op := range BuildAdminDirectoryOps() {
		rebuilt[op.OpID] = op
	}

	replaced := 0
	matched := map[string]bool{}
	for i := range cat.Ops {
		if op, ok := rebuilt[cat.Ops[i].OpID]; ok {
			if len(op.RequestFields) == 0 {
				op.RequestFields = cat.Ops[i].RequestFields
			}
			cat.Ops[i] = op
			matched[cat.Ops[i].OpID] = true
			replaced++
		}
	}
	// Surface builder ops that aren't in the catalog yet — this offline path
	// only REPLACES existing ops; a brand-new op needs a full (network) regen.
	for opID := range rebuilt {
		if !matched[opID] {
			fmt.Fprintf(os.Stderr, "refresh-source-ops: WARNING: builder op %q not present in %s; run a full regen to add it (not added here)\n", opID, catalogPath)
		}
	}

	if err := cat.Validate(); err != nil {
		return fmt.Errorf("refresh-source-ops: validate catalog: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&cat); err != nil {
		return fmt.Errorf("refresh-source-ops: encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("refresh-source-ops: write %s: %w", catalogPath, err)
	}

	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("refresh-source-ops: write %s.sha256: %w", catalogPath, err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: refreshed %d source op(s) in %s (offline)\n", replaced, catalogPath)
	return nil
}

// applyRequestFields sets Op.RequestFields from the central Tier A map onto
// matching ops in catalogPath (by op_id), then rewrites catalog.json + .sha256
// in lockstep. Network-free, minimal diff (only request_fields added/changed),
// generated_at preserved. Warns about map entries with no matching catalog op.
func applyRequestFields(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("apply-request-fields: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("apply-request-fields: parse %s: %w", catalogPath, err)
	}

	fieldsByOp := tierARequestFields()
	applied := 0
	matched := map[string]bool{}
	for i := range cat.Ops {
		if rf, ok := fieldsByOp[cat.Ops[i].OpID]; ok {
			cat.Ops[i].RequestFields = rf
			matched[cat.Ops[i].OpID] = true
			applied++
		}
	}
	for opID := range fieldsByOp {
		if !matched[opID] {
			fmt.Fprintf(os.Stderr, "apply-request-fields: WARNING: op %q in the map is not in %s (skipped)\n", opID, catalogPath)
		}
	}

	if err := cat.Validate(); err != nil {
		return fmt.Errorf("apply-request-fields: validate catalog: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&cat); err != nil {
		return fmt.Errorf("apply-request-fields: encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("apply-request-fields: write %s: %w", catalogPath, err)
	}
	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("apply-request-fields: write %s.sha256: %w", catalogPath, err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: applied request fields to %d op(s) in %s (offline)\n", applied, catalogPath)
	return nil
}

// discoveryDoc is a minimal shape for parsing a Google Discovery document.
type discoveryDoc struct {
	Name      string                       `json:"name"`
	Version   string                       `json:"version"`
	Resources map[string]discoveryResource `json:"resources"`
}

type discoveryResource struct {
	Resources map[string]discoveryResource `json:"resources"`
	Methods   map[string]discoveryMethod   `json:"methods"`
}

type discoveryMethod struct {
	ID          string                 `json:"id"`
	HTTPMethod  string                 `json:"httpMethod"`
	Path        string                 `json:"path"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Scopes      []string               `json:"scopes"`
}

// GenerateFromDiscovery parses a Google discovery doc from r and returns a Catalog
// containing the operations found in the document.
//
// The doc's top-level "name" field determines which service is processed:
//   - "gmail"    → emits gmail.users.messages.list
//   - "calendar" → emits calendar.events.list and calendar.calendarList.list
//
// All variants use auth_strategy = byo_oauth (gum_oauth is disabled in v0.1.0).
// The returned Catalog always has catalog_schema_version=1 and a generated_at timestamp.
// This is the seam the green team must implement; tests call it directly to avoid
// network access.
func GenerateFromDiscovery(disco io.Reader) (*catalog.Catalog, error) {
	data, err := io.ReadAll(disco)
	if err != nil {
		return nil, fmt.Errorf("gen-catalog: read discovery doc: %w", err)
	}

	var doc discoveryDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("gen-catalog: parse discovery doc: %w", err)
	}

	var ops []catalog.Op

	switch doc.Name {
	case "gmail":
		// Walk resources.users.resources.messages.methods.list
		usersRes, ok := doc.Resources["users"]
		if !ok {
			return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users")
		}
		messagesRes, ok := usersRes.Resources["messages"]
		if !ok {
			return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages")
		}
		if _, ok = messagesRes.Methods["list"]; !ok {
			return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages.methods.list")
		}
		// Phase 4: emit send and trash if present in the discovery doc (optional for backwards compat).
		gmailOps := []catalog.Op{
			makeOp(opSpec{
				opID:          "gmail.users.messages.list",
				title:         "List Gmail messages",
				summary:       "List message IDs in a Gmail mailbox.",
				service:       "gmail",
				serviceFamily: "workspace",
				variantID:     "gmail.v1.rest.users.messages.list",
				httpMethod:    "GET",
				httpPath:      "/gmail/v1/users/{userId}/messages",
				goPkg:         "google.golang.org/api/gmail/v1",
				goCall:        "Users.Messages.List",
			}),
		}
		if _, ok = messagesRes.Methods["send"]; ok {
			gmailOps = append(gmailOps, makeWriteOp(opSpec{
				opID:          "gmail.users.messages.send",
				title:         "Send Gmail message",
				summary:       "Sends a Gmail message on behalf of the authenticated user.",
				service:       "gmail",
				serviceFamily: "workspace",
				variantID:     "gmail.v1.rest.users.messages.send",
				httpMethod:    "POST",
				httpPath:      "/gmail/v1/users/{userId}/messages/send",
				goPkg:         "google.golang.org/api/gmail/v1",
				goCall:        "Users.Messages.Send",
			}))
		}
		if _, ok = messagesRes.Methods["trash"]; ok {
			gmailOps = append(gmailOps, makeDestructiveOp(opSpec{
				opID:          "gmail.users.messages.trash",
				title:         "Delete Gmail message (via Trash)",
				summary:       "Moves a Gmail message to the Trash. Permanently deletes after 30 days.",
				service:       "gmail",
				serviceFamily: "workspace",
				variantID:     "gmail.v1.rest.users.messages.trash",
				httpMethod:    "POST",
				httpPath:      "/gmail/v1/users/{userId}/messages/{id}/trash",
				goPkg:         "google.golang.org/api/gmail/v1",
				goCall:        "Users.Messages.Trash",
			}))
		}
		// Tier A gmail.create_draft is backed by gmail.users.drafts.create (gum-7tuq.1).
		if draftsRes, ok := usersRes.Resources["drafts"]; ok {
			if _, ok := draftsRes.Methods["create"]; ok {
				gmailOps = append(gmailOps, makeWriteOp(opSpec{
					opID:          "gmail.users.drafts.create",
					title:         "Create Gmail draft",
					summary:       "Creates a new draft with the DRAFT label.",
					service:       "gmail",
					serviceFamily: "workspace",
					variantID:     "gmail.v1.rest.users.drafts.create",
					httpMethod:    "POST",
					httpPath:      "/gmail/v1/users/{userId}/drafts",
					goPkg:         "google.golang.org/api/gmail/v1",
					goCall:        "Users.Drafts.Create",
				}))
			}
		}
		ops = gmailOps

	case "calendar":
		// Walk resources.events.methods.list and resources.calendarList.methods.list
		eventsRes, ok := doc.Resources["events"]
		if !ok {
			return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.events")
		}
		eventsListMethod, ok := eventsRes.Methods["list"]
		if !ok {
			return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.events.methods.list")
		}
		calListRes, ok := doc.Resources["calendarList"]
		if !ok {
			return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.calendarList")
		}
		calListMethod, ok := calListRes.Methods["list"]
		if !ok {
			return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.calendarList.methods.list")
		}

		// Use paths from the fixture; prepend basePath prefix if relative.
		eventsPath := eventsListMethod.Path
		if !strings.HasPrefix(eventsPath, "/") {
			eventsPath = "/calendar/v3/" + eventsPath
		}
		calListPath := calListMethod.Path
		if !strings.HasPrefix(calListPath, "/") {
			calListPath = "/calendar/v3/" + calListPath
		}

		ops = []catalog.Op{
			makeCalendarOp(opSpec{
				opID:          "calendar.events.list",
				title:         "List Calendar events",
				summary:       "List events on the specified calendar.",
				service:       "calendar",
				serviceFamily: "workspace",
				variantID:     "calendar.v3.rest.events.list",
				httpMethod:    eventsListMethod.HTTPMethod,
				httpPath:      eventsPath,
				goPkg:         "google.golang.org/api/calendar/v3",
				goCall:        "Events.List",
			}),
			makeCalendarOp(opSpec{
				opID:          "calendar.calendarList.list",
				title:         "List Calendar list",
				summary:       "Returns the calendars on the user's calendar list.",
				service:       "calendar",
				serviceFamily: "workspace",
				variantID:     "calendar.v3.rest.calendarList.list",
				httpMethod:    calListMethod.HTTPMethod,
				httpPath:      calListPath,
				goPkg:         "google.golang.org/api/calendar/v3",
				goCall:        "CalendarList.List",
			}),
		}

	default:
		return nil, fmt.Errorf("gen-catalog: unrecognised discovery doc name %q; expected \"gmail\" or \"calendar\"", doc.Name)
	}

	// Hard-fail: no op may use gum_oauth (disabled in v0.1.0 per bd memory gum-auth-strategy-v3).
	for _, op := range ops {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				return nil, fmt.Errorf("gen-catalog: gum_oauth is disabled in v0.1.0; op %s variant %s uses gum_oauth", op.OpID, v.VariantID)
			}
		}
	}

	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "gum/cmd/gen-catalog@phase3",
		Ops:                  ops,
	}

	if err := cat.Validate(); err != nil {
		return nil, fmt.Errorf("gen-catalog: catalog validation failed: %w", err)
	}

	return cat, nil
}

// GenerateFromDiscoveries parses a Gmail and a Calendar discovery doc from the
// provided readers and returns a Catalog with all Phase-3 operations:
//   - gmail.users.messages.list
//   - gmail.users.messages.get
//   - gmail.users.labels.list
//   - calendar.events.list
//   - calendar.calendarList.list
//
// All ops use auth_strategy = "byo_oauth", risk_class = "read",
// interface_kind = "discovery_rest", backend_kind = "typed_rest_sdk".
//
// Hard-fail rule: if any op would be emitted with auth_strategy = "gum_oauth",
// the function returns an error immediately (per bd memory gum-auth-strategy-v3).
func GenerateFromDiscoveries(gmailDisco, calendarDisco io.Reader) (*catalog.Catalog, error) {
	// Parse Gmail discovery doc.
	gmailData, err := io.ReadAll(gmailDisco)
	if err != nil {
		return nil, fmt.Errorf("gen-catalog: read gmail discovery doc: %w", err)
	}
	var gmailDoc discoveryDoc
	if err := json.Unmarshal(gmailData, &gmailDoc); err != nil {
		return nil, fmt.Errorf("gen-catalog: parse gmail discovery doc: %w", err)
	}

	// Parse Calendar discovery doc.
	calData, err := io.ReadAll(calendarDisco)
	if err != nil {
		return nil, fmt.Errorf("gen-catalog: read calendar discovery doc: %w", err)
	}
	var calDoc discoveryDoc
	if err := json.Unmarshal(calData, &calDoc); err != nil {
		return nil, fmt.Errorf("gen-catalog: parse calendar discovery doc: %w", err)
	}

	// Validate required Gmail resources exist.
	gmailUsers, ok := gmailDoc.Resources["users"]
	if !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users")
	}
	gmailMessages, ok := gmailUsers.Resources["messages"]
	if !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages")
	}
	if _, ok := gmailMessages.Methods["list"]; !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages.methods.list")
	}
	if _, ok := gmailMessages.Methods["get"]; !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages.methods.get")
	}
	// Phase 4: send and trash are required.
	if _, ok := gmailMessages.Methods["send"]; !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages.methods.send")
	}
	if _, ok := gmailMessages.Methods["trash"]; !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.messages.methods.trash")
	}
	gmailLabels, ok := gmailUsers.Resources["labels"]
	if !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.labels")
	}
	if _, ok := gmailLabels.Methods["list"]; !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.labels.methods.list")
	}
	// Tier A gmail.create_draft (gum-7tuq.1) requires gmail.users.drafts.create.
	gmailDrafts, ok := gmailUsers.Resources["drafts"]
	if !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.drafts")
	}
	if _, ok := gmailDrafts.Methods["create"]; !ok {
		return nil, fmt.Errorf("gen-catalog: gmail discovery doc missing resources.users.resources.drafts.methods.create")
	}

	// Validate required Calendar resources exist.
	calEvents, ok := calDoc.Resources["events"]
	if !ok {
		return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.events")
	}
	if _, ok := calEvents.Methods["list"]; !ok {
		return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.events.methods.list")
	}
	calCalendarList, ok := calDoc.Resources["calendarList"]
	if !ok {
		return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.calendarList")
	}
	if _, ok := calCalendarList.Methods["list"]; !ok {
		return nil, fmt.Errorf("gen-catalog: calendar discovery doc missing resources.calendarList.methods.list")
	}

	// Build the ops list.
	ops := []catalog.Op{
		makeOp(opSpec{
			opID:          "gmail.users.messages.list",
			title:         "List Gmail messages",
			summary:       "List message IDs in a Gmail mailbox.",
			service:       "gmail",
			serviceFamily: "workspace",
			variantID:     "gmail.v1.rest.users.messages.list",
			httpMethod:    "GET",
			httpPath:      "/gmail/v1/users/{userId}/messages",
			goPkg:         "google.golang.org/api/gmail/v1",
			goCall:        "Users.Messages.List",
		}),
		makeOp(opSpec{
			opID:          "gmail.users.messages.get",
			title:         "Get Gmail message",
			summary:       "Get a specific message from a Gmail mailbox.",
			service:       "gmail",
			serviceFamily: "workspace",
			variantID:     "gmail.v1.rest.users.messages.get",
			httpMethod:    "GET",
			httpPath:      "/gmail/v1/users/{userId}/messages/{id}",
			goPkg:         "google.golang.org/api/gmail/v1",
			goCall:        "Users.Messages.Get",
		}),
		makeOp(opSpec{
			opID:          "gmail.users.labels.list",
			title:         "List Gmail labels",
			summary:       "List labels in a Gmail mailbox.",
			service:       "gmail",
			serviceFamily: "workspace",
			variantID:     "gmail.v1.rest.users.labels.list",
			httpMethod:    "GET",
			httpPath:      "/gmail/v1/users/{userId}/labels",
			goPkg:         "google.golang.org/api/gmail/v1",
			goCall:        "Users.Labels.List",
		}),
		// Phase 4: write and destructive Gmail ops.
		makeWriteOp(opSpec{
			opID:          "gmail.users.messages.send",
			title:         "Send Gmail message",
			summary:       "Sends a Gmail message on behalf of the authenticated user.",
			service:       "gmail",
			serviceFamily: "workspace",
			variantID:     "gmail.v1.rest.users.messages.send",
			httpMethod:    "POST",
			httpPath:      "/gmail/v1/users/{userId}/messages/send",
			goPkg:         "google.golang.org/api/gmail/v1",
			goCall:        "Users.Messages.Send",
		}),
		makeDestructiveOp(opSpec{
			opID:          "gmail.users.messages.trash",
			title:         "Delete Gmail message (via Trash)",
			summary:       "Moves a Gmail message to the Trash. Permanently deletes after 30 days.",
			service:       "gmail",
			serviceFamily: "workspace",
			variantID:     "gmail.v1.rest.users.messages.trash",
			httpMethod:    "POST",
			httpPath:      "/gmail/v1/users/{userId}/messages/{id}/trash",
			goPkg:         "google.golang.org/api/gmail/v1",
			goCall:        "Users.Messages.Trash",
		}),
		// Tier A gmail.create_draft (gum-7tuq.1).
		makeWriteOp(opSpec{
			opID:          "gmail.users.drafts.create",
			title:         "Create Gmail draft",
			summary:       "Creates a new draft with the DRAFT label.",
			service:       "gmail",
			serviceFamily: "workspace",
			variantID:     "gmail.v1.rest.users.drafts.create",
			httpMethod:    "POST",
			httpPath:      "/gmail/v1/users/{userId}/drafts",
			goPkg:         "google.golang.org/api/gmail/v1",
			goCall:        "Users.Drafts.Create",
		}),
		makeOp(opSpec{
			opID:          "calendar.events.list",
			title:         "List Calendar events",
			summary:       "List events on the specified calendar.",
			service:       "calendar",
			serviceFamily: "workspace",
			variantID:     "calendar.v3.rest.events.list",
			httpMethod:    "GET",
			httpPath:      "/calendar/v3/calendars/{calendarId}/events",
			goPkg:         "google.golang.org/api/calendar/v3",
			goCall:        "Events.List",
		}),
		makeOp(opSpec{
			opID:          "calendar.calendarList.list",
			title:         "List Calendar list",
			summary:       "Returns the calendars on the user's calendar list.",
			service:       "calendar",
			serviceFamily: "workspace",
			variantID:     "calendar.v3.rest.calendarList.list",
			httpMethod:    "GET",
			httpPath:      "/calendar/v3/users/me/calendarList",
			goPkg:         "google.golang.org/api/calendar/v3",
			goCall:        "CalendarList.List",
		}),
	}

	// Hard-fail: no op may use gum_oauth (disabled in v0.1.0 per bd memory gum-auth-strategy-v3).
	for _, op := range ops {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				return nil, fmt.Errorf("gen-catalog: gum_oauth is disabled in v0.1.0; op %s variant %s uses gum_oauth", op.OpID, v.VariantID)
			}
		}
	}

	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "gum/cmd/gen-catalog@phase3",
		Ops:                  ops,
	}

	if err := cat.Validate(); err != nil {
		return nil, fmt.Errorf("gen-catalog: catalog validation failed: %w", err)
	}

	return cat, nil
}

// makeOp builds a catalog.Op from an opSpec using Phase-3 defaults:
// auth_strategy=byo_oauth, risk_class=read, interface_kind=discovery-rest, backend_kind=typed-rest-sdk.
// Version is hardcoded to "v1" (use makeCalendarOp for "v3" ops).
func makeOp(spec opSpec) catalog.Op {
	return catalog.Op{
		OpID:             spec.opID,
		OpSchemaVersion:  1,
		Title:            spec.title,
		Summary:          spec.summary,
		Service:          spec.service,
		ServiceFamily:    spec.serviceFamily,
		DefaultVariantID: spec.variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            spec.variantID,
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         spec.opID,
					HTTP: &catalog.HTTPBinding{
						Method: spec.httpMethod,
						Path:   spec.httpPath,
					},
					GoPkg:  spec.goPkg,
					GoCall: spec.goCall,
				},
			},
		},
	}
}

// makeWriteOp builds a catalog.Op with risk_class=write and auth_strategy=byo_oauth.
func makeWriteOp(spec opSpec) catalog.Op {
	op := makeOp(spec)
	op.Variants[0].RiskClass = catalog.RiskClassWrite
	return op
}

// makeDestructiveOp builds a catalog.Op with risk_class=destructive and auth_strategy=byo_oauth.
func makeDestructiveOp(spec opSpec) catalog.Op {
	op := makeOp(spec)
	op.Variants[0].RiskClass = catalog.RiskClassDestructive
	return op
}

// catalogEmbeddedProfiles returns any inlined expression-profile JSON documents
// found in the catalog. In v0.1.0, Variant.OutputProfile is a name-reference
// string only — no JSON is embedded — so this function always returns nil.
// When profiles are inlined in a future version, this function must be updated
// to extract and return their serialised JSON for the validation gate above.
func catalogEmbeddedProfiles(_ *catalog.Catalog) [][]byte {
	return nil
}

// makeCalendarOp builds a catalog.Op for Calendar v3 ops. Identical to makeOp
// but uses Version "v3".
func makeCalendarOp(spec opSpec) catalog.Op {
	return catalog.Op{
		OpID:             spec.opID,
		OpSchemaVersion:  1,
		Title:            spec.title,
		Summary:          spec.summary,
		Service:          spec.service,
		ServiceFamily:    spec.serviceFamily,
		DefaultVariantID: spec.variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            spec.variantID,
				VariantSchemaVersion: 1,
				Version:              "v3",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         spec.opID,
					HTTP: &catalog.HTTPBinding{
						Method: spec.httpMethod,
						Path:   spec.httpPath,
					},
					GoPkg:  spec.goPkg,
					GoCall: spec.goCall,
				},
			},
		},
	}
}
