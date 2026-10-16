package proxy

import (
	"net"
	"strings"
)

func IsLoopbackListen(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}
