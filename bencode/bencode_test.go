package bencode

import (
	"io/ioutil"
	"reflect"
	"testing"
)

const TORRENT_FILE_1 = "test_data/1.torrent"

// tests simple numbers, strings, lists and
// dictionaries
func TestBasicParsing(t *testing.T) {
	var n3, n4, sabcd Bencoder
	n3 = &Number{3}
	n4 = &Number{4}
	sabcd = &String{"abcd"}
	tests := map[string]Bencoder{
		"i0e":            &Number{0},
		"i-3e":           &Number{-3},
		"2:ab":           &String{"ab"},
		"1::":            &String{":"},
		"li3ei4e4:abcde": &List{[]Bencoder{n3, n4, sabcd}},
	}

	for source, expectedOutput := range tests {
		output, rest, err := Parse([]byte(source))
		if err != nil {
			t.Errorf("Test \"%s\" not parsed correctly. Got error: %S", source, err)
		}
		if len(rest) > 0 {
			t.Fatal("Rest is not empty")
		}
		if !reflect.DeepEqual(output, expectedOutput) {
			t.Errorf("Test \"%s\" not parsed correctly.", source)
		}
	}
}

// tests with a real .torrent file
func TestAdvancedParsing(t *testing.T) {
	source, err := ioutil.ReadFile(TORRENT_FILE_1)
	if err != nil {
		panic(err)
	}

	output, rest, err := Parse(source)
	if err != nil {
		t.Fatalf("Got error: %S", err)
	}

	if len(rest) > 0 {
		t.Fatal("Rest is not empty")
	}

	sourceOutput := output.Encode()

	if !reflect.DeepEqual(source, sourceOutput) {
		t.Fatalf("Source and encoding don't match. " + string(source) +
				 " vs " + string(sourceOutput))
	}
}

// benchmarks parsing a real .torrent file
func BenchmarkBasicParsing(b *testing.B) {
	source, err := ioutil.ReadFile(TORRENT_FILE_1)
	if err != nil {
		panic(err)
	}
	for i := 0; i < b.N; i++ {
		Parse(source)
	}
}
