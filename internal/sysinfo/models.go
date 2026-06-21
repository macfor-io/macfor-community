// Package sysinfo provides system information utilities for macfor.
package sysinfo

// HardwareModels provides a comprehensive mapping of Mac model identifiers
// to human-readable marketing names. This database covers models from 2012 onwards.
//
// Model identifiers are obtained via `sysctl hw.model` on macOS.
// Data sourced from Apple's support documentation and EveryMac.com.
var HardwareModels = map[string]string{
	// ==================== Apple Silicon Macs ====================

	// M4 MacBook Pro (2024)
	"Mac16,1":  "MacBook Pro (14-inch, M4, 2024)",
	"Mac16,5":  "MacBook Pro (14-inch, M4 Pro, 2024)",
	"Mac16,6":  "MacBook Pro (14-inch, M4 Max, 2024)",
	"Mac16,7":  "MacBook Pro (16-inch, M4 Pro, 2024)",
	"Mac16,8":  "MacBook Pro (16-inch, M4 Max, 2024)",
	"Mac16,10": "MacBook Pro (14-inch, M4 Pro, Nov 2024)",
	"Mac16,11": "MacBook Pro (16-inch, M4 Max, Nov 2024)",

	// M4 iMac (2024)
	"Mac16,2": "iMac (24-inch, M4, 2024)",
	"Mac16,3": "iMac (24-inch, M4, 2024)",

	// M4 Mac mini (2024)
	"Mac16,12": "Mac mini (M4, 2024)",
	"Mac16,13": "Mac mini (M4 Pro, 2024)",

	// M3 MacBook Air (2024)
	"Mac15,12": "MacBook Air (13-inch, M3, 2024)",
	"Mac15,13": "MacBook Air (15-inch, M3, 2024)",

	// M3 MacBook Pro (2023)
	"Mac15,3":  "MacBook Pro (14-inch, M3, 2023)",
	"Mac15,6":  "MacBook Pro (14-inch, M3 Pro, 2023)",
	"Mac15,8":  "MacBook Pro (14-inch, M3 Max, 2023)",
	"Mac15,7":  "MacBook Pro (16-inch, M3 Pro, 2023)",
	"Mac15,9":  "MacBook Pro (16-inch, M3 Max, 2023)",
	"Mac15,10": "MacBook Pro (14-inch, M3 Pro, Nov 2023)",
	"Mac15,11": "MacBook Pro (16-inch, M3 Max, Nov 2023)",

	// M3 iMac (2023)
	"Mac15,4": "iMac (24-inch, M3, 2023)",
	"Mac15,5": "iMac (24-inch, M3, 2023)",

	// M2 MacBook Air (2022-2023)
	"Mac14,2":  "MacBook Air (M2, 2022)",
	"Mac14,15": "MacBook Air (15-inch, M2, 2023)",

	// M2 MacBook Pro (2022-2023)
	"Mac14,7":  "MacBook Pro (13-inch, M2, 2022)",
	"Mac14,5":  "MacBook Pro (14-inch, M2 Pro, 2023)",
	"Mac14,6":  "MacBook Pro (16-inch, M2 Pro, 2023)",
	"Mac14,9":  "MacBook Pro (14-inch, M2 Max, 2023)",
	"Mac14,10": "MacBook Pro (16-inch, M2 Max, 2023)",

	// M2 Mac mini (2023)
	"Mac14,3":  "Mac mini (M2, 2023)",
	"Mac14,12": "Mac mini (M2 Pro, 2023)",

	// M2 Mac Studio (2023)
	"Mac14,13": "Mac Studio (M2 Max, 2023)",
	"Mac14,14": "Mac Studio (M2 Ultra, 2023)",

	// M2 Mac Pro (2023)
	"Mac14,8": "Mac Pro (M2 Ultra, 2023)",

	// M1 MacBook Air (2020)
	"MacBookAir10,1": "MacBook Air (M1, 2020)",

	// M1 MacBook Pro (2020-2021)
	"MacBookPro17,1": "MacBook Pro (13-inch, M1, 2020)",
	"MacBookPro18,1": "MacBook Pro (16-inch, M1 Pro, 2021)",
	"MacBookPro18,2": "MacBook Pro (16-inch, M1 Max, 2021)",
	"MacBookPro18,3": "MacBook Pro (14-inch, M1 Pro, 2021)",
	"MacBookPro18,4": "MacBook Pro (14-inch, M1 Max, 2021)",

	// M1 Mac mini (2020)
	"Macmini9,1": "Mac mini (M1, 2020)",

	// M1 iMac (2021)
	"iMac21,1": "iMac (24-inch, M1, 2021)",
	"iMac21,2": "iMac (24-inch, M1, 2021)",

	// M1 Mac Studio (2022)
	"Mac13,1": "Mac Studio (M1 Max, 2022)",
	"Mac13,2": "Mac Studio (M1 Ultra, 2022)",

	// ==================== Intel MacBook Pro ====================

	// 2020
	"MacBookPro16,2": "MacBook Pro (13-inch, 2020, Four Thunderbolt 3)",
	"MacBookPro16,3": "MacBook Pro (13-inch, 2020, Two Thunderbolt 3)",

	// 2019
	"MacBookPro16,1": "MacBook Pro (16-inch, 2019)",
	"MacBookPro16,4": "MacBook Pro (16-inch, 2019)",
	"MacBookPro15,3": "MacBook Pro (15-inch, 2019)",
	"MacBookPro15,4": "MacBook Pro (13-inch, 2019, Two Thunderbolt 3)",

	// 2018-2019
	"MacBookPro15,1": "MacBook Pro (15-inch, 2018/2019)",
	"MacBookPro15,2": "MacBook Pro (13-inch, 2018/2019)",

	// 2017
	"MacBookPro14,1": "MacBook Pro (13-inch, 2017, Two Thunderbolt 3)",
	"MacBookPro14,2": "MacBook Pro (13-inch, 2017, Four Thunderbolt 3)",
	"MacBookPro14,3": "MacBook Pro (15-inch, 2017)",

	// 2016
	"MacBookPro13,1": "MacBook Pro (13-inch, 2016, Two Thunderbolt 3)",
	"MacBookPro13,2": "MacBook Pro (13-inch, 2016, Four Thunderbolt 3)",
	"MacBookPro13,3": "MacBook Pro (15-inch, 2016)",

	// 2015
	"MacBookPro12,1": "MacBook Pro (Retina, 13-inch, Early 2015)",
	"MacBookPro11,4": "MacBook Pro (Retina, 15-inch, Mid 2015)",
	"MacBookPro11,5": "MacBook Pro (Retina, 15-inch, Mid 2015)",

	// 2013-2014
	"MacBookPro11,1": "MacBook Pro (Retina, 13-inch, Late 2013/Mid 2014)",
	"MacBookPro11,2": "MacBook Pro (Retina, 15-inch, Late 2013/Mid 2014)",
	"MacBookPro11,3": "MacBook Pro (Retina, 15-inch, Late 2013/Mid 2014)",

	// 2012
	"MacBookPro10,1": "MacBook Pro (Retina, 15-inch, Mid 2012/Early 2013)",
	"MacBookPro10,2": "MacBook Pro (Retina, 13-inch, Late 2012/Early 2013)",
	"MacBookPro9,1":  "MacBook Pro (15-inch, Mid 2012)",
	"MacBookPro9,2":  "MacBook Pro (13-inch, Mid 2012)",

	// ==================== Intel MacBook Air ====================

	// 2020
	"MacBookAir9,1": "MacBook Air (Retina, 13-inch, 2020)",

	// 2019
	"MacBookAir8,2": "MacBook Air (Retina, 13-inch, 2019)",

	// 2018
	"MacBookAir8,1": "MacBook Air (Retina, 13-inch, 2018)",

	// 2017
	"MacBookAir7,2": "MacBook Air (13-inch, 2017)",
	"MacBookAir7,1": "MacBook Air (11-inch, Early 2015)",

	// 2015
	"MacBookAir6,2": "MacBook Air (13-inch, Mid 2013/Early 2014)",
	"MacBookAir6,1": "MacBook Air (11-inch, Mid 2013/Early 2014)",

	// 2012
	"MacBookAir5,2": "MacBook Air (13-inch, Mid 2012)",
	"MacBookAir5,1": "MacBook Air (11-inch, Mid 2012)",

	// ==================== Intel MacBook ====================

	"MacBook10,1": "MacBook (Retina, 12-inch, 2017)",
	"MacBook9,1":  "MacBook (Retina, 12-inch, Early 2016)",
	"MacBook8,1":  "MacBook (Retina, 12-inch, Early 2015)",

	// ==================== Intel iMac ====================

	// 2020
	"iMac20,1": "iMac (Retina 5K, 27-inch, 2020)",
	"iMac20,2": "iMac (Retina 5K, 27-inch, 2020)",

	// 2019
	"iMac19,1": "iMac (Retina 5K, 27-inch, 2019)",
	"iMac19,2": "iMac (Retina 4K, 21.5-inch, 2019)",

	// 2017
	"iMac18,1": "iMac (21.5-inch, 2017)",
	"iMac18,2": "iMac (Retina 4K, 21.5-inch, 2017)",
	"iMac18,3": "iMac (Retina 5K, 27-inch, 2017)",

	// 2015
	"iMac17,1": "iMac (Retina 5K, 27-inch, Late 2015)",
	"iMac16,1": "iMac (21.5-inch, Late 2015)",
	"iMac16,2": "iMac (Retina 4K, 21.5-inch, Late 2015)",

	// 2014
	"iMac15,1": "iMac (Retina 5K, 27-inch, Late 2014/Mid 2015)",
	"iMac14,4": "iMac (21.5-inch, Mid 2014)",

	// 2013
	"iMac14,1": "iMac (21.5-inch, Late 2013)",
	"iMac14,2": "iMac (27-inch, Late 2013)",
	"iMac14,3": "iMac (21.5-inch, Late 2013)",

	// 2012
	"iMac13,1": "iMac (21.5-inch, Late 2012/Early 2013)",
	"iMac13,2": "iMac (27-inch, Late 2012)",

	// ==================== iMac Pro ====================

	"iMacPro1,1": "iMac Pro (2017)",

	// ==================== Intel Mac mini ====================

	// 2018
	"Macmini8,1": "Mac mini (2018)",

	// 2014
	"Macmini7,1": "Mac mini (Late 2014)",

	// 2012
	"Macmini6,1": "Mac mini (Late 2012)",
	"Macmini6,2": "Mac mini (Late 2012)",

	// ==================== Intel Mac Pro ====================

	// 2019
	"MacPro7,1": "Mac Pro (2019)",

	// 2013
	"MacPro6,1": "Mac Pro (Late 2013)",

	// 2010-2012
	"MacPro5,1": "Mac Pro (Mid 2010/Mid 2012)",
}

