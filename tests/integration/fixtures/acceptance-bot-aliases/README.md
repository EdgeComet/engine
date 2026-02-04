# Bot Aliases Acceptance Test Fixtures

Comprehensive test fixtures for validating bot alias expansion in edge-gateway configuration.

## Structure

```
acceptance-bot-aliases/
├── edge-gateway.yaml          # Main gateway config with global bot alias dimensions
└── hosts.d/
    └── testhost.yaml          # Host-specific config with bot alias overrides
```

## Global Dimensions (edge-gateway.yaml)

Tests bot alias expansion at the global level:

### Search Engine Bots
- **googlebot_desktop** (id: 1): Uses `$GooglebotSearchDesktop`
- **googlebot_mobile** (id: 2): Uses `$GooglebotSearchMobile`
- **bing_desktop** (id: 3): Uses `$BingbotDesktop`
- **bing_mobile** (id: 4): Uses `$BingbotMobile`

### AI Bots
- **ai_bots** (id: 5): Uses multiple AI bot aliases
  - `$ChatGPTUserBot`
  - `$PerplexityBot`
  - `$AnthropicBot`

### Advertising Bots
- **google_ads** (id: 6): Uses Google Ads bot aliases
  - `$GoogleBotAds`
  - `$GoogleBotAdsMobileWeb`

### Mixed Patterns
- **mixed_patterns** (id: 7): Combines bot aliases with regular patterns
  - `$OpenAISearchBot` (bot alias)
  - `*CustomBot*` (wildcard pattern)
  - `~^SpecialBot/.*` (regexp pattern)

## Host-Specific Dimensions (hosts.d/testhost.yaml)

Tests bot alias expansion at the host level (overrides global dimensions):

### Host: bottest.example.com (id: 1)

- **host_bing_mobile** (id: 10): Multiple mobile bot aliases
  - `$BingbotMobile`
  - `$GooglebotSearchMobile`

- **host_multi_bot** (id: 11): Multiple AI bot aliases
  - `$ChatGPTTrainingBot`
  - `$PerplexityUserBot`
  - `$AnthropicUserBot`
  - `$AnthropicSearchBot`

- **host_mixed_patterns** (id: 12): Bot aliases + custom patterns
  - `$GooglebotSearchDesktop`
  - `$BingbotDesktop`
  - `*CustomHostBot*` (wildcard)
  - `~^HostBot/\\d+\\.\\d+` (regexp)

### Host: alias-test.example.com (id: 2)

- **default_dimension** (id: 20): Fallback dimension with all major bots
  - `$GooglebotSearchDesktop`
  - `$GooglebotSearchMobile`
  - `$BingbotDesktop`
  - `$BingbotMobile`

## Bot Aliases Coverage

### Tested Aliases (18 total)
- AnthropicBot
- AnthropicSearchBot
- AnthropicUserBot
- BingbotDesktop
- BingbotMobile
- ChatGPTTrainingBot
- ChatGPTUserBot
- GoogleBotAds
- GoogleBotAdsMobileWeb
- GooglebotSearchDesktop
- GooglebotSearchMobile
- OpenAISearchBot
- PerplexityBot
- PerplexityUserBot

### Pattern Type Coverage
- **Exact matches**: Bot aliases expand to exact match patterns
- **Regexp patterns**: Bot aliases expand to regexp patterns with version numbers
- **Mixed patterns**: Bot aliases combined with custom wildcards and regexps

## Test Scenarios

1. **Global dimension bot aliases**: Verify bot aliases expand correctly at global level
2. **Host-level overrides**: Verify host configs can override global dimensions
3. **Multiple aliases per dimension**: Test multiple bot aliases in single dimension
4. **Mixed pattern types**: Test bot aliases combined with custom patterns
5. **Fallback behavior**: Test unmatched_dimension with bot aliases
6. **Priority matching**: Test which dimension matches first when UAs could match multiple

## Configuration Details

- **Redis**: `localhost:6379` (db: 0)
- **Storage**: `/tmp/cache/test-bot-aliases`
- **Server Port**: `9070`
- **Internal Auth**: `test-internal-auth`
- **Render Keys**: `test-render-key-bot-aliases`, `test-render-key-aliases-2`
- **Unmatched Dimension**: `bypass` (global), varies by host

## Usage

These fixtures are designed for acceptance tests that:
1. Start edge-gateway with this configuration
2. Send requests with various bot User-Agents
3. Verify correct dimension matching based on expanded patterns
4. Test that bot aliases expand to their underlying patterns correctly

## Related Files

- `internal/common/config/bot_aliases.go` - Bot alias definitions
- `internal/common/config/alias_expansion.go` - Alias expansion logic
- Source: Based on real bot User-Agents from official documentation
