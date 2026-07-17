package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// payload is the JSON the page fetches (static/data/<territory>-agg.json).
type payload struct {
	Territory string               `json:"territory"`
	FetchedAt string               `json:"fetchedAt"`
	Commit    string               `json:"commit"` // short git hash, for the footer link
	Meta      meta                 `json:"meta"`
	Data      map[string]rangeData `json:"data"`
}

// gitCommit is the short HEAD hash, or "" if git is unavailable.
func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// aggregateCmd implements `tw aggregate`: read the NDJSON feed from stdin,
// aggregate, write dashboard JSON to stdout.
func aggregateCmd(args []string) error {
	fs := flag.NewFlagSet("aggregate", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: tw aggregate < feed.ndjson")
	}
	fs.Parse(args)

	hdr, items, fetchedAt, err := readFeed(os.Stdin)
	if err != nil {
		return err
	}
	if hdr.Territory == "" {
		return fmt.Errorf("no territory header in feed; re-run `tw fetch <territory>`")
	}
	if hdr.Founder == "" || hdr.Founded.IsZero() {
		return fmt.Errorf("feed header for ~%s is missing founder/founded; re-run `tw fetch <territory>`", hdr.Territory)
	}

	data, m := aggregate(items, hdr, time.Now())

	fetched := ""
	if fetchedAt != nil && !fetchedAt.IsZero() {
		fetched = fetchedAt.Format("Jan 2, 2006 15:04 MST")
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload{
		Territory: hdr.Territory,
		FetchedAt: fetched,
		Commit:    gitCommit(),
		Meta:      m,
		Data:      data,
	})
}