// GetModelName returns the human-readable name for a hardware model identifier.
// Falls back to the model identifier itself if no mapping exists.
// This provides a more comprehensive lookup than the public API subset.
func GetModelName(model string) string {
	if name, ok := HardwareModels[model]; ok {
		return name
	}
	return model
}

// GetModelGeneration returns the chip generation for a model identifier.
// Returns "Unknown" for unrecognized models.
func GetModelGeneration(model string) string {
	// Apple Silicon generation detection based on model prefix patterns.
	switch {
	case hasPrefix(model, "Mac16"):
		return "M4"
	case hasPrefix(model, "Mac15"):
		return "M3"
	case hasPrefix(model, "Mac14"):
		return "M2"
	case hasPrefix(model, "Mac13"):
		return "M1 Ultra"
	case hasPrefix(model, "MacBookAir10") ||
		hasPrefix(model, "MacBookPro17") ||
		hasPrefix(model, "MacBookPro18") ||
		hasPrefix(model, "Macmini9") ||
		hasPrefix(model, "iMac21"):
		return "M1"
	default:
		// Intel or unknown.
		return "Intel"
	}
}

// IsAppleSilicon returns true if the model identifier represents an Apple Silicon Mac.
func IsAppleSilicon(model string) bool {
	gen := GetModelGeneration(model)
	return gen != "Intel" && gen != "Unknown"
}

// hasPrefix checks if s starts with prefix without importing strings.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
