package proxy

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"

	"ocm/internal/config"
)

func Listen(options config.Config) (net.Listener, error) {
	var certificate tls.Certificate
	if options.TLS.Enabled {
		var err error
		certificate, err = tls.LoadX509KeyPair(options.TLS.CertificatePath, options.TLS.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("load TLS certificate: %w", err)
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(options.Listen, strconv.Itoa(options.ListenPort)))
	if err != nil {
		return nil, err
	}
	if options.TLS.Enabled {
		listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2", "http/1.1"}})
	}
	return listener, nil
}
