package buildinfo

import (
	"os"
	"strings"
	"testing"
)

func TestArchPackageHasTheReleaseVersion(t *testing.T) {
	version, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	pkgbuild, err := os.ReadFile("../../packaging/arch/PKGBUILD")
	if err != nil {
		t.Fatal(err)
	}
	if want := "pkgver=" + strings.TrimSpace(string(version)) + "\n"; !strings.Contains(string(pkgbuild), want) {
		t.Fatalf("packaging/arch/PKGBUILD should say %q, like VERSION", strings.TrimSpace(want))
	}
}
