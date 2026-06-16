package lro

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestOperationDocToResultShapes pins the four-field projection: a
// bare operation surfaces only {name,done}; the optional response /
// error / metadata raw-JSON fields decode into nested any values when
// present. This lets the LRO poller forward the full op envelope to
// the dispatcher without re-walking the raw bytes per field.
func TestOperationDocToResultShapes(t *testing.T) {
	t.Run("bare_op", func(t *testing.T) {
		o := &operationDoc{Name: "ops/123", Done: false}
		got := o.toResult()
		want := map[string]any{"name": "ops/123", "done": false}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got=%v; want %v", got, want)
		}
	})

	t.Run("with_response", func(t *testing.T) {
		o := &operationDoc{
			Name:     "ops/x",
			Done:     true,
			Response: json.RawMessage(`{"value":42}`),
		}
		got := o.toResult().(map[string]any)
		resp := got["response"].(map[string]any)
		if resp["value"].(float64) != 42 {
			t.Errorf("response.value=%v; want 42", resp["value"])
		}
	})

	t.Run("with_error", func(t *testing.T) {
		o := &operationDoc{
			Name:  "ops/x",
			Done:  true,
			Error: json.RawMessage(`{"code":7}`),
		}
		got := o.toResult().(map[string]any)
		errObj := got["error"].(map[string]any)
		if errObj["code"].(float64) != 7 {
			t.Errorf("error.code=%v; want 7", errObj["code"])
		}
	})

	t.Run("with_metadata", func(t *testing.T) {
		o := &operationDoc{
			Name:     "ops/x",
			Done:     false,
			Metadata: json.RawMessage(`{"phase":"running"}`),
		}
		got := o.toResult().(map[string]any)
		md := got["metadata"].(map[string]any)
		if md["phase"] != "running" {
			t.Errorf("metadata.phase=%v; want running", md["phase"])
		}
	})
}
