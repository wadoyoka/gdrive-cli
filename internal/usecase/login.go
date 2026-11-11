package usecase

import (
	"context"
	"io"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// Login は、Google にログインしてルートフォルダを用意する。
type Login struct {
	Auth   Authenticator
	Remote Remote
}

// LoginInput は、login の入力。
type LoginInput struct {
	ClientSecretPath string
	NoBrowser        bool
	Prompt           io.Writer
	Input            io.Reader
}

// LoginResult は、login の結果。
type LoginResult struct {
	Email          string   `json:"email"`
	RootFolderID   string   `json:"rootFolderId"`
	RootFolderLink string   `json:"rootFolderLink"`
	Warnings       []string `json:"warnings,omitempty"`
}

// Run は、client_secret の取り込み、ログイン、ルートフォルダの解決を順に行う。
func (u Login) Run(ctx context.Context, in LoginInput) (LoginResult, error) {
	switch {
	case in.ClientSecretPath != "":
		if err := u.Auth.ImportClientSecret(in.ClientSecretPath); err != nil {
			return LoginResult{}, err
		}
	case !u.Auth.HasClientSecret():
		return LoginResult{}, domain.Errorf(domain.KindSetup, "初回は --client-secret <JSON のパス> を指定してください")
	}
	if err := u.Auth.Login(ctx, LoginOptions{NoBrowser: in.NoBrowser, Prompt: in.Prompt, Input: in.Input}); err != nil {
		return LoginResult{}, err
	}
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	email, err := s.Drive.AccountEmail(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	root, err := s.Resolver.Root(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Email: email, RootFolderID: root.ID, RootFolderLink: root.WebViewLink, Warnings: s.Resolver.Warnings()}, nil
}
