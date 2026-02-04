package pattern

import (
	"testing"
)

func TestDetectPatternType(t *testing.T) {
	tests := []struct {
		name            string
		pattern         string
		expectedType    PatternType
		expectedClean   string
		expectedCaseIns bool
	}{
		// Exact match patterns
		{"exact match simple", "/exact/path", PatternTypeExact, "/exact/path", false},
		{"exact match with query", "/path?query=value", PatternTypeExact, "/path?query=value", false},
		{"exact match root", "/", PatternTypeExact, "/", false},
		{"exact match domain", "example.com", PatternTypeExact, "example.com", false},

		// Wildcard patterns
		{"wildcard single", "/blog/*", PatternTypeWildcard, "/blog/*", false},
		{"wildcard multiple", "/product/*/reviews/*", PatternTypeWildcard, "/product/*/reviews/*", false},
		{"wildcard extension", "*.pdf", PatternTypeWildcard, "*.pdf", false},
		{"wildcard catch-all", "*", PatternTypeWildcard, "*", false},
		{"wildcard middle", "/api/*/data", PatternTypeWildcard, "/api/*/data", false},

		// Regexp case-sensitive patterns
		{"regexp simple", "~/api/v[0-9]+", PatternTypeRegexp, "/api/v[0-9]+", false},
		{"regexp complex", "~^https?://.*\\.ads\\..*", PatternTypeRegexp, "^https?://.*\\.ads\\..*", false},
		{"regexp tilde only", "~test", PatternTypeRegexp, "test", false},

		// Regexp case-insensitive patterns
		{"regexp case-insensitive simple", "~*googlebot", PatternTypeRegexp, "googlebot", true},
		{"regexp case-insensitive complex", "~*googlebot|bingbot", PatternTypeRegexp, "googlebot|bingbot", true},
		{"regexp case-insensitive prefix", "~*^Mozilla", PatternTypeRegexp, "^Mozilla", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pType, clean, caseIns := DetectPatternType(tt.pattern)
			if pType != tt.expectedType {
				t.Errorf("DetectPatternType(%q) type = %v, want %v", tt.pattern, pType, tt.expectedType)
			}
			if clean != tt.expectedClean {
				t.Errorf("DetectPatternType(%q) clean = %q, want %q", tt.pattern, clean, tt.expectedClean)
			}
			if caseIns != tt.expectedCaseIns {
				t.Errorf("DetectPatternType(%q) caseInsensitive = %v, want %v", tt.pattern, caseIns, tt.expectedCaseIns)
			}
		})
	}
}

