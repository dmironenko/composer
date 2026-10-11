package state_test

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination=ports_mock_test.go -package=state_test github.com/dmironenko/composer/internal/app/composer/service/deployment Backend,Builder
