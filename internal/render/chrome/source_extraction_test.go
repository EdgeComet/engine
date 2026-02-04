package chrome

import (
	"testing"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestExtractSourceInfo(t *testing.T) {
	tests := []struct {
		name             string
		stackTrace       *cdpruntime.StackTrace
		expectedURL      string
		expectedLocation string
	}{
		{
			name:             "nil stack trace",
			stackTrace:       nil,
			expectedURL:      types.AnonymousSourceURL,
			expectedLocation: types.UnknownSourceLocation,
		},
		{
			name:             "empty call frames",
			stackTrace:       &cdpruntime.StackTrace{CallFrames: []*cdpruntime.CallFrame{}},
			expectedURL:      types.AnonymousSourceURL,
			expectedLocation: types.UnknownSourceLocation,
		},
		{
			name: "valid stack trace",
			stackTrace: &cdpruntime.StackTrace{
				CallFrames: []*cdpruntime.CallFrame{
					{URL: "https://example.com/app.js", LineNumber: 141, ColumnNumber: 14},
				},
			},
			expectedURL:      "https://example.com/app.js",
			expectedLocation: "142:15", // 0-based to 1-based
		},
		{
			name: "empty URL in frame",
			stackTrace: &cdpruntime.StackTrace{
				CallFrames: []*cdpruntime.CallFrame{
					{URL: "", LineNumber: 10, ColumnNumber: 5},
				},
			},
			expectedURL:      types.AnonymousSourceURL,
			expectedLocation: "11:6",
		},
		{
			name: "negative line/column clamped",
			stackTrace: &cdpruntime.StackTrace{
				CallFrames: []*cdpruntime.CallFrame{
					{URL: "https://example.com/app.js", LineNumber: -1, ColumnNumber: -5},
				},
			},
			expectedURL:      "https://example.com/app.js",
			expectedLocation: "1:1", // clamped to 0, then +1
		},
		{
			name: "zero-based origin",
			stackTrace: &cdpruntime.StackTrace{
				CallFrames: []*cdpruntime.CallFrame{
					{URL: "https://example.com/app.js", LineNumber: 0, ColumnNumber: 0},
				},
			},
			expectedURL:      "https://example.com/app.js",
			expectedLocation: "1:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, loc := extractSourceInfo(tt.stackTrace)
			assert.Equal(t, tt.expectedURL, url)
			assert.Equal(t, tt.expectedLocation, loc)
		})
	}
}

func TestFormatConsoleArg(t *testing.T) {
	tests := []struct {
		name     string
		arg      *cdpruntime.RemoteObject
		expected string
	}{
		{
			name:     "string value",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`"hello world"`)},
			expected: "hello world",
		},
		{
			name:     "number value",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`42`)},
			expected: "42",
		},
		{
			name:     "float value",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`3.14`)},
			expected: "3.14",
		},
		{
			name:     "boolean true",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`true`)},
			expected: "true",
		},
		{
			name:     "boolean false",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`false`)},
			expected: "false",
		},
		{
			name:     "null value",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`null`)},
			expected: "",
		},
		{
			name:     "object with description",
			arg:      &cdpruntime.RemoteObject{Description: "Error: Something went wrong"},
			expected: "Error: Something went wrong",
		},
		{
			name:     "object with className",
			arg:      &cdpruntime.RemoteObject{ClassName: "TypeError"},
			expected: "[TypeError]",
		},
		{
			name:     "object with type only",
			arg:      &cdpruntime.RemoteObject{Type: cdpruntime.TypeObject},
			expected: "[object]",
		},
		{
			name:     "empty arg",
			arg:      &cdpruntime.RemoteObject{},
			expected: "",
		},
		{
			name:     "string with special chars",
			arg:      &cdpruntime.RemoteObject{Value: []byte(`"Error: code \"42\" occurred"`)},
			expected: `Error: code "42" occurred`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatConsoleArg(tt.arg)
			assert.Equal(t, tt.expected, result)
		})
	}
}
