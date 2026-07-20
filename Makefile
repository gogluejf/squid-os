BINARY := squid-os

.PHONY: build install clean

build:
	GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null) && \
	CGO_ENABLED=0 go build -ldflags="-s -w -X squid-os/internal/version.GitCommit=$$GIT_COMMIT" -o $(BINARY) .

install: build
	cp $(BINARY) /usr/local/bin/squid-os 2>/dev/null || \
		cp $(BINARY) $(HOME)/go/bin/squid-os

clean:
	rm -f $(BINARY)
