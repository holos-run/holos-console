// main_test.go wraps `m.Run` in the envtest-backed shared-env helper so
// the process-singleton apiserver is Stop()'d after the last test in the
// package runs. HOL-622 added the envtest-backed multi-pod freshness
// regression (TestFolderResolver_MultiPodFreshness) which requires the
// shared envtest; wrapping m.Run in crdmgrtesting.RunTestsWithSharedEnv
// ensures `go test` does not leak subprocesses.
package policyresolver

import (
	"os"
	"testing"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
)

func TestMain(m *testing.M) {
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}
