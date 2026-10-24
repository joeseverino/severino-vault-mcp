// Package scripts_test runs the repository's shell scripts under /bin/sh, as
// CI and `mise run` do.
package scripts_test

import (
	"errors"
	"os/exec"
	"testing"
)

// The release workflow runs release-tag-check.sh before it builds anything,
// so a tag it accepts is a tag that ships.
func TestReleaseTagCheck(t *testing.T) {
	cases := []struct {
		name, tag, module string
		ok                bool
	}{
		{"v0 with no suffix", "v0.4.0", "example.com/tool", true},
		{"v1 with no suffix", "v1.2.0", "example.com/tool", true},
		{"v2 with no suffix", "v2.0.0", "example.com/tool", false},
		{"v3 with /v3", "v3.1.0", "example.com/tool/v3", true},
		{"v3 with /v2", "v3.0.0", "example.com/tool/v2", false},
		{"v1 with /v1", "v1.0.0", "example.com/tool/v1", false},
		{"v2 with a path ending in v2 and letters", "v2.0.0", "example.com/tool/v2abc", false},
		{"v1 with a path ending in v1 and letters", "v1.0.0", "example.com/tool/v1abc", true},
		{"v12 with /v12", "v12.0.0", "example.com/tool/v12", true},
		{"no leading v", "1.2.0", "example.com/tool", false},
		{"two parts", "v1.2", "example.com/tool", false},
		{"four parts", "v1.2.3.4", "example.com/tool", false},
		{"a pre-release", "v1.2.0-rc.1", "example.com/tool", false},
		{"a leading zero", "v01.2.0", "example.com/tool", false},
		{"an empty part", "v1..0", "example.com/tool", false},
		{"a name", "latest", "example.com/tool", false},
		{"a trailing line", "v1.2.0\nv2.0.0", "example.com/tool", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := exec.Command("/bin/sh", "release-tag-check.sh", c.tag, c.module).CombinedOutput()
			if c.ok && err != nil {
				t.Fatalf("tag %q, module %q: refused (%v): %s", c.tag, c.module, err, out)
			}
			if !c.ok {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("tag %q, module %q: want exit 1, got %v: %s", c.tag, c.module, err, out)
				}
			}
		})
	}
}
