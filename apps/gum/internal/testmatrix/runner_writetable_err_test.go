package testmatrix

import (
	"errors"
	"testing"
	"time"
)

// failingWriter returns errFail on the Nth Write (1-indexed) and succeeds
// on all earlier ones. Used to drive each of WriteTable's `io.WriteString
// err → return err` arms in isolation.
type failingWriter struct {
	failOn int
	count  int
}

var errFail = errors.New("synthetic write failure")

func (f *failingWriter) Write(p []byte) (int, error) {
	f.count++
	if f.count == f.failOn {
		return 0, errFail
	}
	return len(p), nil
}

// TestWriteTableHeaderWriteErrorPropagates pins the
// `io.WriteString(header) err → return err` arm (runner.go:230-232).
// Reached when the writer fails on the very first write.
func TestWriteTableHeaderWriteErrorPropagates(t *testing.T) {
	w := &failingWriter{failOn: 1}
	s := Summarize([]Result{{Group: Group{Letter: "A"}, Status: StatusPassed}})
	if err := s.WriteTable(w); !errors.Is(err, errFail) {
		t.Errorf("WriteTable(failOn=1) err=%v; want errFail", err)
	}
}

// TestWriteTableSeparatorWriteErrorPropagates pins the
// `io.WriteString(separator) err → return err` arm (runner.go:233-235).
func TestWriteTableSeparatorWriteErrorPropagates(t *testing.T) {
	w := &failingWriter{failOn: 2}
	s := Summarize([]Result{{Group: Group{Letter: "A"}, Status: StatusPassed}})
	if err := s.WriteTable(w); !errors.Is(err, errFail) {
		t.Errorf("WriteTable(failOn=2) err=%v; want errFail", err)
	}
}

// TestWriteTableRowWriteErrorPropagates pins the
// `io.WriteString(row) err → return err` arm (runner.go:244-246).
// Reached when the writer fails on the per-result row (3rd write after
// header + separator).
func TestWriteTableRowWriteErrorPropagates(t *testing.T) {
	w := &failingWriter{failOn: 3}
	s := Summarize([]Result{{Group: Group{Letter: "A"}, Status: StatusPassed, Elapsed: time.Millisecond}})
	if err := s.WriteTable(w); !errors.Is(err, errFail) {
		t.Errorf("WriteTable(failOn=3) err=%v; want errFail", err)
	}
}

// TestWriteTableMissingTestsWriteErrorPropagates pins the
// `fmt.Fprintf(missing) err → return err` arm (runner.go:248-250).
// Reached when MissingTests is non-empty (so a 4th write is attempted)
// and that 4th write fails.
func TestWriteTableMissingTestsWriteErrorPropagates(t *testing.T) {
	w := &failingWriter{failOn: 4}
	s := Summarize([]Result{{
		Group:        Group{Letter: "A"},
		Status:       StatusFailed,
		MissingTests: []string{"TestX"},
	}})
	if err := s.WriteTable(w); !errors.Is(err, errFail) {
		t.Errorf("WriteTable(failOn=4) err=%v; want errFail", err)
	}
}

// TestWriteTableTotalElapsedWriteErrorPropagates pins the final
// `fmt.Fprintf("Total elapsed:") err → return err` arm
// (runner.go:253-255). Reached when the per-row writes all succeed but
// the closing footer write fails.
func TestWriteTableTotalElapsedWriteErrorPropagates(t *testing.T) {
	w := &failingWriter{failOn: 4}
	s := Summarize([]Result{{Group: Group{Letter: "A"}, Status: StatusPassed}})
	if err := s.WriteTable(w); !errors.Is(err, errFail) {
		t.Errorf("WriteTable(failOn=4, no missing tests) err=%v; want errFail", err)
	}
}
