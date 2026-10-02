package mirror

import (
	"context"
	"fmt"
	"io"
	"os"
)

func runRecoveryProbe(ctx context.Context, remote RemoteConfig, runtime, self string, lock *os.File) (SessionInventory, error) {
	return SessionInventory{}, fmt.Errorf("shared recovery probes require Linux")
}
func RunSessionRecoveryProbe(ctx context.Context, remote RemoteConfig, runtime string, lock, parent, cancellation *os.File, output io.Writer) error {
	return fmt.Errorf("shared recovery probes require Linux")
}
