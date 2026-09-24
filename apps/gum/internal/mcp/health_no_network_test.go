package mcp

// docs/test-matrix.md, network half. TestStatusHealthSubsystemEnum
// pins the closed subsystem enum and the TTL; this pins the other normative
// clause on the same line, spec §13: "health probes are local-only
// and emit no network calls".
//
// The clause is checked two ways, because neither alone is sufficient. The
// runtime half catches a probe that dials through the shared http.Client,
// which is how every other outbound call in gum is made. The source half
// catches a probe that builds its own client and so never touches the
// default transport.

import (
	"context"
	"go/parser"
	"go/token"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// networkGuard fails any round-trip instead of performing it, and counts the
// attempts so the test can name what leaked.
type networkGuard struct {
	calls atomic.Int64
	urls  []string
}

func (g *networkGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	g.calls.Add(1)
	g.urls = append(g.urls, req.URL.String())
	return nil, errNetworkDuringHealthProbe
}

type healthProbeNetworkError struct{}

func (healthProbeNetworkError) Error() string {
	return "health probe attempted a network call; spec §13 forbids it"
}

var errNetworkDuringHealthProbe = healthProbeNetworkError{}

func TestStatusHealthNoNetwork(t *testing.T) {
	t.Run("a full health read makes no HTTP request", func(t *testing.T) {
		guard := &networkGuard{}
		origTransport := http.DefaultTransport
		origClient := http.DefaultClient.Transport
		http.DefaultTransport = guard
		http.DefaultClient.Transport = guard
		t.Cleanup(func() {
			http.DefaultTransport = origTransport
			http.DefaultClient.Transport = origClient
		})

		// A real profile dir, so every probe takes its "resolvable" arm and
		// does the most work it can: an unresolvable dir would short-circuit
		// probeAuditLog and probeTeeFilesystem before they touch anything.
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		s := NewServer(noopDispatcher{})
		if err := s.SetProfile("default"); err != nil {
			t.Fatalf("SetProfile: %v", err)
		}

		res, err := s.handleStatusHealthRead(context.Background(), &sdkmcp.ReadResourceRequest{
			Params: &sdkmcp.ReadResourceParams{URI: "gum://status/health"},
		})
		if err != nil {
			t.Fatalf("handleStatusHealthRead: %v", err)
		}
		if n := guard.calls.Load(); n != 0 {
			t.Errorf("health read made %d HTTP request(s): %v", n, guard.urls)
		}

		// Guard against the assertion passing because the read produced
		// nothing: all six subsystems must be on the row.
		body := res.Contents[0].Text
		if want := "count: " + strconv.Itoa(len(staticHealthSubsystems)); !strings.Contains(body, want) {
			t.Errorf("health body missing %q:\n%s", want, body)
		}
		for _, name := range staticHealthSubsystems {
			if !strings.Contains(body, name) {
				t.Errorf("health body omits subsystem %q:\n%s", name, body)
			}
		}
	})

	t.Run("the probe source imports no network package", func(t *testing.T) {
		const src = "health_probes.go"
		file, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s: %v", imp.Path.Value, err)
			}
			if path == "net" || strings.HasPrefix(path, "net/") || strings.Contains(path, "golang.org/x/net") {
				t.Errorf("%s imports %q; probes must stay local-only", src, path)
			}
		}
	})

	t.Run("every probe returns without network access", func(t *testing.T) {
		guard := &networkGuard{}
		origTransport := http.DefaultTransport
		http.DefaultTransport = guard
		t.Cleanup(func() { http.DefaultTransport = origTransport })

		dir := t.TempDir()
		now := time.Now().UTC()
		for name, probe := range healthProbes {
			row := probe(now, dir)
			if row.Subsystem != name {
				t.Errorf("probe %q returned subsystem %q", name, row.Subsystem)
			}
			if row.Status != "healthy" && row.Status != "degraded" && row.Status != "unavailable" {
				t.Errorf("probe %q status %q outside the closed enum", name, row.Status)
			}
		}
		if n := guard.calls.Load(); n != 0 {
			t.Errorf("probes made %d HTTP request(s): %v", n, guard.urls)
		}
	})
}
