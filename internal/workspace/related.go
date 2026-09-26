package workspace

import (
	"path"
	"sort"
	"strings"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/scan"
)

// Suggestion is a related script, with the reasoning that surfaced it.
type Suggestion struct {
	Entry scan.Entry `json:"entry"`
	// Reason is a short human-readable explanation. Suggestions the user
	// cannot interrogate are indistinguishable from noise, so the reason is
	// always populated.
	Reason string `json:"reason"`
	Score  int    `json:"score"`
}

// Suggestion scoring. The bands are chosen so that a script in the same
// directory outranks one that merely shares a name token, and a same-name
// sibling outranks a same-directory script in a different language.
const (
	scoreSameDirSameLang  = 100
	scoreSameStem         = 80
	scoreSameDir          = 40
	scoreSharedNameToken  = 20
	scoreSameLangSameRoot = 10

	// suggestionLimit is the number of suggestions returned. The panel is a
	// navigation aid, not a search result list, so a small fixed set is more
	// useful than a long one.
	suggestionLimit = 6
)

// Suggest returns the scripts most closely related to target, most relevant
// first. Entries are only ever suggested within the same workspace root:
// suggesting a script from a different root would cross a boundary the user
// drew on purpose.
func Suggest(target scan.Entry, all []scan.Entry, limit int) []Suggestion {
	if limit <= 0 {
		limit = suggestionLimit
	}

	targetStem := nameStem(target.Name)
	targetTokens := nameTokens(target.Name)

	var out []Suggestion
	for _, e := range all {
		if e.Rel == target.Rel && e.Root == target.Root {
			continue
		}
		if e.Root != target.Root {
			continue
		}
		// A project is a container, not a peer script, so it is never
		// suggested alongside the scripts it contains.
		if e.Kind != scan.KindScript {
			continue
		}

		if score, reason := score(target, e, targetStem, targetTokens); score > 0 {
			out = append(out, Suggestion{Entry: e, Score: score, Reason: reason})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		// A deterministic tiebreak keeps the panel from reshuffling between
		// refreshes when nothing meaningful distinguishes two candidates.
		return out[i].Entry.Rel < out[j].Entry.Rel
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// score rates a candidate against the target and explains the rating.
func score(target, candidate scan.Entry, targetStem string, targetTokens map[string]bool) (int, string) {
	candidateStem := nameStem(candidate.Name)
	sameDir := target.Dir == candidate.Dir

	switch {
	case sameDir && target.Lang == candidate.Lang:
		return scoreSameDirSameLang, "same folder and language"

	case targetStem != "" && strings.EqualFold(targetStem, candidateStem):
		// The same script in two languages, most often a bash original and its
		// PowerShell port.
		return scoreSameStem, "same name, " + candidate.Lang.DisplayName()

	case sameDir:
		return scoreSameDir, "same folder"

	case target.Lang == candidate.Lang && target.Lang != detect.Unknown:
		if shared := sharedToken(targetTokens, nameTokens(candidate.Name)); shared != "" {
			return scoreSharedNameToken, "shares \"" + shared + "\" in the name"
		}
		return scoreSameLangSameRoot, "also " + candidate.Lang.DisplayName()
	}
	return 0, ""
}

// nameStem strips the extension, so "backup.sh" and "backup.ps1" share a stem.
func nameStem(name string) string {
	ext := path.Ext(name)
	if ext == "" {
		return name
	}
	return strings.TrimSuffix(name, ext)
}

// nameTokens splits a filename into lowercase words, treating the separators
// that appear in real filenames as word boundaries.
func nameTokens(name string) map[string]bool {
	base := nameStem(name)
	fields := strings.FieldsFunc(base, func(r rune) bool {
		switch r {
		case '-', '_', '.', ' ', '+':
			return true
		}
		return false
	})

	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f = strings.ToLower(f); f != "" {
			out[f] = true
		}
	}
	return out
}

// sharedToken returns a word appearing in both names, for use in the reason
// text. Very short words are skipped because they collide constantly.
func sharedToken(a, b map[string]bool) string {
	var best string
	for word := range a {
		if !b[word] || len(word) < 3 {
			continue
		}
		if word > best {
			best = word
		}
	}
	return best
}
