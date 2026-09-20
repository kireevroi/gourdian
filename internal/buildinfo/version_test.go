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
	// Arch forbids a hyphen in pkgver, so a prerelease such as 1.8.0-beta.1 is spelled
	// 1.8.0beta.1 there. Everything else about the two has to agree.
	want := "pkgver=" + strings.ReplaceAll(strings.TrimSpace(string(version)), "-", "") + "\n"
	if !strings.Contains(string(pkgbuild), want) {
		t.Fatalf("packaging/arch/PKGBUILD should say %q, like VERSION", strings.TrimSpace(want))
	}
}
