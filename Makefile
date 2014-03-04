.PHONY: all test-bencode nicecode test

all: test

nicecode:
	gofmt -w .

test: test-bencode

test-bencode:
	export GOPATH=$(PWD)
	cp -R test_data bencode/test_data
	go test ...bencode
	rm -rf bencode/test_data
