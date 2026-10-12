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

			actual := model(t, projects[0]).Extensions["x-stage-network"]
			if actual != projects[1].Network || !slices.Contains(projects[0].Networks, projects[1].Network) {
				t.Fatalf("network reference=%v, expected=%s", actual, projects[1].Network)
			}
		})
	}
}

func TestDefaultTemplateNetworks(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"prod", "alice"} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()

			root, cfg := fixture(t, "demo", "worker")
			cfg.Environments = []string{environment}
			cfg.AllEnvironments = []string{"prod", "alice"}

			write(t, root, "apps/demo/worker/vars/stage.yaml", "{}")
			write(t, root, "apps/demo/worker/vars/base.yaml", `app:
  network_alias: api
  route_namespace: product
  networks:
    storage:
      namespace: data
      environment: prod
`+base)

			project := loadOne(t, cfg, root)
			check(t, project.Error == "", project.Error)
			assertDefaultNetworks(t, project)
		})
	}
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

func assertDefaultNetworks(t *testing.T, project domain.Project) {
	t.Helper()

	compose := model(t, project)
	service := compose.Services["worker"]

	check(t, slices.Equal(service.Networks["default"].Aliases, []string{"api-" + project.Environment}),
		"missing environment-specific alias")
	check(t, len(service.Networks) == 4 && len(project.Networks) == 3, "missing network attachments")

	for _, name := range []string{"storage", "env_prod", "env_alice"} {
		network := compose.Networks[name]
		check(t, bool(network.External) && slices.Contains(project.Networks, network.Name),
			"network was not scheduled for creation", name)
	}

	check(t, compose.Networks["env_prod"].Name != compose.Networks["env_alice"].Name,
		"environment networks must be separate")
}
