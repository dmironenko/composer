//go:build integration

//nolint:testpackage // Integration assertions and cleanup use the owned Engine connection.
package docker

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

func TestBuildWithoutExecutables(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	t.Setenv("PATH", t.TempDir())
	docker, spec, root := buildFixture(t)

	checkImageExists(t, ctx, docker, spec.Image(), false)

	if err := docker.Build(ctx, spec); err != nil {
		t.Fatal(err)
	}

	checkImageExists(t, ctx, docker, spec.Image(), true)

	actual, err := docker.engine.ImageInspect(ctx, spec.Image())
	if err != nil {
		t.Fatal(err)
	}

	if actual.Config.Labels["io.composer.test.tag"] != spec.Tag {
		t.Fatalf("rendered label: actual=%v expected=%s", actual.Config.Labels, spec.Tag)
	}

	checkImageFile(t, ctx, docker, spec.Image(), root)
}

func buildFixture(t *testing.T) (*Client, domain.BuildSpec, string) {
	t.Helper()

	docker := newBuildClient(t)

	root := t.TempDir()

	spec := domain.BuildSpec{
		Repository: root, Tag: "v1.0.0",
		Dockerfile: fmt.Sprintf(`FROM busybox:1.37.0 AS builder
RUN printf %%s %q > /marker
FROM scratch
LABEL io.composer.test.tag=v1.0.0
COPY --from=builder /marker /marker
COPY . /context/
`, root),
	}

	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()

		_, removeErr := docker.engine.ImageRemove(cleanup, spec.Image(), client.ImageRemoveOptions{})
		if removeErr != nil && !errdefs.IsNotFound(removeErr) {
			t.Error(removeErr)
		}
	})

	return docker, spec, root
}

func newBuildClient(t *testing.T) *Client {
	t.Helper()

	docker, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := docker.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return docker
}

func checkImageExists(t *testing.T, ctx context.Context, docker *Client, image string, expected bool) {
	t.Helper()

	actual, err := docker.HasImage(ctx, image)
	if err != nil || actual != expected {
		t.Fatalf("image exists: actual=%v expected=%v err=%v", actual, expected, err)
	}
}

func checkImageFile(t *testing.T, ctx context.Context, docker *Client, image, expected string) {
	t.Helper()

	created, err := docker.engine.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: image, Cmd: []string{"unused"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()

		_, removeErr := docker.engine.ContainerRemove(cleanup, created.ID, client.ContainerRemoveOptions{})
		if removeErr != nil {
			t.Error(removeErr)
		}
	})

	content, err := docker.engine.ContainerExport(ctx, created.ID, client.ContainerExportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := content.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	checkArchive(t, tar.NewReader(content), expected)
}

func checkArchive(t *testing.T, archive *tar.Reader, expected string) {
	t.Helper()

	files := archiveFiles(t, archive)
	if files["marker"] != expected {
		t.Fatalf("copied marker: actual=%q expected=%q", files["marker"], expected)
	}

	for _, name := range []string{"Dockerfile", "context/Dockerfile", "bin/sh"} {
		if _, exists := files[name]; exists {
			t.Fatalf("build input leaked into image: %s", name)
		}
	}
}

func archiveFiles(t *testing.T, archive *tar.Reader) map[string]string {
	t.Helper()

	files := map[string]string{}

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return files
		}

		if err != nil {
			t.Fatal(err)
		}

		content, err := io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}

		files[header.Name] = string(content)
	}
}
