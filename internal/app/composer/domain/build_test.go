package domain_test

import (
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

func TestValidCommit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, commit string
		expected     bool
	}{
		{name: "full lowercase SHA", commit: "0123456789abcdef0123456789abcdef01234567", expected: true},
		{name: "empty"},
		{name: "branch", commit: "master"},
		{name: "short SHA", commit: "abcdef1"},
		{name: "too long", commit: "0123456789abcdef0123456789abcdef012345678"},
		{name: "uppercase", commit: "0123456789ABCDEF0123456789ABCDEF01234567"},
		{name: "non hexadecimal", commit: "0123456789abcdef0123456789abcdef0123456z"},
		{name: "trailing newline", commit: "0123456789abcdef0123456789abcdef01234567\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := domain.BuildSpec{Commit: tc.commit}
			if actual := spec.ValidCommit(); actual != tc.expected {
				t.Fatalf("valid commit %q: actual=%v expected=%v", tc.commit, actual, tc.expected)
			}
		})
	}
}
