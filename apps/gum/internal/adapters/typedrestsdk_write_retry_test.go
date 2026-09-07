package adapters_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

type failedResponseBody struct{ closed *bool }

func (b failedResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b failedResponseBody) Close() error             { *b.closed = true; return nil }

func TestTypedRestUncertainWriteIsNotReplayed(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "GET"} {
		for _, failure := range []string{"transport", "body", "cap"} {
			for _, rateLimited := range []bool{false, true} {
				name := method + "/" + failure
				if rateLimited {
					name += "/after429"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					calls, failedAt := 0, 1
					if rateLimited {
						failedAt++
					}
					closed := false
					ex := adapters.NewTypedRestSDK()
					ex.MaxResponseBytes = 16
					ex.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.Body != nil {
							_, _ = io.Copy(io.Discard, r.Body)
							_ = r.Body.Close()
						}
						response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}
						if rateLimited && calls == 1 {
							response.StatusCode = 429
							return response, nil
						}
						if calls == failedAt {
							switch failure {
							case "transport":
								return nil, io.ErrUnexpectedEOF
							case "body":
								response.Body = failedResponseBody{&closed}
							case "cap":
								response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 17)))
							}
						}
						return response, nil
					})}
					inv, rv := makeTestInvAndVariant("https://datamanager.googleapis.com")
					rv.Variant.Binding.HTTP.Method = method
					inv.Args["body"] = map[string]any{"validateOnly": true}
					response, err := ex.Execute(context.Background(), inv, rv, nil)
					retry := method == "GET" && failure != "cap"
					expected := failedAt
					if retry {
						expected++
					}
					if calls != expected {
						t.Errorf("calls=%d want %d", calls, expected)
					}
					if retry {
						if err != nil || response == nil {
							t.Fatalf("read retry failed: %v", err)
						}
					} else if err == nil || response != nil {
						t.Fatalf("want failure, got response=%v error=%v", response, err)
					}
					if !retry && failure != "cap" && !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Errorf("original failure lost: %v", err)
					}
					if failure == "body" && !closed {
						t.Error("response body not closed")
					}
					if failure == "cap" {
						var se *dispatch.StructuredError
						if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeResponseTooLarge {
							t.Errorf("missing cap error: %v", err)
						}
					}
				})
			}
		}
	}
}
