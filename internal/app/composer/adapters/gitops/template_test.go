package gitops_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops"
	"github.com/dmironenko/composer/internal/app/composer/domain"
)

func TestNamespaceNetworkReference(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, installation string }{
		{name: "normal name", installation: "example"},
		{name: "long installation", installation: strings.Repeat("example", 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, cfg := fixture(t, "demo", "worker")
			cfg.Installation = tc.installation
			cfg.Environments = []string{"prod", "stage"}

			write(t, root, "apps/demo/worker/vars/stage.yaml", "{}")
			write(t, root, "templates/compose.yaml.tmpl", gitops.DefaultTemplate()+
				"\nx-stage-network: {{ network \"stage\" \"demo\" | quote }}\n")

			projects, err := gitops.New(cfg).Load(t.Context(), root)
			if err != nil || len(projects) != 2 {
				t.Fatalf("Load: projects=%v, error=%v", projects, err)
			}

			actual, ok := model(t, projects[0]).Extensions["x-stage-network"].(string)
			if !ok || actual == projects[0].Network || !slices.Contains(projects[0].Networks, actual) {
				t.Fatalf("network reference=%v, installation network=%s", actual, projects[0].Network)
			}
		})
	}
}

func TestDefaultTemplateNetworks(t *testing.T) {
	t.Parallel()

	root, cfg := fixture(t, "demo", "worker")
	cfg.Environments = []string{"prod", "alice"}

	write(t, root, "apps/demo/worker/vars/base.yaml",
		strings.Replace(base, "compose:\n", "compose:\n  x-composer: {enabled: true}\n", 1))
	write(t, root, "apps/demo/worker/vars/stage.yaml", "{}")
	write(t, root, "apps/other/worker/vars/base.yaml", base)
	write(t, root, "apps/other/worker/vars/stage.yaml", "{}")

	projects, err := gitops.New(cfg).Load(t.Context(), root)
	check(t, err == nil && len(projects) == 4, "unexpected projects")

	for _, project := range projects {
		check(t, project.Error == "", project.Error)
		check(t, project.Network == projects[0].Network, "apps use different installation networks")
		check(t, len(project.Networks) == 0, "unexpected additional networks")

		compose := model(t, project)
		_, hasOptions := compose.Extensions["x-composer"]
		check(t, !hasOptions, "Composer options leaked into the deployment manifest")
		check(t, len(compose.Networks) == 1 && bool(compose.Networks["default"].External),
			"unexpected default network")
		check(t, compose.Networks["default"].Name == project.Network, "incorrect default network")
		check(t, len(modelService(t, project, "worker").Networks) == 1, "unexpected service networks")
	}
}

func TestAppSettingsDoNotConfigureCompose(t *testing.T) {
	t.Parallel()

	root, cfg := fixture(t, "demo", "worker")
	before := loadOne(t, cfg, root)
	check(t, before.Error == "", before.Error)

	write(t, root, "apps/demo/worker/vars/base.yaml", `app:
  networks: application-defined-value
  network_alias: application-alias
  route_namespace: application-namespace
`+base)

	after := loadOne(t, cfg, root)
	check(t, after.Error == "", after.Error)
	check(t, before.Hash == after.Hash, "app settings changed Compose configuration")
}

func TestDefaultTemplateServiceNetworks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, settings, networkMode string
		networks                    []string
	}{
		{name: "defaults", networks: []string{"default"}},
		{name: "host networking", settings: "      network_mode: host\n", networkMode: "host"},
		{
			name: "explicit networks", networks: []string{"private"},
			settings: "      networks: [private]\n  networks:\n    private: {}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, cfg := fixture(t, "demo", "worker")
			write(t, root, "apps/demo/worker/vars/base.yaml",
				"message: base\ncompose:\n  services:\n    worker:\n      image: busybox:stable\n"+tc.settings)

			project := loadOne(t, cfg, root)
			check(t, project.Error == "", project.Error)
			service := model(t, project).Services["worker"]

			check(t, service.NetworkMode == tc.networkMode, "unexpected network mode")
			check(t, slices.Equal(slices.Sorted(maps.Keys(service.Networks)), tc.networks), "unexpected networks")
		})
	}
}

