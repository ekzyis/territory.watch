# territory.watch

https://territory.watch/

A static site showing the revenue a [Stacker News](https://stacker.news)
territory earns its founder.

## Layout

```
Makefile           build one territory, or all of them (see below)
main.go            thin entry point; dispatches to the cmd package
cmd/               all CLI logic (package cmd)
  cmd.go             subcommand dispatcher + usage
  fetch.go           tw fetch — pull a territory's items into an NDJSON feed
  aggregate_cmd.go   tw aggregate — reduce the feed to dashboard JSON
  aggregate.go       the sats/posts/comments bucketing per time range
  ndjson.go          the feed format (read/write)
static/            the single-page app
  index.html         the whole UI (search + animated dashboard)
  data/<name>-agg.json  per-territory data produced by tw aggregate
```

## Generate a territory's data

```sh
make build <territory> 
```

This will create two files:

* static/data/<territory>-feed.ndjson: Territory feed as fetched from the SN API
* static/data/<territory>-agg.json:    Aggregated data for the HTML view

See Makefile for more details.

## View the site

This will serve static/ with Caddy on localhost:8080:

```sh
nix run
```

