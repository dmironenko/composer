package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecretsCommands(t *testing.T) {
	t.Parallel()

	const (
		keyFile   = "../../internal/app/composer/adapters/gitops/internal/vars/testdata/age-key.txt"
		plaintext = "stage_token: demo-only\n"
	)

	encrypted := runSecretsCommand(t, "encrypt", keyFile, plaintext)
	if !strings.HasPrefix(encrypted, "age:") {
		t.Fatal("encrypt must only output an encrypted value")
	}

	decrypted := runSecretsCommand(t, "decrypt", keyFile, encrypted)
	if decrypted != plaintext {
		t.Fatal("decrypt did not restore the YAML mapping")
	}
}

func runSecretsCommand(t *testing.T, action, keyFile, input string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := newCommand("test", &stdout, &stderr)
	cmd.SetArgs([]string{"secrets", action, "--key-file", keyFile})
	cmd.SetIn(strings.NewReader(input))

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if stderr.Len() != 0 {
		t.Fatal("secrets command wrote to stderr")
	}

	return stdout.String()
}

func TestSecretsCommandFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{name: "key required", args: []string{"secrets", "encrypt"}},
		{name: "missing key", args: []string{"secrets", "encrypt", "--key-file", "missing"}},
		{name: "invalid value", args: []string{"secrets", "decrypt", "--key-file", "missing"}},
		{name: "positional secret", args: []string{"secrets", "encrypt", "unexpected"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			cmd := newCommand("test", &stdout, &stderr)
			cmd.SetArgs(tc.args)
			cmd.SetIn(strings.NewReader("token: private-marker\n"))

			err := cmd.Execute()
			if err == nil || stdout.Len() != 0 {
				t.Fatal("failed command must return an error without output")
			}

			if strings.Contains(err.Error()+stderr.String(), "private-marker") {
				t.Fatal("failed command exposed the secret")
			}
		})
	}
}
