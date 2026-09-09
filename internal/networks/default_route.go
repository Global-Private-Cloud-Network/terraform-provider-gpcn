package networks

import (
	"encoding/binary"
	"fmt"
	"net"
)

// firstUsableHostFromCIDR returns the network address plus one.
func firstUsableHostFromCIDR(cidr string) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("cidr %q is not IPv4", cidr)
	}
	next := make(net.IP, len(ip))
	copy(next, ip)
	binary.BigEndian.PutUint32(next, binary.BigEndian.Uint32(next)+1)
	return next.String(), nil
}
