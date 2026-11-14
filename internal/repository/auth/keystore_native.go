package auth

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/repository/keychain"
)

const (
	// nativeMarker は、新しい形の項目に付ける目印（kSecAttrComment）。古い形かどうかを見分けるためだけに使う。
	nativeMarker = "gdrive-cli-native-v1"
	// nativeLabel は、新しい形の項目のラベル。
	nativeLabel = "gdrive-cli"
	// migratingSuffix は、移し替えの途中で鍵を置く一時的なアカウント名の後ろに付ける。
	migratingSuffix = "#migrating"

	msgKeychainDenied = "キーチェーンの鍵の使用が拒否されました。gdrive-cli をもう一度実行し、ダイアログで「常に許可」を押してください"
	msgKeychainNoUI   = "キーチェーンの確認ダイアログを出せません（SSH で入っているときなど）。この Mac の画面で一度 `gdrive-cli status` を実行して許可するか、`GDRIVE_CLI_KEYRING=file` を指定してください"
	msgMigrated       = "キーチェーンの鍵を新しい形に移しました"
)

// loadAttempts は、Load が項目を見直す回数の上限。見えた項目が読む前に消えたとき（ほかの gdrive-cli が移し替えている途中）に見直す。
// 移し替えの途中で読むと、古い項目・一時的な項目・新しい項目の順に見えるので、3回あれば足りる。
const loadAttempts = 3

var (
	errKeychainDenied = errors.New("キーチェーンの使用が拒否されました")
	errKeychainNoUI   = errors.New("キーチェーンの確認ダイアログを出せません")
)

// itemState は、値を読まずに見た項目の状態。
type itemState int

const (
	itemAbsent  itemState = iota
	itemNative            // 目印がある（新しい形）
	itemForeign           // 目印がない（go-keyring で作った古い形、またはほかのプログラムが置いたもの）
)

// nativeItems は、新しい形の項目の操作。サービス名は keyringService に決まっている。
type nativeItems interface {
	Inspect(account string) (itemState, error) // 値を読まない
	Get(account string) (string, error)        // ないときは errKeyringNotFound
	Add(account, secret string) error          // ACL は自分だけ、目印とラベルを付ける
	Delete(account string) error
}

// legacyItems は、go-keyring（/usr/bin/security 経由）での操作。goKeyringBackend が満たす。
type legacyItems interface {
	Get(service, user string) (string, error) // ないときは errKeyringNotFound
	Delete(service, user string) error        // ないときは errKeyringNotFound
}

var (
	_ KeyStore    = (*nativeKeyStore)(nil)
	_ legacyItems = goKeyringBackend{}
)

// nativeKeyStore は、gdrive-cli だけが読める新しい形の項目に鍵を置く KeyStore。
// go-keyring で作った古い形の項目は、Load が Unverified で返し、Adopt で新しい形に移す（設計書 5.6）。
// Load はロックの外で呼ばれるので、何も書き換えない。書き換えるのは Adopt と Save だけ（ロックの中）。
type nativeKeyStore struct {
	dir    string
	items  nativeItems
	legacy legacyItems
	guard  func() error // キーチェーンに触れる前に呼ぶ。失敗したら触れない
	notify func(msg string)
}

func newNativeKeyStore(dir string, items nativeItems, legacy legacyItems, guard func() error, notify func(msg string)) *nativeKeyStore {
	return &nativeKeyStore{dir: dir, items: items, legacy: legacy, guard: guard, notify: notify}
}

func (s *nativeKeyStore) Kind() string { return "os" }

// accounts は、本来のアカウント名と、移し替えの一時的なアカウント名。
func (s *nativeKeyStore) accounts() (account, migrating string) {
	account = keyringAccount(s.dir)
	return account, account + migratingSuffix
}

// keySource は、Load が鍵を読む項目。
type keySource struct {
	account    string
	legacy     bool // 目印がないので go-keyring で読む
	unverified bool
}

// locate は、値を読まずに、鍵を読む項目を探す。なければ ok が false。
func (s *nativeKeyStore) locate() (src keySource, ok bool, err error) {
	a, t := s.accounts()
	st, err := s.items.Inspect(a)
	if err != nil {
		return keySource{}, false, nativeUnavailable(err)
	}
	switch st {
	case itemNative:
		return keySource{account: a}, true, nil
	case itemForeign:
		return keySource{account: a, legacy: true, unverified: true}, true, nil
	}
	// 本来の名前の項目がなければ、移し替えの途中で止まった一時的な項目を探す。目印のないものは使わない。
	st, err = s.items.Inspect(t)
	if err != nil {
		return keySource{}, false, nativeUnavailable(err)
	}
	if st != itemNative {
		return keySource{}, false, nil
	}
	return keySource{account: t, unverified: true}, true, nil
}

func (s *nativeKeyStore) read(src keySource) (string, error) {
	if src.legacy {
		return s.legacy.Get(keyringService, src.account)
	}
	return s.items.Get(src.account)
}

