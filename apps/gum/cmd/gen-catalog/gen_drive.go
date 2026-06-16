package main

import "github.com/ehmo/gum/internal/catalog"

const (
	scopeDriveReadonly = "https://www.googleapis.com/auth/drive.readonly"
	scopeDrive         = "https://www.googleapis.com/auth/drive"
)

// BuildDriveOps returns the Tier A Drive v3 operation surface: full file
// lifecycle (list/get/create/update/copy/delete/export), the permission surface
// (list/get/create/update/delete), shared drives (list/get), and about.get.
// Uses the shared makeWorkspaceOp helper — Drive shares the workspace
// service-family and discovery-rest/typed-rest-sdk dispatch shape, so the
// existing per-family rate-limit partition (spec §6.2) covers Drive.
//
// drive.files.list / get and drive.permissions.create back the drive_find /
// drive_get_file / drive_share convenience tools (internal/mcp/tier_a_abi.go).
func BuildDriveOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "drive", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/drive/v3", goCall: goCall,
		})
	}
	ro := []string{scopeDriveReadonly}
	rw := []string{scopeDrive}
	const base = "https://www.googleapis.com/drive/v3"
	return []catalog.Op{
		// --- files ---
		op("drive.files.list", "drive.v3.rest.files.list", "List Drive Files",
			"Search and list files in Google Drive. Backs the drive_find convenience tool.",
			catalog.RiskClassRead, ro, "GET", base+"/files", "Files.List"),
		op("drive.files.get", "drive.v3.rest.files.get", "Get a Drive File",
			"Fetch a Drive file's metadata. Backs the drive_get_file convenience tool.",
			catalog.RiskClassRead, ro, "GET", base+"/files/{fileId}", "Files.Get"),
		op("drive.files.create", "drive.v3.rest.files.create", "Create a Drive File or Folder",
			"Create a file or folder in Drive (metadata create; set mimeType=application/vnd.google-apps.folder for a folder).",
			catalog.RiskClassWrite, rw, "POST", base+"/files", "Files.Create"),
		op("drive.files.update", "drive.v3.rest.files.update", "Update a Drive File",
			"Update a Drive file's metadata (rename, move via addParents/removeParents, star, trash flag).",
			catalog.RiskClassWrite, rw, "PATCH", base+"/files/{fileId}", "Files.Update"),
		op("drive.files.copy", "drive.v3.rest.files.copy", "Copy a Drive File",
			"Create a copy of a Drive file.",
			catalog.RiskClassWrite, rw, "POST", base+"/files/{fileId}/copy", "Files.Copy"),
		op("drive.files.delete", "drive.v3.rest.files.delete", "Permanently Delete a Drive File",
			"Permanently delete a Drive file (bypasses trash). Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/files/{fileId}", "Files.Delete"),
		op("drive.files.export", "drive.v3.rest.files.export", "Export a Google Doc",
			"Export a Google Workspace document (Doc/Sheet/Slides) to another MIME type (e.g. application/pdf).",
			catalog.RiskClassRead, ro, "GET", base+"/files/{fileId}/export", "Files.Export"),
		// --- permissions ---
		op("drive.permissions.list", "drive.v3.rest.permissions.list", "List File Permissions",
			"List the permissions (sharing) on a Drive file or folder.",
			catalog.RiskClassRead, ro, "GET", base+"/files/{fileId}/permissions", "Permissions.List"),
		op("drive.permissions.get", "drive.v3.rest.permissions.get", "Get a File Permission",
			"Fetch a single permission on a Drive file by permission id.",
			catalog.RiskClassRead, ro, "GET", base+"/files/{fileId}/permissions/{permissionId}", "Permissions.Get"),
		op("drive.permissions.create", "drive.v3.rest.permissions.create", "Share a Drive File",
			"Grant a permission on a Drive file or folder. Backs the drive_share convenience tool — requires confirmation per §6.1.",
			catalog.RiskClassWrite, rw, "POST", base+"/files/{fileId}/permissions", "Permissions.Create"),
		op("drive.permissions.update", "drive.v3.rest.permissions.update", "Update a File Permission",
			"Change a permission's role on a Drive file (e.g. reader→writer).",
			catalog.RiskClassWrite, rw, "PATCH", base+"/files/{fileId}/permissions/{permissionId}", "Permissions.Update"),
		op("drive.permissions.delete", "drive.v3.rest.permissions.delete", "Remove a File Permission",
			"Revoke a permission on a Drive file (unshare). Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/files/{fileId}/permissions/{permissionId}", "Permissions.Delete"),
		// --- shared drives + about ---
		op("drive.drives.list", "drive.v3.rest.drives.list", "List Shared Drives",
			"List the shared drives the user can access.",
			catalog.RiskClassRead, ro, "GET", base+"/drives", "Drives.List"),
		op("drive.drives.get", "drive.v3.rest.drives.get", "Get a Shared Drive",
			"Fetch a shared drive's metadata by id.",
			catalog.RiskClassRead, ro, "GET", base+"/drives/{driveId}", "Drives.Get"),
		// about.get is the one Drive method where Google REQUIRES the `fields`
		// query param (omitting it 400s). It takes no other params, so we bake
		// fields=* into the binding path — the op stays param-less for callers
		// and works bare.
		op("drive.about.get", "drive.v3.rest.about.get", "Get Drive About / Quota",
			"Fetch information about the user's Drive: storage quota, user, and import/export formats.",
			catalog.RiskClassRead, ro, "GET", base+"/about?fields=*", "About.Get"),
	}
}
