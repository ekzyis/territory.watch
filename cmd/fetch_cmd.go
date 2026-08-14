package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	sn "github.com/ekzyis/snappy"
)

// pageSize is the fetch limit per API request (max: 1000)
const pageSize = 100

// countWriter counts bytes written so the progress line can report feed size.
type countWriter struct {
	w io.Writer
	n atomic.Int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func toFeedItem(it sn.Item) feedItem {
	fi := feedItem{
		Id:        it.Id,
		ParentId:  it.ParentId,
		Sats:      it.Sats,
		Cost:      it.Cost,
		CreatedAt: it.CreatedAt.UTC(),
		SubNames:  it.SubNames,
	}
	fi.User.Name = it.User.Name
	if it.DeletedAt.Valid {
		t := it.DeletedAt.Time.UTC()
		fi.DeletedAt = &t
	}
	return fi
}

// fetchSubMeta pulls a territory's owner nym and creation time straight from the
// sub. It queries the sub directly (not an item's `sub`, which for a crosspost is
// the item's home territory, not the one being fetched).
func fetchSubMeta(territory string) (founder string, founded time.Time, err error) {
	const query = `query($name:String!){sub(name:$name){createdAt user{name}}}`
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": map[string]string{"name": territory},
	})
	if err != nil {
		return "", time.Time{}, err
	}
	req, err := http.NewRequest("POST", "https://stacker.news/api/graphql", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "territory.watch")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	var out struct {
		Data struct {
			Sub *struct {
				CreatedAt time.Time `json:"createdAt"`
				User      struct {
					Name string `json:"name"`
				} `json:"user"`
			} `json:"sub"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, err
	}
	if len(out.Errors) > 0 {
		return "", time.Time{}, fmt.Errorf("graphql: %s", out.Errors[0].Message)
	}
	if out.Data.Sub == nil {
		return "", time.Time{}, fmt.Errorf("territory ~%s not found", territory)
	}
	return out.Data.Sub.User.Name, out.Data.Sub.CreatedAt.UTC(), nil
}

// streamFeed writes a territory's feed as NDJSON to out, progress to stderr. If
// in is a non-empty existing feed, it's passed through and the fetch resumes from
// its last checkpoint (deduping against items already present).
func streamFeed(out io.Writer, in io.Reader, territory string) error {
	client := sn.NewClient()
	cw := &countWriter{w: out}
	fw := newFeedWriter(cw)

	var (
		cursor        string
		seen          = map[int]bool{}
		headerWritten bool
	)

	// Resume from an existing feed: pass it through, adopt its cursor.
	if in != nil {
		data, err := io.ReadAll(in)
		if err != nil {
			return fmt.Errorf("reading existing feed: %w", err)
		}
		if len(bytes.TrimSpace(data)) > 0 {
			st, err := scanFeed(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("reading existing feed: %w", err)
			}
			if st.Territory != "" && st.Territory != territory {
				return fmt.Errorf("existing feed is for ~%s, not ~%s", st.Territory, territory)
			}
			if _, err := cw.Write(data); err != nil { // pass the existing feed through
				return err
			}
			seen = st.Seen
			headerWritten = st.Territory != ""
			cursor = st.Cursor
			if st.Complete {
				return nil // already a complete feed; nothing to fetch
			}
		}
	}

	// Progress counters shared with the ticker goroutine; guard with mu.
	var (
		mu    sync.Mutex
		items = len(seen)
	)
	start := time.Now()

	// Live \r-updating progress only on a terminal; elsewhere (pipe/redirect, or
	// parallel builds sharing one terminal) print a single final line instead.
	interactive := false
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		interactive = true
	}
	draw := func() {
		mu.Lock()
		n := items
		mu.Unlock()
		cr := ""
		if interactive {
			cr = "\r\x1b[K" // return to column 0 and clear the line
		}
		fmt.Fprintf(os.Stderr, "%s~%s items=%d size=%s elapsed=%.1fs",
			cr, territory, n, humanBytes(int(cw.n.Load())), time.Since(start).Seconds())
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	if interactive {
		draw()
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					draw()
				}
			}
		}()
	}
	defer func() {
		if interactive {
			close(done)
			wg.Wait()
		}
		draw() // final line (both modes)
		fmt.Fprintln(os.Stderr)
	}()

	for {
		res, err := client.Items(&sn.ItemsQuery{
			Sub:    territory,
			Sort:   "new",
			By:     "new",
			Type:   "all",
			When:   "forever",
			Cursor: cursor,
			Limit:  pageSize,
		})
		if err != nil {
			return fmt.Errorf("fetching items for ~%s: %w", territory, err)
		}

		var pageNew []sn.Item
		for _, it := range res.Items {
			// skip duplicates
			if seen[it.Id] {
				continue
			}
			seen[it.Id] = true
			pageNew = append(pageNew, it)
		}

		if !headerWritten && len(pageNew) > 0 {
			founder, founded, err := fetchSubMeta(territory)
			if err != nil {
				return fmt.Errorf("fetching sub meta for ~%s: %w", territory, err)
			}
			if err := fw.header(territory, founder, founded); err != nil {
				return err
			}
			headerWritten = true
		}

		next := res.Cursor
		lastPage := len(pageNew) == 0 || next == "" || next == cursor

		for _, it := range pageNew {
			if err := fw.item(toFeedItem(it)); err != nil {
				return err
			}
		}

		// Checkpoint after the page; nil cursor on the last page marks completion.
		if headerWritten {
			var c *string
			if !lastPage {
				nc := next
				c = &nc
			}
			if err := fw.checkpoint(c, time.Now().UTC()); err != nil {
				return err
			}
		}

		mu.Lock()
		items = len(seen)
		mu.Unlock()

		if lastPage {
			break
		}
		cursor = next
	}

	if !headerWritten {
		return fmt.Errorf("no items for ~%s (does the territory exist?)", territory)
	}
	return nil
}

// humanBytes formats a byte count as a short human-readable string (B/KB/MB…).
func humanBytes(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := int64(n) / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// fetchCmd implements `tw fetch <territory>`, writing the NDJSON feed to stdout and
// resuming from an existing feed piped in on stdin.
func fetchCmd(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: tw fetch <territory> [< existing.ndjson]")
	}
	fs.Parse(args)

	territory := fs.Arg(0)
	if territory == "" {
		fs.Usage()
		return fmt.Errorf("a territory name is required")
	}

	// Only resume when stdin is piped/redirected, not a terminal or /dev/null.
	var in io.Reader
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		in = os.Stdin
	}
	return streamFeed(os.Stdout, in, territory)
}
