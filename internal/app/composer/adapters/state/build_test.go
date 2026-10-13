package state_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
	"github.com/dmironenko/composer/v2/internal/app/composer/service/deployment"
)

//nolint:gocognit,maintidx // Keep build ordering and the resulting deployment markers together.
func TestBuildPrecedesDeployment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failed build preserves running revision", err: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, changes := deployments(t, "app")
			change := &changes[0]

			change.Build = &domain.BuildSpec{
				Repository: "https://example.com/app.git", Tag: "tag", Dockerfile: "FROM scratch",
			}
			if err := os.WriteFile(filepath.Join(change.Directory, "applied"), []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			controller := gomock.NewController(t)
			backend, builder := NewMockBackend(controller), NewMockBuilder(controller)
			network := backend.EXPECT().EnsureNetwork(gomock.Any(), change.Network, cfg.Installation).Return(nil)

			prepared := builder.EXPECT().Ensure(gomock.Any(), *change.Build).
				DoAndReturn(func(context.Context, domain.BuildSpec) error {
					assertMarker(t, change.Directory, "applied", "previous", nil)
					assertMarker(t, change.Directory, "attempted", "", os.ErrNotExist)

					return tc.err
				}).After(network)
			if tc.err == nil {
				backend.EXPECT().Up(gomock.Any(), change.Project, time.Second).
					DoAndReturn(func(context.Context, domain.Project, time.Duration) error {
						assertMarker(t, change.Directory, "applied", "", os.ErrNotExist)
						assertMarker(t, change.Directory, "attempted", "", nil)

						return nil
					}).After(prepared)
			}

			err := deployment.New(backend, builder, cfg.store).Apply(t.Context(), cfg.Options, changes)
			if (err != nil) != (tc.err != nil) {
				t.Fatalf("Apply error=%v, build error=%v", err, tc.err)
			}

			if tc.err != nil {
				assertMarker(t, change.Directory, "applied", "previous", nil)
				assertMarker(t, change.Directory, "attempted", "", os.ErrNotExist)

				if strings.Contains(change.Error, tc.err.Error()) {
					t.Fatalf("report exposes build error: %s", change.Error)
				}

				return
			}

			assertMarker(t, change.Directory, "applied", change.Hash, nil)
		})
	}
}
