package protocol

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type tcpProxy struct {
	Scheme string
	Host   string
	Port   string
}

func parseTCPProxy(value string) (*tcpProxy, error) {
	if value == "" {
		return nil, nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "socks5" && u.Scheme != "http-connect" {
		return nil, fmt.Errorf("tcp_proxy must use socks5:// or http-connect://")
	}
	if u.Hostname() == "" || u.Port() == "" {
		return nil, fmt.Errorf("tcp_proxy must include host and port")
	}
	return &tcpProxy{Scheme: u.Scheme, Host: u.Hostname(), Port: u.Port()}, nil
}

func dialTCP(ctx context.Context, host string, port int, timeout time.Duration, proxyValue string, fallbackDirect bool) (net.Conn, error) {
	proxy, err := parseTCPProxy(proxyValue)
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return dialDirect(ctx, host, port, timeout)
	}
	conn, err := dialViaProxy(ctx, proxy, host, port, timeout)
	if err == nil {
		return conn, nil
	}
	if !fallbackDirect {
		return nil, err
	}
	return dialDirect(ctx, host, port, timeout)
}

func dialDirect(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	var d net.Dialer
	if timeout > 0 {
		d.Timeout = timeout
	}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

func dialViaProxy(ctx context.Context, proxy *tcpProxy, targetHost string, targetPort int, timeout time.Duration) (net.Conn, error) {
	conn, err := dialDirect(ctx, proxy.Host, mustAtoi(proxy.Port), timeout)
	if err != nil {
		return nil, err
	}
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
		defer conn.SetDeadline(time.Time{})
	}
	if proxy.Scheme == "socks5" {
		err = socks5Connect(conn, targetHost, targetPort)
	} else {
		err = httpConnect(conn, targetHost, targetPort)
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func socks5Connect(conn net.Conn, targetHost string, targetPort int) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		return fmt.Errorf("SOCKS5 no-auth negotiation failed: %x", buf)
	}
	hostBytes := []byte(targetHost)
	if len(hostBytes) > 255 {
		return fmt.Errorf("SOCKS5 target host too long")
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(hostBytes))}
	req = append(req, hostBytes...)
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], uint16(targetPort))
	req = append(req, p[:]...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 5 || head[1] != 0 {
		return fmt.Errorf("SOCKS5 connect failed: %x", head)
	}
	switch head[3] {
	case 1:
		_, err := io.CopyN(io.Discard, conn, 6)
		return err
	case 3:
		ln := make([]byte, 1)
		if _, err := io.ReadFull(conn, ln); err != nil {
			return err
		}
		_, err := io.CopyN(io.Discard, conn, int64(ln[0])+2)
		return err
	case 4:
		_, err := io.CopyN(io.Discard, conn, 18)
		return err
	default:
		return fmt.Errorf("SOCKS5 unsupported bind address type: %d", head[3])
	}
}

func httpConnect(conn net.Conn, targetHost string, targetPort int) error {
	target := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target, target)
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return err
	}
	parts := strings.Fields(line)
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "2") {
		return fmt.Errorf("HTTP CONNECT failed: %s", strings.TrimSpace(line))
	}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		if l == "\r\n" || l == "\n" {
			break
		}
	}
	return nil
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
