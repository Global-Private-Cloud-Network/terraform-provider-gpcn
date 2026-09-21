package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var gpcnNetworkTest = "gpcn_network.test"

// networkTestIDEnvVar names an existing GPCN network for the import case below. Creation
// is retired, so the acceptance case can no longer mint the network it exercises.
const networkTestIDEnvVar = "GPCN_TEST_NETWORK_ID"

// TestNetworksResource imports a grandfathered network and checks the state it lands in.
// The step does not persist the imported state. The run therefore never plans a destroy
// against a network the platform still serves.
func TestNetworksResource(t *testing.T) {
	t.Parallel()
	networkID := os.Getenv(networkTestIDEnvVar)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			if networkID == "" {
				t.Skipf("%s is not set: name an existing GPCN network to import", networkTestIDEnvVar)
			}
		},
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
			resource "gpcn_network" "test" {
				name          = "imported-network"
				datacenter_id = "imported-datacenter"
				network_type  = "standard"
			}
			`,
				ResourceName:  gpcnNetworkTest,
				ImportState:   true,
				ImportStateId: networkID,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported state, got %d", len(states))
					}
					if got := states[0].Attributes["id"]; got != networkID {
						return fmt.Errorf("imported id = %q, want %q", got, networkID)
					}
					for _, attribute := range []string{"name", "network_type", "datacenter_id", "created_time", "snat", "connected_vms"} {
						if states[0].Attributes[attribute] == "" {
							return fmt.Errorf("imported %s is empty", attribute)
						}
					}
					return nil
				},
			},
		},
	})
}

/*
*
----- Unit tests -----
*
*/

// TestNetworkResourceSchemaCarriesDeprecation pins the deprecation notice byte for byte.
// Terraform prints the notice on every plan that names the resource. A reworded notice is
// therefore a user-facing change, and it must be a deliberate one.
func TestNetworkResourceSchemaCarriesDeprecation(t *testing.T) {
	t.Parallel()

	schemaResponse := &fwresource.SchemaResponse{}
	NewNetworksResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)

	const want = "gpcn_network is deprecated: GPCN networking is VPC-based. Use gpcn_vpc, gpcn_vpc_subnet and gpcn_l2_segment. Existing networks can still be read and destroyed."
	if got := schemaResponse.Schema.DeprecationMessage; got != want {
		t.Errorf("DeprecationMessage = %q, want %q", got, want)
	}
}

func TestNetworksResourceInvalidType(t *testing.T) {
	t.Run("invalid_network_type", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Validate network_type value not in standard or custom
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "bad_value"
				}
				`,
					ExpectError: regexp.MustCompile("Invalid Attribute Value Match"),
				},
			},
		})
	})
}

func TestNetworksResourceStandardValidator(t *testing.T) {
	missingRequiredAttributeErr := "Missing required attribute"

	t.Run("missing_cidr_block", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					dhcp_start_address = "10.0.0.10"
					dhcp_end_address   = "10.0.0.140"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})

	t.Run("missing_dhcp_start_address", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "10.0.0.0/24"
					dhcp_end_address   = "10.0.0.140"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})

	t.Run("missing_dhcp_end_address", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "10.0.0.0/24"
					dhcp_start_address = "10.0.0.10"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})

	t.Run("missing_dns_servers", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "10.0.0.0/24"
					dhcp_start_address = "10.0.0.10"
					dhcp_end_address   = "10.0.0.140"
				}
				`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})
}

func TestNetworksResourceIpAddressValidator(t *testing.T) {
	t.Run("invalid_dhcp_start_address", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "10.0.0.0/24"
					dhcp_start_address = "10.0.0.1455"
					dhcp_end_address   = "10.0.0.140"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile("does not resolve to a valid IPv4 address"),
				},
			},
		})
	})

	t.Run("invalid_dhcp_end_address", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "10.0.0.0/24"
					dhcp_start_address = "10.0.0.10"
					dhcp_end_address   = "10.0.0.1405"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile("does not resolve to a valid IPv4 address"),
				},
			},
		})
	})

	t.Run("dhcp_start_address_not_in_cidr", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "10.0.0.0/24"
					dhcp_start_address = "192.0.0.10"
					dhcp_end_address   = "10.0.0.140"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile("is not a valid IP address in the CIDR"),
				},
			},
		})
	})
}

func TestNetworksResourceCIDRValidator(t *testing.T) {
	t.Run("dhcp_address_not_in_cidr_block", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_network" "test" {
					name = "terraform-demo-standard"
					datacenter_id = "any-datacenter-id"
					network_type = "standard"
					cidr_block = "100.0.0.0/24"
					dhcp_start_address = "10.0.0.10"
					dhcp_end_address   = "10.0.0.140"
					dns_servers = ["8.8.8.8", "8.8.4.4"]
				}
				`,
					ExpectError: regexp.MustCompile("is not a valid IP address in the CIDR"),
				},
			},
		})
	})
}

