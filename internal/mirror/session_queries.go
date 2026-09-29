package mirror

import (
	"bytes"
	"strings"
)

// zellijClientStart is the first sequence a Zellij client writes on attach
// (enter the alternate screen), ahead of its own capability queries.
const zellijClientStart = "\x1b[?1049h"

// stripPreClientQueries removes queries from buffered pre-ready output that
// precede the Zellij client's own output: anything earlier came from the SSH
// session or the source helper, which never read the replies (the helper's
// stale OSC 11/DSR once made Kitty type ESC[1;1R into the focused pane).
// Zellij's own queries (pixel size, colours, DA, DECRQM 2026, kitty graphics)
// are kept: the client that asked reads those replies itself, and dropping
// them would disable synchronized output, image support and colour reporting
// to panes. Without a recognisable client start, every query is stripped.
func stripPreClientQueries(data []byte) []byte {
	start := bytes.Index(data, []byte(zellijClientStart))
	if start < 0 {
		return stripTerminalQueries(data)
	}
	head := stripTerminalQueries(data[:start])
	if len(head) == start {
		return data
	}
	return append(head, data[start:]...)
}

// stripTerminalQueries removes well-formed terminal *queries* from buffered
// pre-ready output. Anything produced before readiness was written while no
// attachment owned input, so the physical terminal's replies would arrive as
// keystrokes for whatever attachment is current when they land. Only exact,
// complete query forms are removed; every other byte is kept verbatim. A
// query truncated at the end of the buffer is left untouched: the rest of it
// arrives as live post-ready output, which is never filtered.
func stripTerminalQueries(data []byte) []byte {
	if bytes.IndexByte(data, 0x1b) < 0 {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if data[i] == 0x1b {
			if n := terminalQueryLength(data[i:]); n > 0 {
				i += n
				continue
			}
		}
		out = append(out, data[i])
		i++
	}
	return out
}

// terminalQueryLength returns the length of a complete query at the start of
// seq (which begins with ESC), or 0 if seq does not start with one.
func terminalQueryLength(seq []byte) int {
	if len(seq) < 2 {
		return 0
	}
	switch seq[1] {
	case '[':
		return csiQueryLength(seq)
	case ']':
		payload, n := stringSequence(seq, true)
		if n > 0 && oscQuery(payload) {
			return n
		}
	case 'P':
		// XTGETTCAP: DCS + q <hex names> ST
		payload, n := stringSequence(seq, false)
		if n > 0 && strings.HasPrefix(payload, "+q") {
			return n
		}
	case '_':
		// Kitty graphics protocol query: APC G <keys>[;payload] ST with a=q.
		payload, n := stringSequence(seq, false)
		if n > 0 && strings.HasPrefix(payload, "G") {
			keys, _, _ := strings.Cut(payload[1:], ";")
			for _, key := range strings.Split(keys, ",") {
				if key == "a=q" {
					return n
				}
			}
		}
	}
	return 0
}

// stringSequence parses ESC <x> payload terminator, where the terminator is
// ST (ESC \) or, for OSC, also BEL. It returns 0 if unterminated.
func stringSequence(seq []byte, bel bool) (string, int) {
	for i := 2; i < len(seq); i++ {
		switch {
		case bel && seq[i] == 0x07:
			return string(seq[2:i]), i + 1
		case seq[i] == 0x1b:
			if i+1 < len(seq) && seq[i+1] == '\\' {
				return string(seq[2:i]), i + 2
			}
			return "", 0 // ESC not starting ST: malformed, keep verbatim
		}
	}
	return "", 0
}

func oscQuery(payload string) bool {
	switch payload {
	case "10;?", "11;?", "12;?":
		return true
	}
	// OSC 4;<index>;? palette query.
	index, ok := strings.CutPrefix(payload, "4;")
	if !ok {
		return false
	}
	index, ok = strings.CutSuffix(index, ";?")
	return ok && index != "" && strings.Trim(index, "0123456789") == ""
}

var csiQueries = map[string]bool{
	"6n": true, "?6n": true, "5n": true, // DSR
	"c": true, "0c": true, ">c": true, ">0c": true, "=c": true, "=0c": true, // DA1/2/3
	">q": true, ">0q": true, // XTVERSION
	"?u":  true,                           // kitty keyboard flags
	"14t": true, "16t": true, "18t": true, // window/cell/text size reports
}

func csiQueryLength(seq []byte) int {
	i := 2
	for i < len(seq) && seq[i] >= 0x30 && seq[i] <= 0x3f { // parameter bytes
		i++
	}
	for i < len(seq) && seq[i] >= 0x20 && seq[i] <= 0x2f { // intermediate bytes
		i++
	}
	if i >= len(seq) || seq[i] < 0x40 || seq[i] > 0x7e {
		return 0
	}
	body := string(seq[2 : i+1])
	if csiQueries[body] {
		return i + 1
	}
	// DECRQM for private modes: CSI ? <mode> $ p
	if mode, ok := strings.CutPrefix(body, "?"); ok {
		if mode, ok = strings.CutSuffix(mode, "$p"); ok && mode != "" && strings.Trim(mode, "0123456789") == "" {
			return i + 1
		}
	}
	return 0
}
