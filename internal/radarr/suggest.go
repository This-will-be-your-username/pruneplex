package radarr

import (
	"sort"
	"strings"
	"unicode"
)

// minSuggestScore is the lowest similarity for a movie to be offered as a candidate.
const minSuggestScore = 0.3

// Suggest ranks movies by how likely they are to be the same film as identity,
// using fuzzy title similarity and year proximity. It is used to offer a
// shortlist when exact ID/title matching finds nothing. At most limit movies
// are returned, best first.
func Suggest(identity Identity, movies []Movie, limit int) []Movie {
	target := looseTitle(identity.Title)
	if target == "" || limit <= 0 {
		return nil
	}
	type scored struct {
		movie Movie
		score float64
	}
	var results []scored
	for _, movie := range movies {
		score := titleSimilarity(target, looseTitle(movie.Title))
		if score < minSuggestScore {
			continue
		}
		if identity.Year > 0 && movie.Year > 0 {
			switch diff := abs(identity.Year - movie.Year); {
			case diff == 0:
				score += 0.3
			case diff == 1:
				score += 0.15
			case diff > 5:
				score -= 0.15
			}
		}
		results = append(results, scored{movie, score})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].movie.Title < results[j].movie.Title
	})
	if len(results) > limit {
		results = results[:limit]
	}
	out := make([]Movie, len(results))
	for i, r := range results {
		out[i] = r.movie
	}
	return out
}

// looseTitle lowercases, strips punctuation and a leading article so
// "The Lord of the Rings: ..." and "Lord of the Rings - ..." compare closely.
func looseTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	fields := strings.Fields(b.String())
	if len(fields) > 1 && (fields[0] == "the" || fields[0] == "a" || fields[0] == "an") {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

// titleSimilarity is the Dice coefficient over character bigrams (0..1), with
// a boost when one title contains the other (e.g. a subtitle was dropped).
func titleSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	score := dice(bigrams(a), bigrams(b))
	if strings.Contains(a, b) || strings.Contains(b, a) {
		if score < 0.7 {
			score = 0.7
		}
	}
	return score
}

func bigrams(s string) map[string]int {
	runes := []rune(s)
	grams := make(map[string]int)
	if len(runes) < 2 {
		grams[s]++
		return grams
	}
	for i := 0; i < len(runes)-1; i++ {
		grams[string(runes[i:i+2])]++
	}
	return grams
}

func dice(a, b map[string]int) float64 {
	var overlap, total int
	for gram, n := range a {
		total += n
		if m := b[gram]; m > 0 {
			if m < n {
				overlap += m
			} else {
				overlap += n
			}
		}
	}
	for _, n := range b {
		total += n
	}
	if total == 0 {
		return 0
	}
	return 2 * float64(overlap) / float64(total)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
