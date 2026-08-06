package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaCommandEmitsRootTree(t *testing.T) {
	withTempConfigRootCLI(t)

	out, err := runCLI(t, "schema", "--json")
	if err != nil {
		t.Fatalf("gum schema --json: %v\n%s", err, out)
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Command       struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			Subcommands []struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"subcommands"`
		} `json:"command"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("schema output is not JSON: %v\n%s", err, out)
	}
	if payload.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", payload.SchemaVersion)
	}
	if payload.Command.Name != "gum" || payload.Command.Path != "gum" {
		t.Fatalf("root command = %q/%q, want gum/gum", payload.Command.Name, payload.Command.Path)
	}
	if !schemaHasCommand(payload.Command.Subcommands, "search", "gum search") {
		t.Fatalf("schema missing gum search command: %s", out)
	}
	if !schemaHasCommand(payload.Command.Subcommands, "schema", "gum schema") {
		t.Fatalf("schema missing gum schema command: %s", out)
	}
}

func TestSchemaCommandIncludesInheritedFlags(t *testing.T) {
	withTempConfigRootCLI(t)

	out, err := runCLI(t, "schema")
	if err != nil {
		t.Fatalf("gum schema: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"name": "profile"`) {
		t.Fatalf("schema output missing inherited --profile flag:\n%s", out)
	}
}

func schemaHasCommand(commands []struct {
	Name string `json:"name"`
	Path string `json:"path"`
}, name, path string) bool {
	for _, command := range commands {
		if command.Name == name && command.Path == path {
			return true
		}
	}
	return false
}
