package usecase

import "context"

// Logout は、保存したトークンを消す。
type Logout struct{ Auth Authenticator }

// Run は、ログアウトする。
func (u Logout) Run(ctx context.Context) error { return u.Auth.Logout(ctx) }
