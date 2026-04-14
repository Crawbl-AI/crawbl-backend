// Package ptr provides helpers for working with pointers.
package ptr

// Deref returns the value pointed to by p, or the zero value of T if p is nil.
// Use this to replace ad-hoc "if p == nil { return \"\" } return *p" functions
// scattered across the codebase.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// Of returns a pointer to v. Useful when passing a literal into a function that
// expects an optional/pointer parameter, e.g. proto optional fields.
func Of[T any](v T) *T {
	return &v
}
