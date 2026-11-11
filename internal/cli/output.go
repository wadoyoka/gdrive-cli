package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

type errorBody struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Details []string `json:"details"`
}

// writeJSON は、v を JSON として 1 行で書く。
func writeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// writeError は、エラーを標準エラーに書く。
func writeError(w io.Writer, asJSON bool, err error) {
	body := errorBody{Kind: domain.KindOf(err).String(), Message: err.Error(), Details: []string{}}
	var de *domain.Error
	if errors.As(err, &de) {
		body.Message = de.Message
		if len(de.Details) > 0 {
			body.Details = de.Details
		}
	}
	if asJSON {
		_ = writeJSON(w, map[string]errorBody{"error": body})
		return
	}
	_, _ = fmt.Fprintf(w, "エラー: %s\n", body.Message)
	for _, d := range body.Details {
		_, _ = fmt.Fprintf(w, "  - %s\n", d)
	}
}
