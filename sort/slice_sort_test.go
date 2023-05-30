package sort

import (
	"reflect"
	"testing"

	"github.com/go-test/deep"
)

func TestSortComparableSlice(t *testing.T) {
	tests := []struct {
		name     string
		unsorted []string
		expected []string
	}{
		{
			name:     "nil slice",
			unsorted: nil,
			expected: nil,
		},
		{
			name:     "empty slice",
			unsorted: []string{},
			expected: []string{},
		},
		{
			name:     "valid slice with 1 element",
			unsorted: []string{"A"},
			expected: []string{"A"},
		},
		{
			name:     "valid slice with 2 elements",
			unsorted: []string{"B", "A"},
			expected: []string{"A", "B"},
		},
		{
			name:     "valid slice with 3 elements",
			unsorted: []string{"A", "C", "B"},
			expected: []string{"A", "B", "C"},
		},
		{
			name:     "valid slice with 4 elements",
			unsorted: []string{"D", "A", "C", "B"},
			expected: []string{"A", "B", "C", "D"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorted := SortMergeComparableSlice(tt.unsorted)
			if !reflect.DeepEqual(sorted, tt.expected) {
				t.Logf("Diffs: %+v", deep.Equal(sorted, tt.expected))
				t.Fatal("expected and computed result do not match")
			}
		})
	}
}
