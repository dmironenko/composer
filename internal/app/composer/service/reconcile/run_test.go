package reconcile_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/internal/app/composer/domain"
	"github.com/dmironenko/composer/internal/app/composer/service/deployment"
	"github.com/dmironenko/composer/internal/app/composer/service/reconcile"
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := []runCase{
		{name: "plan returns first error", action: "plan", passes: 1, expectedErr: io.ErrUnexpectedEOF},
		{name: "apply without wait", action: "apply", passes: 1, expectedErr: io.ErrUnexpectedEOF},
		{name: "apply retries until cancellation", action: "apply", wait: true, passes: 2, expectedErr: context.Canceled},
		{name: "run keeps polling and stops gracefully", action: "run", passes: 2},
		{
			name: "output failure stops polling", action: "run", passes: 1,
			emitErr: io.ErrClosedPipe, expectedErr: io.ErrClosedPipe,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				testRun(t, tc)
			})
		})
	}
}

type runCase struct {
	name, action         string
	wait                 bool
	passes               int
	emitErr, expectedErr error
}

func testRun(t *testing.T, tc runCase) {
	t.Helper()
	source := NewMockSource(gomock.NewController(t))
	source.EXPECT().Read(gomock.Any()).Return(domain.Snapshot{}, io.ErrUnexpectedEOF).Times(tc.passes)

	options := reconcile.Options{GitTimeout: time.Minute, Deployment: deployment.Options{PollInterval: time.Second}}
	runner := newRunner(options, nil, reconcile.Input{Source: source})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	passes := 0

	err := runner.Run(ctx, tc.action, tc.wait, func(report domain.Report, err error) error {
		passes++

		if !errors.Is(err, io.ErrUnexpectedEOF) || report.Commit != "" {
			t.Fatalf("emitted report=%+v error=%v", report, err)
		}

		if passes == tc.passes {
			cancel()
		}

		return tc.emitErr
	})
	if !errors.Is(err, tc.expectedErr) || passes != tc.passes {
		t.Fatalf("run passes=%d error=%v expected passes=%d error=%v", passes, err, tc.passes, tc.expectedErr)
	}
}
