package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/virtualmachines"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var gpcnVirtualMachineTest = "gpcn_virtualmachine.test"

// vpcAndSubnet returns the VPC and the subnet a virtual machine is born on. A machine
// lives in exactly one VPC, so every case that creates one creates these two first.
// GPCN checks a CIDR for overlap across the whole entity, and the repository has no
// sweepers. Each case therefore takes its own name and its own fixed block. A case
// meets only its own block, and only when an earlier run of that case failed and left
// one. The configuration acknowledges that overlap.
func vpcAndSubnet(suffix string, octet int) string {
	return fmt.Sprintf(`
resource "gpcn_vpc" "vm_vpc" {
	name                = "terraform-demo-vpc-%[1]s"
	datacenter_id       = data.gpcn_datacenters.central_us.datacenters[0].id
	cidr                = "10.%[2]d.0.0/16"
	acknowledge_overlap = true
}

resource "gpcn_vpc_subnet" "vm_subnet" {
	vpc_id = gpcn_vpc.vm_vpc.id
	name   = "terraform-demo-subnet-%[1]s"
	cidr   = "10.%[2]d.1.0/24"
}
`, suffix, octet)
}

// vmOctetSlot names the block each of the six acceptance cases owns. Each case takes
// its own /16, so parallel cases never share a block. Slots 6 and 7 wait for the next
// cases.
var vmOctetSlot = map[string]int{
	"TestVirtualMachinesAuth":                     0,
	"TestVirtualMachinesVolumeAttachment":         1,
	"TestVirtualMachinesSizeUpgrade":              2,
	"TestVirtualMachinesChangePublicIpAllocation": 3,
	"TestVirtualMachinesResource":                 4,
	"TestVirtualMachinesFirstBoot":                5,
}

const (
	vmOctetBase  = 112
	vmOctetSlots = 8
)

// vpcTestOctetFor returns the octet of the named case. It fails a case the table does
// not name.
func vpcTestOctetFor(t testing.TB, name string) int {
	t.Helper()
	slot, named := vmOctetSlot[name]
	if !named {
		t.Fatalf("Expected %s to own a slot in vmOctetSlot", name)
	}
	return vmOctetBase + slot
}

// The guard reads the table alone, not the call sites. It refuses a slot outside this
// file's window, because that slot takes a block this file does not own. It refuses two
// entries that take one octet. The compiler accepts both.
func TestVirtualMachineAcceptanceOctetsAreUnique(t *testing.T) {
	owner := map[int]string{}
	for name, slot := range vmOctetSlot {
		if slot < 0 || slot >= vmOctetSlots {
			t.Errorf("Expected %s to take a slot below %d, got %d", name, vmOctetSlots, slot)
		}
		octet := vpcTestOctetFor(t, name)
		if other, taken := owner[octet]; taken {
			t.Errorf("Expected %s and %s to take different octets, both take %d", name, other, octet)
		}
		owner[octet] = name
	}
}

// dataCenterImagesAndSize returns the common datacenter, image, and size datasource lookup blocks for Chicago.
func dataCenterImagesAndSize() string {
	return `
data "gpcn_datacenters" "central_us" {
	country_name = "United States"
	region_name  = "Central"
	name         = "Chicago"
}

data "gpcn_virtualmachine_images" "vm_image" {
	datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
	image_name    = "Alma Linux 8"
}

data "gpcn_virtualmachine_sizes" "vm_size" {
	datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
	category      = "general-purpose"
	min_cpu       = 2
}
`
}

func TestVirtualMachinesResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcOctet := vpcTestOctetFor(t, t.Name())
	sshKeyName := fmt.Sprintf("vm-basic-key-%s", rName)
	volumeName := fmt.Sprintf("vm-basic-vol-%s", rName)
	vmName := fmt.Sprintf("vm-basic-%s", rName)
	vmNameUpdated := fmt.Sprintf("vm-basic-updated-%s", rName)
	movedGroupName := fmt.Sprintf("terraform-demo-group-moved-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_resource_group" "vm_group" {
				name = "terraform-demo-group"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_volume" "vm_storage" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				volume_type   = "SSD"
				size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id

				allocate_public_ip = false
				subnet_id          = gpcn_vpc_subnet.vm_subnet.id

				resource_group_id = gpcn_resource_group.vm_group.id

				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, volumeName, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_resource_group.vm_group", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_ssh_key.vm_uploaded_key", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_volume.vm_storage", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.country"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "configuration.cpu"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "configuration.ram"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "configuration.base_storage"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.#", "1"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "network_interfaces.0.vpc_subnet_id"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.world", "vpc"),
				),
			},
			// ImportState testing
			{
				ResourceName:            gpcnVirtualMachineTest,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated", "initial_auth.username"},
			},
			// Update and Read testing
			{
				Config: providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_resource_group" "vm_group" {
				name = "terraform-demo-group"
			}

			resource "gpcn_resource_group" "vm_group_moved" {
				name = "%s"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
				allocate_public_ip = false
				subnet_id          = gpcn_vpc_subnet.vm_subnet.id
				resource_group_id  = gpcn_resource_group.vm_group_moved.id
				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, movedGroupName, sshKeyName, vmNameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", vmNameUpdated),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_resource_group.vm_group_moved", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("l2_segment_ids"), knownvalue.ListSizeExact(0)),
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("network_interfaces"), knownvalue.ListSizeExact(1)),
					statecheck.CompareValuePairs(gpcnVirtualMachineTest, tfjsonpath.New("resource_group_id"), "gpcn_resource_group.vm_group_moved", tfjsonpath.New("id"), compare.ValuesSame()),
				},
			},
			// Changing image_id forces a replace
			{
				Config: providerConfig + `
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name         = "Chicago"
			}
			data "gpcn_virtualmachine_images" "vm_image" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				image_name    = "Alma Linux 9"
			}
			data "gpcn_virtualmachine_sizes" "vm_size" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				category      = "general-purpose"
				min_cpu       = 2
			}
			data "gpcn_resource_groups" "all" {}
			` + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_resource_group" "vm_group" {
				name = "terraform-demo-group"
			}

			resource "gpcn_resource_group" "vm_group_moved" {
				name = "%s"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
				allocate_public_ip = false
				subnet_id          = gpcn_vpc_subnet.vm_subnet.id
				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, movedGroupName, sshKeyName, vmNameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "image_id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs(gpcnVirtualMachineTest, tfjsonpath.New("resource_group_id"), "data.gpcn_resource_groups.all", tfjsonpath.New("default_resource_group_id"), compare.ValuesSame()),
				},
			},
		},
	})
}

func TestVirtualMachinesChangePublicIpAllocation(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcOctet := vpcTestOctetFor(t, t.Name())
	sshKeyName := fmt.Sprintf("vm-public-ip-key-%s", rName)
	vmName := fmt.Sprintf("vm-public-ip-%s", rName)

	vmConfig := func(sshKey, vm string, allocatePublicIp bool) string {
		return providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
			  image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
			  allocate_public_ip = %t
			  subnet_id          = gpcn_vpc_subnet.vm_subnet.id
			  initial_auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKey, vm, allocatePublicIp)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Set baseline
			{
				Config: vmConfig(sshKeyName, vmName, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Update allocate_public_ip to true
			{
				Config: vmConfig(sshKeyName, vmName, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("allocate_public_ip"), knownvalue.Bool(true)),
				},
			},
			// Release the IP
			{
				Config: vmConfig(sshKeyName, vmName, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("allocate_public_ip"), knownvalue.Bool(false)),
				},
			},
		},
	})
}

