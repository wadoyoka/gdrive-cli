package repository

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"google.golang.org/api/googleapi"
)

// isRetryable は、やり直せる失敗かどうかを返す。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch {
		case gerr.Code == http.StatusTooManyRequests, gerr.Code >= 500:
			return true
		case gerr.Code == http.StatusForbidden:
			for _, e := range gerr.Errors {
				if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" {
					return true
				}
			}
		}
		return false
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

// retry は、fn が やり直せる失敗を返したとき、1s、2s、4s、8s と待って、最大 attempts 回まで呼ぶ。
func (c *DriveClient) retry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		err = fn()
		if err == nil || !isRetryable(err) || attempt == c.attempts {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		c.sleep(time.Second << (attempt - 1))
	}
	return err
}
