package mirror

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionRecoveryControlDisconnectRetainsPendingCapacity(t *testing.T) {
	root := recoveryRuntimeFixture(t)
	if err := os.WriteFile(filepath.Join(root, "release-probe"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var clients []*SessionRecoveryClient
	for i := 0; i < 3; i++ {
		clients = append(clients, &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root, SelfCommand: filepath.Join(root, "self")})
	}
	t.Cleanup(func() {
		for _, c := range clients {
			c.Close()
		}
		awaitSession(t, func() bool { return recoveryFileLines(root, "daemon-exits") == 1 })
	})
	exchange := func(i int) SessionControlReply {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		reply, err := clients[i].Exchange(ctx, recoveryRequest(i))
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	for i := range clients {
		exchange(i)
	}
	awaitSession(t, func() bool { return exchange(0).Grant != nil && exchange(1).Grant != nil })
	clients[0].mu.Lock()
	clients[0].disconnect()
	clients[0].mu.Unlock() // IO loss, not orderly release
	time.Sleep(100 * time.Millisecond)
	if reply := exchange(2); reply.Grant != nil {
		t.Fatal("control EOF released a still-running pending attachment")
	}
	if reply := exchange(0); reply.Grant == nil || reply.Reset {
		t.Fatal("same live process could not resume its retained grant")
	}
	clients[0].Close() // caller attests its transport is now reaped
	awaitSession(t, func() bool { return exchange(2).Grant != nil })
}
