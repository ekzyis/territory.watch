package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// The NDJSON feed `tw fetch` writes and `tw aggregate` reads looks like:
//
//	{"territory":"<name>","founder":"<nym>","founded":"<ts>"}  ← header, once
//	{"item":{…}}                             ← one per item
//	{"cursor":"<next>","fetchedAt":"<ts>"}   ← checkpoint after each page
//	{"cursor":null,"fetchedAt":"<ts>"}       ← final checkpoint = complete file
//
// founder/founded are the territory's owner nym and creation time, always
// present in the header.
//
// Each page ends with a checkpoint line carrying the cursor to resume from; the
// final page's cursor is null, so a resumer can tell "complete" from
// "interrupted at a page boundary".

// feedItem is the subset of item fields the dashboard needs.
type feedItem struct {
	Id        int        `json:"id"`
	ParentId  int        `json:"parentId"`
	Sats      int        `json:"sats"`
	Cost      int        `json:"cost"`
	CreatedAt time.Time  `json:"createdAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	SubNames  []string   `json:"subNames"`
	User      struct {
		Name string `json:"name"`
	} `json:"user"`
}

type feedHeaderLine struct {
	Territory string    `json:"territory"`
	Founder   string    `json:"founder"`
	Founded   time.Time `json:"founded"`
}

type feedItemLine struct {
	Item feedItem `json:"item"`
}

type feedCursorLine struct {
	Cursor    *string   `json:"cursor"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// feedRecord reads any line of the feed.
type feedRecord struct {
	Territory string     `json:"territory"`
	Founder   string     `json:"founder"`
	Founded   time.Time  `json:"founded"`
	Item      *feedItem  `json:"item"`
	Cursor    *string    `json:"cursor"`
	FetchedAt *time.Time `json:"fetchedAt"`
}

// feedWriter writes the feed one line at a time (Encode appends '\n' → NDJSON).
type feedWriter struct {
	enc *json.Encoder
}

func newFeedWriter(w io.Writer) *feedWriter {
	return &feedWriter{enc: json.NewEncoder(w)}
}

func (fw *feedWriter) header(territory, founder string, founded time.Time) error {
	return fw.enc.Encode(feedHeaderLine{Territory: territory, Founder: founder, Founded: founded})
}

func (fw *feedWriter) item(it feedItem) error {
	return fw.enc.Encode(feedItemLine{Item: it})
}

// checkpoint writes the per-page checkpoint line; a nil cursor marks completion.
func (fw *feedWriter) checkpoint(cursor *string, at time.Time) error {
	return fw.enc.Encode(feedCursorLine{Cursor: cursor, FetchedAt: at})
}

// readFeed parses an NDJSON feed: the territory/founder/founded from the header
// line, every item, and the latest fetchedAt (from the checkpoint lines).
func readFeed(r io.Reader) (hdr feedHeaderLine, items []feedItem, fetchedAt *time.Time, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var rec feedRecord
		if e := json.Unmarshal(b, &rec); e != nil {
			return feedHeaderLine{}, nil, nil, fmt.Errorf("feed line %d: %w", line, e)
		}
		if rec.Territory != "" {
			hdr.Territory = rec.Territory
			hdr.Founder = rec.Founder
			hdr.Founded = rec.Founded
		}
		if rec.Item != nil {
			items = append(items, *rec.Item)
		}
		if rec.FetchedAt != nil {
			fetchedAt = rec.FetchedAt
		}
	}
	if e := sc.Err(); e != nil {
		return feedHeaderLine{}, nil, nil, e
	}
	return hdr, items, fetchedAt, nil
}

// feedState summarizes an existing feed for resuming a fetch.
type feedState struct {
	Territory string
	Seen      map[int]bool // item ids already present, for dedupe
	Cursor    string       // where to resume; "" if complete or no checkpoint
	Complete  bool         // the final checkpoint had cursor:null
}

// scanFeed reads an existing NDJSON feed and returns what a resumer needs: the
// territory, the ids already present, the cursor to continue from, and whether
// the file is already complete. Every non-empty line must parse.
func scanFeed(r io.Reader) (feedState, error) {
	st := feedState{Seen: map[int]bool{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var rec feedRecord
		if e := json.Unmarshal(b, &rec); e != nil {
			return feedState{}, fmt.Errorf("feed line %d: %w", line, e)
		}
		switch {
		case rec.Item != nil:
			st.Seen[rec.Item.Id] = true
		case rec.FetchedAt != nil: // checkpoint line
			if rec.Cursor == nil {
				st.Cursor, st.Complete = "", true
			} else {
				st.Cursor, st.Complete = *rec.Cursor, false
			}
		case rec.Territory != "":
			st.Territory = rec.Territory
		}
	}
	if e := sc.Err(); e != nil {
		return feedState{}, e
	}
	return st, nil
}
