package server

import (
	"context"
	"html"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

type videoNames struct {
	Title    string
	Channel  string
	Track    string
	Artists  []string
	Duration float64
}

var (
	videoBracketRe  = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	videoNoiseRe    = regexp.MustCompile(`(?i)\b(official|officiel|oficial|ufficiale|offizielles?|lyrics?|letra|testo|paroles|audio|video|videoclip|clip|hd|hq|4k|8k|1080p|720p|uhd|visuali[sz]er|mv|m/v|full album|free download|explicit|clean|color coded|legendado|subtitulado|sub español|tradu[cç][aã]o)\b`)
	videoTailRe     = regexp.MustCompile(`(?i)\s+(?:[-|:]\s*)?(?:the\s+)?(?:official(?:\s+(?:music|lyrics?))?(?:\s+(?:video|audio|videoclip|visuali[sz]er|mv))?|(?:music|lyrics?)\s+video)\s*$|\s+(?:with\s+|w/\s*)?(?:lyrics|letra)\s*$|\s+(?:hd|hq|4k|8k|1080p|720p)\s*$|\s+#\S+\s*$`)
	videoSepRe      = regexp.MustCompile(`\s+[-~]+\s*|\s*[-~]+\s+|\s*[–—―‒｜│]+\s*|\s+\|+\s*|\s*\|+\s+|\s+//\s+`)
	videoQuotedRe   = regexp.MustCompile(`^(.*?[\p{L}\p{N}\)\]\.!?])\s*[-–—:|]?\s*["“”«»„]([^"“”«»„]{2,})["“”«»„]\s*((?:[\(\[].*)?)$`)
	videoColonRe    = regexp.MustCompile(`^(.{2,}?):\s+(.{2,})$`)
	videoByRe       = regexp.MustCompile(`(?i)^(.{2,}?)\s+by\s+(.{2,})$`)
	channelSuffixRe = regexp.MustCompile(`(?i)(?:\s*-\s*topic|\s*vevo|\s+official(?:\s+(?:channel|music|youtube))?|\s+oficial|\s+officiel|\s+music|\s+tv|\s+channel)\s*$`)
	collabMarkerRe  = regexp.MustCompile(`(?i)(?:,|&|\s(?:feat\.?|ft\.?|featuring|x|vs\.?)\s)`)
)

var oddBrackets = strings.NewReplacer(
	"【", "[", "】", "]", "（", "(", "）", ")", "［", "[", "］", "]", "〔", "[", "〕", "]",
	"「", "\"", "」", "\"", "『", "\"", "』", "\"", "＂", "\"", "’", "'", "‘", "'",
)

var channelTails = map[string]bool{
	"music": true, "vevo": true, "official": true, "tv": true, "band": true, "channel": true,
	"records": true, "officialmusic": true, "musicvevo": true, "oficial": true, "hq": true,
}

var titleSideWords = []string{"version", "edit", "remaster", "remastered", "pt", "part", "mix", "theme", "ost"}

func hasVariantWord(s string) bool {
	key := normalizeForMatch(s)
	for _, w := range variantWords {
		if hasWord(key, w) {
			return true
		}
	}
	return false
}

func looksLikeTitle(s string) bool {
	if hasVariantWord(s) {
		return true
	}
	key := normalizeForMatch(s)
	for _, w := range titleSideWords {
		if hasWord(key, w) {
			return true
		}
	}
	return false
}

func isNoiseBracket(bracket string) bool {
	if hasVariantWord(bracket) {
		return false
	}
	if videoNoiseRe.MatchString(bracket) {
		return true
	}
	words := strings.Fields(normalizeForMatch(bracket))
	for _, w := range words {
		if !packagingTagWords[w] && !(len(w) == 4 && strings.Trim(w, "0123456789") == "") {
			return false
		}
	}
	return len(words) > 0
}

func dropSymbols(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) && r >= 0xFE00 {
			return ' '
		}
		return r
	}, s)
}

func cleanVideoTitle(raw string) string {
	s := dropSymbols(oddBrackets.Replace(html.UnescapeString(raw)))
	s = videoBracketRe.ReplaceAllStringFunc(s, func(m string) string {
		if isNoiseBracket(m) {
			return " "
		}
		return m
	})
	s = strings.Join(strings.Fields(s), " ")
	for {
		trimmed := strings.TrimSpace(videoTailRe.ReplaceAllString(s, ""))
		if trimmed == s || trimmed == "" {
			break
		}
		s = trimmed
	}
	return strings.Trim(s, " -–—|:~")
}

func cleanChannelName(channel string) string {
	name := strings.TrimSpace(html.UnescapeString(channel))
	for {
		trimmed := strings.TrimSpace(channelSuffixRe.ReplaceAllString(name, ""))
		if trimmed == name || trimmed == "" {
			return name
		}
		name = trimmed
	}
}

func videoSegments(clean string) []string {
	var out []string
	for _, part := range videoSepRe.Split(clean, -1) {
		part = strings.TrimSpace(part)
		if part == "" || len(out) > 0 && isNoiseBracket(part) {
			continue
		}
		out = append(out, part)
	}
	return out
}

