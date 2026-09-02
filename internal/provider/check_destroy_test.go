package provider

import (
	"context"
	"fmt"
	"os"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/gpu"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/resourcegroups"
	"terraform-provider-gpcn/internal/sshkeys"
	"terraform-provider-gpcn/internal/virtualmachines"
	"terraform-provider-gpcn/internal/volumes"

	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// readByID reads one resource of a given type. It returns the error only,
// because a destroy check needs to know whether the resource is gone, not what
// it holds.
var readByID = map[string]func(*client.GpcnClient, context.Context, string) error{
	"gpcn_network": func(c *client.GpcnClient, ctx context.Context, id string) error {
		_, err := networks.GetNetwork(c, ctx, id)
		return err
	},
	"gpcn_volume": func(c *client.GpcnClient, ctx context.Context, id string) error {
		_, err := volumes.GetVolume(c, ctx, id)
		return err
	},
	"gpcn_virtualmachine": func(c *client.GpcnClient, ctx context.Context, id string) error {
		_, err := virtualmachines.GetVirtualMachine(c, ctx, id)
		return err
	},
	"gpcn_gpu": func(c *client.GpcnClient, ctx context.Context, id string) error {
		_, err := gpu.GetGPU(c, ctx, id)
		return err
	},
	"gpcn_ssh_key": func(c *client.GpcnClient, ctx context.Context, id string) error {
		_, err := sshkeys.GetSSHKey(c, ctx, id)
		return err
	},
	"gpcn_resource_group": func(c *client.GpcnClient, ctx context.Context, id string) error {
		_, err := resourcegroups.GetResourceGroup(c, ctx, id)
		return err
	},
}

// testAccCheckDestroy asserts that the GPCN API no longer holds any resource
// that the test created.
//
// An acceptance test creates real infrastructure. Without this check a test
// that leaks a resource still passes: the account pays for the resource and no
// later run finds it. A resource type that is absent from readByID is reported,
// not skipped, so that a new resource type cannot leak silently.
func testAccCheckDestroy(s *terraform.State) error {
	gpcnClient, err := acceptanceClient()
	if err != nil {
		return err
	}

	for name, rs := range s.RootModule().Resources {
		if rs.Type == "" || rs.Primary == nil || rs.Primary.ID == "" {
			continue
		}
		read, ok := readByID[rs.Type]
		if !ok {
			// A data source has no destroy check to make.
			if rs.Type == "gpcn_datacenters" || rs.Type == "gpcn_virtualmachine_sizes" || rs.Type == "gpcn_virtualmachine_images" {
				continue
			}
			return fmt.Errorf("%s has type %q, which has no destroy check; add one to readByID", name, rs.Type)
		}

		err := read(gpcnClient, context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("%s (%s) is still present in the GPCN API after destroy", name, rs.Primary.ID)
		}
		if !client.IsNotFound(err) {
			return fmt.Errorf("%s (%s): cannot confirm the destroy: %w", name, rs.Primary.ID, err)
		}
	}
	return nil
}

// acceptanceClient builds a client from the same environment variables that
// configure the provider under test.
func acceptanceClient() (*client.GpcnClient, error) {
	cfg := client.DefaultConfig(os.Getenv("GPCN_HOST"), os.Getenv("GPCN_API_KEY"))
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot build a client for the destroy check: %w", err)
	}
	return gpcnClient, nil
}