func (s *nativeKeyStore) Load() (LoadedKey, bool, error) {
	if err := s.guard(); err != nil {
		return LoadedKey{}, false, err
	}
	var vanished error
	for range loadAttempts {
		src, ok, err := s.locate()
		if err != nil || !ok {
			return LoadedKey{}, false, err
		}
		v, err := s.read(src)
		if errors.Is(err, errKeyringNotFound) {
			vanished = err // 見えた項目が読む前に消えた。ほかの gdrive-cli が移し替えている途中なので、見直す
			continue
		}
		if err != nil {
			return LoadedKey{}, false, nativeUnavailable(err)
		}
		key, err := decodeKeychainKey(v)
		if err != nil {
			return LoadedKey{}, false, err
		}
		return LoadedKey{Key: key, Unverified: src.unverified}, true, nil
	}
	// 見直しても項目が消え続けた。「ない」とは言い切れないので、使えないとする。
	return LoadedKey{}, false, nativeUnavailable(vanished)
}

// Adopt は、Load が Unverified で返した鍵を新しい形の項目に移す。設定ディレクトリのロックの中で呼ぶ。
// 途中で失敗したら、そこで止めてエラーを返す。どこで止まっても鍵はどれかの項目に残り、次の Load と Adopt が続きを行う。
func (s *nativeKeyStore) Adopt(key []byte) error {
	if err := s.guard(); err != nil {
		return err
	}
	a, t := s.accounts()
	enc := base64.StdEncoding.EncodeToString(key)
	st, err := s.items.Inspect(a)
	if err != nil {
		return nativeUnavailable(err)
	}
	switch st {
	case itemNative:
		// ロックを待つ間に、ほかの gdrive-cli が移し終えている。残った一時的な項目だけを消す。知らせない。
		return s.remove(t)
	case itemForeign:
		err = s.migrateLegacy(a, t, enc)
	default:
		err = s.resumeMigrating(a, t, enc)
	}
	if err != nil {
		return err
	}
	s.notify(msgMigrated)
	return nil
}

// migrateLegacy は、古い形の項目 a を新しい形に移す。同じ名前の項目は2つ置けないので、古い項目を消す前に
// 一時的な項目 t に鍵を置き、消してから作る間に止まっても鍵を失わないようにする（設計書 5.6）。
func (s *nativeKeyStore) migrateLegacy(a, t, enc string) error {
	if err := s.remove(t); err != nil { // 前に止まったときの一時的な項目
		return err
	}
	if err := s.add(t, enc); err != nil {
		return err
	}
	if err := unlessGone(s.legacy.Delete(keyringService, a)); err != nil {
		return err
	}
	if err := s.add(a, enc); err != nil {
		return err
	}
	return unlessGone(s.items.Delete(t))
}

// resumeMigrating は、古い項目を消したあとで止まった移し替えを、一時的な項目の鍵で終える。
func (s *nativeKeyStore) resumeMigrating(a, t, enc string) error {
	if err := s.add(a, enc); err != nil {
		return err
	}
	return s.remove(t)
}

// Save は、本来の名前と一時的な名前の項目を消してから、新しい形の項目を作る。消せなければ作らない。
func (s *nativeKeyStore) Save(key []byte) error {
	if err := s.guard(); err != nil {
		return err
	}
	a, t := s.accounts()
	if err := s.remove(a); err != nil {
		return err
	}
	if err := s.remove(t); err != nil {
		return err
	}
	return s.add(a, base64.StdEncoding.EncodeToString(key))
}

func (s *nativeKeyStore) add(account, enc string) error {
	if err := s.items.Add(account, enc); err != nil {
		return nativeUnavailable(err)
	}
	return nil
}

// remove は、項目があれば消す。目印があれば自前で、なければ go-keyring で消す。
func (s *nativeKeyStore) remove(account string) error {
	st, err := s.items.Inspect(account)
	if err != nil {
		return nativeUnavailable(err)
	}
	switch st {
	case itemNative:
		return unlessGone(s.items.Delete(account))
	case itemForeign:
		return unlessGone(s.legacy.Delete(keyringService, account))
	default:
		return nil
	}
}

// unlessGone は、消したときのエラーを直す。もう消えていた（ほかの gdrive-cli と行き違った）ときは成功とみなす。
func unlessGone(err error) error {
	if err == nil || errors.Is(err, errKeyringNotFound) {
		return nil
	}
	return nativeUnavailable(err)
}

// keychainErr は OSStatus を、上の決まったエラーに直す。StatusOK は nil、ないは errKeyringNotFound。
func keychainErr(st keychain.Status) error {
	switch keychain.Classify(st) {
	case keychain.ResultOK:
		return nil
	case keychain.ResultNotFound:
		return errKeyringNotFound
	case keychain.ResultDenied:
		return errKeychainDenied
	case keychain.ResultNoUI:
		return errKeychainNoUI
	default:
		return fmt.Errorf("キーチェーンの操作に失敗しました（OSStatus %d）", st)
	}
}

// nativeUnavailable は、拒否は msgKeychainDenied、ダイアログを出せないは msgKeychainNoUI、
// それ以外は osUnavailable の KindSetup のエラーにする。
func nativeUnavailable(err error) error {
	switch {
	case errors.Is(err, errKeychainDenied):
		return domain.Errorf(domain.KindSetup, msgKeychainDenied).Wrap(err)
	case errors.Is(err, errKeychainNoUI):
		return domain.Errorf(domain.KindSetup, msgKeychainNoUI).Wrap(err)
	default:
		return osUnavailable(err)
	}
}
