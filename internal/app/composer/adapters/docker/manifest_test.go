//nolint:testpackage // Verify private SDK restoration without exposing adapter internals.
package docker

import (
	"bytes"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

func TestManifestRoundTrip(t *testing.T) {
	t.Parallel()

	working := t.TempDir()

	original, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
		WorkingDir: working, Environment: map[string]string{},
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: []byte(manifestFixture)}},
	}, func(options *loader.Options) {
		options.SetProjectName("manifest-test", true)
		options.SkipInterpolation = true
	})
	if err != nil {
		t.Fatal(err)
	}

	input := strings.ReplaceAll(manifestFixture, "io.composer.config: fixture", "io.composer.config: ignored")

	deployment := domain.Project{
		Name:     original.Name,
		Manifest: domain.Manifest{Content: []byte(input), WorkingDir: working, ConfigHash: "fixture"},
	}

	restored, err := loadProject(t.Context(), deployment)
	if err != nil {
		t.Fatal(err)
	}

	assertCanonicalProject(t, original, restored)

	assertProjectMetadata(t, original, restored)
}

func assertCanonicalProject(t *testing.T, original, restored *types.Project) {
	t.Helper()

	encoded, err := original.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	actual, err := restored.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(actual, encoded) {
		t.Fatalf("manifest normalization changed: before=%s after=%s", encoded, actual)
	}
}

func assertProjectMetadata(t *testing.T, original, restored *types.Project) {
	t.Helper()

	if len(original.DisabledServices) != 1 || len(original.Models) != 1 {
		t.Fatal("fixture must exercise disabled services and models")
	}

	if !reflect.DeepEqual(original.DisabledServices, restored.DisabledServices) ||
		!reflect.DeepEqual(original.Models, restored.Models) {
		t.Fatal("manifest lost disabled services or models")
	}

	for name, service := range restored.Services {
		expected := types.Labels{
			api.ProjectLabel: original.Name, api.ServiceLabel: name, api.VersionLabel: api.ComposeVersion,
			api.WorkingDirLabel: original.WorkingDir, api.OneoffLabel: "False",
		}
		if !maps.Equal(service.CustomLabels, expected) {
			t.Fatalf("missing Compose SDK labels for service %s: %v", name, service.CustomLabels)
		}
	}
}

const manifestFixture = `
x-deployment: {metadata: preserved}
services:
  api:
    image: busybox:stable
    models: [fixture]
    command: [sh, -c, 'echo $$HOME ${SECRET:-fallback}']
    environment:
      PASSWORD: '$PRIVATE-${UNSET:-fallback}-$$'
    labels:
      io.composer.config: fixture
    deploy:
      replicas: 3
    depends_on:
      bootstrap:
        condition: service_completed_successfully
    volumes:
      - ./configs/app.yaml:/etc/app.yaml:ro
      - data:/data
    networks:
      default:
        aliases: [api-http]
    healthcheck:
      test: [CMD, /bin/true]
      interval: 2s
    x-service: {metadata: preserved}
  dormant:
    image: busybox:stable
    profiles: [optional]
  bootstrap:
    image: busybox:stable
    labels: {io.composer.config: fixture}
    command: ['true']
models:
  fixture:
    model: ai/smollm2
volumes:
  data: {}
networks:
  default:
    external: true
    name: namespace-network
`
