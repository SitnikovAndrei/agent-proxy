package proxy

import (
	"reflect"
	"testing"
)

func TestSSELineSplitter(t *testing.T) {
	var pending []byte
	var lines []string
	splitter := SSELineSplitter{Buffer: &pending, Max: 8, OnLine: func(line []byte) { lines = append(lines, string(line)) }, OnOverflow: func() { t.Fatal("unexpected overflow") }}
	for _, chunk := range []string{"da", "ta: x\n\nlast"} {
		splitter.Write([]byte(chunk))
	}
	splitter.Flush()
	if !reflect.DeepEqual(lines, []string{"data: x", "", "last"}) || pending != nil {
		t.Fatalf("lines=%v pending=%q", lines, pending)
	}
}

func TestSSEOverflowAndStop(t *testing.T) {
	for _, pendingOnly := range []bool{false, true} {
		var pending []byte
		lines, overflows := 0, 0
		done := false
		splitter := SSELineSplitter{Buffer: &pending, Max: 3, PendingOnly: pendingOnly, OnLine: func([]byte) { lines++; done = true }, Done: func() bool { return done }, OnOverflow: func() { overflows++ }}
		splitter.Write([]byte("long\nignored\n"))
		splitter.Flush()
		if pendingOnly {
			if lines != 1 || overflows != 0 {
				t.Fatalf("lines=%d overflows=%d", lines, overflows)
			}
		} else if lines != 0 || overflows != 1 {
			t.Fatalf("lines=%d overflows=%d", lines, overflows)
		}
		splitter.Write([]byte("more"))
		if pending != nil {
			t.Fatal("buffered after stop")
		}
	}
}

func TestSSEPendingLimit(t *testing.T) {
	var pending []byte
	overflows := 0
	splitter := SSELineSplitter{Buffer: &pending, Max: 3, OnLine: func([]byte) { t.Fatal("unexpected line") }, OnOverflow: func() { overflows++ }}
	splitter.Write([]byte("abc"))
	if string(pending) != "abc" {
		t.Fatal("exact limit rejected")
	}
	splitter.Write([]byte("d"))
	splitter.Write([]byte("again"))
	splitter.Flush()
	if overflows != 1 || pending != nil {
		t.Fatalf("overflow=%d pending=%q", overflows, pending)
	}
}
