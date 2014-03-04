.PHONY: test-bencode nicecode test

all: test

nicecode:
	gofmt -w .

test: test-bencode

test-bencode:
	export GOPATH=$(PWD)
	cp -R test_data src/bencode/test_data
	go test bencode
	rm -rf src/bencode/test_data
