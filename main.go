package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	statsURL          = "http://srv.msk01.gigacorp.local/_stats"
	pollInterval      = 5 * time.Second
	httpTimeout       = 3 * time.Second
	errorThreshold    = 3
	loadAvgLimit      = 30.0
	memUsageLimit     = 0.80
	diskUsageLimit    = 0.90
	networkUsageLimit = 0.90
)

// ==================================

func main() {
	client := &http.Client{Timeout: httpTimeout}
	errStreak := 0
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := pollOnce(client, statsURL); err != nil {
			errStreak++
			if errStreak >= errorThreshold {
				fmt.Println("Unable to fetch server statistic.")
				errStreak = 0
			}
		} else {
			errStreak = 0
		}
		<-ticker.C
	}
}

func pollOnce(client *http.Client, url string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := readAllTrim(resp.Body)
	if err != nil {
		return err
	}

	values, err := parseCSVNumbers(body)
	if err != nil {
		return err
	}
	if len(values) != 7 {
		return fmt.Errorf("invalid fields count: got %d, want 7", len(values))
	}

	loadAvg := values[0]
	memTotal := uint64(values[1])
	memUsed := uint64(values[2])
	diskTotal := uint64(values[3])
	diskUsed := uint64(values[4])
	netCapBps := uint64(values[5])
	netUsedBps := uint64(values[6])

	// 1) Load Average
	if loadAvg > loadAvgLimit {
		fmt.Printf("Load Average is too high: %s\n", fmtFloat(loadAvg))
	}

	// 2) Memory
	if memTotal > 0 {
		memUsage := float64(memUsed) / float64(memTotal)
		if memUsage > memUsageLimit {
			percent := int64(round(100.0 * memUsage))
			fmt.Printf("Memory usage too high: %d%%\n", percent)
		}
	}

	// 3) Disk
	if diskTotal > 0 {
		diskUsage := float64(diskUsed) / float64(diskTotal)
		if diskUsage > diskUsageLimit {
			freeBytes := int64(diskTotal) - int64(diskUsed)
			if freeBytes < 0 {
				freeBytes = 0
			}
			freeMB := freeBytes / (1024 * 1024) // Мб (бинарные)
			fmt.Printf("Free disk space is too low: %d Mb left\n", freeMB)
		}
	}

	// 4) Network
	if netCapBps > 0 {
		netUsage := float64(netUsedBps) / float64(netCapBps)
		if netUsage > networkUsageLimit {
			freeBps := int64(netCapBps) - int64(netUsedBps)
			if freeBps < 0 {
				freeBps = 0
			}
			// свободная полоса в мегабитах/сек (SI): Bps * 8 / 1_000_000
			freeMbit := float64(freeBps) / 1_000_000.0
			fmt.Printf("Network bandwidth usage high: %s Mbit/s available\n", fmtFloat(freeMbit))
		}
	}

	return nil
}

func readAllTrim(r io.Reader) (string, error) {
	var sb strings.Builder
	sc := bufio.NewScanner(r)
	const maxBuffSize = 1 << 20
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxBuffSize)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(line)
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(sb.String()), nil
}

func parseCSVNumbers(s string) ([]float64, error) {
	line := s
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		line = s[:idx]
	}
	parts := strings.Split(strings.TrimSpace(line), ",")
	var out []float64
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, fmt.Errorf("parse number %q: %w", p, err)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, errors.New("no numbers parsed")
	}
	return out, nil
}

func fmtFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func round(v float64) float64 {
	if v >= 0 {
		return float64(int64(v + 0.5))
	}
	return float64(int64(v - 0.5))
}
