package repository

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// PolicyFile は、設定ディレクトリの policy.toml。
type PolicyFile struct{ path string }

// NewPolicyFile は、dir/policy.toml を扱う PolicyFile を返す。
func NewPolicyFile(dir string) *PolicyFile {
	return &PolicyFile{path: filepath.Join(dir, "policy.toml")}
}

// Path は、policy.toml の場所を返す。
func (f *PolicyFile) Path() string { return f.path }

// CreateIfAbsent は、なければ既定の内容で 0600 で作る。作ったときだけ true を返す。
func (f *PolicyFile) CreateIfAbsent() (bool, error) {
	file, err := os.OpenFile(f.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("policy.toml を作れません: %w", err)
	}
	if _, err := file.WriteString(domain.DefaultPolicyTOML); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("policy.toml を書けません: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("policy.toml を閉じられません: %w", err)
	}
	return true, nil
}

type policyDoc struct {
	Deny []string `toml:"deny"`
}

func unreadable(cause error) error {
	return domain.Errorf(domain.KindDenied, "policy.toml を読めないため、すべてのコマンドを止めています").
		WithDetails(cause.Error()).Wrap(cause)
}

// Load は、policy.toml を読む。読めないときは、安全のため KindDenied を返す。
func (f *PolicyFile) Load() (domain.Policy, error) {
	body, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Policy{}, domain.Errorf(domain.KindSetup, "設定がありません。`gdrive-cli init` を実行してください")
	}
	if err != nil {
		return domain.Policy{}, unreadable(err)
	}
	var doc policyDoc
	dec := toml.NewDecoder(bytes.NewReader(body)).DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return domain.Policy{}, unreadable(err)
	}
	p := domain.Policy{Deny: doc.Deny}
	if err := p.Validate(); err != nil {
		return domain.Policy{}, err
	}
	return p, nil
}
