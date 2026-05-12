package virtualmachines

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300
var DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS int = 300
var DEFAULT_INITIAL_POLL_DELAY_SECONDS int = 30

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

// General purpose tiers (G- prefix) in ascending order by size
const (
	TierGeneralMicro  Tier = "G-Micro-1"
	TierGeneralSmall  Tier = "G-Small-1"
	TierGeneralMedium Tier = "G-Medium-1"
	TierGeneralLarge  Tier = "G-Large-1"
	TierGeneralXL     Tier = "G-Extra Large-1"
)

// Memory optimized tiers (M- prefix) in ascending order by size
const (
	TierMemoryMicro  Tier = "M-Micro-1"
	TierMemorySmall  Tier = "M-Small-1"
	TierMemoryMedium Tier = "M-Medium-1"
	TierMemoryLarge  Tier = "M-Large-1"
	TierMemoryXL     Tier = "M-Extra Large-1"
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
	// General purpose tiers (G- prefix) in ascending order by size
	GeneralTiers = []string{
		string(TierGeneralMicro),
		string(TierGeneralSmall),
		string(TierGeneralMedium),
		string(TierGeneralLarge),
		string(TierGeneralXL),
	}

	// Memory optimized tiers (M- prefix) in ascending order by size
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
	"Acronis",
	"Alma Linux 10.x",
	"Alma Linux 8.x",
	"Alma Linux 9.x",
	"Alpine 3.x",
	"Debian 11.x",
	"Debian 12.x",
	"Fedora 43",
	"OPNSense 26.x",
	"PFSense CE 2.7.2",
	"Rescuezilla 2.6.1",
	"Rocky 10.x",
	"Rocky 8.x",
	"Rocky 9.x",
	"Ubuntu 22.04 LTS",
	"Ubuntu 24.04 LTS",
	"Ubuntu 26.04 LTS",
	"Veeam Recovery",
	"Windows 2019 Standard",
	"Windows 2022 Standard",
	"Windows 2025 Standard",
}
