// Package pagination holds generic slice-pagination helpers.
package pagination

// SlicePage returns the offset-limit window of items along with the total
// count. Callers typically use this to build OffsetPaginationResponse DTOs
// without reimplementing bounds math.
func SlicePage[T any](items []T, offset, limit int) (page []T, total int) {
	total = len(items)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return nil, total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return items[offset:end], total
}
