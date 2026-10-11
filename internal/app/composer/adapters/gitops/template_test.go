package gitops_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops"
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
