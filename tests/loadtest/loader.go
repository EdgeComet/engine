package main

import (
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type URLEntry struct {
	URL            string
	ExpectedStatus int
}

func loadURLs(filePath string) ([]URLEntry, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	delimiter := detectDelimiter(string(content))

	file.Seek(0, 0)
	reader := csv.NewReader(file)
	reader.Comma = delimiter
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	header := records[0]
	urlIdx, statusIdx := findColumnIndices(header)

	if urlIdx == -1 {
		return nil, fmt.Errorf("CSV header missing URL column. Found columns: %s", strings.Join(header, ", "))
	}

	var entries []URLEntry
	var invalidURLs int
	hosts := make(map[string]bool)

	for lineNum, record := range records[1:] {
		if len(record) <= urlIdx {
			continue
		}

		urlStr := strings.TrimSpace(record[urlIdx])

		if urlStr == "" {
			continue
		}

		parsedURL, err := url.Parse(urlStr)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			fmt.Printf("Warning: invalid URL on line %d, skipping: %s\n", lineNum+2, urlStr)
			invalidURLs++
			continue
		}

		hosts[parsedURL.Host] = true

		var expectedStatus int
		if statusIdx != -1 && len(record) > statusIdx {
			expectedStatusStr := strings.TrimSpace(record[statusIdx])
			if expectedStatusStr != "" {
				expectedStatus, err = strconv.Atoi(expectedStatusStr)
				if err != nil || expectedStatus < 100 || expectedStatus > 599 {
					fmt.Printf("Warning: invalid status code on line %d, ignoring: %s\n", lineNum+2, expectedStatusStr)
					expectedStatus = 0
				}
			}
		}

		entries = append(entries, URLEntry{
			URL:            urlStr,
			ExpectedStatus: expectedStatus,
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid URLs found in CSV")
	}

	uniqueHosts := make([]string, 0, len(hosts))
	for host := range hosts {
		uniqueHosts = append(uniqueHosts, host)
	}

	delimiterName := "comma"
	if delimiter == ';' {
		delimiterName = "semicolon"
	}

	fmt.Printf("\nLoaded %d URLs from %s (detected %s delimiter)\n", len(entries), filePath, delimiterName)
	fmt.Printf("  - Valid entries: %d\n", len(entries))
	if invalidURLs > 0 {
		fmt.Printf("  - Invalid URLs skipped: %d\n", invalidURLs)
	}
	fmt.Printf("  - Unique hosts: %d (%s)\n\n", len(uniqueHosts), strings.Join(uniqueHosts, ", "))

	return entries, nil
}

func detectDelimiter(content string) rune {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return ','
	}

	firstLine := lines[0]
	commaCount := strings.Count(firstLine, ",")
	semicolonCount := strings.Count(firstLine, ";")

	if semicolonCount > commaCount {
		return ';'
	}

	return ','
}

func findColumnIndices(header []string) (urlIdx int, statusIdx int) {
	urlIdx = -1
	statusIdx = -1

	urlVariants := []string{"url", "uri", "link", "address"}
	statusVariants := []string{"expected_status", "status code", "status_code", "status", "http_status", "code"}

	for i, col := range header {
		colLower := strings.ToLower(strings.TrimSpace(col))

		for _, variant := range urlVariants {
			if colLower == variant {
				urlIdx = i
				break
			}
		}

		for _, variant := range statusVariants {
			if colLower == variant {
				statusIdx = i
				break
			}
		}
	}

	return urlIdx, statusIdx
}
