package domain_test

import (
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

func TestValidRef(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, ref string
		expected  bool
	}{
		{name: "full sha", ref: "0123456789abcdef0123456789abcdef01234567", expected: true},
		{name: "tag", ref: "v1.0.0", expected: true},
		{name: "branch", ref: "feature/api", expected: true},
		{name: "short sha", ref: "abcdef1", expected: true},
		{name: "empty"},
		{name: "whitespace", ref: " \n\t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := domain.BuildSpec{Ref: tc.ref}
			if actual := spec.ValidRef(); actual != tc.expected {
				t.Fatalf("valid ref %q: actual=%v expected=%v", tc.ref, actual, tc.expected)
			}
		})
	}
}
