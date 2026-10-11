//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func dockerEngine(t *testing.T, installation string) client.APIClient {
	t.Helper()

	docker, err := command.NewDockerCli()
	if err != nil {
		t.Fatal(err)
	}

	if err = docker.Initialize(&flags.ClientOptions{}); err != nil {
		t.Fatal(err)
	}

	engine := docker.Client()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		removeContainers(t, ctx, engine, installation)
		removeNetworks(t, ctx, engine, installation)
		removeVolumes(t, ctx, engine, installation)

		if closeErr := engine.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return engine
}

func dockerContainers(
	t *testing.T, ctx context.Context, engine client.APIClient, prefix string, expected int,
) []container.Summary {
	t.Helper()

	response, err := engine.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}

	var result []container.Summary

	for _, item := range response.Items {
		if strings.HasPrefix(item.Labels[api.ProjectLabel], prefix) {
			result = append(result, item)
		}
	}

	if len(result) != expected {
		t.Fatalf("%s containers: actual=%d expected=%d", prefix, len(result), expected)
	}

	return result
}

func removeContainers(t *testing.T, ctx context.Context, engine client.APIClient, installation string) {
	t.Helper()

	items, err := engine.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		t.Error(err)

		return
	}

	for _, item := range items.Items {
		if !strings.HasPrefix(item.Labels[api.ProjectLabel], installation+"-") {
			continue
		}

		if _, err = engine.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			t.Error(err)
		}
	}
}

func removeNetworks(t *testing.T, ctx context.Context, engine client.APIClient, installation string) {
	t.Helper()

	networks, err := engine.NetworkList(ctx, client.NetworkListOptions{
		Filters: client.Filters{}.Add("label", "io.composer.installation="+installation),
	})
	if err != nil {
		t.Error(err)

		return
	}

	for _, network := range networks.Items {
		if _, err = engine.NetworkRemove(ctx, network.ID, client.NetworkRemoveOptions{}); err != nil {
			t.Error(err)
		}
	}
}

func removeVolumes(t *testing.T, ctx context.Context, engine client.APIClient, installation string) {
	t.Helper()

	volumes, err := engine.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		t.Error(err)

		return
	}

	for _, volume := range volumes.Items {
		if !strings.HasPrefix(volume.Labels[api.ProjectLabel], installation+"-") {
			continue
		}

		if _, err = engine.VolumeRemove(ctx, volume.Name, client.VolumeRemoveOptions{}); err != nil {
			t.Error(err)
		}
	}
}
