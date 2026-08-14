package cmd

import (
	"time"
)

// Founder revenue: 70% of an item's cost (fees) + 21% of its sats (zaps).
const (
	founderCostPercent = 70
	founderSatsPercent = 21
)

// Shapes the page JS consumes. Values are whole sats or counts.
type series struct {
	Revenue  []int `json:"revenue"`
	Posts    []int `json:"posts"`
	Comments []int `json:"comments"`
	Zaps     []int `json:"zaps"`
}

type totals struct {
	Revenue  int `json:"revenue"`
	Posts    int `json:"posts"`
	Comments int `json:"comments"`
	Zaps     int `json:"zaps"`
}

type rangeData struct {
	N      int      `json:"n"`
	Labels []string `json:"labels"`
	Series series   `json:"series"`
	Totals totals   `json:"totals"`
}

type meta struct {
	Name     string `json:"name"`
	Founder  string `json:"founder"`
	Founded  string `json:"founded"`  // e.g. "Jul 2022"; "" if unknown
	Stackers int    `json:"stackers"` // distinct authors seen (a proxy)
}

// aggItem is an item reduced to just what the charts need.
type aggItem struct {
	t       time.Time
	revenue int // crosspost-adjusted founder revenue, whole sats
	sats    int // total sats zapped (not crosspost-adjusted)
	isPost  bool
}

// rangeIDs is the order the range buttons appear in on the page.
var rangeIDs = []string{"day", "month", "ytd", "year", "forever"}

// aggregate reduces raw items to the per-range series the page renders, plus
// territory metadata (name, founder, founded, stackers).
func aggregate(items []feedItem, hdr feedHeaderLine, now time.Time) (map[string]rangeData, meta) {
	prepared, m := prepare(items, hdr)
	data := make(map[string]rangeData, len(rangeIDs))
	for _, id := range rangeIDs {
		data[id] = buildRange(id, prepared, now.UTC())
	}
	return data, m
}

// prepare dedupes, drops deleted items, and splits founder revenue across
// crossposts. Name/founder/founded come from the feed header; stackers is the
// count of distinct authors seen. Founded is formatted "Jan 2006".
func prepare(items []feedItem, hdr feedHeaderLine) ([]aggItem, meta) {
	seen := map[int]bool{}
	authors := map[string]bool{}
	var out []aggItem
	m := meta{Name: hdr.Territory, Founder: hdr.Founder, Founded: hdr.Founded.Format("Jan 2006")}

	for _, it := range items {
		// dedupe
		if seen[it.Id] {
			continue
		}
		seen[it.Id] = true

		// ignore deleted items
		if it.DeletedAt != nil {
			continue
		}

		// revenue, split across the territories a crosspost was posted to
		n := max(1, len(it.SubNames))
		revenue := (founderCostPercent*it.Cost + founderSatsPercent*it.Sats + 50*n) / (100 * n)

		out = append(out, aggItem{
			t:       it.CreatedAt.UTC(),
			revenue: revenue,
			sats:    it.Sats,
			isPost:  it.ParentId == 0,
		})

		// distinct authors → stackers
		if it.User.Name != "" {
			authors[it.User.Name] = true
		}
	}

	m.Stackers = len(authors)
	return out, m
}

func truncDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// buildRange buckets items for one range id, newest bucket rightmost (index n-1
// == "now"); the labels and totals are what the page's charts and table read.
func buildRange(id string, items []aggItem, now time.Time) rangeData {
	var (
		n       int
		idxOf   func(t time.Time) int
		labelOf func(i int) string
	)

	switch id {
	case "day": // 24 hourly buckets
		n = 24
		anchor := now.Truncate(time.Hour)
		idxOf = func(t time.Time) int {
			d := int(anchor.Sub(t.Truncate(time.Hour)).Hours())
			return within(n-1-d, n)
		}
		labelOf = func(i int) string {
			return anchor.Add(time.Duration(-(n - 1 - i)) * time.Hour).Format("3 PM")
		}

	case "month": // daily buckets
		n = 30
		anchor := truncDay(now)
		idxOf = func(t time.Time) int {
			d := int(anchor.Sub(truncDay(t)).Hours() / 24)
			return within(n-1-d, n)
		}
		labelOf = func(i int) string {
			bt := anchor.AddDate(0, 0, -(n - 1 - i))
			return bt.Format("Jan 2")
		}

	case "ytd", "year": // weekly buckets
		anchor := truncDay(now)
		if id == "year" {
			n = 52
		} else {
			n = (now.YearDay()-1)/7 + 1
		}
		if n < 1 {
			n = 1
		}
		idxOf = func(t time.Time) int {
			d := int(anchor.Sub(truncDay(t)).Hours() / 24)
			if d < 0 {
				return -1
			}
			return within(n-1-d/7, n)
		}
		labelOf = func(i int) string {
			return anchor.AddDate(0, 0, -7*(n-1-i)).Format("Jan 2")
		}

	case "forever": // monthly buckets since the earliest item
		am := monthIndex(now)
		minM := am
		for _, it := range items {
			if mi := monthIndex(it.t); mi < minM {
				minM = mi
			}
		}
		n = max(1, am-minM+1)
		idxOf = func(t time.Time) int {
			return within(n-1-(am-monthIndex(t)), n)
		}
		labelOf = func(i int) string {
			return monthFromIndex(am - (n - 1 - i)).Format("Jan '06")
		}
	}

	rd := rangeData{N: n, Labels: make([]string, n)}
	rd.Series.Revenue = make([]int, n)
	rd.Series.Posts = make([]int, n)
	rd.Series.Comments = make([]int, n)
	rd.Series.Zaps = make([]int, n)
	for i := 0; i < n; i++ {
		rd.Labels[i] = labelOf(i)
	}
	for _, it := range items {
		i := idxOf(it.t)
		if i < 0 {
			continue
		}
		rd.Series.Revenue[i] += it.revenue
		rd.Series.Zaps[i] += it.sats
		if it.isPost {
			rd.Series.Posts[i]++
		} else {
			rd.Series.Comments[i]++
		}
	}
	for i := 0; i < n; i++ {
		rd.Totals.Revenue += rd.Series.Revenue[i]
		rd.Totals.Posts += rd.Series.Posts[i]
		rd.Totals.Comments += rd.Series.Comments[i]
		rd.Totals.Zaps += rd.Series.Zaps[i]
	}
	return rd
}

// within returns i if it is a valid bucket index in [0,n), else -1.
func within(i, n int) int {
	if i < 0 || i >= n {
		return -1
	}
	return i
}

func monthIndex(t time.Time) int { return t.Year()*12 + int(t.Month()) - 1 }

func monthFromIndex(mi int) time.Time {
	return time.Date(mi/12, time.Month(mi%12+1), 1, 0, 0, 0, 0, time.UTC)
}