func splitVideoTitle(clean string) (left, right string, quoted, ok bool) {
	if m := videoQuotedRe.FindStringSubmatch(clean); m != nil {
		left, right = strings.Trim(m[1], " -–—|:"), strings.TrimSpace(m[2])
		if rest := strings.TrimSpace(m[3]); rest != "" {
			right += " " + rest
		}
		if left != "" && right != "" {
			return left, right, true, true
		}
	}
	parts := videoSegments(clean)
	if len(parts) > 1 && isTrackNumber(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) < 2 {
		return "", "", false, false
	}
	return parts[0], strings.Join(parts[1:], " - "), false, true
}

func compactKey(s string) string {
	return strings.ReplaceAll(foldForMatch(s), " ", "")
}

func sideNamesArtist(side, name string) bool {
	whole := compactKey(name)
	if len(whole) < 2 {
		return false
	}
	have := compactKey(side)
	if have == whole || len(whole) >= 4 && strings.HasPrefix(have, whole) {
		return true
	}
	if len(have) >= 4 && strings.HasPrefix(whole, have) && channelTails[whole[len(have):]] {
		return true
	}
	sideArtists := matchArtists(side)
	for _, want := range matchArtists(name) {
		for _, a := range sideArtists {
			if a == want {
				return true
			}
		}
	}
	return false
}

func sideIsTitle(side, track string) bool {
	want := foldForMatch(coreTitle(track))
	have := foldForMatch(coreTitle(side))
	return want != "" && (have == want || len(want) >= 4 && hasWord(have, want))
}

type nameResolution struct {
	artist, title string
	left, right   string
	certain       bool
}

func titleFrom(side, track string) string {
	if track != "" && (side == "" || !sideIsTitle(side, track) || titleTags(side) == "") {
		return track
	}
	return side
}

func resolveVideoNames(v videoNames, knownArtist func(string) bool) nameResolution {
	clean := cleanVideoTitle(v.Title)
	channel := cleanChannelName(v.Channel)
	track := strings.TrimSpace(v.Track)
	given := joinArtists(v.Artists)
	known := func(side string) bool { return knownArtist != nil && anyKnownArtist(side, knownArtist) }
	hinted := func(side string) bool {
		return sideNamesArtist(side, channel) || given != "" && sideNamesArtist(side, given) || known(side)
	}

	left, right, quoted, ok := splitVideoTitle(clean)
	if !ok {
		if m := videoColonRe.FindStringSubmatch(clean); m != nil && hinted(m[1]) {
			left, right, quoted, ok = strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true, true
		}
	}
	if !ok {
		res := nameResolution{artist: trimDefault(given, channel), title: titleFrom(clean, track), certain: true}
		if m := videoByRe.FindStringSubmatch(clean); m != nil && given == "" && track == "" && hinted(m[2]) {
			res.artist, res.title = strings.TrimSpace(m[2]), strings.TrimSpace(m[1])
		}
		return res
	}
	if track != "" && given != "" {
		title := track
		for _, side := range []string{right, left} {
			if sideIsTitle(side, track) && titleTags(side) != "" {
				title = side
				break
			}
		}
		return nameResolution{artist: given, title: title, certain: true}
	}

	if given != "" && !sideNamesArtist(left, given) && !sideNamesArtist(right, given) {
		return nameResolution{artist: given, title: titleFrom(clean, track), certain: true}
	}
	if track != "" && !sideIsTitle(left, track) && !sideIsTitle(right, track) {
		artist := left
		if sideNamesArtist(right, channel) && !sideNamesArtist(left, channel) {
			artist = right
		}
		return nameResolution{artist: artist, title: track, certain: true}
	}

	score := 0.5
	if quoted {
		score += 4
	}
	if given != "" {
		if sideNamesArtist(left, given) {
			score += 4
		}
		if sideNamesArtist(right, given) {
			score -= 4
		}
	}
	if track != "" {
		if sideIsTitle(right, track) {
			score += 4
		}
		if sideIsTitle(left, track) {
			score -= 4
		}
	}
	if sideNamesArtist(left, channel) {
		score += 3
	}
	if sideNamesArtist(right, channel) {
		score -= 3
	}
	if known(left) {
		score += 2
	}
	if known(right) {
		score -= 2
	}
	plainLeft, plainRight := videoBracketRe.ReplaceAllString(left, ""), videoBracketRe.ReplaceAllString(right, "")
	if collabMarkerRe.MatchString(plainLeft) && !collabMarkerRe.MatchString(plainRight) {
		score++
	}
	if collabMarkerRe.MatchString(plainRight) && !collabMarkerRe.MatchString(plainLeft) {
		score--
	}
	if looksLikeTitle(right) && !looksLikeTitle(left) {
		score++
	}
	if looksLikeTitle(left) && !looksLikeTitle(right) {
		score--
	}

	res := nameResolution{artist: left, title: right, left: left, right: right, certain: score >= 2.5 || score <= -1.5}
	if score < 0 {
		res.artist, res.title = right, left
	}
	if given != "" {
		res.artist = given
	}
	res.title = titleFrom(res.title, track)
	return res
}

