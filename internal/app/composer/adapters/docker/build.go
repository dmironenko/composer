package docker

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/containerd/errdefs"
	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// HasImage checks Docker's local image store without pulling from a registry.
func (c *Client) HasImage(ctx context.Context, image string) (bool, error) {
	_, err := c.engine.ImageInspect(ctx, image)
	if errdefs.IsNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("inspect source image: %w", err)
	}

	return true, nil
}

// Build executes the rendered Dockerfile with an empty context in Docker's embedded BuildKit.
func (c *Client) Build(ctx context.Context, spec domain.BuildSpec) (err error) {
	const (
		dockerfileMode = 0o600
		contextMode    = 0o755
	)

	builder, err := c.buildkit(ctx)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, builder.Close()) }()

	_, err = builder.Solve(ctx, nil, buildkit.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{"build-arg:COMPOSER_COMMIT": spec.Commit},
		FrontendInputs: map[string]llb.State{
			"context":    llb.Scratch().File(llb.Mkdir("/", contextMode)),
			"dockerfile": llb.Scratch().File(llb.Mkfile("Dockerfile", dockerfileMode, []byte(spec.Dockerfile))),
		},
		Exports: []buildkit.ExportEntry{{Type: "moby", Attrs: map[string]string{"name": spec.Image()}}},
		Session: []session.Attachable{authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: authprovider.LoadAuthConfig(c.cli.ConfigFile()),
		})},
	}, nil)
	if err != nil {
		return fmt.Errorf("build source image: %w", err)
	}

	return nil
}

func (c *Client) buildkit(ctx context.Context) (*buildkit.Client, error) {
	builder, err := buildkit.New(ctx, "", buildkit.WithContextDialer(
		func(ctx context.Context, _ string) (net.Conn, error) {
			return c.engine.DialHijack(ctx, "/grpc", "h2c", nil)
		}), buildkit.WithSessionDialer(
		func(ctx context.Context, protocol string, metadata map[string][]string) (net.Conn, error) {
			return c.engine.DialHijack(ctx, "/session", protocol, metadata)
		}))
	if err != nil {
		return nil, fmt.Errorf("connect buildkit: %w", err)
	}

	return builder, nil
}
