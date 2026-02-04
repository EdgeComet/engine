package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}

func formatPercent(part, total int64) string {
	if total == 0 {
		return "0.0"
	}
	return fmt.Sprintf("%.1f", float64(part)*100.0/float64(total))
}

func formatSeconds(ms int64) string {
	return fmt.Sprintf("%.3f", float64(ms)/1000.0)
}

func drawTableRow(columns []string, widths []int, border string) string {
	var row strings.Builder
	row.WriteString(border)
	for i, col := range columns {
		colLen := len(col)
		width := widths[i]

		if colLen > width {
			row.WriteString(col[:width])
		} else {
			padding := width - colLen
			// Left-align first column if it starts with spaces, otherwise center
			if i == 0 && strings.HasPrefix(col, " ") {
				row.WriteString(col)
				row.WriteString(strings.Repeat(" ", padding))
			} else {
				leftPad := padding / 2
				rightPad := padding - leftPad
				row.WriteString(strings.Repeat(" ", leftPad))
				row.WriteString(col)
				row.WriteString(strings.Repeat(" ", rightPad))
			}
		}

		if i < len(columns)-1 {
			row.WriteString("│")
		}
	}
	row.WriteString(border)
	return row.String()
}

func drawTableDivider(widths []int, left, mid, right, fill string) string {
	var divider strings.Builder
	divider.WriteString(left)
	for i, width := range widths {
		divider.WriteString(strings.Repeat(fill, width))
		if i < len(widths)-1 {
			divider.WriteString(mid)
		}
	}
	divider.WriteString(right)
	return divider.String()
}

func realTimeReporter(ctx context.Context, stats *GlobalStats) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats.UpdateRPS()
			stats.UpdateBandwidthRate()
			stats.UpdateSourceRPS()
			printRealTimeStats(stats)
		}
	}
}

