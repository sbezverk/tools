package sort

type Comparable interface {
	~int | ~string
}

func merge[T Comparable](s []T, low, mid, high int, temp []T) {
	for k := low; k <= high; k++ {
		temp[k] = s[k]
	}
	i := low
	j := mid + 1
	for k := low; k <= high; k++ {
		if i > mid {
			s[k] = temp[j]
			j++
		} else if j > high {
			s[k] = temp[i]
			i++
		} else if temp[j] < temp[i] {
			s[k] = temp[j]
			j++
		} else {
			s[k] = temp[i]
			i++
		}
	}
}

func sort[T Comparable](s []T, low, high int, temp []T) {
	if high <= low {
		return
	}
	mid := low + (high-low)/2
	sort(s, low, mid, temp)
	sort(s, mid+1, high, temp)
	if s[mid] <= s[mid+1] {
		return
	}
	merge(s, low, mid, high, temp)
}

func SortMergeComparableSlice[T Comparable](s []T) []T {
	temp := make([]T, len(s))
	sort(s, 0, len(s)-1, temp)
	return s
}