func TestVirtualMachinesSizeUpgrade(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcOctet := vpcTestOctetFor(t, t.Name())
	sshKeyName := fmt.Sprintf("vm-size-upgrade-key-%s", rName)
	vmName := fmt.Sprintf("vm-size-upgrade-%s", rName)

	vmConfig := func(sshKey, vm, sizeDataSource string) string {
		return providerConfig + `
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name         = "Chicago"
			}

			data "gpcn_virtualmachine_images" "vm_image" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				image_name    = "Alma Linux 8"
			}
		` + sizeDataSource + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
			  image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
			  allocate_public_ip = false
			  subnet_id          = gpcn_vpc_subnet.vm_subnet.id
			  initial_auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKey, vm)
	}

	microSize := `
			data "gpcn_virtualmachine_sizes" "vm_size" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				category      = "general-purpose"
				min_cpu       = 2
				min_memory_gb = 4
			}
	`
	smallSize := `
			data "gpcn_virtualmachine_sizes" "vm_size" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				category      = "general-purpose"
				min_cpu       = 4
			}
	`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with micro size
			{
				Config: vmConfig(sshKeyName, vmName, microSize),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("size_id"), knownvalue.NotNull()),
				},
			},
			// Upgrade to a larger size - should update in place
			{
				Config: vmConfig(sshKeyName, vmName, smallSize),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("size_id"), knownvalue.NotNull()),
				},
			},
			// Downgrade back to micro - should require replacement
			{
				Config: vmConfig(sshKeyName, vmName, microSize),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

func TestVirtualMachinesVolumeAttachment(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcOctet := vpcTestOctetFor(t, t.Name())
	sshKeyName := fmt.Sprintf("vm-vol-attach-key-%s", rName)
	vol1Name := fmt.Sprintf("vm-vol-attach-vol1-%s", rName)
	vol2Name := fmt.Sprintf("vm-vol-attach-vol2-%s", rName)
	vmName := fmt.Sprintf("vm-vol-attach-%s", rName)

	vmBase := func(sshKey, vol1, vol2, vm string) string {
		return providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
			  image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
			  allocate_public_ip = false
			  subnet_id          = gpcn_vpc_subnet.vm_subnet.id
			  initial_auth = {
			    ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
			    username   = "testuser"
			  }
			}
			`, sshKey, vol1, vol2, vm)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with no volumes
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume.vm_vol1", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_volume.vm_vol2", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Attach the first volume
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName) + `
			resource "gpcn_volume_attachment" "vol1_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol1.id
			}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume_attachment.vol1_attach", plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gpcn_volume_attachment.vol1_attach", "id"),
					resource.TestCheckResourceAttrSet("gpcn_volume_attachment.vol1_attach", "virtual_machine_id"),
					resource.TestCheckResourceAttrSet("gpcn_volume_attachment.vol1_attach", "volume_id"),
				),
			},
			// Attach second volume
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName) + `
			resource "gpcn_volume_attachment" "vol1_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol1.id
			}
			resource "gpcn_volume_attachment" "vol2_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol2.id
			  depends_on         = [gpcn_volume_attachment.vol1_attach]
			}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume_attachment.vol2_attach", plancheck.ResourceActionCreate),
					},
				},
			},
			// Detach first volume attachment by removing its attachment resource
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName) + `
			resource "gpcn_volume_attachment" "vol2_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol2.id
			}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume_attachment.vol1_attach", plancheck.ResourceActionDestroy),
					},
				},
			},
		},
	})
}

