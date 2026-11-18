package workbench

import (
	"os"
	"testing"

	"github.com/vivaui/viva/leaks"

	"motty/internal/backup"
)

// The tests must close every shell and worker they start.
func TestMain(m *testing.M) {
	backup.UseCheapKDFForTests()
	// A real trim firing seconds after one test's close would collect the heap
	// in the middle of another test's allocation measurement.
	memoryTrim.trim = func() {}
	// The local terminal names this computer in what it paints; the goldens
	// cannot depend on the machine that runs them.
	localMachineName = func() (string, error) { return "test-host.local", nil }
	os.Exit(leaks.Main(m))
}
