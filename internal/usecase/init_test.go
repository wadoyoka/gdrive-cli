package usecase

import (
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

type fakeDir struct {
	path    string
	created bool
}

func (f fakeDir) Path() string          { return f.path }
func (f fakeDir) Ensure() (bool, error) { return f.created, nil }

type fakePolicy struct {
	path    string
	created bool
}

func (f fakePolicy) Path() string                  { return f.path }
func (f fakePolicy) CreateIfAbsent() (bool, error) { return f.created, nil }
func (f fakePolicy) Load() (domain.Policy, error)  { return domain.Policy{}, nil }

func TestInit(t *testing.T) {
	t.Run("偽物の返した値が結果になる", func(t *testing.T) {
		// Arrange
		u := Init{
			Dir:    fakeDir{path: "/cfg", created: true},
			Policy: fakePolicy{path: "/cfg/policy.toml", created: false},
		}

		// Act
		got, err := u.Run()

		// Assert
		want := InitResult{ConfigDir: "/cfg", DirCreated: true, PolicyPath: "/cfg/policy.toml", PolicyCreated: false}
		if err != nil || got != want {
			t.Errorf("got %+v, %v; want %+v", got, err, want)
		}
	})
}