func TestVirtualMachinesAuth(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcOctet := vpcTestOctetFor(t, t.Name())
	sshKeyName := fmt.Sprintf("vm-auth-key-%s", rName)
	vmName := fmt.Sprintf("vm-auth-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with ssh_key_id and username
			{
				Config: providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`
			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id

				allocate_public_ip = false
				subnet_id          = gpcn_vpc_subnet.vm_subnet.id

				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "initial_auth.ssh_key_id"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "initial_auth.username", "testuser"),
				),
			},
			// Changing initial_auth is a no-op - state is updated with new config values but no API calls are made
			{
				Config: providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + fmt.Sprintf(`

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id

				allocate_public_ip = false
				subnet_id          = gpcn_vpc_subnet.vm_subnet.id

				initial_auth = {
					password = "Test1Password!"
					username = "newuser"
				}
			}
			`, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("initial_auth").AtMapKey("username"), knownvalue.StringExact("newuser")),
				},
			},
		},
	})
}

// GPCN takes both texts only at creation, so each change replaces the machine. The image
// must be one GPCN sets up, because only such an image takes first_boot_script.
func TestVirtualMachinesFirstBoot(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcOctet := vpcTestOctetFor(t, t.Name())
	sshKeyName := fmt.Sprintf("vm-first-boot-key-%s", rName)
	vmName := fmt.Sprintf("vm-first-boot-%s", rName)

	const (
		scriptOne = "#!/bin/sh\necho one > /root/first-boot.txt\n"
		scriptTwo = "#!/bin/sh\necho two > /root/first-boot.txt\n"
		userData  = "#cloud-config\nusers:\n  - name: firstboot\n    shell: /bin/bash\n"
	)

	sshKey := fmt.Sprintf(`
			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}
			`, sshKeyName)

	withScript := func(script string) string {
		return providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + sshKey + fmt.Sprintf(`
			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size_id   = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id  = data.gpcn_virtualmachine_images.vm_image.images[0].id
				subnet_id = gpcn_vpc_subnet.vm_subnet.id

				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
				first_boot_script = %q
			}
			`, vmName, script)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: withScript(scriptOne),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "first_boot_script", scriptOne),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "initial_auth.username", "testuser"),
				),
			},
			{
				Config: withScript(scriptTwo),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "first_boot_script", scriptTwo),
			},
			{
				Config: providerConfig + dataCenterImagesAndSize() + vpcAndSubnet(rName, vpcOctet) + sshKey + fmt.Sprintf(`
			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size_id   = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id  = data.gpcn_virtualmachine_images.vm_image.images[0].id
				subnet_id = gpcn_vpc_subnet.vm_subnet.id

				user_data = %q
			}
			`, vmName, userData),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "user_data", userData),
			},
		},
	})
}

/*
*
----- Unit tests -----
*
*/
func TestVirtualMachinesMissingSizeId(t *testing.T) {
	config := providerConfig + `
		resource "gpcn_virtualmachine" "test" {
		  name          = "terraform-volume-test-vm"
		  datacenter_id = "any-datacenter-id"
		  image_id           = "eb7da49d-cc71-480a-968d-fbf2841bedf7"
		  subnet_id          = "subnet-abc-123"
		  allocate_public_ip = false
		  initial_auth = {
		    ssh_key_id = "ssh-key-123"
		    username   = "testuser"
		  }
		}
		`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`The argument "size_id" is required`),
			},
		},
	})
}

func TestVirtualMachinesInvalidAuth(t *testing.T) {
	// vmConfigWithAuth is a helper that builds a minimal VM config with the given auth block content.
	vmConfigWithAuth := func(authBlock string) string {
		return providerConfig + `
		resource "gpcn_virtualmachine" "test" {
		  name          = "terraform-auth-test-vm"
		  datacenter_id = "any-datacenter-id"
		  size_id            = "sku-abc-123"
		  image_id         = "eb7da49d-cc71-480a-968d-fbf2841bedf7"
		  subnet_id          = "subnet-abc-123"
		  allocate_public_ip = false
		  initial_auth = {
		    username = "testuser"
		    ` + authBlock + `
		  }
		}
		`
	}

	// Password validation is tested directly in internal/virtualmachines/validators_test.go.
	// The cases below test schema-level constraints (framework validators) that require
	// a full provider roundtrip to exercise.
	tests := []struct {
		name      string
		authBlock string
		wantErr   string
	}{
		{
			name:      "ssh_key_id_and_password_conflict",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `password = "Test1Password!"`,
			wantErr:   "cannot be specified when",
		},
		{
			name:      "username_too_short",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "ab"`,
			wantErr:   "string length must be between",
		},
		{
			name:      "username_too_long",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "averylongusernamethatexceedslimit"`,
			wantErr:   "string length must be between",
		},
		{
			name:      "username_starts_with_digit",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "1testuser"`,
			wantErr:   "Username must start with a letter",
		},
		{
			name:      "username_invalid_chars", // spaces are not allowed
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "test user"`,
			wantErr:   "Username must start with a letter",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      vmConfigWithAuth(tc.authBlock),
						ExpectError: regexp.MustCompile(tc.wantErr),
					},
				},
			})
		})
	}
}

func TestVirtualMachinesFirstBootValidation(t *testing.T) {
	vmConfigWithFirstBoot := func(attributes string) string {
		return providerConfig + `
		resource "gpcn_virtualmachine" "test" {
		  name               = "terraform-first-boot-test-vm"
		  datacenter_id      = "any-datacenter-id"
		  size_id            = "sku-abc-123"
		  image_id           = "eb7da49d-cc71-480a-968d-fbf2841bedf7"
		  subnet_id          = "subnet-abc-123"
		  allocate_public_ip = false
		  ` + attributes + `
		}
		`
	}
	keyAuth := `initial_auth = {
		    ssh_key_id = "ssh-key-123"
		    username   = "testuser"
		  }`

	tests := []struct {
		name       string
		attributes string
		wantErr    string
	}{
		{
			name:       "user_data_with_initial_auth",
			attributes: `user_data = "#cloud-config"` + "\n" + keyAuth,
			wantErr:    `Attribute "initial_auth" cannot be specified when "user_data" is specified`,
		},
		{
			name:       "user_data_with_first_boot_script",
			attributes: `user_data = "#cloud-config"` + "\n" + `first_boot_script = "echo hi"`,
			wantErr:    `Attribute "first_boot_script" cannot be specified when "user_data" is specified`,
		},
		{
			name:       "empty_user_data",
			attributes: `user_data = ""`,
			wantErr:    `Attribute user_data string length must be at least 1, got: 0`,
		},
		{
			name:       "empty_first_boot_script",
			attributes: `first_boot_script = ""` + "\n" + keyAuth,
			wantErr:    `Attribute first_boot_script string length must be at least 1, got: 0`,
		},
		{
			name:       "user_data_over_16384_bytes",
			attributes: `user_data = "` + strings.Repeat("a", 16385) + `"`,
			wantErr:    fmt.Sprintf(virtualmachines.ErrDetailTextTooLong, 16384, 16385),
		},
		{
			name:       "first_boot_script_over_8192_bytes",
			attributes: `first_boot_script = "` + strings.Repeat("a", 8193) + `"` + "\n" + keyAuth,
			wantErr:    fmt.Sprintf(virtualmachines.ErrDetailTextTooLong, 8192, 8193),
		},
		{
			name:       "user_data_above_basic_plane",
			attributes: `user_data = "#cloud-config \U00010000"`,
			wantErr:    virtualmachines.ErrDetailUserDataCharacter,
		},
		{
			name: "empty_ssh_key_id",
			attributes: `initial_auth = {
			    ssh_key_id = ""
			    username   = "testuser"
			  }`,
			wantErr: `Attribute initial_auth.ssh_key_id string length must be at least 1, got: 0`,
		},
		{
			name: "username_alone",
			attributes: `initial_auth = {
			    username = "testuser"
			  }`,
			wantErr: `At least one attribute out of`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      vmConfigWithFirstBoot(tc.attributes),
						ExpectError: vmPlanTestSentencePattern(tc.wantErr),
					},
				},
			})
		})
	}
}

// The two address attributes carry the whole import consequence, and a release pins
// them. An imported machine records a held address. An operator who leaves that address
// out of the configuration loses it on the next apply.
func TestVirtualMachineAddressDescriptions(t *testing.T) {
	schemaResponse := &fwresource.SchemaResponse{}
	NewVirtualMachinesResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", schemaResponse.Diagnostics)
	}

	want := map[string]string{
		"allocate_public_ip": "Whether to acquire an elastic IP on the VPC that holds the birth interface and attach it to that interface. Changing this value in place needs the vpc-public-ip:create, vpc-public-ip:update and vpc-public-ip:delete permissions. Destroying the virtual machine releases an address acquired this way. Never inferred on import: an imported machine records its address as public_ip_id, so destroying it leaves the address held",
		"public_ip_id":       "ID of a held gpcn_vpc_public_ip to attach to the primary interface. Cannot be set together with allocate_public_ip. The address outlives the virtual machine, because your organization holds it. An import fills this from the address the primary interface carries. Name this address in the configuration after an import; otherwise the next apply detaches it. Import that address as a gpcn_vpc_public_ip too when Terraform should own its release. Do not name an address that a gpcn_vpc_public_ip_attachment also binds; one resource owns a binding",
	}
	for name, description := range want {
		attribute, ok := schemaResponse.Schema.Attributes[name]
		if !ok {
			t.Fatalf("Expected a %q attribute", name)
		}
		if got := attribute.GetDescription(); got != description {
			t.Errorf("Expected %q description %q, got %q", name, description, got)
		}
	}
}

// An import fills l2_segment_ids from the interfaces, and the attribute carries a
// default of the empty list. A configuration that leaves it out therefore detaches
// every segment on the next apply. The release pins the sentence that says so.
func TestVirtualMachineSegmentIdsDescription(t *testing.T) {
	schemaResponse := &fwresource.SchemaResponse{}
	NewVirtualMachinesResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", schemaResponse.Diagnostics)
	}

	const want = "IDs of the L2 segments the virtual machine carries. They attach after the machine is created, and the machine is stopped for a change unless its image supports network hotplug. After an import, name the segments the machine carries; otherwise the next apply detaches them. Maximum of 4, because the birth subnet interface holds one of the five interfaces GPCN™ allows"

	attribute, ok := schemaResponse.Schema.Attributes["l2_segment_ids"]
	if !ok {
		t.Fatal("Expected an l2_segment_ids attribute")
	}
	if got := attribute.GetDescription(); got != want {
		t.Errorf("Expected the description %q, got %q", want, got)
	}
}
