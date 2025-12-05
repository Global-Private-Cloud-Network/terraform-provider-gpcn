package helpers

import "slices"

// Helper function to check a list for differences and return added and removed values
func CheckListForDifferences(oldList, newList []string) ([]string, []string) {
	// Check for any values in newList that are NOT in oldList. These are our "added" values
	var addedValues []string
	for _, elem := range newList {
		if !slices.Contains(oldList, elem) {
			addedValues = append(addedValues, elem)
		}
	}

	// Check for any values in oldList that are NOT in newList. These are our "removed" values
	var removedValues []string
	for _, elem := range oldList {
		if !slices.Contains(newList, elem) {
			removedValues = append(removedValues, elem)
		}
	}

	return addedValues, removedValues
}

// Helper function to join strings with comma separator
func JoinStrings(strs []string) string {
	result := ""
	for i, str := range strs {
		if i > 0 {
			result += ","
		}
		result += str
	}
	return result
}
