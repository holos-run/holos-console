// main_test.go wraps m.Run through crdmgrtesting.RunTestsWithSharedEnv so
// the process-singleton envtest Environment is Stop()'d after the last
// test in this package runs.
//
// Post-HOL-723 the package no longer installs a package-global
// scopeshim.DefaultResolver; tests pass a newTestResolver() explicitly
// wherever a resolver is required (see k8s_test.go).
package templatepolicybindings

import (
	"os"
	"testing"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
)

func TestMain(m *testing.M) {
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}
