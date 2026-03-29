// Package style defines the shared message styles used by the Crawbl CLI.
package style

// Enum identifies a CLI message style.
type Enum int

const (
	Empty Enum = iota
	Running
	Stopping
	Waiting
	Success
	Failure
	Warning
	Tip
	Delete
	Check
	Database
	Migrate
	Docker
	Setup
	Celebrate
	Infra
	Deploy
	Test
	Lint
	Format
	URL
	Doc
	Step
	Indent
	Destroyed
	Backup
	Reaper
	Config
	Ready
)

// Options defines the rich and low-fidelity prefixes for a style.
type Options struct {
	Prefix    string
	LowPrefix string
}

var config = map[Enum]Options{
	Empty:     {},
	Running:   {Prefix: "🚀", LowPrefix: "*"},
	Stopping:  {Prefix: "✋", LowPrefix: "*"},
	Waiting:   {Prefix: "⏳", LowPrefix: "*"},
	Success:   {Prefix: "✅", LowPrefix: "*"},
	Failure:   {Prefix: "❌", LowPrefix: "X"},
	Warning:   {Prefix: "⚠️", LowPrefix: "!"},
	Tip:       {Prefix: "💡", LowPrefix: "*"},
	Delete:    {Prefix: "🗑️", LowPrefix: "*"},
	Check:     {Prefix: "✅", LowPrefix: "+"},
	Database:  {Prefix: "🐘", LowPrefix: "*"},
	Migrate:   {Prefix: "🔄", LowPrefix: "*"},
	Docker:    {Prefix: "🐳", LowPrefix: "*"},
	Setup:     {Prefix: "🧠", LowPrefix: "*"},
	Celebrate: {Prefix: "🎉", LowPrefix: "*"},
	Infra:     {Prefix: "☁️", LowPrefix: "*"},
	Deploy:    {Prefix: "📦", LowPrefix: "*"},
	Test:      {Prefix: "🧪", LowPrefix: "*"},
	Lint:      {Prefix: "🔍", LowPrefix: "*"},
	Format:    {Prefix: "📐", LowPrefix: "*"},
	URL:       {Prefix: "👉", LowPrefix: ">"},
	Doc:       {Prefix: "📚", LowPrefix: "*"},
	Step:      {},
	Indent:    {},
	Destroyed: {Prefix: "💣", LowPrefix: "X"},
	Backup:    {Prefix: "💾", LowPrefix: "*"},
	Reaper:    {Prefix: "🧹", LowPrefix: "*"},
	Config:    {Prefix: "⚙️", LowPrefix: "*"},
	Ready:     {Prefix: "🏄", LowPrefix: "*"},
}

// Get returns the options for a style enum.
func Get(e Enum) Options {
	if opts, ok := config[e]; ok {
		return opts
	}
	return Options{}
}