func TestDefaultTemplateLogging(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, settings, driver string
		options                map[string]string
	}{
		{name: "defaults", driver: "json-file", options: map[string]string{"max-size": "10m", "max-file": "3"}},
		{name: "override", driver: "journald", settings: "      logging:\n        driver: journald\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, cfg := fixture(t, "demo", "worker")
			write(t, root, "apps/demo/worker/vars/base.yaml", base+tc.settings)

			project := loadOne(t, cfg, root)
			check(t, project.Error == "", project.Error)
			service := model(t, project).Services["worker"]

			check(t, service.Restart == "always", "missing restart policy")
			check(t, service.Logging.Driver == tc.driver, "unexpected logging driver")
			check(t, maps.Equal(service.Logging.Options, tc.options), "unexpected logging options")
		})
	}
}

func TestEnvironmentServiceNames(t *testing.T) {
	t.Parallel()

	for environment, suffix := range map[string]string{"prod": "", "stage": "-stage", "alice": "-alice"} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()

			root, cfg := fixture(t, "demo", "worker")
			cfg.Environments = []string{environment}

			write(t, root, "apps/demo/worker/vars/base.yaml", base+`
      networks:
        default:
          interface_name: media
      environment:
        LITERAL: $UNSET
    sidecar:
      image: busybox:stable
      depends_on: [worker]
      network_mode: service:worker
`)
			write(t, root, "apps/demo/worker/vars/stage.yaml", "{}")

			project := loadOne(t, cfg, root)
			check(t, project.Error == "", project.Error)
			assertSuffixedServices(t, project, suffix)
		})
	}
}

func assertSuffixedServices(t *testing.T, project domain.Project, suffix string) {
	t.Helper()

	services := model(t, project).Services
	check(t, len(services) == 2, "unexpected service count")

	worker, sidecar := services["worker"+suffix], services["sidecar"+suffix]
	check(t, worker.Name == "worker"+suffix, "unexpected service name")
	check(t, len(worker.Networks["default"].Aliases) == 0, "unexpected unsuffixed aliases")
	check(t, worker.Networks["default"].InterfaceName == "media", "network options lost")
	check(t, *worker.Environment["LITERAL"] == "$UNSET", "literal dollar changed")
	_, dependsOnWorker := sidecar.DependsOn[worker.Name]
	check(t, dependsOnWorker, "dependency was not renamed")
	check(t, sidecar.NetworkMode == "service:"+worker.Name, "network reference was not renamed")
}

func TestServiceSuffixReferences(t *testing.T) {
	t.Parallel()

	root, cfg := fixture(t, "demo", "worker")
	cfg.Environments = []string{"stage"}

	write(t, root, "apps/demo/worker/vars/stage.yaml", "{}")
	write(t, root, "apps/demo/worker/vars/base.yaml", base+`
      build:
        context: .
        dockerfile_inline: FROM scratch
    sidecar:
      image: busybox:stable
      links: [worker:legacy]
      volumes_from: [worker:ro, container:external:ro]
      ipc: service:worker
      pid: service:worker
      build:
        context: .
        dockerfile_inline: FROM scratch
        additional_contexts:
          source: service:worker
    manual:
      image: busybox:stable
      profiles: [manual]
      depends_on: [worker]
`)

	project := loadOne(t, cfg, root)
	check(t, project.Error == "", project.Error)
	assertServiceReferences(t, project)
}

func assertServiceReferences(t *testing.T, project domain.Project) {
	t.Helper()

	const reference = "service:worker-stage"

	compose := model(t, project)
	service := compose.Services["sidecar-stage"]

	check(t, slices.Equal(service.Links, []string{"worker-stage:legacy"}), "link target changed incorrectly")
	check(t, slices.Equal(service.VolumesFrom, []string{"worker-stage:ro", "container:external:ro"}),
		"volume references changed incorrectly")
	check(t, service.Ipc == reference && service.Pid == reference, "namespace references lost")
	check(t, service.Build.AdditionalContexts["source"] == reference, "build reference lost")

	manual, disabled := compose.DisabledServices["manual-stage"]
	_, dependency := manual.DependsOn["worker-stage"]
	check(t, disabled && dependency, "disabled service lost")
}
