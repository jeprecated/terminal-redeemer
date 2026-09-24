package mirror

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const pasteStart = "\x1b[200~"
const pasteEnd = "\x1b[201~"

type sessionInput struct {
	generation uint64
	data       []byte
	paste      bool
}

// The reader and readiness drain share one lock. Reads are nonblocking; neither
// queue delivery nor transport/control IO takes this lock. A paste retains its
// originating generation even if its closing delimiter arrives after recovery.
type sessionInputGate struct {
	mu               sync.Mutex
	generation, next uint64
	paste            bool
	pasteGeneration  uint64
	prefix           []byte
	prefixGeneration uint64
}

func (g *sessionInputGate) close()          { g.mu.Lock(); g.generation = 0; g.mu.Unlock() }
func (g *sessionInputGate) current() uint64 { g.mu.Lock(); defer g.mu.Unlock(); return g.generation }
func (g *sessionInputGate) open(fd int) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Parse and discard the kernel backlog as well: blind TCIFLUSH could erase
	// an unread paste-start delimiter, letting its later tail cross readiness.
	// The empty read is the boundary. Bytes arriving afterwards are new input.
	if err := unix.SetNonblock(fd, true); err != nil {
		return false, err
	}
	buf := make([]byte, 4096)
	empty := false
	for i := 0; i < 16; i++ {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			_ = g.decode(buf[:n])
		}
		if err == unix.EAGAIN {
			empty = true
			break
		}
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		if n == 0 {
			return false, errors.New("physical terminal closed")
		}
	}
	if !empty || g.paste || len(g.prefix) > 0 {
		return false, nil
	}
	g.next++
	if g.next == 0 {
		return false, errors.New("attachment generation exhausted")
	}
	g.generation = g.next
	return true, nil
}

func (g *sessionInputGate) decode(data []byte) []sessionInput {
	var result []sessionInput
	emit := func(data []byte, epoch uint64, paste bool) {
		if len(result) > 0 && result[len(result)-1].generation == epoch && result[len(result)-1].paste == paste {
			result[len(result)-1].data = append(result[len(result)-1].data, data...)
		} else {
			result = append(result, sessionInput{epoch, bytes.Clone(data), paste})
		}
	}
	for _, b := range data {
		epoch := g.generation
		if g.paste {
			epoch = g.pasteGeneration
		}
		if len(g.prefix) == 0 {
			g.prefixGeneration = epoch
		}
		g.prefix = append(g.prefix, b)
		for len(g.prefix) > 0 {
			target := pasteStart
			if g.paste {
				target = pasteEnd
			}
			if bytes.HasPrefix([]byte(target), g.prefix) {
				if len(g.prefix) == len(target) {
					origin := g.prefixGeneration
					emit(g.prefix, origin, true)
					if !g.paste {
						g.pasteGeneration = origin
					}
					g.paste = !g.paste
					g.prefix = nil
				}
				break
			}
			emit(g.prefix[:1], g.prefixGeneration, g.paste)
			g.prefix = g.prefix[1:]
		}
	}
	return result
}

func (g *sessionInputGate) read(ctx context.Context, fd int, input chan<- sessionInput) {
	defer close(input)
	buf := make([]byte, 4096)
	for ctx.Err() == nil {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 50)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n > 0 && poll[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return
		}
		if n == 0 {
			continue
		}
		g.mu.Lock()
		n, err = unix.Read(fd, buf)
		var items []sessionInput
		if n > 0 {
			items = g.decode(buf[:n])
		}
		g.mu.Unlock()
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return
		}
		for _, item := range items {
			select {
			case input <- item:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Fd/resize calls can reset Go's deadline integration. Use explicit nonblocking
// syscalls so a stalled PTY or terminal can never strand the event loop.
func writeSessionTerminal(ctx context.Context, fd int, data []byte) error {
	if err := unix.SetNonblock(fd, true); err != nil {
		return err
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := unix.Write(fd, data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			return err
		}
		if len(data) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("terminal write stalled")
		}
		_, err = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}, 10)
		if err != nil && err != unix.EINTR {
			return err
		}
	}
	return nil
}
