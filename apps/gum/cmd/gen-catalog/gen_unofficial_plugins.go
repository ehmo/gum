package main

import "github.com/ehmo/gum/internal/catalog"

// BuildUnofficialPluginOps returns catalog entries for the four "defensible-
// surface unofficial API" plugins enumerated in spec §1.1: Scholar, Patents,
// YouTube Transcripts, and Trends. Each op points to an mcp-plugin variant the
// host resolves through the bundled-plugin manifests under apps/gum/plugins/.
//
// These ops surface in gum.search_apis (BM25 over title+summary+op_id), so
// callers can discover them even before the corresponding plugin subprocess
// is installed. Dispatch against an uninstalled plugin surfaces SERVICE_DOWN
// with the adapter_key in the error envelope — the documented "plugin not
// installed" failure shape (spec §8 line 1631; same path as flights.search
// when google-flights is missing).
//
// All four variants use auth_strategy=plugin_managed: each plugin owns its
// own credential descriptors and rate-limit budget; the gum host does not
// participate in their authentication. Risk class is read across the board —
// Scholar/Patents/Trends/Transcripts are query-only endpoints.
func BuildUnofficialPluginOps() []catalog.Op {
	return []catalog.Op{
		buildScholarSearchOp(),
		buildPatentsSearchOp(),
		buildYouTubeTranscriptsOp(),
		buildTrendsDailyOp(),
	}
}

// buildScholarSearchOp returns the scholar.search op (Google Scholar passive
// query). Plugin name matches the apps/gum/plugins/google-scholar/manifest.json
// plugin_id; tool_name matches the manifest's advertised tool.
func buildScholarSearchOp() catalog.Op {
	return catalog.Op{
		OpID:             "scholar.search",
		OpSchemaVersion:  1,
		Title:            "Search Google Scholar",
		Summary:          "Search Google Scholar for academic papers, citations, and author profiles. Backed by the bundled google-scholar Shape 1 plugin.",
		Service:          "scholar",
		ServiceFamily:    "plugin",
		DefaultVariantID: "scholar.v1.plugin.search",
		Variants: []catalog.Variant{
			{
				VariantID:            "scholar.v1.plugin.search",
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityAlpha,
				InterfaceKind:        catalog.InterfaceKindPluginMCP,
				BackendKind:          catalog.BackendKindMCPPlugin,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyPluginManaged,
				Scopes:               []string{},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "plugin.mcp",
					OperationKey:         "scholar.search",
					PluginName:           "google-scholar",
					ToolName:             "scholar_search",
				},
			},
		},
	}
}

// buildPatentsSearchOp returns the patents.search op (Google Patents passive
// query). Plugin name matches apps/gum/plugins/google-patents/manifest.json.
func buildPatentsSearchOp() catalog.Op {
	return catalog.Op{
		OpID:             "patents.search",
		OpSchemaVersion:  1,
		Title:            "Search Google Patents",
		Summary:          "Search Google Patents for patent filings, prior art, and assignee profiles. Backed by the bundled google-patents Shape 1 plugin.",
		Service:          "patents",
		ServiceFamily:    "plugin",
		DefaultVariantID: "patents.v1.plugin.search",
		Variants: []catalog.Variant{
			{
				VariantID:            "patents.v1.plugin.search",
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityAlpha,
				InterfaceKind:        catalog.InterfaceKindPluginMCP,
				BackendKind:          catalog.BackendKindMCPPlugin,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyPluginManaged,
				Scopes:               []string{},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "plugin.mcp",
					OperationKey:         "patents.search",
					PluginName:           "google-patents",
					ToolName:             "patents_search",
				},
			},
		},
	}
}

// buildYouTubeTranscriptsOp returns the youtube.transcripts.get op. Backed by
// the youtube-transcripts plugin which proxies the public timedtext endpoint
// that the YouTube player consumes (not the YouTube Data API).
func buildYouTubeTranscriptsOp() catalog.Op {
	return catalog.Op{
		OpID:             "youtube.transcripts.get",
		OpSchemaVersion:  1,
		Title:            "Get YouTube transcript",
		Summary:          "Fetch the auto-generated or human-authored transcript for a YouTube video by video_id. Backed by the bundled youtube-transcripts Shape 1 plugin.",
		Service:          "youtube",
		ServiceFamily:    "plugin",
		DefaultVariantID: "youtube.transcripts.v1.plugin.get",
		Variants: []catalog.Variant{
			{
				VariantID:            "youtube.transcripts.v1.plugin.get",
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityAlpha,
				InterfaceKind:        catalog.InterfaceKindPluginMCP,
				BackendKind:          catalog.BackendKindMCPPlugin,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyPluginManaged,
				Scopes:               []string{},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "plugin.mcp",
					OperationKey:         "youtube.transcripts.get",
					PluginName:           "youtube-transcripts",
					ToolName:             "youtube_transcripts_get",
				},
			},
		},
	}
}

// buildTrendsDailyOp returns the trends.daily op. Backed by the google-trends
// plugin which proxies trends.google.com daily and realtime trending JSON
// endpoints (the same data the public Trends dashboard renders).
func buildTrendsDailyOp() catalog.Op {
	return catalog.Op{
		OpID:             "trends.daily",
		OpSchemaVersion:  1,
		Title:            "Google Trends daily report",
		Summary:          "Fetch Google Trends daily and realtime trending searches for a region. Backed by the bundled google-trends Shape 1 plugin.",
		Service:          "trends",
		ServiceFamily:    "plugin",
		DefaultVariantID: "trends.v1.plugin.daily",
		Variants: []catalog.Variant{
			{
				VariantID:            "trends.v1.plugin.daily",
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityAlpha,
				InterfaceKind:        catalog.InterfaceKindPluginMCP,
				BackendKind:          catalog.BackendKindMCPPlugin,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyPluginManaged,
				Scopes:               []string{},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "plugin.mcp",
					OperationKey:         "trends.daily",
					PluginName:           "google-trends",
					ToolName:             "trends_daily",
				},
			},
		},
	}
}
