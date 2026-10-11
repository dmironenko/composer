package git

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Auth supplies explicit transport credentials; file contents never enter snapshots.
type Auth struct {
	Type           string
	KeyFile        string
	KnownHostsFile string
	TokenFile      string
}

var (
	errToken      = errors.New("empty git token file")
	errKey        = errors.New("cannot parse git private key (encrypted keys are not supported yet)")
	errKnownHosts = errors.New("cannot read git known_hosts")
	errAuth       = errors.New("unsupported git authentication")
)

func authentication(auth Auth, url string) (transport.AuthMethod, error) {
	switch auth.Type {
	case "", "none":
		return nil, nil //nolint:nilnil // go-git uses nil credentials for public repositories.
	case "https":
		token, err := os.ReadFile(auth.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("read git token file: %w", err)
		}

		password := strings.TrimSpace(string(token))

		if password == "" {
			return nil, errToken
		}

		return &githttp.BasicAuth{Username: "git", Password: password}, nil
	case "ssh":
		return sshAuthentication(auth, url)
	default:
		return nil, errAuth
	}
}

func sshAuthentication(auth Auth, url string) (transport.AuthMethod, error) {
	endpoint, err := transport.NewEndpoint(url)
	if err != nil {
		return nil, ErrFetch
	}

	user := endpoint.User
	if user == "" {
		user = "git"
	}

	key, err := os.ReadFile(auth.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("read git key: %w", err)
	}

	publicKeys, err := gitssh.NewPublicKeys(user, key, "")
	if err != nil {
		return nil, errKey
	}

	callback, err := knownhosts.New(auth.KnownHostsFile)
	if err != nil {
		return nil, errKnownHosts
	}

	publicKeys.HostKeyCallback = callback

	return publicKeys, nil
}
