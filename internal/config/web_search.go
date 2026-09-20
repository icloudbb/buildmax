package config

// WebSearchConfig configures the optional credential for built-in web search.
// Searches without a key use the provider's keyless allowance.
type WebSearchConfig struct {
	APIKey string `mapstructure:"api_key" json:"api_key,omitempty" yaml:"api_key,omitempty"`
}
