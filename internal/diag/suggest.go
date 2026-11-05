package diag

// Suggest returns the candidate closest to word, if one is close enough to
// be a likely typo: at most two edits away, and at most half the length of
// word. Swapping two adjacent letters counts as one edit.
func Suggest(word string, candidates []string) (string, bool) {
	best, bestDist := "", -1
	limit := min(2, len(word)/2)
	for _, c := range candidates {
		if c == word {
			continue
		}
		d := distance(word, c)
		if d <= limit && (bestDist < 0 || d < bestDist) {
			best, bestDist = c, d
		}
	}
	return best, bestDist >= 0
}

// distance is the optimal string alignment distance between a and b: the
// Levenshtein distance plus transposition of adjacent bytes.
func distance(a, b string) int {
	rows := make([][]int, len(a)+1)
	for i := range rows {
		rows[i] = make([]int, len(b)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d := min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d = min(d, rows[i-2][j-2]+1)
			}
			rows[i][j] = d
		}
	}
	return rows[len(a)][len(b)]
}
