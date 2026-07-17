# Master catalog of territories, one per line.
TERRITORIES := $(shell cat static/territories.txt 2>/dev/null)

# Allow a positional argument instead of only `make build TERRITORY=security`.
ifeq (build,$(firstword $(MAKECMDGOALS)))
  ARG := $(word 2,$(MAKECMDGOALS))
  ifneq ($(ARG),)
    TERRITORY := $(ARG)
    $(eval $(ARG):;@:)
  endif
endif

# Rebuild the binary whenever any Go source or the module files change.
tw: $(wildcard *.go cmd/*.go) go.mod go.sum
	go build -o tw .

# Fetch and render ("build") one territory.
#
# On success the territory is added to the generated TERRITORIES list in
# static/index.html if it isn't already there.
build: tw
	@test -n "$(TERRITORY)" || { echo "usage: make build <territory>" >&2; exit 1; }
	
	@t=$$(echo '$(TERRITORY)' | tr A-Z a-z); f=static/data/$$t-feed.ndjson; \
	tmp=$$(mktemp /tmp/$$t-feed.XXXXXX); \
	if [ -f $$f ]; then mv $$f $$tmp; fi; \
	cat $$tmp 2>/dev/null | ./tw fetch $(TERRITORY) > $$f && \
	./tw aggregate < $$f > static/data/$$t-agg.json
	
	@if grep -qi "\"$(TERRITORY)\"" static/index.html; then \
		echo "~$(TERRITORY) already listed in static/index.html"; \
	else \
		sed -i "/const TERRITORIES = \[/a\\  \"$(TERRITORY)\"," static/index.html; \
		echo "added ~$(TERRITORY) to static/index.html"; \
	fi

J ?= 4

# Add every territory to the index.html list once (serial), so the parallel
# builds below only read it and never write it concurrently.
list:
	@for t in $(TERRITORIES); do \
		grep -qi "\"$$t\"" static/index.html || \
			sed -i "/const TERRITORIES = \[/a\\  \"$$t\"," static/index.html; \
	done

# Build every territory in static/territories.txt, J at a time. Piping through
# cat makes each child's stderr non-tty, so they print one summary line each
# instead of fighting over the terminal with live progress.
build-all: tw list
	@printf '%s\n' $(TERRITORIES) | xargs -P$(J) -I{} $(MAKE) --no-print-directory build {} 2>&1 | cat

.PHONY: build list build-all
