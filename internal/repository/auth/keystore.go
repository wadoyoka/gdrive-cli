package auth

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/repository/atomicfile"
)

const keyringService = "gdrive-cli"

const keyLength = 32

// keyFileName は、GDRIVE_CLI_KEYRING=file のときに鍵を置く、設定ディレクトリの中のファイルの名前。
const keyFileName = "key"

// KeyStore は、暗号化の鍵の置き場。
type KeyStore interface {
	// Kind は "os" か "file"。不正な GDRIVE_CLI_KEYRING のときはその値。
	Kind() string
	// Load は、確かにないときだけ (nil, false, nil) を返す。
	// 使えない・読めない・形が違うときは KindSetup、不正な GDRIVE_CLI_KEYRING は KindUsage。
	Load() (key []byte, found bool, err error)
	Save(key []byte) error
}

// keyringBackend は、OS のキーチェーンとのやりとり。テストでは偽物を使う。
type keyringBackend interface {
	Get(service, user string) (string, error) // ないときは errKeyringNotFound
	Set(service, user, secret string) error
}

var errKeyringNotFound = errors.New("キーチェーンに項目がありません")

// NewKeyStore は、mode（GDRIVE_CLI_KEYRING の値）に合う KeyStore を返す。
func NewKeyStore(dir, mode string) KeyStore {
	switch mode {
	case "", "os":
		return newOSKeyStore(dir, goKeyringBackend{})
	case "file":
		return newFileKeyStore(dir)
	default:
		return invalidKeyStore{mode: mode}
	}
}

// goKeyringBackend は go-keyring を keyringBackend に合わせる。
type goKeyringBackend struct{}

func (goKeyringBackend) Get(service, user string) (string, error) {
	v, err := keyring.Get(service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errKeyringNotFound
	}
	return v, err
}

func (goKeyringBackend) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}

func decodeKey(s string) ([]byte, bool) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(key) != keyLength {
		return nil, false
	}
	return key, true
}

// invalidKeyStore は、不正な GDRIVE_CLI_KEYRING の値を持つ。
type invalidKeyStore struct{ mode string }

func (s invalidKeyStore) Kind() string { return s.mode }

func (s invalidKeyStore) err() error {
	return domain.Errorf(domain.KindUsage, "GDRIVE_CLI_KEYRING の値が正しくありません（os か file を指定してください）: %s", s.mode)
}

func (s invalidKeyStore) Load() ([]byte, bool, error) { return nil, false, s.err() }
func (s invalidKeyStore) Save([]byte) error           { return s.err() }

type osKeyStore struct {
	dir string
	b   keyringBackend
}

func newOSKeyStore(dir string, b keyringBackend) KeyStore {
	return &osKeyStore{dir: dir, b: b}
}

func (s *osKeyStore) Kind() string { return "os" }

func (s *osKeyStore) account() string {
	abs, err := filepath.Abs(s.dir)
	if err != nil {
		return s.dir
	}
	return abs
}

func osUnavailable(err error) error {
	return domain.Errorf(domain.KindSetup, "キーチェーンが使えません。`GDRIVE_CLI_KEYRING=file` を指定すると、鍵を設定ディレクトリのファイルに置きます（守りは弱くなります）").Wrap(err)
}

func (s *osKeyStore) Load() ([]byte, bool, error) {
	v, err := s.b.Get(keyringService, s.account())
	if errors.Is(err, errKeyringNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, osUnavailable(err)
	}
	key, ok := decodeKey(v)
	if !ok {
		return nil, false, domain.Errorf(domain.KindSetup, "キーチェーンの鍵の形が正しくありません。`GDRIVE_CLI_KEYRING=file` を指定すると、鍵を設定ディレクトリのファイルに置けます")
	}
	return key, true, nil
}

func (s *osKeyStore) Save(key []byte) error {
	if err := s.b.Set(keyringService, s.account(), base64.StdEncoding.EncodeToString(key)); err != nil {
		return osUnavailable(err)
	}
	return nil
}

type fileKeyStore struct{ dir string }

func newFileKeyStore(dir string) KeyStore { return &fileKeyStore{dir: dir} }

func (s *fileKeyStore) Kind() string { return "file" }

func (s *fileKeyStore) path() string { return filepath.Join(s.dir, keyFileName) }

func (s *fileKeyStore) unreadable(err error) error {
	e := domain.Errorf(domain.KindSetup, "鍵のファイル（%s）を読めません。消すと、暗号化したファイルは読めなくなります", s.path())
	if err != nil {
		e = e.Wrap(err)
	}
	return e
}

func (s *fileKeyStore) Load() ([]byte, bool, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, s.unreadable(err)
	}
	key, ok := decodeKey(string(data))
	if !ok {
		return nil, false, s.unreadable(nil)
	}
	return key, true, nil
}

func (s *fileKeyStore) Save(key []byte) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return domain.Errorf(domain.KindSetup, "鍵のファイル（%s）を書けません", s.path()).Wrap(err)
	}
	if err := atomicfile.Write(s.path(), []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return domain.Errorf(domain.KindSetup, "鍵のファイル（%s）を書けません", s.path()).Wrap(err)
	}
	return nil
}
