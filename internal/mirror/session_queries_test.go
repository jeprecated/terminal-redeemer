package mirror

import (
	"bytes"
	"testing"
)

func TestStripTerminalQueriesKeepsFrameBytesExactly(t *testing.T) {
	frame := []string{
		"\x1b[0m\x1b[?25l", "\x1b[1;1H", "\x1b[38;2;163;174;210mhello", "\x1b[m\r\n",
		"\x1b]8;;\x1b\\", "\x1b[2;5H world", "\x1b]0;title\x07", "\x1b[?2004h", "\x1b[>1u",
		"\x1b[?1049h", "\x1bP26661nCgwK\x1b\\", "\x1b[?996n", "\x1b[3 q", "\x1b[?2026h", "tail",
	}
	queries := []string{
		"\x1b]11;?\x1b\\", "\x1b]10;?\x07", "\x1b]12;?\x1b\\", "\x1b]4;12;?\x07",
		"\x1b[6n", "\x1b[?6n", "\x1b[5n", "\x1b[c", "\x1b[0c", "\x1b[>c", "\x1b[=c",
		"\x1b[>q", "\x1b[?u", "\x1b[14t", "\x1b[16t", "\x1b[18t", "\x1b[?2026$p",
		"\x1bP+q544e\x1b\\", "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\",
	}
	var mixed, want bytes.Buffer
	for i, part := range frame {
		want.WriteString(part)
		mixed.WriteString(part)
		mixed.WriteString(queries[i%len(queries)])
	}
	for _, query := range queries[len(frame)%len(queries):] {
		mixed.WriteString(query)
	}
	for _, query := range queries[:len(frame)%len(queries)] {
		mixed.WriteString(query)
	}
	if got := stripTerminalQueries(mixed.Bytes()); !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("frame changed:\n got %q\nwant %q", got, want.Bytes())
	}
}

func TestStripTerminalQueriesLeavesLookalikesAndTruncation(t *testing.T) {
	for _, keep := range []string{
		"\x1b]11;rgb:0000/0000/0000\x1b\\", // a reply/set, not a query
		"\x1b]4;x;?\x07", "\x1b[12c", "\x1b[?1u", "\x1b[8;24;80t", "\x1b[?25$q",
		"\x1b_Ga=T,f=100;AAAA\x1b\\", "\x1bPq#0\x1b\\",
		"\x1b]11;?", "\x1b]11;?\x1b", "\x1b[6", "\x1b[?2026$", "\x1b_Ga=q", // truncated at buffer end
		"\x1b", "\x1b[", "plain",
	} {
		if got := stripTerminalQueries([]byte("x" + keep)); string(got) != "x"+keep {
			t.Fatalf("%q became %q", keep, got)
		}
	}
	// A truncated query only hides nothing before it.
	if got := stripTerminalQueries([]byte("\x1b[6nA\x1b]11;?")); string(got) != "A\x1b]11;?" {
		t.Fatalf("got %q", got)
	}
}

func TestStripPreClientQueriesKeepsZellijsOwnQueries(t *testing.T) {
	helper := "\x1b]11;?\x1b\\\x1b[6n"
	client := zellijClientStart + "\x1b[14t\x1b]11;?\x1b\\\x1b[?2026$p\x1b[cframe"
	if got := stripPreClientQueries([]byte("motd" + helper + client)); string(got) != "motd"+client {
		t.Fatalf("got %q", got)
	}
	if got := stripPreClientQueries([]byte("x" + helper + "y")); string(got) != "xy" {
		t.Fatalf("without a client start every query goes: %q", got)
	}
}
