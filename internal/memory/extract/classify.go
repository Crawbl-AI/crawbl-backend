package extract

import (
	"regexp"
	"sort"
	"strings"
)

const (
	minSegmentLenForBonus = 200
	confidenceDivisor     = 5.0
	defaultChunkSize      = 25
)

type classifier struct {
	markers   map[string][]*regexp.Regexp
	positive  map[string]bool
	negative  map[string]bool
	resolvers []*regexp.Regexp
	codeLines []*regexp.Regexp
}

// NewClassifier returns a heuristic memory classifier.
func NewClassifier() Classifier {
	return &classifier{
		markers:   compileAllMarkers(),
		positive:  positiveWords,
		negative:  negativeWords,
		resolvers: compileResolvers(),
		codeLines: compileCodeLinePatterns(),
	}
}

// Classify splits text into segments, scores each against 5 memory types,
// disambiguates, and returns those meeting minConfidence.
func (c *classifier) Classify(text string, minConfidence float64) []ClassifiedMemory {
	segments := splitIntoSegments(text)
	var results []ClassifiedMemory

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if len(seg) < 20 {
			continue
		}

		prose := c.extractProse(seg)

		scores := make(map[string]float64)
		for memType, markers := range c.markers {
			score := c.scoreMarkers(prose, markers)
			if score > 0 {
				scores[memType] = score
			}
		}
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
	words := tokenize(text)
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
func splitIntoSegments(text string) []string {
	lines := strings.Split(text, "\n")

	turnPatterns := []*regexp.Regexp{
		regexp.MustCompile(`^>\s`),
		regexp.MustCompile(`(?i)^(Human|User|Q)\s*:`),
		regexp.MustCompile(`(?i)^(Assistant|AI|A|Claude|ChatGPT)\s*:`),
	}

	turnCount := 0
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		for _, p := range turnPatterns {
			if p.MatchString(stripped) {
				turnCount++
				break
			}
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
		stripped := strings.TrimSpace(line)
		isTurn := false
		for _, p := range turnPatterns {
			if p.MatchString(stripped) {
				isTurn = true
				break
			}
		}
		if isTurn && len(current) > 0 {
			segments = append(segments, strings.Join(current, "\n"))
			current = []string{line}
		} else {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		segments = append(segments, strings.Join(current, "\n"))
	}
	return segments
}

// tokenize extracts lowercase words from text.
func tokenize(text string) []string {
	re := regexp.MustCompile(`\b\w+\b`)
	raw := re.FindAllString(strings.ToLower(text), -1)
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

// =============================================================================
// Marker definitions
// =============================================================================

func compileAllMarkers() map[string][]*regexp.Regexp {
	return map[string][]*regexp.Regexp{
		"decision":   compileList(decisionMarkers),
		"preference": compileList(preferenceMarkers),
		"milestone":  compileList(milestoneMarkers),
		"problem":    compileList(problemMarkers),
		"emotional":  compileList(emotionMarkers),
	}
}

func compileResolvers() []*regexp.Regexp {
	return compileList([]string{
		`\bfixed\b`,
		`\bsolved\b`,
		`\bresolved\b`,
		`\bpatched\b`,
		`\bgot it working\b`,
		`\bit works\b`,
		`\bnailed it\b`,
		`\bfigured (it )?out\b`,
		`\bthe (fix|answer|solution)\b`,
	})
}

func compileCodeLinePatterns() []*regexp.Regexp {
	return compileList([]string{
		`^\s*[$#]\s`,
		`^\s*(cd|source|echo|export|pip|npm|git|python|bash|curl|wget|mkdir|rm|cp|mv|ls|cat|grep|find|chmod|sudo|brew|docker)\s`,
		"^\\s*```",
		`^\s*(import|from|def|class|function|const|let|var|return)\s`,
		`^\s*[A-Z_]{2,}=`,
		`^\s*\|`,
		`^\s*[-]{2,}`,
		`^\s*[{}\[\]]\s*$`,
		`^\s*(if|for|while|try|except|elif|else:)\b`,
		`^\s*\w+\.\w+\(`,
		`^\s*\w+ = \w+\.\w+`,
	})
}

func compileList(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		r, err := regexp.Compile(p)
		if err == nil {
			compiled = append(compiled, r)
		}
	}
	return compiled
}

var decisionMarkers = []string{
	`\blet'?s (use|go with|try|pick|choose|switch to)\b`,
	`\bwe (should|decided|chose|went with|picked|settled on)\b`,
	`\bi'?m going (to|with)\b`,
	`\bbetter (to|than|approach|option|choice)\b`,
	`\binstead of\b`,
	`\brather than\b`,
	`\bthe reason (is|was|being)\b`,
	`\bbecause\b`,
	`\btrade-?off\b`,
	`\bpros and cons\b`,
	`\bover\b.*\bbecause\b`,
	`\barchitecture\b`,
	`\bapproach\b`,
	`\bstrategy\b`,
	`\bpattern\b`,
	`\bstack\b`,
	`\bframework\b`,
	`\binfrastructure\b`,
	`\bset (it |this )?to\b`,
	`\bconfigure\b`,
	`\bdefault\b`,
}

var preferenceMarkers = []string{
	`\bi prefer\b`,
	`\balways use\b`,
	`\bnever use\b`,
	`\bdon'?t (ever |like to )?(use|do|mock|stub|import)\b`,
	`\bi like (to|when|how)\b`,
	`\bi hate (when|how|it when)\b`,
	`\bplease (always|never|don'?t)\b`,
	`\bmy (rule|preference|style|convention) is\b`,
	`\bwe (always|never)\b`,
	`\bfunctional\b.*\bstyle\b`,
	`\bimperative\b`,
	`\bsnake_?case\b`,
	`\bcamel_?case\b`,
	`\btabs\b.*\bspaces\b`,
	`\bspaces\b.*\btabs\b`,
	`\buse\b.*\binstead of\b`,
}

var milestoneMarkers = []string{
	`\bit works\b`,
	`\bit worked\b`,
	`\bgot it working\b`,
	`\bfixed\b`,
	`\bsolved\b`,
	`\bbreakthrough\b`,
	`\bfigured (it )?out\b`,
	`\bnailed it\b`,
	`\bcracked (it|the)\b`,
	`\bfinally\b`,
	`\bfirst time\b`,
	`\bfirst ever\b`,
	`\bnever (done|been|had) before\b`,
	`\bdiscovered\b`,
	`\brealized\b`,
	`\bfound (out|that)\b`,
	`\bturns out\b`,
	`\bthe key (is|was|insight)\b`,
	`\bthe trick (is|was)\b`,
	`\bnow i (understand|see|get it)\b`,
	`\bbuilt\b`,
	`\bcreated\b`,
	`\bimplemented\b`,
	`\bshipped\b`,
	`\blaunched\b`,
	`\bdeployed\b`,
	`\breleased\b`,
	`\bprototype\b`,
	`\bproof of concept\b`,
	`\bdemo\b`,
	`\bversion \d`,
	`\bv\d+\.\d+`,
	`\d+x (compression|faster|slower|better|improvement|reduction)`,
	`\d+% (reduction|improvement|faster|better|smaller)`,
}

var problemMarkers = []string{
	`\b(bug|error|crash|fail|broke|broken|issue|problem)\b`,
	`\bdoesn'?t work\b`,
	`\bnot working\b`,
	`\bwon'?t\b.*\bwork\b`,
	`\bkeeps? (failing|crashing|breaking|erroring)\b`,
	`\broot cause\b`,
	`\bthe (problem|issue|bug) (is|was)\b`,
	`\bturns out\b.*\b(was|because|due to)\b`,
	`\bthe fix (is|was)\b`,
	`\bworkaround\b`,
	`\bthat'?s why\b`,
	`\bthe reason it\b`,
	`\bfixed (it |the |by )\b`,
	`\bsolution (is|was)\b`,
	`\bresolved\b`,
	`\bpatched\b`,
	`\bthe answer (is|was)\b`,
	`\b(had|need) to\b.*\binstead\b`,
}

var emotionMarkers = []string{
	`\blove\b`,
	`\bscared\b`,
	`\bafraid\b`,
	`\bproud\b`,
	`\bhurt\b`,
	`\bhappy\b`,
	`\bsad\b`,
	`\bcry\b`,
	`\bcrying\b`,
	`\bmiss\b`,
	`\bsorry\b`,
	`\bgrateful\b`,
	`\bangry\b`,
	`\bworried\b`,
	`\blonely\b`,
	`\bbeautiful\b`,
	`\bamazing\b`,
	`\bwonderful\b`,
	`i feel`,
	`i'm scared`,
	`i love you`,
	`i'm sorry`,
	`i can't`,
	`i wish`,
	`i miss`,
	`i need`,
	`never told anyone`,
	`nobody knows`,
	`\*[^*]+\*`,
}

// =============================================================================
// Sentiment word sets
// =============================================================================

var positiveWords = map[string]bool{
	"pride": true, "proud": true, "joy": true, "happy": true,
	"love": true, "loving": true, "beautiful": true, "amazing": true,
	"wonderful": true, "incredible": true, "fantastic": true, "brilliant": true,
	"perfect": true, "excited": true, "thrilled": true, "grateful": true,
	"warm": true, "breakthrough": true, "success": true, "works": true,
	"working": true, "solved": true, "fixed": true, "nailed": true,
	"heart": true, "hug": true, "precious": true, "adore": true,
}

var negativeWords = map[string]bool{
	"bug": true, "error": true, "crash": true, "crashing": true,
	"crashed": true, "fail": true, "failed": true, "failing": true,
	"failure": true, "broken": true, "broke": true, "breaking": true,
	"breaks": true, "issue": true, "problem": true, "wrong": true,
	"stuck": true, "blocked": true, "unable": true, "impossible": true,
	"missing": true, "terrible": true, "horrible": true, "awful": true,
	"worse": true, "worst": true, "panic": true, "disaster": true,
	"mess": true,
}
