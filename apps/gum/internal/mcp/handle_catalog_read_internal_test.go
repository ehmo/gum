package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestHandleCatalogReadEmptySnapshotReturnsNotFound pins the negative
// branch of handleCatalogRead: when embedded.CatalogJSON is empty (an
// "unofficial" build configuration), the handler MUST surface a
// RESOURCE_NOT_FOUND envelope instead of returning a 200 with an empty
// payload. We swap CatalogJSON in place with a deferred restore so the
// test is hermetic.
func TestHandleCatalogReadEmptySnapshotReturnsNotFound(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	s := &Server{profile: "default"}
	res, err := s.handleCatalogRead(context.Background(), newReadReq("gum://catalog"))
	if err == nil {
		t.Fatal("want RESOURCE_NOT_FOUND for empty catalog; got nil")
	}
	if res != nil {
		t.Errorf("res=%+v; want nil on error path", res)
	}
	// The envelope itself lives in err.(*jsonrpc.Error).Data; surface the
	// RESOURCE_NOT_FOUND code so a regression that returns a different
	// envelope (e.g. INTERNAL) is caught.
	if !strings.Contains(err.Error(), "Resource not found") {
		t.Errorf("err=%v; want 'Resource not found' message", err)
	}
}
