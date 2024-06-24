package vmware

import (
	"crypto/sha1"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func GetEndpointThumbprint(url *url.URL) (string, error) {
	config := tls.Config{
		InsecureSkipVerify: true,
	}

	port := url.Port()
	if port == "" {
		port = "443"
	}

	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%s", url.Hostname(), port), &config)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if len(conn.ConnectionState().PeerCertificates) == 0 {
		return "", errors.New("no certificates found")
	}

	certificate := conn.ConnectionState().PeerCertificates[0]
	sha1Bytes := sha1.Sum(certificate.Raw)

	thumbprint := make([]string, len(sha1Bytes))
	for i, b := range sha1Bytes {
		thumbprint[i] = fmt.Sprintf("%02X", b)
	}

	return strings.Join(thumbprint, ":"), nil
}
