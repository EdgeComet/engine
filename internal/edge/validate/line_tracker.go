package validate

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// LineTracker tracks line numbers for YAML fields
type LineTracker struct {
	lines map[string]int // map of field path to line number
}

// NewLineTracker creates a new line tracker for a YAML file
func NewLineTracker(filePath string) (*LineTracker, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}

	tracker := &LineTracker{
		lines: make(map[string]int),
	}

	// Extract line numbers from YAML structure
	tracker.extractLines(&node, "")

	return tracker, nil
}

// GetLine returns the line number for a field path (e.g., "server.port", "hosts[0].domain")
func (lt *LineTracker) GetLine(path string) int {
	if line, ok := lt.lines[path]; ok {
		return line
	}
	return 0
}

// extractLines recursively extracts line numbers from YAML nodes
func (lt *LineTracker) extractLines(node *yaml.Node, path string) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.DocumentNode:
		// Process document content
		if len(node.Content) > 0 {
			lt.extractLines(node.Content[0], path)
		}

	case yaml.MappingNode:
		// Process key-value pairs
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]

			key := keyNode.Value
			newPath := path
			if newPath == "" {
				newPath = key
			} else {
				newPath = path + "." + key
			}

			// Store line number for this field
			lt.lines[newPath] = keyNode.Line

			// Recursively process nested structures
			lt.extractLines(valueNode, newPath)
		}

	case yaml.SequenceNode:
		// Process array elements
		for i, item := range node.Content {
			indexPath := path + "[" + strconv.Itoa(i) + "]"
			lt.lines[indexPath] = item.Line
			lt.extractLines(item, indexPath)
		}

	case yaml.ScalarNode:
		// Leaf node - line already tracked by parent mapping
		lt.lines[path] = node.Line
	}
}

// Helper functions for common path patterns

// GetServerLine returns line number for server config field
func (lt *LineTracker) GetServerLine(field string) int {
	return lt.GetLine("server." + field)
}

// GetRedisLine returns line number for redis config field
func (lt *LineTracker) GetRedisLine(field string) int {
	return lt.GetLine("redis." + field)
}

// GetCacheLine returns line number for cache config field
func (lt *LineTracker) GetCacheLine(field string) int {
	return lt.GetLine("cache." + field)
}

// GetRenderLine returns line number for render config field
func (lt *LineTracker) GetRenderLine(field string) int {
	return lt.GetLine("render." + field)
}

// GetBypassLine returns line number for bypass config field
func (lt *LineTracker) GetBypassLine(field string) int {
	return lt.GetLine("bypass." + field)
}

// GetHostLine returns line number for host field
func (lt *LineTracker) GetHostLine(index int, field string) int {
	if index < 0 {
		return 0
	}
	path := "hosts[" + strconv.Itoa(index) + "]"
	if field != "" {
		path += "." + field
	}
	return lt.GetLine(path)
}

// FindURLRuleLine searches for a URL rule line by matching pattern
func (lt *LineTracker) FindURLRuleLine(hostIndex, ruleIndex int) int {
	if hostIndex < 0 || ruleIndex < 0 {
		return 0
	}

	basePath := "hosts[" + strconv.Itoa(hostIndex) + "].url_rules"

	// Try to find the rule in the sequence
	for i := 0; i <= ruleIndex && i < 100; i++ {
		rulePath := basePath + "[" + strconv.Itoa(i) + "]"
		if line := lt.GetLine(rulePath); line > 0 && i == ruleIndex {
			return line
		}
	}

	return 0
}

// HostsLineTracker tracks line numbers for hosts.yaml
type HostsLineTracker struct {
	*LineTracker
}

// NewHostsLineTracker creates a line tracker specifically for hosts.yaml
func NewHostsLineTracker(filePath string) (*HostsLineTracker, error) {
	tracker, err := NewLineTracker(filePath)
	if err != nil {
		return nil, err
	}
	return &HostsLineTracker{tracker}, nil
}

// GetHostFieldLine returns line number for a specific host field
func (hlt *HostsLineTracker) GetHostFieldLine(hostIndex int, field string) int {
	return hlt.GetHostLine(hostIndex, field)
}

// GetURLRuleLine returns line number for a URL rule
func (hlt *HostsLineTracker) GetURLRuleLine(hostIndex, ruleIndex int) int {
	return hlt.FindURLRuleLine(hostIndex, ruleIndex)
}
