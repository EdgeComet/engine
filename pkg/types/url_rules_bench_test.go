package types

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// BenchmarkURLRule_GetMatchPatterns_Cached benchmarks cached pattern retrieval (YAML unmarshaled)
func BenchmarkURLRule_GetMatchPatterns_Cached(b *testing.B) {
	benchmarks := []struct {
		name string
		yaml string
	}{
		{
			name: "single_pattern",
			yaml: `
match: "/blog/*"
action: render
`,
		},
		{
			name: "three_patterns",
			yaml: `
match:
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
action: render
`,
		},
		{
			name: "ten_patterns",
			yaml: `
match:
  - "/api/v1/*"
  - "/api/v2/*"
  - "/api/v3/*"
  - "/admin/*"
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
  - "/products/*"
  - "/search*"
  - "*.pdf"
action: render
`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup: unmarshal rule once (populates cache)
			var rule URLRule
			if err := yaml.Unmarshal([]byte(bm.yaml), &rule); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Benchmark: should be zero allocations
			for i := 0; i < b.N; i++ {
				patterns := rule.GetMatchPatterns()
				if len(patterns) == 0 {
					b.Fatal("expected non-empty patterns")
				}
			}
		})
	}
}

// BenchmarkURLRule_GetMatchPatterns_Programmatic benchmarks fallback logic (programmatic creation)
func BenchmarkURLRule_GetMatchPatterns_Programmatic(b *testing.B) {
	benchmarks := []struct {
		name  string
		match interface{}
	}{
		{
			name:  "string",
			match: "/blog/*",
		},
		{
			name:  "slice_string_3",
			match: []string{"/blog/*", "/articles/*", "/news/*"},
		},
		{
			name:  "slice_interface_3",
			match: []interface{}{"/blog/*", "/articles/*", "/news/*"},
		},
		{
			name:  "slice_string_10",
			match: []string{"/a/*", "/b/*", "/c/*", "/d/*", "/e/*", "/f/*", "/g/*", "/h/*", "/i/*", "/j/*"},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			rule := URLRule{Match: bm.match}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				patterns := rule.GetMatchPatterns()
				if len(patterns) == 0 {
					b.Fatal("expected non-empty patterns")
				}
			}
		})
	}
}

// BenchmarkURLRule_UnmarshalYAML benchmarks the unmarshaling process with pattern pre-computation
func BenchmarkURLRule_UnmarshalYAML(b *testing.B) {
	benchmarks := []struct {
		name string
		yaml string
	}{
		{
			name: "single_pattern",
			yaml: `
match: "/blog/*"
action: render
`,
		},
		{
			name: "three_patterns",
			yaml: `
match:
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
action: render
`,
		},
		{
			name: "ten_patterns",
			yaml: `
match:
  - "/api/v1/*"
  - "/api/v2/*"
  - "/api/v3/*"
  - "/admin/*"
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
  - "/products/*"
  - "/search*"
  - "*.pdf"
action: render
`,
		},
		{
			name: "with_overrides",
			yaml: `
match: "/api/*"
action: render
cache:
  ttl: 1h
render:
  timeout: 30s
  dimension: mobile
`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			yamlData := []byte(bm.yaml)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				var rule URLRule
				if err := yaml.Unmarshal(yamlData, &rule); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkURLRule_GetMatchPatterns_Comparison compares cached vs non-cached performance
func BenchmarkURLRule_GetMatchPatterns_Comparison(b *testing.B) {
	yamlData := `
match:
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
  - "/products/*"
  - "/search*"
action: render
`

	b.Run("cached_yaml_unmarshaled", func(b *testing.B) {
		// Cached version: unmarshal from YAML
		var rule URLRule
		if err := yaml.Unmarshal([]byte(yamlData), &rule); err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			patterns := rule.GetMatchPatterns()
			if len(patterns) != 5 {
				b.Fatal("unexpected pattern count")
			}
		}
	})

	b.Run("fallback_programmatic_slice_string", func(b *testing.B) {
		// Fallback version: programmatic creation with []string
		rule := URLRule{
			Match: []string{"/blog/*", "/articles/*", "/news/*", "/products/*", "/search*"},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			patterns := rule.GetMatchPatterns()
			if len(patterns) != 5 {
				b.Fatal("unexpected pattern count")
			}
		}
	})

	b.Run("fallback_programmatic_slice_interface", func(b *testing.B) {
		// Fallback version: programmatic creation with []interface{}
		rule := URLRule{
			Match: []interface{}{"/blog/*", "/articles/*", "/news/*", "/products/*", "/search*"},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			patterns := rule.GetMatchPatterns()
			if len(patterns) != 5 {
				b.Fatal("unexpected pattern count")
			}
		}
	})
}

// BenchmarkURLRule_RealWorldScenario simulates realistic usage pattern
func BenchmarkURLRule_RealWorldScenario(b *testing.B) {
	// Simulate a configuration with 100 rules (typical production config)
	rulesYAML := `
rules:
  - match: "/admin/*"
    action: block
  - match: ["/blog/*", "/articles/*", "/news/*"]
    action: render
  - match: "/api/*"
    action: bypass
  - match: "/static/*"
    action: bypass
  - match: "*.pdf"
    action: render
`

	type Config struct {
		Rules []URLRule `yaml:"rules"`
	}

	var config Config
	if err := yaml.Unmarshal([]byte(rulesYAML), &config); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate 100k RPS checking against all rules
	for i := 0; i < b.N; i++ {
		for _, rule := range config.Rules {
			patterns := rule.GetMatchPatterns()
			// Simulate pattern matching work
			_ = patterns
		}
	}
}

// BenchmarkURLRule_HighThroughput simulates very high request rate
func BenchmarkURLRule_HighThroughput(b *testing.B) {
	// Create 100 rules to simulate production environment
	var rules []URLRule
	for i := 0; i < 100; i++ {
		yamlData := `
match:
  - "/path1/*"
  - "/path2/*"
  - "/path3/*"
action: render
`
		var rule URLRule
		if err := yaml.Unmarshal([]byte(yamlData), &rule); err != nil {
			b.Fatal(err)
		}
		rules = append(rules, rule)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate 100k RPS × 100 rules = 10M GetMatchPatterns() calls/sec
	for i := 0; i < b.N; i++ {
		for j := range rules {
			patterns := rules[j].GetMatchPatterns()
			if len(patterns) == 0 {
				b.Fatal("unexpected empty patterns")
			}
		}
	}
}

// BenchmarkURLRule_Memory measures memory usage of cached vs non-cached
func BenchmarkURLRule_Memory(b *testing.B) {
	b.Run("cached_memory_usage", func(b *testing.B) {
		yamlData := `
match:
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
  - "/products/*"
  - "/search*"
action: render
`

		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var rule URLRule
			if err := yaml.Unmarshal([]byte(yamlData), &rule); err != nil {
				b.Fatal(err)
			}

			// Call GetMatchPatterns 1000 times (simulating 1000 requests)
			for j := 0; j < 1000; j++ {
				patterns := rule.GetMatchPatterns()
				_ = patterns
			}
		}
	})

	b.Run("programmatic_memory_usage", func(b *testing.B) {
		match := []interface{}{"/blog/*", "/articles/*", "/news/*", "/products/*", "/search*"}

		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			rule := URLRule{Match: match}

			// Call GetMatchPatterns 1000 times (simulating 1000 requests)
			for j := 0; j < 1000; j++ {
				patterns := rule.GetMatchPatterns()
				_ = patterns
			}
		}
	})
}
