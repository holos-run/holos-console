// main_test.go wires a package-level scopeshim.DefaultResolver for the
// tests in this package. HOL-619 replaced the proto-level TemplateScopeRef
// with a namespace field; test helpers call
// DefaultResolver().*Namespace directly, which panics when no resolver
// has been installed. Registering the same resolver the fake K8sClient
// uses keeps namespace strings and the shim's classification in sync.
package templatepolicybindings

import (
	"os"
	"testing"

	"github.com/holos-run/holos-console/console/scopeshim"
)

func TestMain(m *testing.M) {
	scopeshim.SetDefaultResolver(newTestResolver())
	os.Exit(m.Run())
}
