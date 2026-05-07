BINARY := bin/squid-os

.PHONY: build install clean

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .

install: build
	cp $(BINARY) /usr/local/bin/squid-os 2>/dev/null || \
		cp $(BINARY) $(HOME)/go/bin/squid-os

clean:
	rm -f $(BINARY)
