package gitops_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/dmironenko/composer/internal/app/composer/domain"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops"
	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

const secret = "private-render-value-123"

const base = `message: base
compose:
  services:
    worker:
      image: busybox:stable
      command: [sh, -c, 'cat /etc/message']
      volumes: ['./configs/message:/etc/message:ro']
      deploy:
        replicas: 1
`

//nolint:maintidx // Keep environment selection cases together.
func TestEnvironmentValues(t *testing.T) {
	t.Parallel()

	cases := []environmentCase{
		{name: "prod uses base", environment: "prod", enabled: true, expected: "base"},
		{
			name:        "legacy prod override ignored",
			environment: "prod",
			present:     true,
			override:    "message: prod",
			enabled:     true,
			expected:    "base",
		},
		{name: "malformed prod ignored", environment: "prod", present: true, override: "[", enabled: true, expected: "base"},
		{name: "missing stage", environment: "stage"},
		{name: "personal stage disabled", environment: "dan"},
		{
			name: "personal stage override", environment: "dan", present: true,
			override: "message: personal", enabled: true, expected: "personal",
		},
		{name: "empty stage enables base", environment: "stage", present: true, enabled: true, expected: "base"},
		{
			name:        "stage override excludes prod",
			environment: "stage",
			present:     true,
			override:    "message: stage",
			enabled:     true,
			expected:    "stage",
		},
		{name: "disabled stage", environment: "stage", present: true, override: "enabled: false"},
		{name: "disabled prod", environment: "prod", base: "enabled: false\n" + base},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testEnvironmentValues(t, tc)
		})
	}
}

func TestArbitraryProjects(t *testing.T) {
	t.Parallel()

	cases := []struct{ namespace, app, service string }{
		{namespace: "edge", app: "gateway", service: "haproxy"},
		{namespace: "accounting", app: "reports", service: "nightly-job"},
	}
	for _, tc := range cases {
		t.Run(tc.namespace+"/"+tc.app, func(t *testing.T) {
			t.Parallel()
			root, cfg := fixture(t, tc.namespace, tc.app)
			cfg.Environments = []string{"prod", "stage"}
			folder := "apps/" + tc.namespace + "/" + tc.app
			write(t, root, folder+"/vars/base.yaml", strings.ReplaceAll(base, "worker:", tc.service+":"))
			write(t, root, folder+"/vars/stage.yaml", "")

			projects, err := gitops.New(cfg).Load(t.Context(), root)
			check(t, err == nil && len(projects) == 2, fmt.Sprintf("projects=%v error=%v", projects, err))

			for _, project := range projects {
				check(t, project.Error == "" && project.Enabled && model(t, project).Services[tc.service].Image == "busybox:stable",
					fmt.Sprintf("project=%+v", project))
			}

			check(t, projects[0].Name != projects[1].Name && projects[0].Network != projects[1].Network,
				"environments share project or namespace network")
		})
	}
}

func TestChanges(t *testing.T) {
	t.Parallel()

	cases := []changeCase{
		{
			name:          "config only",
			updated:       strings.Replace(base, "message: base", "message: changed", 1),
			expected:      "changed",
			configChanged: true,
			replicas:      1,
		},
		{name: "scale only", updated: strings.Replace(base, "replicas: 1", "replicas: 3", 1), expected: "base", replicas: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testChanges(t, tc)
		})
	}
}

func TestTemplate(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, template, expected string }{
		{name: "default", expected: "always"},
		{
			name:     "repository override",
			template: strings.Replace(gitops.DefaultTemplate(), `"restart" "always"`, `"restart" "unless-stopped"`, 1),
			expected: "unless-stopped",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, cfg := fixture(t, "jobs", "processor")
			if tc.template != "" {
				write(t, root, "templates/compose.yaml.tmpl", tc.template)
			}

			project := loadOne(t, cfg, root)
			check(t, project.Error == "", project.Error)

			if actual := model(t, project).Services["worker"].Restart; actual != tc.expected {
				t.Fatalf("restart=%q expected=%q", actual, tc.expected)
			}
		})
	}
}

func TestOutputDoesNotExposeValues(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, document, template string }{
		{name: "valid", document: strings.Replace(base, "message: base", "message: "+secret, 1)},
		{name: "malformed yaml", document: "message: [" + secret},
		{
			name:     "invalid compose",
			document: "message: value\ncompose: {services: {worker: {image: {private: " + secret + "}}}}",
		},
		{name: "template failure", document: "message: " + secret, template: "{{ indent .Vars.message .Vars.message }}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, cfg := fixture(t, "jobs", "processor")
			write(t, root, "apps/jobs/processor/vars/base.yaml", tc.document)

			if tc.template != "" {
				write(t, root, "templates/compose.yaml.tmpl", tc.template)
			}

			project := loadOne(t, cfg, root)
			check(t, tc.name != "valid" || project.Error == "", project.Error)

			check(t, tc.name == "valid" || (project.Error != "" && project.Name != ""),
				fmt.Sprintf("invalid directory lost its identity or error: %+v", project))

			assertPrivate(t, project, secret)
		})
	}
}

