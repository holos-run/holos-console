// main_test.go wires a package-level scopeshim.DefaultResolver for the
// tests in this package. HOL-619 replaced the proto-level TemplateScopeRef
// with a namespace field backed by the shim's default resolver; test
// fixtures that exercise (scope, scopeName) via
// scopeshim.NewLinkedTemplateRef / NewLinkedTemplatePolicyRef rely on a
// resolver being registered. This TestMain installs the canonical HOL-567
// fixture resolver (holos-org- / holos-fld- / holos-prj- prefixes) once
// per test binary so individual tests do not need to plumb one through.
package policyresolver

import (
	"os"
	"testing"

	"github.com/holos-run/holos-console/console/scopeshim"
)

func TestMain(m *testing.M) {
	scopeshim.SetDefaultResolver(baseResolver())
	os.Exit(m.Run())
}
