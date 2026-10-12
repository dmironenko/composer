package gitops

import (
	"fmt"
	"maps"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

func renderEnvironmentServices(project *types.Project, output []byte, environment string) ([]byte, error) {
	if environment == "prod" {
		return output, nil
	}

	suffixProject(project, environment)

	return suffixedManifest(project)
}

func suffixProject(project *types.Project, suffix string) {
	services := maps.Clone(project.Services)
	maps.Copy(services, project.DisabledServices)

	names := make(map[string]string, len(services))
	for name := range services {
		names[name] = name + "-" + suffix
	}

	project.Services = suffixServices(project.Services, names)

	project.DisabledServices = suffixServices(project.DisabledServices, names)
	for name, job := range project.Jobs {
		job.DependsOn = suffixDependencies(job.DependsOn, names)
		project.Jobs[name] = job
	}
}

func suffixServices(services types.Services, names map[string]string) types.Services {
	result := make(types.Services, len(services))
	for name, service := range services {
		service.Name = names[name]
		service.DependsOn = suffixDependencies(service.DependsOn, names)
		service.Links = suffixReferences(service.Links, names)
		service.VolumesFrom = suffixReferences(service.VolumesFrom, names)
		service.NetworkMode = suffixServiceReference(service.NetworkMode, names)
		service.Ipc = suffixServiceReference(service.Ipc, names)

		service.Pid = suffixServiceReference(service.Pid, names)
		if service.Build != nil {
			for key, value := range service.Build.AdditionalContexts {
				service.Build.AdditionalContexts[key] = suffixServiceReference(value, names)
			}
		}

		result[service.Name] = service
	}

	return result
}

func suffixedManifest(project *types.Project) ([]byte, error) {
	portable := *project
	portable.Services = maps.Clone(project.Services)
	maps.Copy(portable.Services, project.DisabledServices)

	output, err := portable.MarshalJSON(types.WithSecretContent)
	if err != nil {
		return nil, fmt.Errorf("encode suffixed compose: %w", err)
	}

	return output, nil
}

func suffixDependencies(dependencies types.DependsOnConfig, names map[string]string) types.DependsOnConfig {
	result := make(types.DependsOnConfig, len(dependencies))
	for name, dependency := range dependencies {
		if renamed, ok := names[name]; ok {
			name = renamed
		}

		result[name] = dependency
	}

	return result
}

func suffixReferences(references []string, names map[string]string) []string {
	for index, reference := range references {
		name, rest, found := strings.Cut(reference, ":")
		if renamed, ok := names[name]; ok {
			references[index] = renamed
			if found {
				references[index] += ":" + rest
			}
		}
	}

	return references
}

func suffixServiceReference(reference string, names map[string]string) string {
	name, ok := strings.CutPrefix(reference, "service:")
	if renamed, exists := names[name]; ok && exists {
		return "service:" + renamed
	}

	return reference
}
