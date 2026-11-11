package usecase

import (
	"context"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// Status は、ログインの状態を調べる。
type Status struct {
	Dir    ConfigDir
	Auth   Authenticator
	Remote Remote
}

// StatusResult は、status の結果。
type StatusResult struct {
	ConfigDir      string `json:"configDir"`
	LoggedIn       bool   `json:"loggedIn"`
	Email          string `json:"email,omitempty"`
	RootFolderID   string `json:"rootFolderId,omitempty"`
	RootFolderLink string `json:"rootFolderLink,omitempty"`
}

// Run は、状態を返す。ログインしていなければ、LoggedIn=false の結果と KindSetup を両方返す。
func (u Status) Run(ctx context.Context) (StatusResult, error) {
	res := StatusResult{ConfigDir: u.Dir.Path()}
	if !u.Auth.HasToken() {
		return res, domain.Errorf(domain.KindSetup, "ログインしていません。gdrive-cli login を実行してください")
	}
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return res, err
	}
	email, err := s.Drive.AccountEmail(ctx)
	if err != nil {
		return res, err
	}
	root, err := s.Resolver.Root(ctx)
	if err != nil {
		return res, err
	}
	res.LoggedIn = true
	res.Email = email
	res.RootFolderID = root.ID
	res.RootFolderLink = root.WebViewLink
	return res, nil
}
