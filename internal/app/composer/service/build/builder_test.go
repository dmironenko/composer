package build_test

import (
	"cmp"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
	"github.com/dmironenko/composer/v2/internal/app/composer/service/build"
)

func TestEnsure(t *testing.T) {
	t.Parallel()

	cases := []ensureCase{
		{name: "existing image skips build", exists: true},
		{name: "missing image is built"},
		{name: "inspect failure", inspectErr: io.ErrUnexpectedEOF},
		{name: "build failure", buildErr: io.ErrUnexpectedEOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testEnsure(t, tc)
		})
	}
}

func TestFailedBuildCanRetry(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	backend := NewMockBackend(controller)
	spec := fixture()

	builder := build.New(backend)

	for _, expectedErr := range []error{io.ErrUnexpectedEOF, nil} {
		backend.EXPECT().HasImage(gomock.Any(), spec.Image()).Return(false, nil)
		backend.EXPECT().Build(gomock.Any(), spec).Return(expectedErr)

		if err := builder.Ensure(t.Context(), spec); !errors.Is(err, expectedErr) {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}
	}
}

func TestDockerRemainsImageAuthority(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	backend := NewMockBackend(controller)
	spec := fixture()
	gomock.InOrder(
		backend.EXPECT().HasImage(gomock.Any(), spec.Image()).Return(true, nil),
		backend.EXPECT().HasImage(gomock.Any(), spec.Image()).Return(false, nil),
		backend.EXPECT().Build(gomock.Any(), spec).Return(nil),
	)

	builder := build.New(backend)
	for range 2 {
		checkError(t, builder.Ensure(t.Context(), spec), nil)
	}
}

func TestConcurrentBuilds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		different bool
	}{
		{name: "same image shares one build"},
		{name: "different images build independently", different: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) { testConcurrentBuilds(t, tc.different) })
		})
	}
}

func TestWaiterCancellation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		cancelOwner bool
	}{
		{name: "waiter cancellation waits for shared build"},
		{name: "owner cancellation stops shared build", cancelOwner: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) { testWaiterCancellation(t, tc.cancelOwner) })
		})
	}
}

func TestCancelledRequestSkipsDependencies(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	builder := build.New(NewMockBackend(controller))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := builder.Ensure(ctx, fixture()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

type ensureCase struct {
	name       string
	exists     bool
	inspectErr error
	buildErr   error
}

func testEnsure(t *testing.T, tc ensureCase) {
	t.Helper()

	controller := gomock.NewController(t)
	backend := NewMockBackend(controller)
	spec := fixture()
	backend.EXPECT().HasImage(t.Context(), spec.Image()).Return(tc.exists, tc.inspectErr)

	if !tc.exists && tc.inspectErr == nil {
		backend.EXPECT().Build(t.Context(), spec).Return(tc.buildErr)
	}

	expectedErr := cmp.Or(tc.inspectErr, tc.buildErr)
	checkError(t, build.New(backend).Ensure(t.Context(), spec), expectedErr)
}

func testConcurrentBuilds(t *testing.T, different bool) {
	t.Helper()

	spec := fixture()
	second := spec
	count := 1

	if different {
		second.Tag = "v2.0.0"
		count++
	}

	builder, started, release := blockedBuilder(t, count)
	results := make(chan error, 2)

	go func() { results <- builder.Ensure(t.Context(), spec) }()

	<-started

	go func() { results <- builder.Ensure(t.Context(), second) }()

	if different {
		<-started
	}

	synctest.Wait()
	close(release)

	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func testWaiterCancellation(t *testing.T, cancelOwner bool) {
	t.Helper()

	builder, started, release := blockedBuilder(t, 1)

	ownerCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()

	waiterCtx, cancelWaiter := context.WithCancel(t.Context())
	defer cancelWaiter()

	ownerResult := make(chan error, 1)
	waiterResult := make(chan error, 1)

	go func() { ownerResult <- builder.Ensure(ownerCtx, fixture()) }()

	<-started

	go func() { waiterResult <- builder.Ensure(waiterCtx, fixture()) }()

	synctest.Wait()

	var expectedOwnerErr error

	if cancelOwner {
		cancelFirst()

		expectedOwnerErr = context.Canceled
	} else {
		cancelWaiter()
	}

	checkPending(t, ownerResult, waiterResult)
	close(release)

	checkError(t, <-waiterResult, context.Canceled)
	checkError(t, <-ownerResult, expectedOwnerErr)
}

func fixture() domain.BuildSpec {
	return domain.BuildSpec{
		Repository: "https://example.test/repository.git",
		Tag:        "v1.0.0", Dockerfile: "FROM scratch\n",
	}
}

func blockedBuilder(t *testing.T, count int) (*build.Builder, <-chan struct{}, chan<- struct{}) {
	t.Helper()

	controller := gomock.NewController(t)
	backend := NewMockBackend(controller)
	started := make(chan struct{}, count)
	release := make(chan struct{})

	backend.EXPECT().HasImage(gomock.Any(), gomock.Any()).Return(false, nil).Times(count)
	backend.EXPECT().Build(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ domain.BuildSpec) error {
			started <- struct{}{}

			// Finishing backend work remains owned by Ensure even after cancellation.
			<-release

			return ctx.Err()
		},
	).Times(count)

	return build.New(backend), started, release
}

func checkError(t *testing.T, err, expectedErr error) {
	t.Helper()

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func checkPending(t *testing.T, results ...<-chan error) {
	t.Helper()
	synctest.Wait()

	for _, result := range results {
		select {
		case err := <-result:
			t.Fatalf("Ensure returned before backend finished: %v", err)
		default:
		}
	}
}

func TestInvalidTagSkipsDependencies(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, tag string }{
		{name: "whitespace", tag: " \n\t"},
		{name: "missing revision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			builder := build.New(NewMockBackend(controller))
			spec := fixture()
			spec.Tag = tc.tag
			checkError(t, builder.Ensure(t.Context(), spec), domain.ErrInvalid)
		})
	}
}
