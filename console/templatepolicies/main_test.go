// main_test.go wraps m.Run through crdmgrtesting.RunTestsWithSharedEnv so
// the process-singleton envtest Environment is Stop()'d after the last
// test in this package runs, preventing kube-apiserver/etcd subprocesses
// from being reaped on os.Exit mid-shutdown.
//
// Post-HOL-723 the package no longer installs a package-global
// scopeshim.DefaultResolver — tests that need one call newTestResolver()
// (see k8s_test.go) and pass it explicitly.
package templatepolicies

import (
	"os"
	"testing"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
)

func TestMain(m *testing.M) {
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}
