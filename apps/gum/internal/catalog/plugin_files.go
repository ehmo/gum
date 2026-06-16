// Package catalog — plugin file ABI loaders (spec.md §8.7, docs/catalog-abi.md).
//
// Implements ABI version gates for the three plugin-managed files:
//   - plugin-catalog.json  (PLUGIN_CATALOG_SCHEMA_UNSUPPORTED)
//   - plugins.lock         (PLUGIN_LOCK_SCHEMA_UNSUPPORTED)
//   - plugin-state.json    (PLUGIN_STATE_SCHEMA_UNSUPPORTED)
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Sentinel errors for plugin file schema version mismatches.
var (
	ErrUnsupportedPluginCatalogSchemaVersion = errors.New("catalog: unsupported plugin_catalog_schema_version")
	ErrUnsupportedPluginsLockSchemaVersion   = errors.New("catalog: unsupported plugins_lock_schema_version")
	ErrUnsupportedPluginStateSchemaVersion   = errors.New("catalog: unsupported plugin_state_schema_version")
)

// SupportedPluginCatalogSchemaVersions is the set of plugin_catalog_schema_version values accepted.
var SupportedPluginCatalogSchemaVersions = []int{1}

// SupportedPluginsLockSchemaVersions is the set of plugins_lock_schema_version values accepted.
var SupportedPluginsLockSchemaVersions = []int{1}

// SupportedPluginStateSchemaVersions is the set of plugin_state_schema_version values accepted.
var SupportedPluginStateSchemaVersions = []int{1}

// PluginCatalog is the top-level shape of plugin-catalog.json per spec.md §8.7.
type PluginCatalog struct {
	PluginCatalogSchemaVersion int    `json:"plugin_catalog_schema_version"`
	UpdatedAt                  string `json:"updated_at,omitempty"`
	Variants                   []any  `json:"variants,omitempty"`
}

// PluginsLock is the top-level shape of plugins.lock per spec.md §8.7.
type PluginsLock struct {
	PluginsLockSchemaVersion int    `json:"plugins_lock_schema_version"`
	InstallGeneration        int    `json:"install_generation,omitempty"`
	InstallTxID              string `json:"install_txid,omitempty"`
	Plugins                  []any  `json:"plugins,omitempty"`
}

// PluginState is the top-level shape of plugin-state.json per spec.md §8.7.
type PluginState struct {
	PluginStateSchemaVersion int    `json:"plugin_state_schema_version"`
	InstallGeneration        int    `json:"install_generation,omitempty"`
	InstallTxID              string `json:"install_txid,omitempty"`
	Plugins                  []any  `json:"plugins,omitempty"`
}

// LoadPluginCatalog parses data as plugin-catalog.json and rejects unsupported
// plugin_catalog_schema_version values with ErrUnsupportedPluginCatalogSchemaVersion.
func LoadPluginCatalog(data []byte) (*PluginCatalog, error) {
	var pc PluginCatalog
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("catalog: LoadPluginCatalog: %w", err)
	}
	if !slices.Contains(SupportedPluginCatalogSchemaVersions, pc.PluginCatalogSchemaVersion) {
		return nil, fmt.Errorf("plugin_catalog_schema_version %d: %w",
			pc.PluginCatalogSchemaVersion, ErrUnsupportedPluginCatalogSchemaVersion)
	}
	return &pc, nil
}

// LoadPluginsLock parses data as plugins.lock and rejects unsupported
// plugins_lock_schema_version values with ErrUnsupportedPluginsLockSchemaVersion.
func LoadPluginsLock(data []byte) (*PluginsLock, error) {
	var pl PluginsLock
	if err := json.Unmarshal(data, &pl); err != nil {
		return nil, fmt.Errorf("catalog: LoadPluginsLock: %w", err)
	}
	if !slices.Contains(SupportedPluginsLockSchemaVersions, pl.PluginsLockSchemaVersion) {
		return nil, fmt.Errorf("plugins_lock_schema_version %d: %w",
			pl.PluginsLockSchemaVersion, ErrUnsupportedPluginsLockSchemaVersion)
	}
	return &pl, nil
}

// LoadPluginState parses data as plugin-state.json and rejects unsupported
// plugin_state_schema_version values with ErrUnsupportedPluginStateSchemaVersion.
func LoadPluginState(data []byte) (*PluginState, error) {
	var ps PluginState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("catalog: LoadPluginState: %w", err)
	}
	if !slices.Contains(SupportedPluginStateSchemaVersions, ps.PluginStateSchemaVersion) {
		return nil, fmt.Errorf("plugin_state_schema_version %d: %w",
			ps.PluginStateSchemaVersion, ErrUnsupportedPluginStateSchemaVersion)
	}
	return &ps, nil
}