func TestCompile(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		shouldError bool
		checkType   PatternType
	}{
		// Valid patterns
		{"compile exact", "/exact/path", false, PatternTypeExact},
		{"compile wildcard", "/blog/*", false, PatternTypeWildcard},
		{"compile regexp", "~/api/v[0-9]+", false, PatternTypeRegexp},
		{"compile regexp case-insensitive", "~*googlebot", false, PatternTypeRegexp},

		// Invalid patterns
		{"empty pattern", "", true, PatternTypeExact},
		{"invalid regexp", "~[invalid(", true, PatternTypeRegexp},
		{"invalid case-insensitive regexp", "~*[unclosed", true, PatternTypeRegexp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(tt.pattern)
			if tt.shouldError {
				if err == nil {
					t.Errorf("Compile(%q) expected error, got nil", tt.pattern)
				}
			} else {
				if err != nil {
					t.Errorf("Compile(%q) unexpected error: %v", tt.pattern, err)
				}
				if p == nil {
					t.Errorf("Compile(%q) returned nil pattern", tt.pattern)
				}
				if p != nil && p.Type != tt.checkType {
					t.Errorf("Compile(%q) type = %v, want %v", tt.pattern, p.Type, tt.checkType)
				}
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		// Exact match tests (case-insensitive)
		{"exact match success", "/exact/path", "/exact/path", true},
		{"exact match fail", "/exact/path", "/exact/other", false},
		{"exact match case-insensitive lower", "/path", "/path", true},
		{"exact match case-insensitive upper", "/path", "/PATH", true},
		{"exact match case-insensitive mixed", "/Path", "/pAtH", true},
		{"exact match case-insensitive googlebot", "Googlebot", "googlebot", true},
		{"exact match case-insensitive GOOGLEBOT", "Googlebot", "GOOGLEBOT", true},
		{"exact match root", "/", "/", true},

		// Wildcard match tests
		{"wildcard trailing match", "/blog/*", "/blog/post", true},
		{"wildcard trailing deep match", "/blog/*", "/blog/2024/jan/post", true},
		{"wildcard trailing no match", "/blog/*", "/news/post", false},
		{"wildcard extension match", "*.pdf", "/docs/report.pdf", true},
		{"wildcard extension deep match", "*.pdf", "/reports/2024/Q1/summary.pdf", true},
		{"wildcard extension no match", "*.pdf", "/docs/report.doc", false},
		{"wildcard middle match", "/product/*/reviews", "/product/123/reviews", true},
		{"wildcard middle deep match", "/product/*/reviews", "/product/123/details/reviews", true},
		{"wildcard middle no match", "/product/*/reviews", "/product/123/ratings", false},
		{"wildcard multiple match", "/a/*/b/*/c", "/a/1/b/2/c", true},
		{"wildcard multiple deep match", "/a/*/b/*/c", "/a/1/x/y/b/2/z/c", true},
		{"wildcard catch-all", "*", "/any/path/here", true},
		{"wildcard empty segments", "a**b", "ab", true},
		{"wildcard empty segments with text", "a**b", "axxxb", true},

		// Regexp match tests (case-sensitive)
		{"regexp simple match", "~/api/v[0-9]+", "/api/v1", true},
		{"regexp simple no match", "~/api/v[0-9]+", "/api/v", false},
		{"regexp complex match", "~^/product/[0-9]+$", "/product/12345", true},
		{"regexp complex no match", "~^/product/[0-9]+$", "/product/abc", false},
		{"regexp case-sensitive match", "~Googlebot", "Googlebot/2.1", true},
		{"regexp case-sensitive no match", "~Googlebot", "googlebot/2.1", false},

		// Regexp match tests (case-insensitive)
		{"regexp case-insensitive match lower", "~*googlebot", "googlebot/2.1", true},
		{"regexp case-insensitive match upper", "~*googlebot", "GOOGLEBOT/2.1", true},
		{"regexp case-insensitive match mixed", "~*googlebot", "GoogleBot/2.1", true},
		{"regexp case-insensitive or match", "~*googlebot|bingbot", "BingBot/2.0", true},
		{"regexp case-insensitive no match", "~*googlebot", "yandex/1.0", false},

		// Edge cases
		{"wildcard at start", "*/test", "/path/test", true},
		{"wildcard at end", "test/*", "test/path", true},
		{"regexp dot matches", "~a.b", "aXb", true},
		{"regexp escaped dot", "~a\\.b", "a.b", true},
		{"regexp escaped dot no match", "~a\\.b", "aXb", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			result := p.Match(tt.input)
			if result != tt.expected {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.input, result, tt.expected)
			}
		})
	}
}

func TestMatchNilPattern(t *testing.T) {
	var p *Pattern
	result := p.Match("/any/input")
	if result != false {
		t.Errorf("(*Pattern)(nil).Match(input) = %v, want false", result)
	}
}

// Benchmarks

func BenchmarkDetectPatternType(b *testing.B) {
	patterns := []string{
		"/exact/path",
		"/blog/*",
		"~/api/v[0-9]+",
		"~*googlebot|bingbot",
	}

	for i := 0; i < b.N; i++ {
		for _, p := range patterns {
			DetectPatternType(p)
		}
	}
}

func BenchmarkCompile(b *testing.B) {
	patterns := []string{
		"/exact/path",
		"/blog/*",
		"~/api/v[0-9]+",
		"~*googlebot|bingbot",
	}

	for i := 0; i < b.N; i++ {
		for _, p := range patterns {
			Compile(p)
		}
	}
}

func BenchmarkMatchExact(b *testing.B) {
	p, _ := Compile("/exact/path")
	input := "/exact/path"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Match(input)
	}
}

func BenchmarkMatchWildcard(b *testing.B) {
	p, _ := Compile("/blog/*")
	input := "/blog/2024/january/post-1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Match(input)
	}
}

func BenchmarkMatchRegexp(b *testing.B) {
	p, _ := Compile("~/api/v[0-9]+/.*")
	input := "/api/v2/users/123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Match(input)
	}
}
