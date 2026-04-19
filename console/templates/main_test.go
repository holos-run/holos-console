// main_test.go wires a package-level scopeshim.DefaultResolver for the
// tests in this package. HOL-619 replaced the proto-level TemplateScopeRef
// with a namespace field backed by the shim's default resolver; test
// helpers (orgScopeRef / folderScopeRef / projectScopeRef) call
// DefaultResolver().*Namespace directly, which panics when no resolver
// has been installed. Registering the same resolver the fake K8sClient
// uses keeps the tests honest — the namespace strings the helpers emit
// round-trip through scopeshim.FromNamespace exactly as production does.
package templates

import (
	"os"
	"testing"

	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
)

func TestMain(m *testing.M) {
	scopeshim.SetDefaultResolver(&resolver.Resolver{
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	})
	os.Exit(m.Run())
}
