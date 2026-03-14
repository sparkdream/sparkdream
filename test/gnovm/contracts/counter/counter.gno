package counter

import "strconv"

var count int

func Increment(_ realm) int {
	count++
	return count
}

func Decrement(_ realm) int {
	count--
	return count
}

func Reset(_ realm) {
	count = 0
}

func GetCount() int {
	return count
}

func Render(_ string) string {
	return strconv.Itoa(count)
}
