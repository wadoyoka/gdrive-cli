package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// StateFile は、設定ディレクトリの state.json。ルートフォルダの ID を覚える。
type StateFile struct {
	path string
}

type stateData struct {
	RootFolderID string `json:"rootFolderId"`
}

// NewStateFile は、dir/state.json を扱う StateFile を作る。
func NewStateFile(dir string) *StateFile {
	return &StateFile{path: filepath.Join(dir, "state.json")}
}

// RootFolderID は、保存した ID を返す。ファイルがなければ空文字。
func (s *StateFile) RootFolderID() (string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("状態ファイルを読めません: %w", err)
	}
	var st stateData
	if err := json.Unmarshal(data, &st); err != nil {
		return "", fmt.Errorf("状態ファイルが壊れています（%s）: %w", s.path, err)
	}
	return st.RootFolderID, nil
}

// SetRootFolderID は、ID を保存する。
func (s *StateFile) SetRootFolderID(id string) error {
	data, err := json.Marshal(stateData{RootFolderID: id})
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, data, 0o600)
}
