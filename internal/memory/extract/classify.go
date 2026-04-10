package extract

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/config"
)

const (
	minSegmentLenForBonus = 200
	confidenceDivisor     = 5.0
	defaultChunkSize      = 25
)

type classifier struct {
	markers          map[string][]*regexp.Regexp
	positive         map[string]bool
	negative         map[string]bool
	resolvers        []*regexp.Regexp
	codeLines        []*regexp.Regexp
	blockquote       *regexp.Regexp
	humanSpeaker     *regexp.Regexp
	assistantSpeaker *regexp.Regexp
	wordTokenizer    *regexp.Regexp
}

// NewClassifier returns a heuristic memory classifier.
// Patterns are loaded from the embedded classify_patterns.json config.
// If config loading fails, it panics — this is a programmer error (missing embedded file).
func NewClassifier() Classifier {
	cfg, err := config.LoadClassifyConfig()
	if err != nil {
		panic("classify: failed to load classify_patterns.json: " + err.Error())
	}

	segments := cfg.CompileSegmentPatterns()

	return &classifier{
		markers:          cfg.CompileMarkers(),
		positive:         cfg.CompilePositiveWords(),
		negative:         cfg.CompileNegativeWords(),
		resolvers:        cfg.CompileResolvers(),
		codeLines:        cfg.CompileCodeLines(),
		blockquote:       segments["blockquote"],
		humanSpeaker:     segments["human_speaker"],
		assistantSpeaker: segments["assistant_speaker"],
		wordTokenizer:    segments["word_tokenizer"],
	}
}

// Classify splits text into segments, scores each against 5 memory types,
// disambiguates, and returns those meeting minConfidence.
func (c *classifier) Classify(text string, minConfidence float64) []ClassifiedMemory {
	segments := c.splitIntoSegments(text)
	var results []ClassifiedMemory

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		// Ignore segments under 20 characters — typically stray speaker
		// labels or one-word replies with no classifiable signal.
		if len(seg) < 20 {
			continue
		}

		prose := c.extractProse(seg)

		scores := c.scoreAllMarkers(prose)
		if len(scores) == 0 {
			continue
		}

		// Length bonus
		lengthBonus := 0.0
		switch {
		case len(seg) > 500:
			lengthBonus = 2
		case len(seg) > minSegmentLenForBonus:
			lengthBonus = 1
		}

		bestType := maxKey(scores)
		bestScore := scores[bestType] + lengthBonus

		bestType = c.disambiguate(bestType, prose, scores)

		confidence := bestScore / confidenceDivisor
		if confidence > 1.0 {
			confidence = 1.0
		}
		if confidence < minConfidence {
			continue
		}

		results = append(results, ClassifiedMemory{
			Content:    seg,
			MemoryType: bestType,
			ChunkIndex: len(results),
			Confidence: confidence,
		})
	}

	return results
}

// scoreAllMarkers returns the non-zero marker scores for one segment across every memory type.
func (c *classifier) scoreAllMarkers(prose string) map[string]float64 {
	scores := make(map[string]float64, len(c.markers))
	for memType, markers := range c.markers {
		if score := c.scoreMarkers(prose, markers); score > 0 {
			scores[memType] = score
		}
	}
	return scores
}

// scoreMarkers counts how many times any marker matches the text.
func (c *classifier) scoreMarkers(text string, markers []*regexp.Regexp) float64 {
	lower := strings.ToLower(text)
	score := 0.0
	for _, m := range markers {
		matches := m.FindAllString(lower, -1)
		score += float64(len(matches))
	}
	return score
}

// getSentiment returns "positive", "negative", or "neutral".
func (c *classifier) getSentiment(text string) string {
	words := c.tokenize(text)
	pos, neg := 0, 0
	for _, w := range words {
		if c.positive[w] {
			pos++
		}
		if c.negative[w] {
			neg++
		}
	}
	switch {
	case pos > neg:
		return "positive"
	case neg > pos:
		return "negative"
	default:
		return "neutral"
	}
}

