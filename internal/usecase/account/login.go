// Package account は、ログイン・ログアウト・状態の確認（login・logout・status）の手順を持つ。
package account

import (
	"context"
	"io"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// Login は、Google にログインしてルートフォルダを用意する。
type Login struct {
	Auth   usecase.Authenticator
	Remote usecase.Remote
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
	if in.ClientSecretPath != "" {
		if err := u.Auth.ImportClientSecret(in.ClientSecretPath); err != nil {
			return LoginResult{}, err
		}
	} else {
		insp, err := u.Auth.Inspect()
		if err != nil {
			return LoginResult{}, err
		}
		if !insp.ClientSecret {
			msg := insp.Problem
			if msg == "" {
				msg = "初回は --client-secret <JSON のパス> を指定してください"
			}
			return LoginResult{}, domain.Errorf(domain.KindSetup, "%s", msg)
		}
	}
	if err := u.Auth.Login(ctx, usecase.LoginOptions{NoBrowser: in.NoBrowser, Prompt: in.Prompt, Input: in.Input}); err != nil {
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
