package proxy

import "bytes"

// SSELineSplitter buffers incomplete lines without interpreting SSE events.
type SSELineSplitter struct {
	Max        int
	OnLine     func([]byte)
	OnOverflow func()
	Done       func() bool
	// PendingOnly preserves protocols that limit only an unfinished line.
	PendingOnly bool
	// Buffer belongs to the caller so its pending state remains observable.
	Buffer   *[]byte
	overflow bool
}

func (s *SSELineSplitter) stopped() bool {
	return s.overflow || (s.Done != nil && s.Done())
}

func (s *SSELineSplitter) Write(chunk []byte) {
	for len(chunk) > 0 && !s.stopped() {
		line, rest, found := bytes.Cut(chunk, []byte("\n"))
		if s.Max > 0 && len(*s.Buffer)+len(line) > s.Max && (!s.PendingOnly || !found) {
			*s.Buffer = nil
			s.overflow = true
			s.OnOverflow()
			return
		}
		*s.Buffer = append(*s.Buffer, line...)
		if !found {
			return
		}
		s.OnLine(*s.Buffer)
		*s.Buffer = nil
		chunk = rest
	}
}

func (s *SSELineSplitter) Flush() {
	if !s.stopped() {
		s.OnLine(*s.Buffer)
	}
	*s.Buffer = nil
}