func printRealTimeStats(stats *GlobalStats) {
	elapsed := time.Since(stats.startTime)
	total := atomic.LoadInt64(&stats.TotalRequests)
	success2xx := atomic.LoadInt64(&stats.Success2xx)
	redirect3xx := atomic.LoadInt64(&stats.Redirect3xx)
	error4xx := atomic.LoadInt64(&stats.ClientError4xx)
	error5xx := atomic.LoadInt64(&stats.ServerError5xx)
	netErrors := atomic.LoadInt64(&stats.NetworkErrors)
	timeoutErrors := atomic.LoadInt64(&stats.TimeoutErrors)
	connectionErrors := atomic.LoadInt64(&stats.ConnectionErrors)
	cacheHits := atomic.LoadInt64(&stats.CacheHits)
	rendered := atomic.LoadInt64(&stats.Rendered)
	bypass := atomic.LoadInt64(&stats.Bypass)
	bypassCache := atomic.LoadInt64(&stats.BypassCache)
	totalBytes := atomic.LoadInt64(&stats.TotalBytes)
	mismatches := atomic.LoadInt64(&stats.StatusMismatches)
	activeRequests := stats.GetActiveRequests()
	currentRPS := stats.GetCurrentRPS()
	currentBWRate := stats.GetCurrentBWRate()

	fmt.Print("\033[H\033[J")

	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Load Test Running - %s elapsed | RPS: %.1f | Active: %d/%d\n",
		formatDuration(elapsed), currentRPS, activeRequests, stats.baseConcurrency)
	fmt.Println(strings.Repeat("=", 80))

	stats.histogramMu.Lock()
	hasResponseTimes := stats.ResponseTimes.TotalCount() > 0
	hasCacheTimes := stats.ResponseTimesCache.TotalCount() > 0
	hasRenderedTimes := stats.ResponseTimesRendered.TotalCount() > 0
	hasBypassTimes := stats.ResponseTimesBypass.TotalCount() > 0
	hasBypassCacheTimes := stats.ResponseTimesBypassCache.TotalCount() > 0

	type responseTimeRow struct {
		label string
		min   int64
		p50   int64
		p95   int64
		p99   int64
		max   int64
		rps   float64
	}

	var rows []responseTimeRow

	if hasResponseTimes {
		rows = append(rows, responseTimeRow{
			label: "Total",
			min:   stats.ResponseTimes.Min(),
			p50:   stats.ResponseTimes.ValueAtQuantile(50),
			p95:   stats.ResponseTimes.ValueAtQuantile(95),
			p99:   stats.ResponseTimes.ValueAtQuantile(99),
			max:   stats.ResponseTimes.Max(),
			rps:   stats.GetCurrentRPS(),
		})
	}
	if hasCacheTimes {
		rows = append(rows, responseTimeRow{
			label: "Cache",
			min:   stats.ResponseTimesCache.Min(),
			p50:   stats.ResponseTimesCache.ValueAtQuantile(50),
			p95:   stats.ResponseTimesCache.ValueAtQuantile(95),
			p99:   stats.ResponseTimesCache.ValueAtQuantile(99),
			max:   stats.ResponseTimesCache.Max(),
			rps:   stats.GetCacheRPS(),
		})
	}
	if hasRenderedTimes {
		rows = append(rows, responseTimeRow{
			label: "Rendered",
			min:   stats.ResponseTimesRendered.Min(),
			p50:   stats.ResponseTimesRendered.ValueAtQuantile(50),
			p95:   stats.ResponseTimesRendered.ValueAtQuantile(95),
			p99:   stats.ResponseTimesRendered.ValueAtQuantile(99),
			max:   stats.ResponseTimesRendered.Max(),
			rps:   stats.GetRenderedRPS(),
		})
	}
	if hasBypassTimes {
		rows = append(rows, responseTimeRow{
			label: "Bypass",
			min:   stats.ResponseTimesBypass.Min(),
			p50:   stats.ResponseTimesBypass.ValueAtQuantile(50),
			p95:   stats.ResponseTimesBypass.ValueAtQuantile(95),
			p99:   stats.ResponseTimesBypass.ValueAtQuantile(99),
			max:   stats.ResponseTimesBypass.Max(),
			rps:   stats.GetBypassRPS(),
		})
	}
	if hasBypassCacheTimes {
		rows = append(rows, responseTimeRow{
			label: "Bypass Cache",
			min:   stats.ResponseTimesBypassCache.Min(),
			p50:   stats.ResponseTimesBypassCache.ValueAtQuantile(50),
			p95:   stats.ResponseTimesBypassCache.ValueAtQuantile(95),
			p99:   stats.ResponseTimesBypassCache.ValueAtQuantile(99),
			max:   stats.ResponseTimesBypassCache.Max(),
			rps:   stats.GetBypassCacheRPS(),
		})
	}
	stats.histogramMu.Unlock()

	if len(rows) > 0 {
		fmt.Println("\nRESPONSE TIMES (seconds)")
		widths := []int{14, 9, 9, 9, 9, 9, 12}
		fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
		fmt.Println(drawTableRow([]string{"", "Min", "P50", "P95", "P99", "Max", "RPS"}, widths, "│"))
		fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
		for _, row := range rows {
			fmt.Println(drawTableRow([]string{
				row.label,
				formatSeconds(row.min),
				formatSeconds(row.p50),
				formatSeconds(row.p95),
				formatSeconds(row.p99),
				formatSeconds(row.max),
				fmt.Sprintf("%.1f", row.rps),
			}, widths, "│"))
		}
		fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))
	}

	if total > 0 {
		fmt.Println("\nSTATUS CODES")
		widths := []int{17, 14, 14, 14, 14}
		fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
		fmt.Println(drawTableRow([]string{"2xx", "3xx", "4xx", "5xx", "Network"}, widths, "│"))
		fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
		fmt.Println(drawTableRow([]string{
			fmt.Sprintf("%s (%s%%)", formatNumber(success2xx), formatPercent(success2xx, total)),
			fmt.Sprintf("%s (%s%%)", formatNumber(redirect3xx), formatPercent(redirect3xx, total)),
			fmt.Sprintf("%s (%s%%)", formatNumber(error4xx), formatPercent(error4xx, total)),
			fmt.Sprintf("%s (%s%%)", formatNumber(error5xx), formatPercent(error5xx, total)),
			fmt.Sprintf("%s (%s%%)", formatNumber(netErrors), formatPercent(netErrors, total)),
		}, widths, "│"))
		fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))

		if netErrors > 0 {
			otherNet := netErrors - timeoutErrors - connectionErrors
			if otherNet < 0 {
				otherNet = 0
			}
			if otherNet > 0 {
				fmt.Printf("  Network breakdown: Timeout=%s | Connection=%s | Other=%s\n",
					formatNumber(timeoutErrors), formatNumber(connectionErrors), formatNumber(otherNet))
			} else {
				fmt.Printf("  Network breakdown: Timeout=%s | Connection=%s\n",
					formatNumber(timeoutErrors), formatNumber(connectionErrors))
			}
		}

		renderTotal := cacheHits + rendered + bypass + bypassCache
		if renderTotal > 0 {
			fmt.Println("\nRENDER SOURCES")
			widths := []int{18, 20, 18, 22}
			fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
			fmt.Println(drawTableRow([]string{"Cache", "Rendered", "Bypass", "Bypass Cache"}, widths, "│"))
			fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
			fmt.Println(drawTableRow([]string{
				fmt.Sprintf("%s (%s%%)", formatNumber(cacheHits), formatPercent(cacheHits, total)),
				fmt.Sprintf("%s (%s%%)", formatNumber(rendered), formatPercent(rendered, total)),
				fmt.Sprintf("%s (%s%%)", formatNumber(bypass), formatPercent(bypass, total)),
				fmt.Sprintf("%s (%s%%)", formatNumber(bypassCache), formatPercent(bypassCache, total)),
			}, widths, "│"))
			fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))
		}

		fmt.Println("\nBANDWIDTH")
		fmt.Printf("  Total: %s | Rate: %.1f MB/s\n", formatBytes(totalBytes), currentBWRate/1024/1024)

		if mismatches > 0 {
			fmt.Println("\nWARNINGS")
			fmt.Printf("  Status Mismatches: %s requests (%s%%)\n",
				formatNumber(mismatches), formatPercent(mismatches, total))
		}
	}

	fmt.Println(strings.Repeat("=", 80))
}

