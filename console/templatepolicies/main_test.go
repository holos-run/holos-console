// main_test.go wires a package-level scopeshim.DefaultResolver for the
// tests in this package. HOL-619 replaced the proto-level TemplateScopeRef
// with a namespace field backed by the shim's default resolver; test
// helpers (newFolderScope / newOrgScope / newProjectScope) call
// DefaultResolver().*Namespace directly, which panics when no resolver
// has been installed. Registering the same resolver the fake K8sClient
// uses keeps the tests honest — the namespace strings the helpers emit
// round-trip through scopeshim.FromNamespace exactly as production does.
package templatepolicies

import (
	"os"
	"testing"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/scopeshim"
)

func TestMain(m *testing.M) {
	scopeshim.SetDefaultResolver(newTestResolver())
	// Wrap m.Run through crdmgrtesting.RunTestsWithSharedEnv so the
	// process-singleton envtest Environment is Stop()'d after the last
	// test in this package runs.
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}
