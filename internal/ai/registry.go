package ai

// Env is what providers need from the trainer: where to run CLIs, their configured paths,
// stored API keys and the custom endpoint URL.
type Env struct {
	WorkDir   string
	CLIPath   func(id string) string
	Key       func(id string) string
	CustomURL func() string
}

// NewProviders returns every provider in the order the AI page lists them.
func NewProviders(env Env) []Provider {
	path := func(id string) func() string { return func() string { return env.CLIPath(id) } }
	list := []Provider{
		&Claude{Path: path("claude"), WorkDir: env.WorkDir},
		&Codex{Path: path("codex"), WorkDir: env.WorkDir},
		&Anthropic{Key: func() string { return env.Key("anthropic") }},
	}
	for _, c := range CompatiblePresets(env.Key, env.CustomURL) {
		list = append(list, c)
	}
	return list
}
