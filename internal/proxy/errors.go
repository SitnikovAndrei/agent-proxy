package proxy

import (
	"errors"
	"net/url"
)

// net/http includes the full request URL in url.Error; query values stay out of logs.
func TransportFailure(err error) error {
	var requestError *url.Error
	if errors.As(err, &requestError) {
		return requestError.Err
	}
	return err
}
