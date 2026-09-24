package mirror

import (
	"bytes"
	"strings"
)

// A decoder belongs to exactly one disposable transport. It consumes lifecycle
// markers throughout the stream, not just the first readiness marker. Only the
// current attempt may change state; SSH exit alone never proves detach.
type attachmentOutputDecoder struct {
	attempt         string
	pending         []byte
	ready, finished bool
}

func (d *attachmentOutputDecoder) feed(data []byte, eof bool) (output []byte, events []string) {
	d.pending = append(d.pending, data...)
	prefix := []byte(attachmentMarkerPrefix)
	for len(d.pending) > 0 {
		start := bytes.Index(d.pending, prefix)
		if start < 0 {
			keep := 0
			if !eof {
				for n := 1; n < len(prefix) && n <= len(d.pending); n++ {
					if bytes.HasSuffix(d.pending, prefix[:n]) {
						keep = n
					}
				}
			}
			flush := max(0, len(d.pending)-keep)
			output = append(output, d.pending[:flush]...)
			d.pending = d.pending[flush:]
			break
		}
		output = append(output, d.pending[:start]...)
		d.pending = d.pending[start:]
		end := bytes.IndexByte(d.pending, '\x1f')
		if end < 0 && len(d.pending) < 128 && !eof {
			break
		}
		if end < 0 || end >= 128 {
			// An overlong/incomplete non-marker stays ordinary terminal data. Never
			// accumulate an unbounded string while waiting for a delimiter.
			output = append(output, d.pending[0])
			d.pending = d.pending[1:]
			continue
		}
		marker := string(d.pending[:end+1])
		token, event, ok := strings.Cut(string(d.pending[len(prefix):end]), ":")
		valid := ok && AttachmentMarker(token, event) == marker
		if !valid {
			output = append(output, d.pending[:end+1]...)
		}
		d.pending = d.pending[end+1:]
		if !valid || token != d.attempt || d.finished {
			continue
		}
		if event == "ready" {
			if !d.ready {
				d.ready = true
				events = append(events, event)
			}
			continue
		}
		d.finished = true
		if event == "detached" && !d.ready {
			event = "failed"
		}
		events = append(events, event)
	}
	// Retain only the small suffix, not a reference to a large output chunk.
	d.pending = bytes.Clone(d.pending)
	return
}
