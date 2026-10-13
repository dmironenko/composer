package domain_test

import (
	"testing"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

func TestValidTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, tag string
		expected  bool
	}{
		{name: "full sha", tag: "0123456789abcdef0123456789abcdef01234567", expected: true},
		{name: "tag", tag: "v1.0.0", expected: true},
		{name: "branch", tag: "feature/api", expected: true},
		{name: "short sha", tag: "abcdef1", expected: true},
		{name: "empty"},
		{name: "whitespace", tag: " \n\t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := domain.BuildSpec{Tag: tc.tag}
			if actual := spec.ValidTag(); actual != tc.expected {
				t.Fatalf("valid tag %q: actual=%v expected=%v", tc.tag, actual, tc.expected)
			}
		})
	}
}
