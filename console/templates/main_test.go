// main_test.go exposes a package-level testResolver used by test helpers
// such as orgScopeRef / folderScopeRef / projectScopeRef. Post-HOL-723
// the handler speaks in Kubernetes namespace directly, so the test
// helpers also resolve namespaces by calling methods on this resolver
// rather than a package-global scopeshim registry.
package templates

import (
	"os"
	"testing"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/resolver"
)

// testResolver mirrors the resolver the fake K8sClient uses so the
// namespace strings the helpers emit round-trip through
// resolver.ResourceTypeFromNamespace exactly as production does. The
// existing fixtures in k8s_test.go use bare `org-`, `fld-`, `prj-`
// prefixes (no `holos-` NamespacePrefix) — keeping the resolver consistent
// with those fixtures avoids a cross-file rename.
var testResolver = &resolver.Resolver{
	OrganizationPrefix: "org-",
	FolderPrefix:       "fld-",
	ProjectPrefix:      "prj-",
}

func TestMain(m *testing.M) {
	// Wrap m.Run through crdmgrtesting.RunTestsWithSharedEnv so the
	// process-singleton envtest Environment is Stop()'d after the last
	// test in this package, rather than being reaped on os.Exit and
	// leaving kube-apiserver/etcd subprocesses behind for a long-enough
	// `go test` shutdown to race.
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}
