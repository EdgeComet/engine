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

	// EventLogTemplate carries the PageSEO-sourced placeholders the edge gateway
	// exposes. All of them are read off the struct the extractor produced, so a line
	// carrying the page's real title proves that extraction output survived every hop
	// into the emitted event.
	EventLogTemplate = "{request_id}\t{event_type}\t{url}\t{title}\t{index_status}\t{page_minhash}"

	eventLogFieldCount = 6

	eventLogRequestIDField   = 0
	eventLogEventTypeField   = 1
	eventLogURLField         = 2
	eventLogTitleField       = 3
	eventLogIndexStatusField = 4
	eventLogMinHashField     = 5

	// emptyFieldMarker is what the template formatter writes for an empty string.
	emptyFieldMarker = "-"

	// minHashSlotSeparator is how the formatter joins the fingerprint slots.
	minHashSlotSeparator = ","

	// minHashSlotCount is the fixed signature width. The fingerprint format is frozen at
	// this width, so any other count means a damaged field rather than a shorter
	// signature, and rejecting it is what makes a torn tail line skippable.
	minHashSlotCount = 24

	minHashValueBase    = 10
	minHashValueBitSize = 64
)

// EventLogEntry is one parsed line of the request event log.
type EventLogEntry struct {
	RequestID   string
	EventType   string
	URL         string
	Title       string
	IndexStatus int
	PageMinHash []uint64
}

// EventLogPath returns the request event log location for a test run.
func EventLogPath(tempDir string) string {
	return filepath.Join(tempDir, eventLogFileName)
}

// MatchingSlots counts the positions where two fingerprints hold the same value, which is
// the numerator of the estimated Jaccard similarity between the two page texts.
//
// Returns 0 whenever the lengths differ, nil arguments included: two signatures of
// different widths describe nothing comparable, and a zero keeps a caller from reading an
// accidental prefix overlap as similarity. Two nil fingerprints also score 0 - "neither
// page had enough text" is not evidence that they carry the same text.
func MatchingSlots(a, b []uint64) int {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	matching := 0
	for i := range a {
		if a[i] == b[i] {
			matching++
		}
	}
	return matching
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

	minHash, ok := parseMinHashField(fields[eventLogMinHashField])
	if !ok {
		return EventLogEntry{}, false
	}

	return EventLogEntry{
		RequestID:   unquoteEventField(fields[eventLogRequestIDField]),
		EventType:   unquoteEventField(fields[eventLogEventTypeField]),
		URL:         unquoteEventField(fields[eventLogURLField]),
		Title:       unquoteEventField(fields[eventLogTitleField]),
		IndexStatus: indexStatus,
		PageMinHash: minHash,
	}, true
}

// parseMinHashField reads the fingerprint the formatter writes as bare comma-separated
// decimals - unquoted, unlike the string fields. A page with too little text to fingerprint
// carries the empty marker and parses to nil, which is a valid reading rather than damage.
func parseMinHashField(field string) ([]uint64, bool) {
	if field == emptyFieldMarker {
		return nil, true
	}

	parts := strings.Split(field, minHashSlotSeparator)
	if len(parts) != minHashSlotCount {
		return nil, false
	}

	values := make([]uint64, len(parts))
	for i, part := range parts {
		value, err := strconv.ParseUint(part, minHashValueBase, minHashValueBitSize)
		if err != nil {
			return nil, false
		}
		values[i] = value
	}
	return values, true
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
