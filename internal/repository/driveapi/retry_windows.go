//go:build windows

package driveapi

import "syscall"

// connErrnos は、接続が切れたことを表す OS のエラー番号の一覧。Windows では WSA のエラー番号も使う。
var connErrnos = []syscall.Errno{syscall.ECONNRESET, syscall.EPIPE, syscall.WSAECONNRESET, syscall.WSAECONNABORTED}
