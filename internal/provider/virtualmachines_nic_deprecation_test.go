package provider

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// TestVirtualMachineNetworkInterfaceDeprecations pins the four notices byte for byte.
// The generated docs show each notice. Terraform core does not print one for a
// computed nested attribute, so a reworded notice changes the docs only.
func TestVirtualMachineNetworkInterfaceDeprecations(t *testing.T) {
	t.Parallel()

	schemaResponse := &fwresource.SchemaResponse{}
	NewVirtualMachinesResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)

	interfaces, ok := schemaResponse.Schema.Attributes["network_interfaces"].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("network_interfaces is not a nested list attribute")
	}

	want := map[string]string{
		"network_name": "network_name is deprecated: GPCN™ has retired legacy networks, so it's null for every VPC and L2 interface.",
		"network_id":   "network_id is deprecated: GPCN™ has retired legacy networks, so it's null for every VPC and L2 interface.",
		"gateway_ip":   "gateway_ip is deprecated: GPCN™ has retired legacy networks, so it's null for every VPC and L2 interface.",
		"network_type": "network_type is deprecated: GPCN™ has retired legacy networks, so it's null for every VPC and L2 interface.",
	}
	for name, message := range want {
		attribute, found := interfaces.NestedObject.Attributes[name]
		if !found {
			t.Errorf("network_interfaces has no %s attribute", name)
			continue
		}
		if got := attribute.GetDeprecationMessage(); got != message {
			t.Errorf("%s DeprecationMessage = %q, want %q", name, got, message)
		}
	}
}
