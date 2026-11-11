package usecase

import "context"

// RootFolderName は、ドライブ上のルートフォルダの名前。
const RootFolderName = "gdrive-cli"

// Remote は、ドライブ側に接続するための部品。
type Remote struct {
	Open   DriveOpener
	State  StateStore
	Locker Locker
}

// Session は、接続した Drive とパスの解決。
type Session struct {
	Drive    Drive
	Resolver *Resolver
}

// Connect は、Drive を開いて Session を返す。
func (r Remote) Connect(ctx context.Context) (*Session, error) {
	d, err := r.Open(ctx)
	if err != nil {
		return nil, err
	}
	return &Session{Drive: d, Resolver: NewResolver(d, r.State, r.Locker)}, nil
}
