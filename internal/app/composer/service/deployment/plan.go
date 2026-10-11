package deployment

import (
	"fmt"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// Plan compares rendered hashes with successful applications, never container state.
func Plan(state Inventory, projects []domain.Project) ([]domain.Change, error) {
	var err error

	projects, err = includeRemoved(state, projects)
	if err != nil {
		return nil, err
	}

	if len(projects) == 0 {
		return nil, nil
	}

	changes := make([]domain.Change, 0, len(projects))
	for _, project := range projects {
		change, err := plan(state, project)
		if err != nil {
			return nil, err
		}

		changes = append(changes, change)
	}

	return changes, nil
}

func includeRemoved(state Inventory, projects []domain.Project) ([]domain.Project, error) {
	previous, err := state.List()
	if err != nil {
		return nil, fmt.Errorf("list previous deployments: %w", err)
	}

	seen := map[string]bool{}
	for _, project := range projects {
		seen[project.Name] = true
	}

	for _, project := range previous {
		if !seen[project.Name] {
			projects = append(projects, project)
		}
	}

	return projects, nil
}

func plan(state Inventory, project domain.Project) (domain.Change, error) {
	change := domain.Change{Project: project, Action: "noop"}

	applied, err := state.Applied(project)
	if err != nil {
		return change, fmt.Errorf("read deployment state: %w", err)
	}

	switch {
	case project.Error != "":
		change.Action = "failed"
	case !project.Enabled:
		attempted, err := state.Attempted(project)
		if err != nil {
			return change, fmt.Errorf("read deployment state: %w", err)
		}

		if attempted {
			change.Action = "remove"
		}
	case applied != project.Hash:
		change.Action = "apply"
	}

	return change, nil
}
