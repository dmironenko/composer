//go:build integration

package git_test

import (
	"errors"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504 targets old Go versions; this local fixture uses the current stdlib.
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/dmironenko/composer/v2/internal/app/composer/adapters/git"
)

//nolint:paralleltest // The HTTPS transport registry is global and restored before parallel tests run.
func TestHTTPSAuthentication(t *testing.T) {
	root := t.TempDir()
	repo := newRepository(t, root, "release/source")
	commitFile(t, repo, root, "version.txt", "selected")
	commitFile(t, repo, root, "version.txt", "branch moved")
	server := httpsRepository(t, root)
	tokenFile := filepath.Join(t.TempDir(), "token")
	auth := git.Auth{Type: "https", TokenFile: tokenFile}
	source := git.SourceConfig{Mode: "remote", URL: server + "/.git", Branch: "release/source", Auth: auth}
	state := t.TempDir()

	for _, token := range []string{"composer-test-token", "wrong-token"} {
		t.Run(token, func(t *testing.T) {
			if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			checkHTTPSAuthentication(t, source, state, token == "composer-test-token")
		})
	}

	t.Run("public", func(t *testing.T) {
		source.URL, source.Auth = server+"/public/.git", git.Auth{Type: "none"}
		checkHTTPSAuthentication(t, source, t.TempDir(), true)
	})
}

func checkHTTPSAuthentication(t *testing.T, source git.SourceConfig, state string, authorized bool) {
	t.Helper()

	snapshot, readErr := git.New(source, state).Read(t.Context())
	if !authorized {
		if !errors.Is(readErr, git.ErrFetch) || snapshot.Root != "" {
			t.Fatalf("unauthorized fetch: snapshot=%v read=%v", snapshot, readErr)
		}

		return
	}

	if readErr != nil {
		t.Fatalf("authenticated fetch: read=%v", readErr)
	}

	assertFile(t, filepath.Join(snapshot.Root, "version.txt"), "branch moved")
}

//nolint:maintidx // One local fixture owns the backend, TLS listener, authentication and transport cleanup.
func httpsRepository(t *testing.T, root string) string {
	t.Helper()

	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}

	backend := &cgi.Handler{
		Path: gitBinary, Args: []string{"http-backend"},
		Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/public/") {
			http.StripPrefix("/public", backend).ServeHTTP(writer, request)

			return
		}

		user, token, ok := request.BasicAuth()
		if !ok || user != "git" || token != "composer-test-token" {
			writer.Header().Set("WWW-Authenticate", `Basic realm="Git"`)
			http.Error(writer, "authentication required", http.StatusUnauthorized)

			return
		}

		backend.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)

	previous := gitclient.Protocols["https"]

	gitclient.InstallProtocol("https", githttp.NewClient(server.Client()))
	t.Cleanup(func() { gitclient.InstallProtocol("https", previous) })

	return server.URL
}
