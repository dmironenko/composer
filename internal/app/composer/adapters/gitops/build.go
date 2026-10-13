package gitops

import (
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

func prepareBuild(files fs.FS, folder string, data *templateData, project *domain.Project) error {
	value, exists := data.Vars["build"]
	if !exists {
		return nil
	}

	values, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: build must be a map", domain.ErrInvalid)
	}

	repository, _ := values["repository"].(string)

	tag, _ := values["tag"].(string)
	if !remoteRepository(repository) || !(domain.BuildSpec{Tag: tag}).ValidTag() {
		return fmt.Errorf("%w: build requires an HTTPS/SSH repository and nonempty tag", domain.ErrInvalid)
	}

	source, err := fs.ReadFile(files, folder+"/Dockerfile.tmpl")
	if err != nil {
		return fmt.Errorf("read Dockerfile template: %w", err)
	}

	output, err := execute("Dockerfile.tmpl", string(source), *data)
	if err != nil {
		return err
	}

	project.Build = &domain.BuildSpec{Repository: repository, Tag: tag, Dockerfile: string(output)}
	data.Runtime.Image = project.Build.Image()

	return nil
}

func remoteRepository(repository string) bool {
	// Accept the usual git@host:path spelling as well as explicit SSH URLs.
	if strings.HasPrefix(repository, "git@") && strings.Contains(repository, ":") {
		repository = "ssh://" + strings.Replace(repository, ":", "/", 1)
	}

	address, err := url.Parse(repository)
	if err != nil || address.Host == "" || address.Path == "" {
		return false
	}

	return address.Scheme == "https" || address.Scheme == "ssh"
}
