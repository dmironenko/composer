// Package gitserver serves temporary Git repositories over authenticated SSH in tests.
package gitserver

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const keyFileMode = 0o600

// Auth contains the temporary credentials generated for a test server.
type Auth struct {
	Type           string
	KeyFile        string
	KnownHostsFile string
	TokenFile      string
}

// New starts an SSH server that trusts a generated client key and pins its host key.
//
//nolint:gocognit,gocyclo,cyclop,maintidx // One real SSH fixture owns credentials, listener and server cleanup.
func New(t *testing.T, root string) (string, Auth) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())

	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}

	clientSigner, clientKey := checkoutSSHKey(t)
	hostSigner, _ := checkoutSSHKey(t)
	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), clientSigner.PublicKey().Marshal()) {
				return nil, fmt.Errorf("client key: %w", os.ErrPermission)
			}

			return &ssh.Permissions{}, nil
		},
	}
	serverConfig.AddHostKey(hostSigner)

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	auth := Auth{
		Type: "ssh", KeyFile: filepath.Join(directory, "key"),
		KnownHostsFile: filepath.Join(directory, "known_hosts"),
	}

	knownHost := knownhosts.Line([]string{listener.Addr().String()}, hostSigner.PublicKey()) + "\n"
	for path, data := range map[string][]byte{auth.KeyFile: clientKey, auth.KnownHostsFile: []byte(knownHost)} {
		if writeErr := os.WriteFile(path, data, keyFileMode); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			socket, acceptErr := listener.Accept()
			if errors.Is(acceptErr, net.ErrClosed) {
				return
			}

			if acceptErr != nil {
				t.Error(acceptErr)

				return
			}

			workers.Go(func() {
				if serverErr := serveCheckoutConnection(ctx, socket, serverConfig, gitBinary, root); serverErr != nil {
					t.Error(serverErr)
				}
			})
		}
	})
	t.Cleanup(func() {
		cancel()

		if closeErr := listener.Close(); closeErr != nil {
			t.Error(closeErr)
		}

		workers.Wait()
	})

	return "ssh://git@" + listener.Addr().String() + "/source.git", auth
}

func checkoutSSHKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	block, err := ssh.MarshalPrivateKey(key, "checkout test")
	if err != nil {
		t.Fatal(err)
	}

	return signer, pem.EncodeToMemory(block)
}

func serveCheckoutConnection(
	ctx context.Context, socket net.Conn, cfg *ssh.ServerConfig, gitBinary, root string,
) error {
	stop := context.AfterFunc(ctx, func() { _ = socket.Close() })
	defer stop()
	defer socket.Close()

	connection, channels, requests, err := ssh.NewServerConn(socket, cfg)
	if err != nil {
		return fmt.Errorf("SSH handshake: %w", err)
	}

	defer connection.Close()

	go ssh.DiscardRequests(requests)

	for incoming := range channels {
		if sessionErr := serveCheckoutSession(ctx, incoming, gitBinary, root); sessionErr != nil {
			return sessionErr
		}
	}

	return nil
}

//nolint:gocognit,gocyclo,cyclop,maintidx // The SSH fixture rejects non-exec requests before running Git.
func serveCheckoutSession(ctx context.Context, incoming ssh.NewChannel, gitBinary, root string) error {
	channel, requests, err := incoming.Accept()
	if err != nil {
		return fmt.Errorf("accept SSH session: %w", err)
	}

	defer channel.Close()

	for request := range requests {
		if request.Type != "exec" {
			if err = request.Reply(false, nil); err != nil {
				return fmt.Errorf("reject SSH request: %w", err)
			}

			continue
		}

		if err = request.Reply(true, nil); err != nil {
			return fmt.Errorf("accept SSH command: %w", err)
		}

		command := exec.CommandContext(ctx, gitBinary, "-c", "uploadpack.allowReachableSHA1InWant=true", "upload-pack", root)
		status := struct{ Status uint32 }{}

		command.Stdin, command.Stdout, command.Stderr = channel, channel, channel.Stderr()
		if err = command.Run(); err != nil {
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) {
				return fmt.Errorf("upload-pack: %w", err)
			}

			status.Status = uint32(exitError.ExitCode()) //nolint:gosec // SSH encodes the child's exit status as uint32.
		}

		_, err = channel.SendRequest("exit-status", false, ssh.Marshal(status))
		// A client may close the session after consuming the complete pack.
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("SSH exit status: %w", err)
		}

		return nil
	}

	return nil
}
