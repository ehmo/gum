package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestBackendKindGRPCSDK pins spec §5.7 + §14: the grpc-sdk backend kind
// is dispatchable via internal/adapters/grpc. We stand up a real
// in-process gRPC server (via google.golang.org/grpc/test/bufconn) hosting
// the standard `grpc.health.v1.Health` service, register an InvokerFunc
// that wraps Health.Check, and verify the executor returns a 200 JSON
// Response carrying the SERVING status.
//
// Using grpc.health is deliberate: it's a real proto service that ships
// with grpc-go, so the contract test verifies a true gRPC round-trip
// (codec, headers, status codes) without pulling in any heavy
// cloud.google.com/go SDK.
func TestBackendKindGRPCSDK(t *testing.T) {
	// 1. Stand up the in-process gRPC server.
	lis := bufconn.Listen(1024 * 1024)
	defer func() { _ = lis.Close() }()
	srv := grpc.NewServer()
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("gum.test", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, healthSrv)
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("grpc Serve: %v", err)
		}
	}()
	defer srv.Stop()

	// 2. Build the Adapter with a bufconn-backed Dialer and a single
	//    InvokerFunc that wraps Health.Check.
	adapter := NewAdapter()
	adapter.Dialer = func(ctx context.Context, _ *dispatch.ResolvedVariant) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough://bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	adapter.Register("grpc.health.check", func(ctx context.Context, conn *grpc.ClientConn, args map[string]any) (any, error) {
		client := healthpb.NewHealthClient(conn)
		svc, _ := args["service"].(string)
		resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: svc})
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": resp.GetStatus().String()}, nil
	})

	// 3. Dispatch.
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindGRPCSDK,
			Binding:     &catalog.Binding{AdapterKey: "grpc.health.check"},
		},
		AdapterKey: "grpc.health.check",
	}
	inv := &dispatch.Invocation{
		OpID: "grpc.health.check",
		Args: map[string]any{"service": "gum.test"},
	}
	resp, err := adapter.Execute(context.Background(), inv, rv, &dispatch.Credentials{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d; want 200", resp.StatusCode)
	}
	if resp.Format != "json" {
		t.Errorf("Format = %q; want json", resp.Format)
	}
	var got map[string]any
	if err := json.Unmarshal(resp.Body, &got); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, resp.Body)
	}
	if got["status"] != "SERVING" {
		t.Errorf("status = %v; want SERVING", got["status"])
	}
}

// TestGRPCAdapterUnknownAdapterKey verifies the closed-registry invariant
// — a binding pointing at an unregistered adapter_key fails fast with a
// typed error, so catalog drift can't dispatch to a nil InvokerFunc.
func TestGRPCAdapterUnknownAdapterKey(t *testing.T) {
	adapter := NewAdapter()
	adapter.Dialer = func(context.Context, *dispatch.ResolvedVariant) (*grpc.ClientConn, error) {
		t.Fatal("Dialer should not be called when no InvokerFunc is registered")
		return nil, nil
	}
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.spanner.read"}}},
		&dispatch.Credentials{},
	)
	if err == nil {
		t.Fatal("expected error for unregistered adapter_key, got nil")
	}
	if !strings.Contains(err.Error(), "no InvokerFunc registered") {
		t.Errorf("error = %q; want `no InvokerFunc registered` hint", err.Error())
	}
}

// TestGRPCAdapterNilDialer verifies the dispatcher-wiring precondition:
// a missing Dialer surfaces as a typed error rather than a nil pointer
// dereference at Execute time.
func TestGRPCAdapterNilDialer(t *testing.T) {
	adapter := NewAdapter()
	adapter.Register("grpc.x", func(context.Context, *grpc.ClientConn, map[string]any) (any, error) { return nil, nil })
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.x"}}},
		&dispatch.Credentials{},
	)
	if err == nil || !strings.Contains(err.Error(), "Dialer is nil") {
		t.Errorf("expected `Dialer is nil` error, got %v", err)
	}
}
