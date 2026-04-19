// main_test.go wires TestMain for the shared-helper's own smoke tests
// so the process-singleton envtest Environment is Stop()'d after every
// test in this package runs, matching the pattern the three consumer
// storage suites use.
package crdmgrtesting

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(RunTestsWithSharedEnv(m))
}
