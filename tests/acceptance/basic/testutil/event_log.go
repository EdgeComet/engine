package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	eventLogFileName = "events.log"

	// EventLogTemplate carries the two PageSEO-sourced placeholders the edge gateway
	// already exposes. Both are read off the struct the extractor produced, so a line
	// carrying the page's real title proves that extraction output survived every hop
	// into the emitted event.
	EventLogTemplate = "{request_id}\t{event_type}\t{url}\t{title}\t{index_status}"

	eventLogFieldCount = 5

	eventLogRequestIDField   = 0
	eventLogEventTypeField   = 1
	eventLogURLField         = 2
	eventLogTitleField       = 3
	eventLogIndexStatusField = 4

	// emptyFieldMarker is what the template formatter writes for an empty string.
	emptyFieldMarker = "-"
)

// EventLogEntry is one parsed line of the request event log.
type EventLogEntry struct {
	RequestID   string
	EventType   string
	URL         string
	Title       string
	IndexStatus int
}

// EventLogPath returns the request event log location for a test run.
func EventLogPath(tempDir string) string {
	return filepath.Join(tempDir, eventLogFileName)
}

// FindEventByRequestID returns the logged event for one request. The suite randomizes
// spec order and every spec appends to the same log, so correlating on the request id
// the edge gateway echoed back is the only lookup that cannot pick up another spec's
// event. Reports found=false while the line has not been written yet: emission happens
// after the response is served, so a caller polls rather than reading once.
func FindEventByRequestID(logPath, requestID string) (entry EventLogEntry, found bool, err error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return EventLogEntry{}, false, nil
		}
		return EventLogEntry{}, false, fmt.Errorf("failed to read event log %s: %w", logPath, err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		parsed, ok := parseEventLogLine(line)
		if !ok {
			continue
		}
		if parsed.RequestID == requestID {
			return parsed, true, nil
		}
	}

	return EventLogEntry{}, false, nil
}

// parseEventLogLine reports ok=false for anything it cannot read as a full line. The
// gateway appends while the poll reads, so the tail can be a partially written line;
// skipping it lets the next poll see it complete instead of failing the spec on a
// transient read.
func parseEventLogLine(line string) (EventLogEntry, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) != eventLogFieldCount {
		return EventLogEntry{}, false
	}

	indexStatus, err := strconv.Atoi(fields[eventLogIndexStatusField])
	if err != nil {
		return EventLogEntry{}, false
	}

	return EventLogEntry{
		RequestID:   unquoteEventField(fields[eventLogRequestIDField]),
		EventType:   unquoteEventField(fields[eventLogEventTypeField]),
		URL:         unquoteEventField(fields[eventLogURLField]),
		Title:       unquoteEventField(fields[eventLogTitleField]),
		IndexStatus: indexStatus,
	}, true
}

// eventFieldUnescaper reverses the template formatter's escaping. Single pass, so an
// escaped backslash is consumed before the character after it can be read as an escape.
var eventFieldUnescaper = strings.NewReplacer(
	`\n`, "\n",
	`\t`, "\t",
	`\r`, "\r",
	`\"`, `"`,
	`\\`, `\`,
)

// unquoteEventField reverses the template formatter's string encoding: values are
// wrapped in quotes with backslash escapes, and an empty value is written as a dash.
func unquoteEventField(field string) string {
	if field == emptyFieldMarker {
		return ""
	}
	return eventFieldUnescaper.Replace(strings.TrimPrefix(strings.TrimSuffix(field, `"`), `"`))
}
