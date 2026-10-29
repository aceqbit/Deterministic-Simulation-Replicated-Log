GO ?= go
BIN := seedlog
ifeq ($(OS),Windows_NT)
BIN := seedlog.exe
endif
export CGO_ENABLED=0

.PHONY: build test lint vet golden sim-smoke bench gate-1.1 gate-1.2 gate-1.3 gate-1.4 gate-p1 gate-2.2 gate-2.5 study

build:
	$(GO) build -o $(BIN) ./cmd/seedlog

vet:
	$(GO) vet ./...

test:
	CGO_ENABLED=1 $(GO) test -race ./...

lint: vet
	staticcheck ./...
	golangci-lint run ./...

golden: build
	./$(BIN) sim --seeds 100 --hash-only > testdata/golden.txt

sim-smoke: build
	./$(BIN) explore --seeds 2000

bench:
	$(GO) test -bench=. -benchmem -run '^$$' ./sim

gate-1.1: build
	$(GO) test ./core ./sim
	./$(BIN) explore --seeds 1000 --duration 5 --nofaults
	./$(BIN) sim --seeds 1000 --duration 5 --nofaults --hash-only > h1.txt
	./$(BIN) sim --seeds 1000 --duration 5 --nofaults --hash-only > h2.txt
	cmp h1.txt h2.txt
	rm -f h1.txt h2.txt

gate-1.2:
	CGO_ENABLED=1 $(GO) test -race ./...
	$(GO) test -fuzz=FuzzRecover -fuzztime=60s ./wal
	$(GO) test -cover ./core ./kv ./wal

gate-1.3: build
	$(GO) test ./sim
	./$(BIN) explore --seeds 5000

gate-1.4:
	$(GO) test ./check

gate-p1: build
	$(GO) test ./...
	./$(BIN) explore --seeds 10000
	./$(BIN) sim --seeds 100 --hash-only | diff - testdata/golden.txt

gate-2.2: build
	$(GO) test ./sim -run TestBugRegression
	./$(BIN) pilot --seeds 10000 --out study/pilot.csv

gate-2.5: build
	$(GO) test ./shrink
	for b in 1 2 3 4 5 6 7 8 9 10; do ./$(BIN) replay --schedule shrink/schedules/bug$$b.json || exit 1; done

study: build
	./$(BIN) study --method sim --trials 10 --budget 20
	python study/analyze.py
