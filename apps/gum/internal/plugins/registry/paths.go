// Package registry implements the spec §8.7 three-file plugin install
// transaction protocol (plugin-catalog.json, plugins.lock, plugin-state.json)
// guarded by the plugins.install.lock advisory file lock.
//
// All writes go through WriteTransaction, which acquires the lock, runs a
// caller-supplied mutate callback, and publishes the three files atomically
// under one shared install_generation + install_txid. On startup, callers use
// SelectGeneration to pick the last complete shared generation and refuse to
// dispatch from an incomplete one (spec §8.7 step 5).
package registry

import "path/filepath"

// File names under the profile directory. Spec §8.7 lines 1700-1702.
const (
	CatalogFilename     = "plugin-catalog.json"
	LockFilename        = "plugins.lock"
	StateFilename       = "plugin-state.json"
	InstallLockFilename = "plugins.install.lock"
)

// CatalogPath returns the absolute path of plugin-catalog.json under profileDir.
func CatalogPath(profileDir string) string {
	return filepath.Join(profileDir, CatalogFilename)
}

// LockPath returns the absolute path of plugins.lock under profileDir.
func LockPath(profileDir string) string {
	return filepath.Join(profileDir, LockFilename)
}

// StatePath returns the absolute path of plugin-state.json under profileDir.
func StatePath(profileDir string) string {
	return filepath.Join(profileDir, StateFilename)
}

// InstallLockPath returns the absolute path of the advisory lock file.
func InstallLockPath(profileDir string) string {
	return filepath.Join(profileDir, InstallLockFilename)
}

// tempPath returns the staging path used during a transaction (step 3).
func tempPath(profileDir, finalName, txid string) string {
	return filepath.Join(profileDir, finalName+".tmp."+txid)
}
