package deployment_test

import (
	"fmt"
	"io"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/internal/app/composer/domain"
	"github.com/dmironenko/composer/internal/app/composer/service/deployment"
)

const removal = "remove"

func TestJournalFailureStopsDeployment(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"invalidate", "attempt", "save", removal} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			backend, journal := NewMockBackend(controller), NewMockJournal(controller)
			project := domain.Project{Name: "app", Enabled: operation != removal, Network: "network", Hash: "revision"}
			options := deployment.Options{Installation: "test", Parallelism: 1, DeployTimeout: time.Minute}

			action := "apply"
			if operation == removal {
				action = removal
			} else {
				backend.EXPECT().EnsureNetwork(gomock.Any(), project.Network, options.Installation).Return(nil)
			}

			expectJournalFailure(backend, journal, project, operation)
			changes := []domain.Change{{Project: project, Action: action}}

			err := deployment.New(backend, nil, journal).Apply(t.Context(), options, changes)
			if err == nil || changes[0].Action != "failed" || changes[0].Error != "compose operation failed" {
				t.Fatalf("failed journal operation: changes=%+v error=%v", changes, err)
			}
		})
	}
}

func expectJournalFailure(backend *MockBackend, journal *MockJournal, project domain.Project, operation string) {
	if operation == "invalidate" {
		journal.EXPECT().Invalidate(project).Return(io.ErrUnexpectedEOF)

		return
	}

	invalidated := journal.EXPECT().Invalidate(project).Return(nil)
	if operation == removal {
		removed := backend.EXPECT().Down(gomock.Any(), project.Name).Return(nil).After(invalidated)
		journal.EXPECT().Remove(project).Return(io.ErrUnexpectedEOF).After(removed)

		return
	}

	if operation == "attempt" {
		journal.EXPECT().Attempt(project).Return(io.ErrUnexpectedEOF).After(invalidated)

		return
	}

	attempted := journal.EXPECT().Attempt(project).Return(nil).After(invalidated)
	started := backend.EXPECT().Up(gomock.Any(), project, time.Second).Return(nil).After(attempted)
	journal.EXPECT().Save(project).Return(io.ErrUnexpectedEOF).After(started)
}

func TestReferencedNetworkBarrier(t *testing.T) {
	t.Parallel()

	for _, networkErr := range []error{nil, io.ErrUnexpectedEOF} {
		t.Run(fmt.Sprint(networkErr), func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			backend, journal := NewMockBackend(controller), NewMockJournal(controller)
			project := domain.Project{Name: "gateway", Enabled: true, Network: "edge", Networks: []string{"dan"}}
			options := deployment.Options{Installation: "test", Parallelism: 1, DeployTimeout: time.Minute}
			own := backend.EXPECT().EnsureNetwork(gomock.Any(), "edge", "test").Return(nil)
			referenced := backend.EXPECT().EnsureNetwork(gomock.Any(), "dan", "test").Return(networkErr)

			if networkErr == nil {
				invalidated := journal.EXPECT().Invalidate(project).Return(nil).After(own).After(referenced)
				attempted := journal.EXPECT().Attempt(project).Return(nil).After(invalidated)
				up := backend.EXPECT().Up(gomock.Any(), project, time.Second).Return(nil).After(attempted)
				journal.EXPECT().Save(project).Return(nil).After(up)
			}

			changes := []domain.Change{{Project: project, Action: "apply"}}

			err := deployment.New(backend, nil, journal).Apply(t.Context(), options, changes)
			if (err != nil) != (networkErr != nil) {
				t.Fatalf("network barrier: %v", err)
			}
		})
	}
}
