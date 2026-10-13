package proxy

import (
	"errors"
	"io"
	"net/http"
)

func CopyStream(writer http.ResponseWriter, body io.Reader, consume func([]byte)) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return errors.New("streaming not supported")
	}
	buffer := make([]byte, 32*1024)
	for {
		count, readError := body.Read(buffer)
		if count > 0 {
			consume(buffer[:count])
			if _, err := writer.Write(buffer[:count]); err != nil {
				return err
			}
			flusher.Flush()
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				return nil
			}
			return readError
		}
	}
}
