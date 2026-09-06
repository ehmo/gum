package googleads

import (
	"net/http"
	"strings"
	"testing"
)

// The envelope below is the shape a v24 mutate rejection actually returns:
// the generic top-level message plus a GoogleAdsFailure detail carrying the
// operation index, the field path, and the reason.
const mutateFailureBody = `{
  "error": {
    "code": 400,
    "message": "Request contains an invalid argument.",
    "status": "INVALID_ARGUMENT",
    "details": [
      {
        "@type": "type.googleapis.com/google.ads.googleads.v24.errors.GoogleAdsFailure",
        "errors": [
          {
            "errorCode": {"stringLengthError": "TOO_LONG"},
            "message": "Too long.",
            "location": {
              "fieldPathElements": [
                {"fieldName": "mutate_operations", "index": 72},
                {"fieldName": "ad_group_ad_operation"},
                {"fieldName": "create"},
                {"fieldName": "ad"},
                {"fieldName": "responsive_search_ad"},
                {"fieldName": "headlines", "index": 0},
                {"fieldName": "text"}
              ]
            }
          }
        ],
        "requestId": "abc123"
      }
    ]
  }
}`

func TestUpstreamErrorReportsFailureDetails(t *testing.T) {
	e := newUpstreamError(http.StatusBadRequest, []byte(mutateFailureBody), http.Header{})

	got := e.Error()
	want := "googleads upstream error HTTP 400: Request contains an invalid argument. " +
		"(mutate_operations[72].ad_group_ad_operation.create.ad.responsive_search_ad.headlines[0].text: Too long.)"
	if got != want {
		t.Errorf("Error()\n got: %s\nwant: %s", got, want)
	}
}

func TestUpstreamErrorWithoutDetails(t *testing.T) {
	body := `{"error":{"code":403,"message":"The caller does not have permission."}}`
	e := newUpstreamError(http.StatusForbidden, []byte(body), http.Header{})

	want := "googleads upstream error HTTP 403: The caller does not have permission."
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestSummarizeFailuresCapsAndCounts(t *testing.T) {
	var fail googleAdsFail
	for i := 0; i < maxReportedFailures+3; i++ {
		fail.Errors = append(fail.Errors, googleAdsFailError{Message: "Too long."})
	}

	got := summarizeFailures([]googleAdsFail{fail})
	if len(got) != maxReportedFailures+1 {
		t.Fatalf("got %d lines, want %d", len(got), maxReportedFailures+1)
	}
	if last := got[len(got)-1]; last != "and 3 more" {
		t.Errorf("last line = %q, want %q", last, "and 3 more")
	}
}

func TestSummarizeFailuresSkipsEmptyErrors(t *testing.T) {
	fail := googleAdsFail{Errors: []googleAdsFailError{{}, {Message: "Too long."}}}

	got := summarizeFailures([]googleAdsFail{fail})
	if len(got) != 1 || got[0] != "Too long." {
		t.Errorf("got %v, want [Too long.]", got)
	}
}

func TestFieldPathSkipsUnnamedElements(t *testing.T) {
	idx := 4
	got := fieldPath([]fieldPathElement{
		{FieldName: "operations", Index: &idx},
		{FieldName: ""},
		{FieldName: "create"},
	})
	if want := "operations[4].create"; got != want {
		t.Errorf("fieldPath() = %q, want %q", got, want)
	}
}

func TestUpstreamErrorFallsBackToRawBody(t *testing.T) {
	e := newUpstreamError(http.StatusBadGateway, []byte("upstream is down"), http.Header{})

	if !strings.Contains(e.Error(), "upstream is down") {
		t.Errorf("Error() = %q, want the raw body", e.Error())
	}
}
