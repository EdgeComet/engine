package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecacheMember_JSONMarshal(t *testing.T) {
	member := RecacheMember{
		URL:         "https://example.com/page?a=1&b=2",
		DimensionID: 2,
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(member)
	require.NoError(t, err)

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonBytes, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, "https://example.com/page?a=1&b=2", jsonMap["url"])
	assert.Equal(t, float64(2), jsonMap["dimension_id"]) // JSON numbers are float64
}

func TestRecacheMember_JSONUnmarshal(t *testing.T) {
	jsonStr := `{"url":"https://example.com/test","dimension_id":3}`

	var member RecacheMember
	err := json.Unmarshal([]byte(jsonStr), &member)
	require.NoError(t, err)

	assert.Equal(t, "https://example.com/test", member.URL)
	assert.Equal(t, 3, member.DimensionID)
}

func TestRecacheMember_JSONRoundTrip(t *testing.T) {
	original := RecacheMember{
		URL:         "https://example.com/path",
		DimensionID: 1,
	}

	// Marshal
	jsonBytes, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal
	var decoded RecacheMember
	err = json.Unmarshal(jsonBytes, &decoded)
	require.NoError(t, err)

	// Verify equality
	assert.Equal(t, original, decoded)
}

func TestRecacheMember_FieldNames(t *testing.T) {
	member := RecacheMember{
		URL:         "https://example.com",
		DimensionID: 5,
	}

	jsonBytes, err := json.Marshal(member)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"url"`)
	assert.Contains(t, jsonStr, `"dimension_id"`)
	assert.NotContains(t, jsonStr, `"URL"`)         // Not capitalized
	assert.NotContains(t, jsonStr, `"DimensionID"`) // Not capitalized
}