func TestInterpolation(t *testing.T) {
	for _, name := range []string{"literal dollar and isolated host environment"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("COMPOSER_RENDER_TEST", "host-value")
			root, cfg := fixture(t, "jobs", "processor")
			document := strings.Replace(base, "cat /etc/message",
				"echo $$COMPOSER_RENDER_TEST ${COMPOSER_RENDER_TEST:-fallback}", 1)
			write(t, root, "apps/jobs/processor/vars/base.yaml", document)

			project := loadOne(t, cfg, root)
			check(t, project.Error == "", project.Error)

			expected := "echo $$COMPOSER_RENDER_TEST ${COMPOSER_RENDER_TEST:-fallback}"
			if actual := model(t, project).Services["worker"].Command[2]; actual != expected {
				t.Fatalf("command=%q expected=%q", actual, expected)
			}
		})
	}
}

func fixture(t *testing.T, namespace, app string) (string, gitops.Options) {
	t.Helper()
	root := t.TempDir()
	folder := fmt.Sprintf("apps/%s/%s", namespace, app)
	write(t, root, folder+"/vars/base.yaml", base)
	write(t, root, folder+"/configs/message.tmpl", "{{ .Vars.message }}")

	return root, gitops.Options{Installation: "test", StateDir: t.TempDir(), Environments: []string{"prod"}}
}

func loadOne(t *testing.T, cfg gitops.Options, root string) domain.Project {
	t.Helper()

	projects, err := gitops.New(cfg).Load(t.Context(), root)
	check(t, err == nil && len(projects) == 1, fmt.Sprintf("projects=%v error=%v", projects, err))

	return projects[0]
}

func write(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, expected string) {
	t.Helper()

	actual, err := os.ReadFile(path)
	check(t, err == nil && string(actual) == expected,
		fmt.Sprintf("file=%q actual=%q expected=%q error=%v", path, actual, expected, err))
}

func TestEncryptedAppConfig(t *testing.T) {
	t.Parallel()

	cases := []encryptedCase{
		{name: "encrypted base", environment: "prod", expected: "base-test-secret"},
		{name: "encrypted stage override", environment: "stage", override: "stage.yaml", expected: "stage-test-secret"},
		{
			name:        "literal dollars",
			environment: "stage",
			override:    "dollars.yaml",
			expected:    "literal-$PRIVATE_MARKER-${UNSET:-fallback}-$$",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testEncryptedAppConfig(t, tc)
		})
	}
}

func TestSecretChanges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, source string
		expected     bool
	}{
		{name: "secret value changed", source: "base-updated.yaml", expected: true},
		{name: "reencryption only", source: "base-reencrypted.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, cfg := encryptedFixture(t, "prod")

			before := loadOne(t, cfg, root)

			copyEncrypted(t, root, tc.source, "base.yaml")

			after := loadOne(t, cfg, root)
			check(t, before.Error == "" && after.Error == "", before.Error, after.Error)

			check(t, (before.Hash != after.Hash) == tc.expected, "hash does not reflect plaintext change")

			previous, actual := model(t, before).Services["api-http"], model(t, after).Services["api-http"]
			check(t, (previous.Volumes[0].Source != actual.Volumes[0].Source) == tc.expected,
				"config path does not reflect plaintext change")
		})
	}
}

func check(t *testing.T, condition bool, message ...any) {
	t.Helper()

	if !condition {
		t.Fatal(message...)
	}
}

func encryptedFixture(t *testing.T, environment string) (string, gitops.Options) {
	t.Helper()
	root, cfg := fixture(t, "demo", "service")
	cfg.Environments = []string{environment}
	key, err := filepath.Abs("internal/vars/testdata/age-key.txt")
	check(t, err == nil, err)

	cfg.AgeKeyFile = key
	folder := "apps/demo/service/"
	err = os.RemoveAll(filepath.Join(root, folder, "configs"))
	check(t, err == nil, err)
	write(t, root, folder+"configs/config.yaml.tmpl", "{{ merge .Vars.app .Secrets | toYAML }}")
	copyEncrypted(t, root, "base.yaml", "base.yaml")

	return root, cfg
}

func copyEncrypted(t *testing.T, root, source, target string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("internal/vars/testdata", source))
	check(t, err == nil, err)
	write(t, root, "apps/demo/service/vars/"+target, string(data))
}

func assertPrivate(t *testing.T, project domain.Project, secret string) {
	t.Helper()

	check(t, !strings.Contains(project.Error, secret), "render error exposes plaintext values")
}

func assertMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	check(t, err == nil, err)
	check(t, info.Mode().Perm() == expected, "unexpected permissions", path)
}

