package usecasetest

import (
	"context"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// MustPath は、s をドライブ側のパスにする。解釈できなければテストを止める。
func MustPath(t *testing.T, s string) domain.RemotePath {
	t.Helper()
	p, err := domain.ParseRemotePath(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// NewRemote は、d を開く Remote を、偽の状態とロックで作る。
func NewRemote(d usecase.Drive) usecase.Remote {
	return usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return d, nil },
		State:  &State{},
		Locker: &Locker{},
	}
}
