package cli

import (
	"os"
	"strings"
	"testing"
)

// TestDoctorPathsTrim verifies ~ shortening of home-dir paths.
func TestDoctorPathsTrim(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory available")
	}
	got := trimDoctorPath(home + "/bin/forged")
	if !strings.HasPrefix(got, "~") {
		t.Errorf("trimDoctorPath(%q) = %q, want a ~-prefixed path", home+"/bin/forged", got)
	}
	if got == trimDoctorPath("/usr/bin/git") {
		t.Error("different inputs must not collapse to the same output")
	}
	// Absolute paths outside home are returned untouched.
	if p := "/usr/bin/git"; trimDoctorPath(p) != p {
		t.Errorf("system paths must be unchanged, got %q", trimDoctorPath(p))
	}
}

func TestDoctorResultAllOK(t *testing.T) {
	res := &doctorResult{checks: []check{{name: "a", ok: true}, {name: "b", ok: true}}}
	if !res.allOK() {
		t.Error("all-pass result must report allOK")
	}
	res.checks = append(res.checks, check{name: "c", ok: false})
	if res.allOK() {
		t.Error("one failing check must flip allOK")
	}
}

func TestDiskSpaceCheckGraceful(t *testing.T) {
	checks := diskSpaceCheck(t.TempDir())
	if len(checks) != 1 {
		t.Fatalf("expected exactly one disk check, got %d", len(checks))
	}
	// A freshly created temp dir must not crash; ok may be true or false
	// depending on the host filesystem, but a note must always exist.
	if checks[0].note == "" {
		t.Error("disk check must carry a note")
	}
}
