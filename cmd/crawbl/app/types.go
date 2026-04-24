package app

import (
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/config"
)

// const declarations

const (
	// RepoSlugBackend is the GitHub repo slug for the backend repository.
	RepoSlugBackend = "Crawbl-AI/crawbl-backend"
	// RepoSlugDocs is the GitHub repo slug for the docs repository.
	RepoSlugDocs = "Crawbl-AI/crawbl-docs"
	// RepoSlugWebsite is the GitHub repo slug for the website repository.
	RepoSlugWebsite = "Crawbl-AI/crawbl-website"

	gcDescription = "Run registry garbage collection after deploy (keep latest 5 per repo)"
)

// prodKubeContext is the kubectl context name for the production cluster.
// Direct CLI deploys to this context are not allowed — use deploy-prod.yml in CI.
const prodKubeContext = "do-fra1-crawbl-prod"

const errTagRequired = "--tag is required"

// defaultGCKeep is the number of latest tags to retain per repository when
// GC runs automatically after a deploy.
const defaultGCKeep = 5

const reportFileMode = 0o644

// var declarations

var (
	registryBase = config.StringOr("CRAWBL_REGISTRY", "registry.digitalocean.com/crawbl")

	buildPlatformImageRepo     = registryBase + "/crawbl-platform"
	buildAgentRuntimeImageRepo = registryBase + "/crawbl-agent-runtime"
	buildAuthFilterImageRepo   = registryBase + "/envoy-auth-filter"
	buildAuthFilterDockerfile  = "dockerfiles/envoy-auth-filter.dockerfile"
	buildAuthFilterContext     = "cmd/envoy-auth-filter"

	buildDocsRepoDir    = "crawbl-docs"
	buildWebsiteRepoDir = "crawbl-website"
)

// type declarations

// tagPair holds a resolved tag and its predecessor (for changelog links).
type tagPair struct {
	Tag string
	// PrevTag is the previous tag used to bound the changelog range. It may be
	// empty when the caller supplied an explicit --tag value; release.TagAndRelease
	// must tolerate an empty PrevTag (it will omit the "compare" link in that case).
	PrevTag string
}

// koBuildOpts holds parameters for a ko build.
type koBuildOpts struct {
	importPath   string // Go import path, e.g. "./cmd/crawbl"
	imageRepo    string // full image name, e.g. "registry.digitalocean.com/crawbl/crawbl-platform"
	tag          string
	push         bool
	buildVersion string // injected as KO_BUILD_VERSION for ldflags template
}

// dockerBuildOpts holds parameters for a docker buildx build (auth-filter only).
type dockerBuildOpts struct {
	imageRepo  string
	dockerfile string
	contextDir string
	tag        string
	platform   string
	push       bool
}

// staticDeployOpts describes a Cloudflare Pages deploy for a static site repo.
type staticDeployOpts struct {
	Use         string
	Short       string
	Long        string
	Example     string
	RepoDir     string
	RepoSlug    string
	OutputDir   string
	PagesName   string
	PathDefault string
}

// gcRepo represents a repository returned by doctl registry repository list-v2.
type gcRepo struct {
	Name string `json:"name"`
}

// gcTag represents a single tag returned by doctl registry repository list-tags.
type gcTag struct {
	Tag            string    `json:"tag"`
	ManifestDigest string    `json:"manifest_digest"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// staticcheckDiag represents a single staticcheck JSON diagnostic.
type staticcheckDiag struct {
	Code     string `json:"code"`
	Location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
	End struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"end"`
	Message string `json:"message"`
}

// sonarGenericReport is the SonarQube generic issue import format.
type sonarGenericReport struct {
	Issues []sonarGenericIssue `json:"issues"`
}

// sonarGenericIssue is a single issue in the SonarQube generic format.
type sonarGenericIssue struct {
	EngineID        string               `json:"engineId"`
	RuleID          string               `json:"ruleId"`
	Severity        string               `json:"severity"`
	Type            string               `json:"type"`
	PrimaryLocation sonarGenericLocation `json:"primaryLocation"`
}

// sonarGenericLocation describes where an issue occurs.
type sonarGenericLocation struct {
	Message   string                `json:"message"`
	FilePath  string                `json:"filePath"`
	TextRange sonarGenericTextRange `json:"textRange"`
}

// sonarGenericTextRange describes the text range of an issue.
type sonarGenericTextRange struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}
