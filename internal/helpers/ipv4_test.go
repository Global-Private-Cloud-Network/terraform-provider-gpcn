package helpers_test

import (
	"context"
	"fmt"
	"testing"

	"terraform-provider-gpcn/internal/helpers"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestIPv4AddressValidatorUnit(t *testing.T) {
	t.Parallel()

	const (
		format      = "peer_address must be an IPv4 address in dotted-quad form, such as 203.0.113.10. Got: '%s'"
		leadingZero = "peer_address must not have leading zeros in an octet. Got: '%s'"
		notPublic   = "peer_address must be a public IPv4 address. %s is in the non-public range %s"
	)
	sprintf := func(f string, a ...any) string { return fmt.Sprintf(f, a...) }

	cases := []struct {
		name       string
		value      string
		publicOnly bool
		want       string
	}{
		{name: "public", value: "203.0.113.10", publicOnly: true},
		{name: "doc range allowed", value: "198.51.100.7", publicOnly: true},
		{name: "172.15 is public", value: "172.15.255.255", publicOnly: true},
		{name: "172.32 is public", value: "172.32.0.1", publicOnly: true},
		{name: "100.63 is public", value: "100.63.255.255", publicOnly: true},
		{name: "100.128 is public", value: "100.128.0.1", publicOnly: true},
		{name: "any address when not public only", value: "10.0.0.1"},
		{name: "this network", value: "0.1.2.3", publicOnly: true, want: sprintf(notPublic, "0.1.2.3", "0.0.0.0/8")},
		{name: "ten", value: "10.1.2.3", publicOnly: true, want: sprintf(notPublic, "10.1.2.3", "10.0.0.0/8")},
		{name: "shared space", value: "100.64.0.1", publicOnly: true, want: sprintf(notPublic, "100.64.0.1", "100.64.0.0/10")},
		{name: "loopback", value: "127.0.0.1", publicOnly: true, want: sprintf(notPublic, "127.0.0.1", "127.0.0.0/8")},
		{name: "link local", value: "169.254.1.1", publicOnly: true, want: sprintf(notPublic, "169.254.1.1", "169.254.0.0/16")},
		{name: "172.16", value: "172.16.0.1", publicOnly: true, want: sprintf(notPublic, "172.16.0.1", "172.16.0.0/12")},
		{name: "172.31", value: "172.31.255.255", publicOnly: true, want: sprintf(notPublic, "172.31.255.255", "172.16.0.0/12")},
		{name: "192.168", value: "192.168.1.1", publicOnly: true, want: sprintf(notPublic, "192.168.1.1", "192.168.0.0/16")},
		{name: "multicast", value: "224.0.0.1", publicOnly: true, want: sprintf(notPublic, "224.0.0.1", "224.0.0.0/4")},
		{name: "reserved", value: "240.0.0.1", publicOnly: true, want: sprintf(notPublic, "240.0.0.1", "240.0.0.0/4")},
		{name: "broadcast", value: "255.255.255.255", publicOnly: true, want: sprintf(notPublic, "255.255.255.255", "240.0.0.0/4")},
		{name: "leading zero", value: "203.0.113.010", want: sprintf(leadingZero, "203.0.113.010")},
		{name: "leading zero first octet", value: "010.0.0.1", want: sprintf(leadingZero, "010.0.0.1")},
		{name: "three octets", value: "203.0.113", want: sprintf(format, "203.0.113")},
		{name: "five octets", value: "1.2.3.4.5", want: sprintf(format, "1.2.3.4.5")},
		{name: "octet over 255", value: "203.0.113.256", want: sprintf(format, "203.0.113.256")},
		{name: "hostname", value: "peer.example.com", want: sprintf(format, "peer.example.com")},
		{name: "ipv6", value: "2001:db8::1", want: sprintf(format, "2001:db8::1")},
		{name: "mapped ipv6", value: "::ffff:203.0.113.10", want: sprintf(format, "::ffff:203.0.113.10")},
		{name: "empty", value: "", want: sprintf(format, "")},
		{name: "cidr", value: "203.0.113.0/24", want: sprintf(format, "203.0.113.0/24")},
		{name: "signed octet", value: "+3.0.113.10", want: sprintf(format, "+3.0.113.10")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := helpers.IPv4AddressValidator{Attribute: "peer_address", PublicOnly: tc.publicOnly}
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("peer_address"), ConfigValue: types.StringValue(tc.value)}, resp)

			if tc.want == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("Expected %q to pass, got %v", tc.value, resp.Diagnostics)
				}
				return
			}
			if !resp.Diagnostics.HasError() {
				t.Fatalf("Expected %q to fail", tc.value)
			}
			if got := resp.Diagnostics[0].Summary(); got != "Invalid IPv4 address" {
				t.Errorf("Expected summary %q, got %q", "Invalid IPv4 address", got)
			}
			if got := resp.Diagnostics[0].Detail(); got != tc.want {
				t.Errorf("Expected detail %q, got %q", tc.want, got)
			}
		})
	}
}

