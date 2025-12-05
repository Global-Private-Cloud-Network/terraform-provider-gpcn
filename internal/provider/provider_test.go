package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// Uses environment variable configuration to populate provider values
const (
	providerConfig = `
provider "gpcn" {}
`
)

var (
	// testAccProtoV6ProviderFactories are used to instantiate a provider during
	// acceptance testing. The factory function will be invoked for every Terraform
	// CLI command executed to create a provider server to which the CLI can
	// reattach.
	testProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
		"gpcn": providerserver.NewProtocol6WithError(New("test")()),
	}
)
