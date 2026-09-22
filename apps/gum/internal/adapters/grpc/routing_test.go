package grpc

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestRoutingHeaderReachesTheWire is the runtime half of the catalog-abi.md
// `routing_headers` invariant (gum-dvn6). It runs a real gRPC round trip over
// bufconn and reads x-goog-request-params off the server's incoming metadata,
// so the assertion is about what the peer received, not about what the builder
// returned.
func TestRoutingHeaderReachesTheWire(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
		args    map[string]any
		want    []string
	}{
		{
			name:    "declaration order is preserved",
			headers: []string{"database", "session", "table"},
			args:    map[string]any{"database": "projects/p/databases/d", "session": "s1", "table": "Singers"},
			want:    []string{"database=projects%2Fp%2Fdatabases%2Fd&session=s1&table=Singers"},
		},
		{
			name:    "an absent field contributes nothing",
			headers: []string{"database", "session"},
			args:    map[string]any{"database": "d1"},
			want:    []string{"database=d1"},
		},
		{
			name:    "an empty field contributes nothing",
			headers: []string{"database", "session"},
			args:    map[string]any{"database": "d1", "session": ""},
			want:    []string{"database=d1"},
		},
		{
			name:    "a nested path resolves",
			headers: []string{"instance.config.name"},
			args:    map[string]any{"instance": map[string]any{"config": map[string]any{"name": "regional-eu"}}},
			want:    []string{"instance.config.name=regional-eu"},
		},
		{
			name:    "no declared header emits no metadata",
			headers: nil,
			args:    map[string]any{"database": "d1"},
			want:    nil,
		},
		{
			name:    "every declared field absent emits no metadata",
			headers: []string{"database"},
			args:    map[string]any{"session": "s1"},
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, seen := routingTestAdapter(t)
			rv := &dispatch.ResolvedVariant{
				Variant: &catalog.Variant{
					BackendKind: catalog.BackendKindGRPCSDK,
					Binding: &catalog.Binding{
						AdapterKey:     "grpc.health.check",
						RoutingHeaders: tc.headers,
					},
				},
				AdapterKey: "grpc.health.check",
			}
			inv := &dispatch.Invocation{OpID: "grpc.health.check", Args: tc.args}

			if _, err := adapter.Execute(context.Background(), inv, rv, &dispatch.Credentials{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			got := seen.get(routingParamsHeader)
			if len(got) != len(tc.want) {
				t.Fatalf("%s = %v; want %v", routingParamsHeader, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("%s[%d] = %q; want %q", routingParamsHeader, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRoutingHeaderValueRendersScalars covers the arg shapes the builder must
// render or refuse. A composite value has no canonical AIP-4222 rendering, so
// it contributes nothing rather than an arbitrary encoding.
func TestRoutingHeaderValueRendersScalars(t *testing.T) {
	binding := &catalog.Binding{RoutingHeaders: []string{"s", "b", "i", "i64", "f", "whole", "list", "obj", "nil"}}
	args := map[string]any{
		"s":     "text",
		"b":     true,
		"i":     7,
		"i64":   int64(9),
		"f":     1.5,
		"whole": float64(50),
		"list":  []any{"a"},
		"obj":   map[string]any{"k": "v"},
		"nil":   nil,
	}

	got := routingHeaderValue(binding, args)
	const want = "s=text&b=true&i=7&i64=9&f=1.5&whole=50"
	if got != want {
		t.Errorf("routingHeaderValue = %q; want %q", got, want)
	}
}

// TestRoutingHeaderValueHandlesMissingBinding pins the nil guards: a variant
// with no binding and one with no args emit nothing.
func TestRoutingHeaderValueHandlesMissingBinding(t *testing.T) {
	if got := routingHeaderValue(nil, map[string]any{"database": "d"}); got != "" {
		t.Errorf("nil binding produced %q; want empty", got)
	}
	binding := &catalog.Binding{RoutingHeaders: []string{"database"}}
	if got := routingHeaderValue(binding, nil); got != "" {
		t.Errorf("nil args produced %q; want empty", got)
	}
	// A path that walks through a scalar resolves nothing.
	nested := &catalog.Binding{RoutingHeaders: []string{"database.name"}}
	if got := routingHeaderValue(nested, map[string]any{"database": "d"}); got != "" {
		t.Errorf("path through a scalar produced %q; want empty", got)
	}
}

// incomingMD records the metadata the server saw. The interceptor writes it on
// a server goroutine and the test reads it after Execute returns, so the map
// needs its own lock.
type incomingMD struct {
	mu sync.Mutex
	md metadata.MD
}

func (i *incomingMD) record(md metadata.MD) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for k, v := range md {
		i.md[k] = v
	}
}

func (i *incomingMD) get(key string) []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.md.Get(key)
}

// routingTestAdapter stands up a bufconn health server behind a unary
// interceptor that records the incoming metadata of the last call, and returns
// an Adapter wired to it.
func routingTestAdapter(t *testing.T) (*Adapter, *incomingMD) {
	t.Helper()

	seen := &incomingMD{md: metadata.MD{}}
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer(grpc.UnaryInterceptor(
		func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			seen.record(md)
			return handler(ctx, req)
		}))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("gum.test", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, healthSrv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	adapter := NewAdapter()
	adapter.Dialer = func(context.Context, *dispatch.ResolvedVariant) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough://bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	adapter.Register("grpc.health.check", func(ctx context.Context, conn *grpc.ClientConn, args map[string]any) (any, error) {
		return healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: "gum.test"})
	})

	return adapter, seen
}
