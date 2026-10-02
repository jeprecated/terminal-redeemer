package mirror

import (
	"fmt"
	"net"
	"os"
)

func recoveryPeer(conn *net.UnixConn) (int, *os.File, error) {
	return 0, nil, fmt.Errorf("shared recovery IPC requires Linux")
}
func recoveryPeerAlive(peer *os.File) bool { return false }
