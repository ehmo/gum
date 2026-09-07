package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

type dataManagerTransport func(*http.Request) (*http.Response, error)

func (f dataManagerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDataManagerValidateOnlyConflict(t *testing.T) {
	for _, flat := range []string{"validateOnly=true", "--validate-only=true"} {
		t.Run(flat, func(t *testing.T) {
			var requests int
			var sent map[string]any
			adapter := adapters.NewTypedRestSDK()
			adapter.HTTPClient = &http.Client{Transport: dataManagerTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				defer func() { _ = r.Body.Close() }()
				_ = json.NewDecoder(r.Body).Decode(&sent)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"requestId":"accepted"}`)), Header: http.Header{}}, nil
			})}
			d := dispatch.NewDispatcherWithConfig(loadCatalog(), map[string]dispatch.Adapter{"rest.typed-rest-sdk": adapter}, dispatch.DispatcherConfig{Policy: dispatch.ProfilePolicy{AllowedScopes: []string{"https://www.googleapis.com/auth/datamanager"}}})
			old := newCallDispatcher
			defer func() { newCallDispatcher = old }()
			newCallDispatcher = func(string) dispatch.Dispatcher { return d }
			args := []string{"call", "datamanager.events.ingest", "--risk=write", `body:={"destinations":[{"operatingAccount":{"accountType":"GOOGLE_ADS","accountId":"1234567890"},"productDestinationId":"123"}],"events":[{"eventTimestamp":"2026-09-06T12:00:00Z","adIdentifiers":{"gclid":"test"}}],"validateOnly":false}`, flat}
			root := newRootCmd()
			registerDynamicCallFlags(root, args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetIn(strings.NewReader(""))
			root.SetArgs(args)
			err := root.ExecuteContext(context.Background())
			t.Logf("err=%v requests=%d wire.validateOnly=%#v", err, requests, sent["validateOnly"])
			if err == nil || requests != 0 {
				t.Errorf("conflict should fail before transport: err=%v requests=%d", err, requests)
			}
		})
	}
}
func TestDataManagerReadFailureRetry(t *testing.T) {
	var op *catalog.Op
	for i := range loadCatalog().Ops {
		if loadCatalog().Ops[i].OpID == "datamanager.events.ingest" {
			op = &loadCatalog().Ops[i]
			break
		}
	}
	if op == nil {
		t.Fatal("missing op")
	}
	requests := 0
	adapter := adapters.NewTypedRestSDK()
	adapter.HTTPClient = &http.Client{Transport: dataManagerTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if requests == 1 {
			return &http.Response{StatusCode: 200, Body: dataManagerReadFailure{}, Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"requestId":"second-acceptance"}`)), Header: http.Header{}}, nil
	})}
	rv := &dispatch.ResolvedVariant{Variant: &op.Variants[0]}
	res, err := adapter.Execute(context.Background(), &dispatch.Invocation{OpID: op.OpID, Args: map[string]any{"body": map[string]any{"events": []any{map[string]any{"eventTimestamp": "2026-09-06T12:00:00Z"}}}}}, rv, nil)
	if err == nil || res != nil {
		t.Errorf("want error without response, got %v, %v", res, err)
	}
	if requests != 1 {
		t.Error("accepted POST was repeated after response read failure")
	}
}

type dataManagerReadFailure struct{}

