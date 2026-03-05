package virtualmachines

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300
var DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS int = 300

// VMStatus represents the lifecycle status of a virtual machine
type VMStatus string

// Virtual Machine lifecycle statuses
const (
	VMStatusRunning VMStatus = "Running"
	VMStatusShutoff VMStatus = "Shutoff"
)

// String returns the string representation of VMStatus
func (s VMStatus) String() string {
	return string(s)
}

// Category represents a virtual machine size category
type Category string

// Virtual Machine categories
const (
	CategoryGeneral Category = "general"
	CategoryMemory  Category = "memory"
)

// String returns the string representation of Category
func (c Category) String() string {
	return string(c)
}

// IsValid returns true if the category is a known valid value
func (c Category) IsValid() bool {
	switch c {
	case CategoryGeneral, CategoryMemory:
		return true
	default:
		return false
	}
}

// AllCategories returns all valid categories
func AllCategories() []Category {
	return []Category{CategoryGeneral, CategoryMemory}
}

// AllCategoryStrings returns all valid category strings for validation
func AllCategoryStrings() []string {
	return []string{string(CategoryGeneral), string(CategoryMemory)}
}

// Tier represents a virtual machine size tier
type Tier string

// General purpose tiers (g- prefix) in ascending order by size
const (
	TierGeneralMicro  Tier = "g-micro-1"
	TierGeneralSmall  Tier = "g-small-1"
	TierGeneralMedium Tier = "g-medium-1"
	TierGeneralLarge  Tier = "g-large-1"
	TierGeneralXL     Tier = "g-xl-1"
)

// Memory optimized tiers (m- prefix) in ascending order by size
const (
	TierMemoryMicro  Tier = "m-micro-1"
	TierMemorySmall  Tier = "m-small-1"
	TierMemoryMedium Tier = "m-medium-1"
	TierMemoryLarge  Tier = "m-large-1"
	TierMemoryXL     Tier = "m-xl-1"
)

// String returns the string representation of Tier
func (t Tier) String() string {
	return string(t)
}

// Category returns the category for this tier
func (t Tier) Category() Category {
	switch t {
	case TierGeneralMicro, TierGeneralSmall, TierGeneralMedium, TierGeneralLarge, TierGeneralXL:
		return CategoryGeneral
	case TierMemoryMicro, TierMemorySmall, TierMemoryMedium, TierMemoryLarge, TierMemoryXL:
		return CategoryMemory
	default:
		return ""
	}
}

// Virtual Machine size tiers in ascending order (smallest to largest)
var (
	// General purpose tiers (g- prefix) in ascending order by size
	GeneralTiers = []string{
		string(TierGeneralMicro),
		string(TierGeneralSmall),
		string(TierGeneralMedium),
		string(TierGeneralLarge),
		string(TierGeneralXL),
	}

	// Memory optimized tiers (m- prefix) in ascending order by size
	MemoryTiers = []string{
		string(TierMemoryMicro),
		string(TierMemorySmall),
		string(TierMemoryMedium),
		string(TierMemoryLarge),
		string(TierMemoryXL),
	}

	// AllTiers combines all tiers for validation purposes
	AllTiers = append(append([]string{}, GeneralTiers...), MemoryTiers...)
)

// TiersForCategory returns the ordered tiers for a given category
func TiersForCategory(category Category) []Tier {
	switch category {
	case CategoryGeneral:
		return []Tier{TierGeneralMicro, TierGeneralSmall, TierGeneralMedium, TierGeneralLarge, TierGeneralXL}
	case CategoryMemory:
		return []Tier{TierMemoryMicro, TierMemorySmall, TierMemoryMedium, TierMemoryLarge, TierMemoryXL}
	default:
		return nil
	}
}

// Valid virtual machine image names
var ValidImageNames = []string{
	"Ubuntu 20.04 LTS",
	"NetBSD 10.x",
	"OpenSUSE Leap 15.x JeOS. Cloud",
	"OpenSUSE Leap 15.x Minimal VM. Cloud",
	"OPNSense 25.x",
	"PFSense CE 2.7.2",
	"Rocky 8.x",
	"Rocky 9.x",
	"Rocky 10.x",
	"Ubuntu 18.04 LTS",
	"Gentoo",
	"Ubuntu 22.04 LTS",
	"Ubuntu 24.04 LTS",
	"Ubuntu 25.04",
	"Windows 2012 Standard",
	"Windows 2016 Standard",
	"Windows 2019 Standard",
	"Windows 2022 Standard",
	"Windows 2025 Standard",
	"Windows 11 Pro (BYOL)",
	"Cloudlinux 9.5",
	"Alma Linux 9.x",
	"Alma Linux 10.x",
	"Alpine 3.x",
	"Arch Linux Cloudimg",
	"CentOS Stream 10.x",
	"CentOS Stream 8.x",
	"CentOS Stream 9.x",
	"Cirros 0.6.2",
	"Cirros 0.6.3",
	"Alma Linux 8.x",
	"Cloudlinux 9.5 Cpanel",
	"Coriolis Appliance",
	"Debian 10.x",
	"Debian 11.x",
	"Debian 12.x",
	"Debian 13.x",
	"Fedora CoreOS k8saas",
	"Fedora Generic 42.x",
	"FreeBSD 14.x",
}