type environmentCase struct {
	name, environment, base, override, expected string
	present, enabled                            bool
}

func testEnvironmentValues(t *testing.T, tc environmentCase) {
	t.Helper()
	root, cfg := fixture(t, "jobs", "processor")

	cfg.Environments = []string{tc.environment}
	if tc.base != "" {
		write(t, root, "apps/jobs/processor/vars/base.yaml", tc.base)
	}

	if tc.environment == "stage" {
		write(t, root, "apps/jobs/processor/vars/prod.yaml", "message: prod-only")
	}

	if tc.present {
		filename := "stage.yaml"
		if tc.environment == "prod" {
			filename = "prod.yaml"
		}

		write(t, root, "apps/jobs/processor/vars/"+filename, tc.override)
	}

	project := loadOne(t, cfg, root)
	check(t, project.Error == "", project.Error)
	check(t, project.Enabled == tc.enabled, "unexpected enabled status")

	if !tc.enabled {
		check(t, len(project.Manifest.Content) == 0, "disabled project has a Compose model")

		return
	}

	assertFile(t, model(t, project).Services["worker"].Volumes[0].Source, tc.expected)
}

type changeCase struct {
	name, updated, expected string
	configChanged           bool
	replicas                int
}

func testChanges(t *testing.T, tc changeCase) {
	t.Helper()
	root, cfg := fixture(t, "jobs", "processor")
	before := loadOne(t, cfg, root)
	write(t, root, "apps/jobs/processor/vars/base.yaml", tc.updated)

	after := loadOne(t, cfg, root)
	check(t, before.Error == "" && after.Error == "" && before.Hash != after.Hash,
		fmt.Sprintf("before=%+v after=%+v", before, after))

	previous, actual := model(t, before).Services["worker"], model(t, after).Services["worker"]
	check(t, actual.GetScale() == tc.replicas, fmt.Sprintf("replicas=%d expected=%d", actual.GetScale(), tc.replicas))

	check(t, (previous.Labels["io.composer.config"] != actual.Labels["io.composer.config"]) == tc.configChanged,
		"config label did not follow config changes")
	check(t, (previous.Volumes[0].Source != actual.Volumes[0].Source) == tc.configChanged,
		"mount identity did not follow config changes")

	assertFile(t, previous.Volumes[0].Source, "base")
	assertFile(t, actual.Volumes[0].Source, tc.expected)

	if again := loadOne(t, cfg, root); again.Hash != after.Hash {
		t.Fatal("unchanged project hash is unstable")
	}
}

type encryptedCase struct{ name, environment, override, expected string }

func testEncryptedAppConfig(t *testing.T, tc encryptedCase) {
	t.Helper()
	root, cfg := encryptedFixture(t, tc.environment)
	write(t, root, "templates/compose.yaml.tmpl", strings.Replace(gitops.DefaultTemplate(),
		`{{ toYAML (merge $defaults $service) | indent 4 }}`,
		`{{ toYAML (merge (dict "restart" "always") $service) | indent 4 }}
    environment:
      PASSWORD: {{ quote $.Secrets.database.password }}`, 1))

	if tc.override != "" {
		copyEncrypted(t, root, tc.override, tc.environment+".yaml")
	}

	project := loadOne(t, cfg, root)
	check(t, project.Error == "", project.Error)

	assertEncryptedConfig(t, project, tc.expected)

	cfg.AgeKeyFile = ""

	failed := loadOne(t, cfg, root)
	check(t, failed.Error != "" && failed.Name == project.Name, "decryption error lost project identity")
}

func assertEncryptedConfig(t *testing.T, project domain.Project, expected string) {
	t.Helper()

	service := model(t, project).Services["api-http"]
	check(t, service.Environment["PASSWORD"] != nil && *service.Environment["PASSWORD"] == expected,
		"Compose changed the secret")

	data, err := os.ReadFile(service.Volumes[0].Source)
	check(t, err == nil, err)

	actual, err := vars.Decode(data)
	check(t, err == nil, err)

	database, ok := actual["database"].(map[string]any)
	check(t, ok, "database must be a map")
	check(t, database["password"] == expected && actual["retries"] == 3,
		"encrypted app subtree was not merged/rendered")

	assertPrivate(t, project, expected)
	assertMode(t, service.Volumes[0].Source, 0o444)
	assertMode(t, project.Directory, 0o700)
}

func model(t *testing.T, project domain.Project) *types.Project {
	t.Helper()
	model, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
		WorkingDir: project.Manifest.WorkingDir, Environment: map[string]string{},
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: project.Manifest.Content}},
	}, func(options *loader.Options) {
		options.SetProjectName(project.Name, true)
		options.SkipInterpolation = true
	})
	check(t, err == nil, err)

	for name, service := range model.Services {
		service.Labels = service.Labels.Add("io.composer.config", project.Manifest.ConfigHash)
		model.Services[name] = service
	}

	return model
}
