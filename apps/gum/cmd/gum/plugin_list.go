package main

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ehmo/gum/internal/plugins"
)

// Plugin lifecycle statuses reported by `gum plugin list`. They name what the
// operator has to do next, which is why quarantine outranks everything else: a
// quarantined plugin refuses every call until it is reloaded or unquarantined.
const (
	pluginStatusQuarantinedPermanent = "quarantined-permanent"
	pluginStatusQuarantined          = "quarantined"
	pluginStatusPendingRestart       = "pending-restart"
	// pluginStatusUnloadable marks a state row with no loadable manifest and no
	// quarantine: the install dir is gone or the manifest is invalid, and
	// nothing has tried to spawn it yet.
	pluginStatusUnloadable = "unloadable"
	pluginStatusActive     = "active"
)

// pluginListEntry is the merged view one row of `gum plugin list` renders:
// manifest facts joined with the plugin-state.json row. Either side can be
// absent — a plugin whose manifest fails to load has state but no manifest,
// and that is precisely the case the operator needs to see (gum-mmzr).
type pluginListEntry struct {
	id          string
	version     string
	name        string
	hasManifest bool
	state       *plugins.InventoryRow
}

// formatPluginList renders the merged listing. Columns are tab-aligned so the
// output stays readable at a terminal and still splits on whitespace in a
// script. An empty listing returns "" so the caller can keep stdout clean for
// pipes and tell a human on stderr.
func formatPluginList(manifests []*plugins.Manifest, states []plugins.InventoryRow) string {
	entries := mergePluginRows(manifests, states)
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tVERSION\tSTATUS\tRETRIES\tNEXT-RETRY\tLAST-ERROR\tNAME")
	quarantined := 0
	for _, e := range entries {
		status := pluginEntryStatus(e)
		if status == pluginStatusQuarantined || status == pluginStatusQuarantinedPermanent {
			quarantined++
		}
		retries, nextRetry, lastErr := "-", "-", "-"
		if e.state != nil {
			if e.state.State.RetryCount > 0 {
				retries = fmt.Sprint(e.state.State.RetryCount)
			}
			if !e.state.State.NextRetryAt.IsZero() {
				nextRetry = e.state.State.NextRetryAt.UTC().Format(time.RFC3339)
			}
			if e.state.State.LastErrorCode != "" {
				lastErr = e.state.State.LastErrorCode
			}
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.id, dashIfEmpty(e.version), status, retries, nextRetry, lastErr, dashIfEmpty(e.name))
	}
	_ = w.Flush()

	// The whole point of the listing is that the operator can act on it. A
	// quarantine is cleared by exactly two commands; name them.
	if quarantined > 0 {
		noun := "plugins are"
		if quarantined == 1 {
			noun = "plugin is"
		}
		fmt.Fprintf(&sb, "\n%d %s quarantined and will refuse every call. "+
			"Run `gum plugin reload <id>` to retry the spawn, or `gum plugin unquarantine <id>` "+
			"to clear the backoff without restarting.\n", quarantined, noun)
	}
	return sb.String()
}

// mergePluginRows joins the installed manifests with the state rows by plugin
// ID, sorted by ID. A plugin present on only one side still gets a row.
func mergePluginRows(manifests []*plugins.Manifest, states []plugins.InventoryRow) []pluginListEntry {
	byID := map[string]*pluginListEntry{}
	for _, m := range manifests {
		if m == nil {
			continue
		}
		byID[m.PluginID] = &pluginListEntry{
			id:          m.PluginID,
			version:     m.Version,
			name:        m.Name,
			hasManifest: true,
		}
	}
	for i := range states {
		s := states[i]
		e, ok := byID[s.Name]
		if !ok {
			e = &pluginListEntry{id: s.Name}
			byID[s.Name] = e
		}
		e.state = &s
	}

	out := make([]pluginListEntry, 0, len(byID))
	for _, e := range byID {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// pluginEntryStatus reduces the merged row to the one word that tells the
// operator what to do next.
func pluginEntryStatus(e pluginListEntry) string {
	if e.state != nil {
		switch {
		case e.state.State.Permanent:
			return pluginStatusQuarantinedPermanent
		case e.state.State.Quarantined:
			return pluginStatusQuarantined
		case e.state.Status == plugins.StatusInstalledPendingRestart:
			return pluginStatusPendingRestart
		}
	}
	if !e.hasManifest {
		return pluginStatusUnloadable
	}
	return pluginStatusActive
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