func anyKnownArtist(side string, known func(string) bool) bool {
	if known(side) {
		return true
	}
	for _, a := range splitArtists(side) {
		if known(a) {
			return true
		}
	}
	return false
}

func parseYouTubeTitle(videoTitle, channelTitle string) (artist, title string) {
	res := resolveVideoNames(videoNames{Title: videoTitle, Channel: channelTitle}, nil)
	return tidyArtist(res.artist), res.title
}

type nameSearch func(ctx context.Context, query string) ([]metadataCandidate, error)

var nameSearchers = []nameSearch{searchDeezer, searchItunes}

const (
	nameConfirmTimeout = 4 * time.Second
	nameConfirmScore   = 0.85
	nameConfirmMargin  = 0.3
)

func nameSimilarity(a, b string) float64 {
	ka, kb := foldForMatch(a), foldForMatch(b)
	if ka == "" || kb == "" {
		return 0
	}
	if ka == kb {
		return 1
	}
	return jaccard(tokenSet(ka), tokenSet(kb))
}

func artistSimilarity(side, artist string) float64 {
	best := nameSimilarity(side, artist)
	for _, a := range matchArtists(side) {
		for _, b := range matchArtists(artist) {
			best = max(best, nameSimilarity(a, b))
		}
	}
	return best
}

func titleSimilarity(side, title string) float64 {
	return nameSimilarity(coreTitle(side), coreTitle(title))
}

func plainSide(side string) string {
	return strings.TrimSpace(featRe.ReplaceAllString(videoBracketRe.ReplaceAllString(side, ""), ""))
}

func confirmNamesOnline(ctx context.Context, res nameResolution, searchers []nameSearch) nameResolution {
	if res.certain || res.left == "" || res.right == "" {
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, nameConfirmTimeout)
	defer cancel()
	query := plainSide(res.left) + " " + plainSide(res.right)
	results := make([][]metadataCandidate, len(searchers))
	var wg sync.WaitGroup
	for i, search := range searchers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logPanic("name check")
			if cands, err := search(ctx, query); err == nil {
				results[i] = cands
			}
		}()
	}
	wg.Wait()

	var straight, swapped float64
	var straightHit, swappedHit *metadataCandidate
	for _, cands := range results {
		for i := range cands {
			c := &cands[i]
			if s := (artistSimilarity(res.left, c.artist) + titleSimilarity(res.right, c.title)) / 2; s > straight {
				straight, straightHit = s, c
			}
			if s := (artistSimilarity(res.right, c.artist) + titleSimilarity(res.left, c.title)) / 2; s > swapped {
				swapped, swappedHit = s, c
			}
		}
	}
	switch {
	case straight >= nameConfirmScore && straight-swapped >= nameConfirmMargin:
		res.artist, res.title, res.certain = res.left, res.right, true
		res.adoptSpelling(straightHit)
	case swapped >= nameConfirmScore && swapped-straight >= nameConfirmMargin:
		res.artist, res.title, res.certain = res.right, res.left, true
		res.adoptSpelling(swappedHit)
	}
	return res
}

func (r *nameResolution) adoptSpelling(c *metadataCandidate) {
	if r.artist == strings.ToLower(r.artist) && foldForMatch(c.artist) == foldForMatch(r.artist) {
		r.artist = strings.TrimSpace(c.artist)
	}
	if r.title == strings.ToLower(r.title) && foldForMatch(c.title) == foldForMatch(r.title) {
		r.title = strings.TrimSpace(c.title)
	}
}

var bracketedFeatRe = regexp.MustCompile(`(?i)\s*[\(\[]\s*(?:feat\.?|ft\.?|featuring)\s+([^\)\]]+)[\)\]]`)

func tidyArtist(artist string) string {
	return strings.TrimSpace(bracketedFeatRe.ReplaceAllString(artist, " feat. $1"))
}

func (s *Server) videoNames(ctx context.Context, userID string, v videoNames) (artist, title string) {
	res := resolveVideoNames(v, s.knownArtistLookup(userID))
	if !res.certain && !metadataLookupDisabled() {
		res = confirmNamesOnline(ctx, res, nameSearchers)
	}
	return tidyArtist(res.artist), res.title
}

const knownArtistMinTracks = 2

func (s *Server) knownArtistLookup(userID string) func(string) bool {
	rows, err := s.db.Query(`SELECT t.artist, COUNT(*) FROM tracks t WHERE `+sameLibrary("t.user_id")+` GROUP BY t.artist`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	tally := map[string]int{}
	for rows.Next() {
		var credit string
		var n int
		if rows.Scan(&credit, &n) != nil {
			continue
		}
		for _, a := range matchArtists(dropPlaceholder(credit, "Unknown Artist")) {
			tally[a] += n
		}
	}
	return func(name string) bool {
		key := artistMatchKey(name)
		return len(key) >= 3 && tally[key] >= knownArtistMinTracks
	}
}