// hasResolution checks whether text describes a resolved problem.
func (c *classifier) hasResolution(text string) bool {
	lower := strings.ToLower(text)
	for _, r := range c.resolvers {
		if r.MatchString(lower) {
			return true
		}
	}
	return false
}

// disambiguate corrects common misclassifications using sentiment and resolution cues.
func (c *classifier) disambiguate(memType, text string, scores map[string]float64) string {
	sentiment := c.getSentiment(text)

	if memType == "problem" && c.hasResolution(text) {
		if scores["emotional"] > 0 && sentiment == "positive" {
			return "emotional"
		}
		return "milestone"
	}

	if memType == "problem" && sentiment == "positive" {
		if scores["milestone"] > 0 {
			return "milestone"
		}
		if scores["emotional"] > 0 {
			return "emotional"
		}
	}

	return memType
}

// extractProse strips code blocks and code-like lines from text.
func (c *classifier) extractProse(text string) string {
	lines := strings.Split(text, "\n")
	var prose []string
	inCode := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if !c.isCodeLine(stripped) {
			prose = append(prose, line)
		}
	}
	if result := strings.TrimSpace(strings.Join(prose, "\n")); result != "" {
		return result
	}
	return text
}

func (c *classifier) isCodeLine(line string) bool {
	if line == "" {
		return false
	}
	for _, p := range c.codeLines {
		if p.MatchString(line) {
			return true
		}
	}
	alpha := 0
	for _, ch := range line {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			alpha++
		}
	}
	if len(line) > 10 && float64(alpha)/float64(len(line)) < 0.4 {
		return true
	}
	return false
}

// splitIntoSegments splits text by speaker turns or double newlines.
func (c *classifier) splitIntoSegments(text string) []string {
	lines := strings.Split(text, "\n")

	turnPatterns := []*regexp.Regexp{
		c.blockquote,
		c.humanSpeaker,
		c.assistantSpeaker,
	}

	turnCount := 0
	for _, line := range lines {
		if lineMatchesAny(strings.TrimSpace(line), turnPatterns) {
			turnCount++
		}
	}

	if turnCount >= 3 {
		return splitByTurns(lines, turnPatterns)
	}

	// Paragraph split
	raw := strings.Split(text, "\n\n")
	var paragraphs []string
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			paragraphs = append(paragraphs, p)
		}
	}

	// Single giant block: chunk by 25-line groups
	if len(paragraphs) <= 1 && len(lines) > 20 {
		var segments []string
		for i := 0; i < len(lines); i += defaultChunkSize {
			end := i + defaultChunkSize
			if end > len(lines) {
				end = len(lines)
			}
			group := strings.TrimSpace(strings.Join(lines[i:end], "\n"))
			if group != "" {
				segments = append(segments, group)
			}
		}
		return segments
	}

	return paragraphs
}

func splitByTurns(lines []string, turnPatterns []*regexp.Regexp) []string {
	var segments []string
	var current []string

	for _, line := range lines {
		isTurn := lineMatchesAny(strings.TrimSpace(line), turnPatterns)
		if isTurn && len(current) > 0 {
			segments = append(segments, strings.Join(current, "\n"))
			current = []string{line}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		segments = append(segments, strings.Join(current, "\n"))
	}
	return segments
}

// lineMatchesAny reports whether any of the supplied regexps matches line.
func lineMatchesAny(line string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(line) {
			return true
		}
	}
	return false
}

// tokenize extracts lowercase words from text.
func (c *classifier) tokenize(text string) []string {
	raw := c.wordTokenizer.FindAllString(strings.ToLower(text), -1)
	seen := make(map[string]bool, len(raw))
	var unique []string
	for _, w := range raw {
		if !seen[w] {
			seen[w] = true
			unique = append(unique, w)
		}
	}
	return unique
}

func maxKey(m map[string]float64) string {
	var best string
	var bestVal float64
	// Stable ordering for determinism when scores tie
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if m[k] > bestVal {
			bestVal = m[k]
			best = k
		}
	}
	return best
}
