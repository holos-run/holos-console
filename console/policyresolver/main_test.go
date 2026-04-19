// main_test.go wires a package-level scopeshim.DefaultResolver for the
// tests in this package. HOL-619 replaced the proto-level TemplateScopeRef
// with a namespace field backed by the shim's default resolver; test
// fixtures that exercise (scope, scopeName) via
// scopeshim.NewLinkedTemplateRef / NewLinkedTemplatePolicyRef rely on a
// resolver being registered. This TestMain installs the canonical HOL-567
// fixture resolver (holos-org- / holos-fld- / holos-prj- prefixes) once
// per test binary so individual tests do not need to plumb one through.
//
// HOL-622 added an envtest-backed multi-pod freshness regression
// (TestFolderResolver_MultiPodFreshness) that spins up a controller-runtime
// Manager through console/crdmgr/testing. Wrapping m.Run in
// crdmgrtesting.RunTestsWithSharedEnv ensures the process-singleton
// envtest apiserver is Stop()'d after the last test in the package runs
// so `go test` does not leak subprocesses.
package policyresolver

import (
	"os"
	"testing"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/scopeshim"
)

func TestMain(m *testing.M) {
	scopeshim.SetDefaultResolver(baseResolver())
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}
