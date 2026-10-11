package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, version string }{{name: "injected version", version: "0.1.0-test"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			cmd := newCommand(tc.version, &stdout, &stderr)
			cmd.SetArgs([]string{"--version"})

			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(stdout.String(), tc.version) || stderr.Len() != 0 {
				t.Fatalf("version output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
