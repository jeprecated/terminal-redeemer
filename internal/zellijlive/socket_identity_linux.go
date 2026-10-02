package zellijlive

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ExactSocketIDAt is the reconnect identity, distinct from the historical
// checkpoint ID. Inodes can be reused immediately after a session ends. Birth
// time distinguishes that reuse and, unlike ctime, is stable across hard links.
// An unsupported filesystem is unknown evidence, never a name-only fallback.
func ExactSocketIDAt(dirfd int, path, boot, name string) (string, error) {
	if boot == "" || !SafeSessionName(name) {
		return "", fmt.Errorf("missing exact socket identity inputs")
	}
	var st unix.Statx_t
	if err := unix.Statx(dirfd, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BASIC_STATS|unix.STATX_BTIME, &st); err != nil {
		return "", err
	}
	return exactSocketID(boot, name, st)
}

func exactSocketID(boot, name string, st unix.Statx_t) (string, error) {
	if st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("unsafe session socket")
	}
	if st.Mask&unix.STATX_BTIME == 0 || (st.Btime.Sec == 0 && st.Btime.Nsec == 0) {
		return "", fmt.Errorf("socket birth time unavailable; exact reconnect unsupported on this filesystem")
	}
	payload := fmt.Sprintf("terminal-redeemer/session/v2\x00%d:%s\x00%d:%d:%d:%d:%d\x00%s", len(boot), boot, st.Dev_major, st.Dev_minor, st.Ino, st.Btime.Sec, st.Btime.Nsec, name)
	sum := sha256.Sum256([]byte(payload))
	return "ses_" + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