func TestNetworksResourceDNSServersValidator(t *testing.T) {
	t.Run("dns_servers_empty_list", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
			resource "gpcn_network" "test" {
				name = "terraform-demo-standard"
				datacenter_id = "any-datacenter-id"
				network_type = "standard"
				cidr_block = "10.0.0.0/24"
				dhcp_start_address = "10.0.0.10"
				dhcp_end_address   = "10.0.0.140"
				dns_servers = []
			}
			`,
					ExpectError: regexp.MustCompile("Missing required attribute"),
				},
			},
		})
	})
}

// Read needs a state that the resource schema accepts. Only the id and the
// network type decide the path under test.
func networkReadTestState(t *testing.T, networkID string, networkType string) tfsdk.State {
	t.Helper()

	schemaResponse := &fwresource.SchemaResponse{}
	NewNetworksResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)

	state := tfsdk.State{Schema: schemaResponse.Schema}
	diags := state.Set(context.Background(), networks.ResourceModel{
		ID:          types.StringValue(networkID),
		NetworkType: types.StringValue(networkType),
		Location:    types.MapNull(types.StringType),
		DNSServers:  types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("failed to build the prior state: %v", diags)
	}
	return state
}

// TestNetworkResourceReadWarnsWhenCustomNetworkGone drives Read against a 404. The
// acceptance harness cannot observe a warning diagnostic, so the branch needs a direct
// call. A standard network keeps the silent removal, so the second case pins the absence.
func TestNetworkResourceReadWarnsWhenCustomNetworkGone(t *testing.T) {
	t.Parallel()

	const networkID = "net-gone-1"

	cases := []struct {
		name        string
		networkType string
		wantWarning bool
	}{
		{name: "custom", networkType: networks.NETWORK_TYPE_CUSTOM, wantWarning: true},
		{name: "standard", networkType: networks.NETWORK_TYPE_STANDARD, wantWarning: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var mutex sync.Mutex
			var requestedPath string
			_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
				T: t,
				Handler: func(w http.ResponseWriter, r *http.Request) {
					mutex.Lock()
					requestedPath = r.URL.Path
					mutex.Unlock()
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"success":false,"message":"Network not found"}`))
				},
			})

			networkResource := &networksResource{client: gpcnClient}
			priorState := networkReadTestState(t, networkID, testCase.networkType)
			readResponse := &fwresource.ReadResponse{State: priorState}

			networkResource.Read(context.Background(), fwresource.ReadRequest{State: priorState}, readResponse)

			mutex.Lock()
			gotPath := requestedPath
			mutex.Unlock()
			if want := networks.BASE_URL_V1 + networkID; gotPath != want {
				t.Fatalf("requested path = %q, want %q", gotPath, want)
			}
			if !readResponse.State.Raw.IsNull() {
				t.Errorf("expected Read to remove the resource from state")
			}
			if readResponse.Diagnostics.HasError() {
				t.Fatalf("expected no error diagnostic, got %v", readResponse.Diagnostics)
			}

			if !testCase.wantWarning {
				if count := len(readResponse.Diagnostics); count != 0 {
					t.Fatalf("expected 0 diagnostics for a standard network, got %d: %v", count, readResponse.Diagnostics)
				}
				return
			}

			if count := len(readResponse.Diagnostics); count != 1 {
				t.Fatalf("expected 1 diagnostic for a custom network, got %d: %v", count, readResponse.Diagnostics)
			}
			warning := readResponse.Diagnostics[0]
			if warning.Severity() != diag.SeverityWarning {
				t.Errorf("severity = %v, want %v", warning.Severity(), diag.SeverityWarning)
			}
			if got := warning.Summary(); got != networks.WarnSummaryNetworkRemovedFromState {
				t.Errorf("summary = %q, want %q", got, networks.WarnSummaryNetworkRemovedFromState)
			}
			want := fmt.Sprintf(networks.WarnDetailCustomNetworkGone, networkID)
			if got := warning.Detail(); got != want {
				t.Errorf("detail = %q, want %q", got, want)
			}
		})
	}
}
