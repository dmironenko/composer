package deployment_test

import (
	"errors"
	"io"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
	"github.com/dmironenko/composer/v2/internal/app/composer/service/deployment"
)

func TestPlan(t *testing.T) {
	t.Parallel()

	cases := []planCase{
		{name: "unchanged", enabled: true, applied: "current", expected: "noop"},
		{name: "changed", enabled: true, applied: "old", expected: "apply"},
		{name: "disabled never attempted", expected: "noop"},
		{name: "disabled partial creation", attempted: true, expected: "remove"},
		{name: "invalid preserved", renderError: "invalid", expected: "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testPlan(t, tc)
		})
	}
}

func TestPlanRemoved(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name string }{{name: "absent attempted project removed"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			project := domain.Project{Name: "removed"}
			inventory := NewMockInventory(gomock.NewController(t))
			inventory.EXPECT().List().Return([]domain.Project{project}, nil)
			inventory.EXPECT().Applied(project).Return("old", nil)
			inventory.EXPECT().Attempted(project).Return(true, nil)

			actual, err := deployment.Plan(inventory, nil)
			if err != nil {
				t.Fatal(err)
			}

			if len(actual) != 1 || actual[0].Name != project.Name || actual[0].Action != "remove" {
				t.Fatalf("removal: actual=%+v", actual)
			}
		})
	}
}

func TestPlanReadFailure(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"list", "applied", "attempted"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			testPlanReadFailure(t, operation)
		})
	}
}

type planCase struct {
	name, applied, expected, renderError string
	enabled, attempted                   bool
}

func testPlan(t *testing.T, tc planCase) {
	t.Helper()

	project := domain.Project{Name: "app", Enabled: tc.enabled, Hash: "current", Error: tc.renderError}
	inventory := NewMockInventory(gomock.NewController(t))
	inventory.EXPECT().List().Return([]domain.Project{{Name: "app"}}, nil)
	inventory.EXPECT().Applied(project).Return(tc.applied, nil)

	if !tc.enabled && tc.renderError == "" {
		inventory.EXPECT().Attempted(project).Return(tc.attempted, nil)
	}

	actual, err := deployment.Plan(inventory, []domain.Project{project})
	if err != nil {
		t.Fatal(err)
	}

	if len(actual) != 1 || actual[0].Action != tc.expected || actual[0].Error != tc.renderError {
		t.Fatalf("plan: actual=%+v expected action=%s error=%s", actual, tc.expected, tc.renderError)
	}
}

func testPlanReadFailure(t *testing.T, operation string) {
	t.Helper()

	project := domain.Project{Name: "app"}
	inventory := NewMockInventory(gomock.NewController(t))

	expectedErr := io.ErrUnexpectedEOF
	if operation == "list" {
		inventory.EXPECT().List().Return(nil, expectedErr)
	} else {
		inventory.EXPECT().List().Return(nil, nil)

		if operation == "applied" {
			inventory.EXPECT().Applied(project).Return("", expectedErr)
		} else {
			inventory.EXPECT().Applied(project).Return("", nil)
			inventory.EXPECT().Attempted(project).Return(false, expectedErr)
		}
	}

	_, err := deployment.Plan(inventory, []domain.Project{project})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("read failure: actual=%v expected=%v", err, expectedErr)
	}
}
