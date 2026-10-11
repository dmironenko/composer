package git //nolint:testpackage // Verify credential construction without starting an SSH or HTTP server.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

//nolint:gocognit,gocyclo,cyclop,maintidx // Table assertions cover distinct authentication transports in one fixture.
func TestAuthentication(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, kind, url, expectedUser string }{
		{name: "public", kind: "none", url: "https://example.invalid/repo"},
		{name: "token file", kind: "https", url: "https://example.invalid/repo", expectedUser: "git"},
		{name: "SSH scp user", kind: "ssh", url: "deploy@example.invalid:repo.git", expectedUser: "deploy"},
		{name: "SSH URL user", kind: "ssh", url: "ssh://git@example.invalid/repo.git", expectedUser: "git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			auth := Auth{
				Type: tc.kind, KeyFile: filepath.Join(root, "key"),
				KnownHostsFile: filepath.Join(root, "known_hosts"), TokenFile: filepath.Join(root, "token"),
			}

			_, privateKey, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}

			block, err := ssh.MarshalPrivateKey(privateKey, "test")
			if err != nil {
				t.Fatal(err)
			}

			for name, data := range map[string][]byte{
				auth.KeyFile: pem.EncodeToMemory(block), auth.KnownHostsFile: {}, auth.TokenFile: []byte("test-token\n"),
			} {
				if err = os.WriteFile(name, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			method, err := authentication(auth, tc.url)
			if err != nil {
				t.Fatal(err)
			}

			switch tc.kind {
			case "none":
				if method != nil {
					t.Fatal("public source has credentials")
				}
			case "https":
				value, ok := method.(*githttp.BasicAuth)
				if !ok || value.Username != tc.expectedUser || value.Password != "test-token" {
					t.Fatal("incorrect token authentication")
				}
			case "ssh":
				value, ok := method.(*gitssh.PublicKeys)
				if !ok || value.User != tc.expectedUser || value.HostKeyCallback == nil {
					t.Fatal("incorrect SSH authentication")
				}
			}
		})
	}
}