func printFinalReport(stats *GlobalStats, duration time.Duration) {
	total := atomic.LoadInt64(&stats.TotalRequests)
	success2xx := atomic.LoadInt64(&stats.Success2xx)
	redirect3xx := atomic.LoadInt64(&stats.Redirect3xx)
	error4xx := atomic.LoadInt64(&stats.ClientError4xx)
	error5xx := atomic.LoadInt64(&stats.ServerError5xx)
	netErrors := atomic.LoadInt64(&stats.NetworkErrors)
	timeoutErrors := atomic.LoadInt64(&stats.TimeoutErrors)
	connectionErrors := atomic.LoadInt64(&stats.ConnectionErrors)
	mismatches := atomic.LoadInt64(&stats.StatusMismatches)
	cacheHits := atomic.LoadInt64(&stats.CacheHits)
	rendered := atomic.LoadInt64(&stats.Rendered)
	bypass := atomic.LoadInt64(&stats.Bypass)
	bypassCache := atomic.LoadInt64(&stats.BypassCache)
	totalBytes := atomic.LoadInt64(&stats.TotalBytes)

	successful := success2xx
	failed := error4xx + error5xx + netErrors

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("                         LOAD TEST FINAL REPORT")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Test Duration:  %s\n", formatDuration(duration))
	fmt.Printf("Started:        %s\n", stats.startTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("Ended:          %s\n", stats.startTime.Add(duration).Format("2006-01-02 15:04:05"))
	fmt.Printf("Total Requests: %s\n", formatNumber(total))
	fmt.Printf("Successful:     %s (%s%%)\n", formatNumber(successful), formatPercent(successful, total))
	fmt.Printf("Failed:         %s (%s%%)\n", formatNumber(failed), formatPercent(failed, total))

	stats.histogramMu.Lock()
	hasFinalResponseTimes := stats.ResponseTimes.TotalCount() > 0
	hasFinalCacheTimes := stats.ResponseTimesCache.TotalCount() > 0
	hasFinalRenderedTimes := stats.ResponseTimesRendered.TotalCount() > 0
	hasFinalBypassTimes := stats.ResponseTimesBypass.TotalCount() > 0
	hasFinalBypassCacheTimes := stats.ResponseTimesBypassCache.TotalCount() > 0

	type finalResponseTimeRow struct {
		label string
		min   int64
		p50   int64
		p75   int64
		p95   int64
		p99   int64
		max   int64
		rps   float64
	}

	var finalRows []finalResponseTimeRow

	if hasFinalResponseTimes {
		finalRows = append(finalRows, finalResponseTimeRow{
			label: "Total",
			min:   stats.ResponseTimes.Min(),
			p50:   stats.ResponseTimes.ValueAtQuantile(50),
			p75:   stats.ResponseTimes.ValueAtQuantile(75),
			p95:   stats.ResponseTimes.ValueAtQuantile(95),
			p99:   stats.ResponseTimes.ValueAtQuantile(99),
			max:   stats.ResponseTimes.Max(),
			rps:   stats.GetAverageRPS("total", duration),
		})
	}
	if hasFinalCacheTimes {
		finalRows = append(finalRows, finalResponseTimeRow{
			label: "Cache",
			min:   stats.ResponseTimesCache.Min(),
			p50:   stats.ResponseTimesCache.ValueAtQuantile(50),
			p75:   stats.ResponseTimesCache.ValueAtQuantile(75),
			p95:   stats.ResponseTimesCache.ValueAtQuantile(95),
			p99:   stats.ResponseTimesCache.ValueAtQuantile(99),
			max:   stats.ResponseTimesCache.Max(),
			rps:   stats.GetAverageRPS("cache", duration),
		})
	}
	if hasFinalRenderedTimes {
		finalRows = append(finalRows, finalResponseTimeRow{
			label: "Rendered",
			min:   stats.ResponseTimesRendered.Min(),
			p50:   stats.ResponseTimesRendered.ValueAtQuantile(50),
			p75:   stats.ResponseTimesRendered.ValueAtQuantile(75),
			p95:   stats.ResponseTimesRendered.ValueAtQuantile(95),
			p99:   stats.ResponseTimesRendered.ValueAtQuantile(99),
			max:   stats.ResponseTimesRendered.Max(),
			rps:   stats.GetAverageRPS("rendered", duration),
		})
	}
	if hasFinalBypassTimes {
		finalRows = append(finalRows, finalResponseTimeRow{
			label: "Bypass",
			min:   stats.ResponseTimesBypass.Min(),
			p50:   stats.ResponseTimesBypass.ValueAtQuantile(50),
			p75:   stats.ResponseTimesBypass.ValueAtQuantile(75),
			p95:   stats.ResponseTimesBypass.ValueAtQuantile(95),
			p99:   stats.ResponseTimesBypass.ValueAtQuantile(99),
			max:   stats.ResponseTimesBypass.Max(),
			rps:   stats.GetAverageRPS("bypass", duration),
		})
	}
	if hasFinalBypassCacheTimes {
		finalRows = append(finalRows, finalResponseTimeRow{
			label: "Bypass Cache",
			min:   stats.ResponseTimesBypassCache.Min(),
			p50:   stats.ResponseTimesBypassCache.ValueAtQuantile(50),
			p75:   stats.ResponseTimesBypassCache.ValueAtQuantile(75),
			p95:   stats.ResponseTimesBypassCache.ValueAtQuantile(95),
			p99:   stats.ResponseTimesBypassCache.ValueAtQuantile(99),
			max:   stats.ResponseTimesBypassCache.Max(),
			rps:   stats.GetAverageRPS("bypass_cache", duration),
		})
	}
	stats.histogramMu.Unlock()

	if len(finalRows) > 0 {
		fmt.Println("\nRESPONSE TIMES (seconds)")
		widths := []int{14, 9, 9, 9, 9, 9, 10, 12}
		fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
		fmt.Println(drawTableRow([]string{"", "Min", "P50", "P75", "P95", "P99", "Max", "RPS"}, widths, "│"))
		fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
		for _, row := range finalRows {
			fmt.Println(drawTableRow([]string{
				row.label,
				formatSeconds(row.min),
				formatSeconds(row.p50),
				formatSeconds(row.p75),
				formatSeconds(row.p95),
				formatSeconds(row.p99),
				formatSeconds(row.max),
				fmt.Sprintf("%.1f", row.rps),
			}, widths, "│"))
		}
		fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))
	}

	fmt.Println("\nSTATUS CODE DISTRIBUTION")
	widths := []int{22, 10, 14}
	fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
	fmt.Println(drawTableRow([]string{"Category", "Count", "Percentage"}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
	fmt.Println(drawTableRow([]string{"2xx Success", formatNumber(success2xx), formatPercent(success2xx, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"3xx Redirect", formatNumber(redirect3xx), formatPercent(redirect3xx, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"4xx Client Error", formatNumber(error4xx), formatPercent(error4xx, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"5xx Server Error", formatNumber(error5xx), formatPercent(error5xx, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"Network Errors", formatNumber(netErrors), formatPercent(netErrors, total) + "%"}, widths, "│"))
	otherNet := netErrors - timeoutErrors - connectionErrors
	if otherNet > 0 {
		fmt.Println(drawTableRow([]string{"    - Timeout", formatNumber(timeoutErrors), formatPercent(timeoutErrors, total) + "%"}, widths, "│"))
		fmt.Println(drawTableRow([]string{"    - Connection", formatNumber(connectionErrors), formatPercent(connectionErrors, total) + "%"}, widths, "│"))
		fmt.Println(drawTableRow([]string{"    - Other", formatNumber(otherNet), formatPercent(otherNet, total) + "%"}, widths, "│"))
	} else {
		fmt.Println(drawTableRow([]string{"    - Timeout", formatNumber(timeoutErrors), formatPercent(timeoutErrors, total) + "%"}, widths, "│"))
		fmt.Println(drawTableRow([]string{"    - Connection", formatNumber(connectionErrors), formatPercent(connectionErrors, total) + "%"}, widths, "│"))
	}
	fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))

	fmt.Println("\nTHROUGHPUT")
	avgRPS := float64(total) / duration.Seconds()
	avgBW := float64(totalBytes) / duration.Seconds()
	widths = []int{22, 26}
	fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
	fmt.Println(drawTableRow([]string{"Metric", "Value"}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
	fmt.Println(drawTableRow([]string{"Average RPS", fmt.Sprintf("%.1f requests/sec", avgRPS)}, widths, "│"))
	fmt.Println(drawTableRow([]string{"Total Bandwidth", formatBytes(totalBytes)}, widths, "│"))
	fmt.Println(drawTableRow([]string{"Average Bandwidth", fmt.Sprintf("%.1f MB/sec", avgBW/1024/1024)}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))

	fmt.Println("\nRENDER SOURCE DISTRIBUTION")
	widths = []int{18, 10, 14}
	fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
	fmt.Println(drawTableRow([]string{"Source", "Count", "Percentage"}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
	fmt.Println(drawTableRow([]string{"cache", formatNumber(cacheHits), formatPercent(cacheHits, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"rendered", formatNumber(rendered), formatPercent(rendered, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"bypass", formatNumber(bypass), formatPercent(bypass, total) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"bypass_cache", formatNumber(bypassCache), formatPercent(bypassCache, total) + "%"}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))

	if mismatches > 0 {
		fmt.Println("\nSTATUS CODE MISMATCHES")
		fmt.Printf("Total Mismatches: %s (%s%% of validated URLs)\n\n", formatNumber(mismatches), formatPercent(mismatches, total))

		stats.mismatchMu.Lock()
		mismatchList := make([]MismatchDetail, len(stats.Mismatches))
		copy(mismatchList, stats.Mismatches)
		stats.mismatchMu.Unlock()

		type mismatchKey struct {
			url            string
			expectedStatus int
			actualStatus   int
		}
		type mismatchEntry struct {
			url            string
			expectedStatus int
			actualStatus   int
			count          int
			requestIDs     []string
		}

		mismatchMap := make(map[mismatchKey]*mismatchEntry)
		for _, mismatch := range mismatchList {
			key := mismatchKey{
				url:            mismatch.URL,
				expectedStatus: mismatch.ExpectedStatus,
				actualStatus:   mismatch.ActualStatus,
			}

			entry, exists := mismatchMap[key]
			if !exists {
				entry = &mismatchEntry{
					url:            key.url,
					expectedStatus: key.expectedStatus,
					actualStatus:   key.actualStatus,
					requestIDs:     make([]string, 0, 5),
				}
				mismatchMap[key] = entry
			}

			entry.count++
			if len(entry.requestIDs) < 5 && mismatch.RequestID != "" {
				entry.requestIDs = append(entry.requestIDs, mismatch.RequestID)
			}
		}

		aggregated := make([]mismatchEntry, 0, len(mismatchMap))
		for _, entry := range mismatchMap {
			aggregated = append(aggregated, *entry)
		}

		sort.Slice(aggregated, func(i, j int) bool {
			return aggregated[i].url < aggregated[j].url
		})

		widths := []int{50, 12, 12, 8}
		fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
		fmt.Println(drawTableRow([]string{"URL", "Expected", "Actual", "Count"}, widths, "│"))
		fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))

		for _, entry := range aggregated {
			url := entry.url
			if len(url) > 50 {
				url = url[:47] + "..."
			}
			fmt.Println(drawTableRow([]string{
				url,
				fmt.Sprintf("%d", entry.expectedStatus),
				fmt.Sprintf("%d", entry.actualStatus),
				fmt.Sprintf("%d", entry.count),
			}, widths, "│"))

			if len(entry.requestIDs) > 0 {
				requestIDsStr := strings.Join(entry.requestIDs, ", ")
				fmt.Printf("  Request IDs: %s\n", requestIDsStr)
			}
		}
		fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))
	}

	stats.mu.RLock()
	hostCount := len(stats.HostStats)
	if hostCount > 0 {
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("PER-HOST BREAKDOWN")
		fmt.Println(strings.Repeat("=", 80))

		type hostEntry struct {
			host  string
			stats *HostStats
		}
		hosts := make([]hostEntry, 0, hostCount)
		for host, hs := range stats.HostStats {
			hosts = append(hosts, hostEntry{host: host, stats: hs})
		}
		sort.Slice(hosts, func(i, j int) bool {
			return atomic.LoadInt64(&hosts[i].stats.TotalRequests) > atomic.LoadInt64(&hosts[j].stats.TotalRequests)
		})

		for _, entry := range hosts {
			printHostStats(entry.host, entry.stats, total, totalBytes)
		}
	}
	stats.mu.RUnlock()

	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("TEST COMPLETE")
	fmt.Println(strings.Repeat("=", 80))
}

func printHostStats(host string, hs *HostStats, globalTotal int64, globalTotalBytes int64) {
	hostTotal := atomic.LoadInt64(&hs.TotalRequests)
	success2xx := atomic.LoadInt64(&hs.Success2xx)
	redirect3xx := atomic.LoadInt64(&hs.Redirect3xx)
	error4xx := atomic.LoadInt64(&hs.ClientError4xx)
	error5xx := atomic.LoadInt64(&hs.ServerError5xx)
	netErrors := atomic.LoadInt64(&hs.NetworkErrors)
	totalBytes := atomic.LoadInt64(&hs.TotalBytes)
	mismatches := atomic.LoadInt64(&hs.StatusMismatches)

	fmt.Printf("\n%s\n", host)
	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("Total Requests: %s (%s%% of all requests)\n\n", formatNumber(hostTotal), formatPercent(hostTotal, globalTotal))

	widths := []int{22, 10, 14}
	fmt.Println(drawTableDivider(widths, "┌", "┬", "┐", "─"))
	fmt.Println(drawTableRow([]string{"Status/Metric", "Count", "Percentage"}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "├", "┼", "┤", "─"))
	fmt.Println(drawTableRow([]string{"2xx", formatNumber(success2xx), formatPercent(success2xx, hostTotal) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"3xx", formatNumber(redirect3xx), formatPercent(redirect3xx, hostTotal) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"4xx", formatNumber(error4xx), formatPercent(error4xx, hostTotal) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"5xx", formatNumber(error5xx), formatPercent(error5xx, hostTotal) + "%"}, widths, "│"))
	fmt.Println(drawTableRow([]string{"Network Errors", formatNumber(netErrors), formatPercent(netErrors, hostTotal) + "%"}, widths, "│"))
	fmt.Println(drawTableDivider(widths, "└", "┴", "┘", "─"))

	hs.histogramMu.Lock()
	hasHostResponseTimes := hs.ResponseTimes.TotalCount() > 0
	var hostMin, hostP50, hostP95, hostP99, hostMax int64
	if hasHostResponseTimes {
		hostMin = hs.ResponseTimes.Min()
		hostP50 = hs.ResponseTimes.ValueAtQuantile(50)
		hostP95 = hs.ResponseTimes.ValueAtQuantile(95)
		hostP99 = hs.ResponseTimes.ValueAtQuantile(99)
		hostMax = hs.ResponseTimes.Max()
	}
	hs.histogramMu.Unlock()

	if hasHostResponseTimes {
		fmt.Printf("\nResponse Times (seconds): min=%s | p50=%s | p95=%s | p99=%s | max=%s\n",
			formatSeconds(hostMin), formatSeconds(hostP50), formatSeconds(hostP95),
			formatSeconds(hostP99), formatSeconds(hostMax))
	}

	fmt.Printf("Bandwidth: %s (%s%% of total)\n", formatBytes(totalBytes), formatPercent(totalBytes, globalTotalBytes))

	if mismatches > 0 {
		fmt.Printf("Status Mismatches: %s (%s%%)\n", formatNumber(mismatches), formatPercent(mismatches, hostTotal))
	}

	fmt.Println(strings.Repeat("─", 80))
}
