package model

import (
	"time"
	"unicode"
)

// DurationThreshold is the min video length in seconds to satify 179s >= 3 minutes
const DurationThreshold = 4*60 - 1

type Video struct {
	URL      string `bson:"_id"`
	Title    string `bson:"title"`
	Date     string `bson:"date"`
	Duration int    `bson:"duration"` // seconds

	// Individual match flags - for additional query in MongoDB
	MatchDate     bool `bson:"match_date"`
	MatchDuration bool `bson:"match_duration"`
	HasCJKChar    bool `bson:"has_cjk_char"`

	// Stored as metadata - for additional query in MongoDB
	HasNonEnglishChar bool `bson:"has_non_english_char"`

	// IsTarget = MatchDate && HasCJKChar && MatchDuration
	IsTarget bool `bson:"is_target"`
}

// Match evaluates all conditions and set flags in place
func (v *Video) Match(cutoffDate time.Time) {
	v.MatchDate = v.matchesCutoffDate(cutoffDate)
	v.MatchDuration = v.Duration > DurationThreshold
	v.HasCJKChar = hasCJK(v.Title)
	v.HasNonEnglishChar = hasNonLatin(v.Title)
	v.IsTarget = v.MatchDate && v.MatchDuration && v.HasCJKChar
}

func (v *Video) matchesCutoffDate(cutoffDate time.Time) bool {
	if v.Date == "" {
		return false
	}
	formats := []string{
		"Jan 2, 2006",
		"January 2, 2006",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, v.Date); err != nil {
			return !t.After(cutoffDate)
		}
	}
	return false
}

func hasCJK(title string) bool {
	for _, ch := range title {
		if unicode.In(ch,
			unicode.Han,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Hangul,
		) {
			return true
		}
	}
	return false
}

func hasNonLatin(title string) bool {
	for _, ch := range title {
		if unicode.IsLetter(ch) && ch > '\u024F' {
			return true
		}
	}
	return false
}
