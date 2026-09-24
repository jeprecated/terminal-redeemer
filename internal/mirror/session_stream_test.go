package mirror

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestAttachmentOutputDecodesFragmentedCurrentAttemptOnly(t *testing.T) {
	stale := AttachmentMarker(strings.Repeat("f", 32), "ready")
	ready := AttachmentMarker(testAttachmentAttempt, "ready")
	detached := AttachmentMarker(testAttachmentAttempt, "detached")
	payload := "before" + stale + ready + "shell" + ready + detached + "after"
	for split := 0; split <= len(payload); split++ {
		decoder := attachmentOutputDecoder{attempt: testAttachmentAttempt}
		out, events := decoder.feed([]byte(payload[:split]), false)
		tail, more := decoder.feed([]byte(payload[split:]), true)
		out = append(out, tail...)
		events = append(events, more...)
		if string(out) != "beforeshellafter" || !reflect.DeepEqual(events, []string{"ready", "detached"}) {
			t.Fatalf("split %d: output=%q events=%v", split, out, events)
		}
	}
}

func TestAttachmentOutputNeverGuessesReadinessOrDetach(t *testing.T) {
	for _, text := range []string{"SSH connected", AttachmentMarker(testAttachmentAttempt, "ready")[:40], AttachmentMarker(strings.Repeat("f", 32), "ready")} {
		decoder := attachmentOutputDecoder{attempt: testAttachmentAttempt}
		_, events := decoder.feed([]byte(text), true)
		if len(events) != 0 {
			t.Fatalf("unverified readiness: %q => %v", text, events)
		}
	}
	decoder := attachmentOutputDecoder{attempt: testAttachmentAttempt}
	text := AttachmentMarker(testAttachmentAttempt, "detached") + AttachmentMarker(testAttachmentAttempt, "ready")
	_, events := decoder.feed([]byte(text), true)
	if !reflect.DeepEqual(events, []string{"failed"}) {
		t.Fatalf("detach without readiness or late readiness accepted: %v", events)
	}
	decoder = attachmentOutputDecoder{attempt: testAttachmentAttempt}
	text = AttachmentMarker(testAttachmentAttempt, "ready") + AttachmentMarker(testAttachmentAttempt, "failed") + AttachmentMarker(testAttachmentAttempt, "detached")
	_, events = decoder.feed([]byte(text), true)
	if !reflect.DeepEqual(events, []string{"ready", "failed"}) {
		t.Fatalf("conflicting outcome accepted: %v", events)
	}
}

func TestAttachmentOutputDoesNotDelayOrdinaryPrompt(t *testing.T) {
	decoder := attachmentOutputDecoder{attempt: testAttachmentAttempt}
	out, events := decoder.feed([]byte("$ "), false)
	if string(out) != "$ " || len(events) != 0 {
		t.Fatalf("prompt held until another read: %q %v", out, events)
	}
}

func TestAttachmentOutputPreservesMalformedDataAndBoundsPendingBytes(t *testing.T) {
	decoder := attachmentOutputDecoder{attempt: testAttachmentAttempt}
	text := []byte("\x1b[2J\x00\x1eREDEEM_ATTACH_V1:" + strings.Repeat("x", 10000) + "\x1f\x1eREDEEM_ATT")
	var output []byte
	for _, b := range text {
		data, events := decoder.feed([]byte{b}, false)
		output = append(output, data...)
		if len(events) != 0 || len(decoder.pending) > 128 {
			t.Fatalf("events=%v pending=%d", events, len(decoder.pending))
		}
	}
	tail, events := decoder.feed(nil, true)
	output = append(output, tail...)
	if !bytes.Equal(output, text) || len(events) != 0 {
		t.Fatal("changed non-protocol terminal bytes")
	}
}
