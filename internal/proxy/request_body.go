package proxy

import (
	"errors"
	"io"
	"net/http"
)

const MaxRequestBodyBytes = 64 << 20

// ReadRequestBody bounds buffering while leaving the provider's error format to its handler.
func ReadRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes))
}

func RequestBodyError(err error) (int, string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge, "request body too large"
	}
	return http.StatusBadRequest, "cannot read request body"
}
