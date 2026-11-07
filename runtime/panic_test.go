package vibe

// divide panics on a line that a line directive maps to books.vibe:12,
// as generated code does.
func divide(c *Ctx) error {
	zero := int64(len(c.r.URL.RawQuery))
//line books.vibe:12
	n := 1 / zero
	return c.OK(n)
}