func (dataManagerReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (dataManagerReadFailure) Close() error             { return nil }

func TestDataManagerResponseShaping(t *testing.T) {
	for _, tc := range []struct{ op, body, needle string }{
		{"datamanager.events.ingest", `{"requestId":"accepted","fieldWarnings":[{"field":"events.events[0].cart_data.items[0].merchant_product_id","reason":"WARNING_REASON_CART_DATA_ITEM_MERCHANT_PRODUCT_ID_MISSING","description":"missing id"}]}`, "fieldWarnings"},
		{"datamanager.requestStatus.retrieve", `{"requestStatusPerDestination":[{"requestStatus":"PARTIAL_SUCCESS","errorInfo":{"errorCounts":[{"recordCount":"1","reason":"PROCESSING_ERROR_REASON_EVENT_TOO_OLD"}]},"warningInfo":{"warningCounts":[{"recordCount":"2","reason":"PROCESSING_WARNING_REASON_INTERNAL_ERROR"}]},"eventsIngestionStatus":{"recordCount":"3"}}]}`, "PROCESSING_ERROR_REASON_EVENT_TOO_OLD"},
	} {
		for _, format := range []string{"", "json", "toon", "raw"} {
			t.Run(tc.op+format, func(t *testing.T) {
				adapter := adapters.NewTypedRestSDK()
				adapter.HTTPClient = &http.Client{Transport: dataManagerTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
				})}
				d := dispatch.NewDispatcherWithConfig(loadCatalog(), map[string]dispatch.Adapter{"rest.typed-rest-sdk": adapter}, dispatch.DispatcherConfig{Policy: dispatch.ProfilePolicy{AllowedScopes: []string{"https://www.googleapis.com/auth/datamanager"}}})
				inv := &dispatch.Invocation{OpID: tc.op, Args: map[string]any{}, Format: format}
				if tc.op == "datamanager.events.ingest" {
					inv.AllowWrite = true
					inv.Args["body"] = map[string]any{"events": []any{}, "destinations": []any{}, "validateOnly": true}
				} else {
					inv.Args["requestId"] = "accepted"
				}
				res, err := d.Dispatch(context.Background(), inv)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(res.StructuredContent)
				t.Logf("format=%s dropped=%v body=%s", format, res.DroppedPaths, res.Body)
				if !strings.Contains(string(res.Body), tc.needle) || !strings.Contains(string(b), tc.needle) {
					t.Error("diagnostics lost")
				}
			})
		}
	}
}
func TestDataManagerValidationDetails(t *testing.T) {
	adapter := adapters.NewTypedRestSDK()
	adapter.HTTPClient = &http.Client{Transport: dataManagerTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"code":400,"message":"There was a problem with the request.","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.RequestInfo","requestId":"t-request"},{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"destinations[0].login_account.account_id","description":"String is not a valid number.","reason":"INVALID_NUMBER_FORMAT"}]}]}}`)), Header: http.Header{}}, nil
	})}
	d := dispatch.NewDispatcherWithConfig(loadCatalog(), map[string]dispatch.Adapter{"rest.typed-rest-sdk": adapter}, dispatch.DispatcherConfig{Policy: dispatch.ProfilePolicy{AllowedScopes: []string{"https://www.googleapis.com/auth/datamanager"}}})
	_, err := d.Dispatch(context.Background(), &dispatch.Invocation{OpID: "datamanager.events.ingest", AllowWrite: true, Args: map[string]any{"body": map[string]any{"validateOnly": true}}})
	b, _ := json.Marshal(err)
	t.Logf("error=%s", b)
	for _, needle := range []string{"INVALID_NUMBER_FORMAT", "destinations[0].login_account.account_id", "t-request"} {
		if !strings.Contains(string(b), needle) || !strings.Contains(err.Error(), needle) {
			t.Errorf("validation diagnostic %s was lost", needle)
		}
	}
}

func TestDataManagerWireAndScope(t *testing.T) {
	scopes, err := resolveLoginScopes(loadCatalog(), nil, []string{"datamanager"}, false)
	if err != nil || len(scopes) != 1 || scopes[0] != "https://www.googleapis.com/auth/datamanager" {
		t.Fatalf("wrong login scopes: %v, %v", scopes, err)
	}
	for _, op := range []string{"datamanager.events.ingest", "datamanager.requestStatus.retrieve"} {
		t.Run(op, func(t *testing.T) {
			calls := 0
			adapter := adapters.NewTypedRestSDK()
			adapter.HTTPClient = &http.Client{Transport: dataManagerTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "datamanager.googleapis.com" {
					t.Errorf("wrong host: %s", r.URL.Host)
				}
				if op == "datamanager.events.ingest" {
					if r.Method != "POST" || r.URL.Path != "/v1/events:ingest" || r.URL.RawQuery != "" {
						t.Errorf("wrong ingestion route: %s %s", r.Method, r.URL)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					_ = r.Body.Close()
					if body["validateOnly"] != true {
						t.Errorf("validation flag missing: %v", body)
					}
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON content type")
					}
				} else if r.Method != "GET" || r.URL.Path != "/v1/requestStatus:retrieve" || r.URL.Query().Get("requestId") != "request/test+id" {
					t.Errorf("wrong diagnostics route: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"requestId":"accepted"}`))}, nil
			})}
			d := dispatch.NewDispatcherWithConfig(loadCatalog(), map[string]dispatch.Adapter{"rest.typed-rest-sdk": adapter}, dispatch.DispatcherConfig{Policy: dispatch.ProfilePolicy{AllowedScopes: scopes}})
			inv := &dispatch.Invocation{OpID: op, Format: "json", Args: map[string]any{"requestId": "request/test+id"}}
			if op == "datamanager.events.ingest" {
				inv.Args = map[string]any{"body": map[string]any{"destinations": []any{}, "events": []any{}, "validateOnly": true}}
				if _, err := d.Dispatch(context.Background(), inv); err == nil || calls != 0 {
					t.Fatalf("write without consent reached HTTP: calls=%d error=%v", calls, err)
				}
				inv.AllowWrite = true
			}
			if _, err := d.Dispatch(context.Background(), inv); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("calls=%d want 1", calls)
			}
		})
	}
}
