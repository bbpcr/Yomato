.PHONY: all test-bencode nicecode test yomato

all: test yomato

nicecode:
	gofmt -w .

test: test-bencode

test-bencode:
	export GOPATH=$(PWD)
	cp -R test_data bencode/test_data
	go test ...bencode ...bitfield
	rm -rf bencode/test_data

yomato:
	go get ./yomato
	go install ./yomato