func TestIPv4AddressValidatorSkipsNullAndUnknownUnit(t *testing.T) {
	t.Parallel()
	v := helpers.IPv4AddressValidator{Attribute: "peer_address", PublicOnly: true}
	for _, value := range []types.String{types.StringNull(), types.StringUnknown()} {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("Expected %v to pass, got %v", value, resp.Diagnostics)
		}
	}
}

func TestIPv4CIDRValidatorUnit(t *testing.T) {
	t.Parallel()

	const (
		format      = "peer_cidrs must be an IPv4 CIDR, such as 192.168.10.0/24. Got: '%s'"
		leadingZero = "peer_cidrs must not have leading zeros in an octet or in the prefix length. Got: '%s'"
		bounds      = "peer_cidrs must have a prefix length between /8 and /32. Got: '%s'"
		hostBits    = "peer_cidrs must be written as its network address. '%s' has host bits set. Use '%s'"
	)
	sprintf := func(f string, a ...any) string { return fmt.Sprintf(f, a...) }

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "slash 24", value: "192.168.10.0/24"},
		{name: "slash 8", value: "10.0.0.0/8"},
		{name: "slash 32", value: "203.0.113.7/32"},
		{name: "slash 31", value: "203.0.113.6/31"},
		{name: "zero network", value: "0.0.0.0/8"},
		{name: "host bits slash 24", value: "192.168.10.7/24", want: sprintf(hostBits, "192.168.10.7/24", "192.168.10.0/24")},
		{name: "host bits slash 8", value: "10.1.0.0/8", want: sprintf(hostBits, "10.1.0.0/8", "10.0.0.0/8")},
		{name: "host bits slash 31", value: "203.0.113.7/31", want: sprintf(hostBits, "203.0.113.7/31", "203.0.113.6/31")},
		{name: "prefix 7", value: "10.0.0.0/7", want: sprintf(bounds, "10.0.0.0/7")},
		{name: "prefix 0", value: "0.0.0.0/0", want: sprintf(bounds, "0.0.0.0/0")},
		{name: "prefix 33", value: "10.0.0.0/33", want: sprintf(bounds, "10.0.0.0/33")},
		{name: "leading zero octet", value: "192.168.010.0/24", want: sprintf(leadingZero, "192.168.010.0/24")},
		{name: "leading zero prefix", value: "192.168.10.0/024", want: sprintf(leadingZero, "192.168.10.0/024")},
		{name: "leading zero single digit prefix", value: "192.168.10.0/08", want: sprintf(leadingZero, "192.168.10.0/08")},
		{name: "no prefix", value: "192.168.10.0", want: sprintf(format, "192.168.10.0")},
		{name: "empty prefix", value: "192.168.10.0/", want: sprintf(format, "192.168.10.0/")},
		{name: "two slashes", value: "192.168.10.0/24/1", want: sprintf(format, "192.168.10.0/24/1")},
		{name: "text prefix", value: "192.168.10.0/ab", want: sprintf(format, "192.168.10.0/ab")},
		{name: "signed prefix", value: "192.168.10.0/+24", want: sprintf(format, "192.168.10.0/+24")},
		{name: "bad octet", value: "192.168.10.256/24", want: sprintf(format, "192.168.10.256/24")},
		{name: "ipv6", value: "2001:db8::/32", want: sprintf(format, "2001:db8::/32")},
		{name: "empty", value: "", want: sprintf(format, "")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := helpers.IPv4CIDRValidator{Attribute: "peer_cidrs", MinPrefix: 8}
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("peer_cidrs"), ConfigValue: types.StringValue(tc.value)}, resp)

			if tc.want == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("Expected %q to pass, got %v", tc.value, resp.Diagnostics)
				}
				return
			}
			if !resp.Diagnostics.HasError() {
				t.Fatalf("Expected %q to fail", tc.value)
			}
			if got := resp.Diagnostics[0].Summary(); got != "Invalid IPv4 CIDR" {
				t.Errorf("Expected summary %q, got %q", "Invalid IPv4 CIDR", got)
			}
			if got := resp.Diagnostics[0].Detail(); got != tc.want {
				t.Errorf("Expected detail %q, got %q", tc.want, got)
			}
		})
	}
}

func TestIPv4CIDRValidatorSkipsNullAndUnknownUnit(t *testing.T) {
	t.Parallel()
	v := helpers.IPv4CIDRValidator{Attribute: "peer_cidrs", MinPrefix: 8}
	for _, value := range []types.String{types.StringNull(), types.StringUnknown()} {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("Expected %v to pass, got %v", value, resp.Diagnostics)
		}
	}
}
