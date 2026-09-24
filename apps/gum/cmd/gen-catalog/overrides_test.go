package main

// Spec §5.2 overrides manifest and its schema gate (docs/test-matrix.md,
// bead gum-lpra). The manifest must load, carry the moved
// discovery-URL table, and refuse every malformed shape with
// OVERRIDES_SCHEMA_INVALID before any generation runs.

import (
	"strings"
	"testing"
)

// requireInvalid asserts the manifest is rejected with the spec §5.2 code.
func requireInvalid(t *testing.T, manifest string) {
	t.Helper()

	_, err := validateOverrides([]byte(manifest), overridesSchemaJSON)
	if err == nil {
		t.Fatalf("validateOverrides accepted:\n%s", manifest)
	}
	if !strings.Contains(err.Error(), "OVERRIDES_SCHEMA_INVALID") {
		t.Fatalf("err = %v; every refusal carries OVERRIDES_SCHEMA_INVALID", err)
	}
}

func TestOverridesManifestSchema(t *testing.T) {
	t.Run("the shipped manifest is schema-valid", func(t *testing.T) {
		if _, err := validateOverrides(overridesTOML, overridesSchemaJSON); err != nil {
			t.Fatalf("validateOverrides on the embedded manifest: %v", err)
		}
	})

	t.Run("the manifest carries the moved discovery table", func(t *testing.T) {
		m, err := loadOverrides()
		if err != nil {
			t.Fatalf("loadOverrides: %v", err)
		}
		if len(m.APIs) != 22 {
			t.Errorf("len(apis) = %d; want the 22 services moved from enrich_discovery.go", len(m.APIs))
		}
		want := map[string]string{
			"gmail":          "https://gmail.googleapis.com/$discovery/rest?version=v1",
			"calendar":       "https://www.googleapis.com/discovery/v1/apis/calendar/v3/rest",
			"groupssettings": "https://www.googleapis.com/discovery/v1/apis/groupssettings/v1/rest",
		}
		for service, url := range want {
			if got := m.APIs[service].DiscoveryURL; got != url {
				t.Errorf("apis.%s.discovery_url = %q; want %q", service, got, url)
			}
		}
		for service, api := range m.APIs {
			if !strings.HasPrefix(api.DiscoveryURL, "https://") {
				t.Errorf("apis.%s.discovery_url = %q; want an https URL", service, api.DiscoveryURL)
			}
		}
	})

	t.Run("discoveryURLFor resolves through the manifest", func(t *testing.T) {
		url, ok := discoveryURLFor("gmail")
		if !ok || url != "https://gmail.googleapis.com/$discovery/rest?version=v1" {
			t.Errorf("discoveryURLFor(gmail) = %q, %v", url, ok)
		}
		// searchconsole is hand-authored and deliberately not in the manifest.
		if url, ok := discoveryURLFor("searchconsole"); ok {
			t.Errorf("discoveryURLFor(searchconsole) = %q; want absent", url)
		}
	})

	t.Run("a TOML syntax error is refused", func(t *testing.T) {
		requireInvalid(t, "[apis.gmail\ndiscovery_url = 3")
	})

	t.Run("an unknown top-level table is refused", func(t *testing.T) {
		requireInvalid(t, "[apis.gmail]\ndiscovery_url = \"https://x\"\n[plugins]\n")
	})

	t.Run("a service without discovery_url is refused", func(t *testing.T) {
		requireInvalid(t, "[apis.gmail]\n")
	})

	t.Run("a non-https discovery_url is refused", func(t *testing.T) {
		requireInvalid(t, "[apis.gmail]\ndiscovery_url = \"http://gmail.googleapis.com/$discovery/rest\"\n")
	})

	t.Run("an sdk_only entry missing adapter_key is refused", func(t *testing.T) {
		requireInvalid(t, `
[apis.gmail]
discovery_url = "https://gmail.googleapis.com/$discovery/rest?version=v1"
[sdk_only.bigtable]
go_pkg = "cloud.google.com/go/bigtable"
go_call = "Client.Open"
`)
	})

	t.Run("a risk override outside the enum is refused", func(t *testing.T) {
		requireInvalid(t, `
[apis.gmail]
discovery_url = "https://gmail.googleapis.com/$discovery/rest?version=v1"
[risk_overrides]
"gmail.users.messages.list" = "harmless"
`)
	})

	t.Run("a valid entry in every table is accepted", func(t *testing.T) {
		manifest := `
[apis.gmail]
discovery_url = "https://gmail.googleapis.com/$discovery/rest?version=v1"
[sdk_only.bigtable]
go_pkg = "cloud.google.com/go/bigtable"
go_call = "Client.Open"
adapter_key = "grpc-sdk"
[grpc_preferred."pubsub.v1.grpc.Publisher.Publish"]
go_pkg = "cloud.google.com/go/pubsub"
go_call = "Topic.Publish"
[op_id_overrides]
"admin/directory" = "admin.directory"
[risk_overrides]
"gmail.users.messages.send" = "write"
`
		if _, err := validateOverrides([]byte(manifest), overridesSchemaJSON); err != nil {
			t.Fatalf("validateOverrides: %v", err)
		}
	})
}
