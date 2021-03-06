// Bencoding encode/decode functionalities. Note that all
// the Parse* functions also return a "rest" byte slice.
// this is because those functions only parse the first
// bencoded type they find, and simply give back the rest
// of the source
package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
)

// this is the basic bencoded interface
// the only allowed types that will implement
// this interface are Number, String, List
// and Dictionary
type Bencoder interface {
	// shows a JSON-like human-readable output
	Dump() string

	// re-encodes as bencoded format
	Encode() []byte
}

// A mapping from String to another bencoded type
type Dictionary struct {
	Values map[String]Bencoder
}

// A list of bencoded types
type List struct {
	Values []Bencoder
}

// A bencoded string is actually a byte array. This is
// used a lot in .torrent files, where strings are
// actually binary blobs
// Unfortunately we need the String type to be comparable so it must
// not have slices
type String struct {
	Value string
}

type byStringValue []String

func (v byStringValue) Len() int {
	return len(v)
}

func (v byStringValue) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

func (v byStringValue) Less(i, j int) bool {
	return v[i].Value < v[j].Value
}

type Number struct {
	Value int64
}

func (d Dictionary) Dump() string {
	var buffer bytes.Buffer

	keys := []String{}

	for key, _ := range d.Values {
		keys = append(keys, key)
	}

	sort.Sort(byStringValue(keys))

	buffer.WriteString("{ ")

	for _, key := range keys {
		val := d.Values[key]
		buffer.WriteString(key.Dump())
		buffer.WriteString(" : ")
		buffer.WriteString(val.Dump())
		buffer.WriteString(", ")
	}
	buffer.WriteString(" }")

	return buffer.String()
}

func (d Dictionary) Encode() []byte {
	var buffer bytes.Buffer

	keys := []String{}

	for key, _ := range d.Values {
		keys = append(keys, key)
	}

	sort.Sort(byStringValue(keys))

	buffer.WriteByte('d')

	for _, key := range keys {
		val := d.Values[key]
		buffer.Write(key.Encode())
		buffer.Write(val.Encode())
	}
	buffer.WriteByte('e')

	return buffer.Bytes()
}

func (l List) Dump() string {
	var buffer bytes.Buffer

	buffer.WriteString("[ ")
	for _, elem := range l.Values {
		buffer.WriteString(elem.Dump())
		buffer.WriteString(", ")
	}

	buffer.WriteString(" ]")

	return buffer.String()
}

func (l List) Encode() []byte {
	var buffer bytes.Buffer

	buffer.WriteByte('l')
	for _, val := range l.Values {
		buffer.Write(val.Encode())
	}
	buffer.WriteByte('e')
	return buffer.Bytes()
}

func (s String) Dump() string {
	return s.Value
}

func (s String) Encode() []byte {
	var buffer bytes.Buffer

	buffer.WriteString(fmt.Sprintf("%d:", len(s.Value)))
	buffer.Write([]byte(s.Value))

	return buffer.Bytes()
}

func (n Number) Dump() string {
	return fmt.Sprintf("%d", n.Value)
}

func (n Number) Encode() []byte {
	return []byte(fmt.Sprintf("i%de", n.Value))
}

func ParseDictionary(source []byte) (res *Dictionary, rest []byte, err error) {
	if len(source) == 0 || source[0] != 'd' {
		return &Dictionary{}, []byte{}, errors.New("Malformed string given")
	}
	dict := Dictionary{make(map[String]Bencoder)}

	// fake it as a "list" and then process the resulting elements
	source[0] = 'l'

	var list *List
	if list, rest, err = ParseList(source); err != nil {
		return &Dictionary{}, []byte{}, err
	}

	source[0] = 'd'
	// must have elements in key -> value pairs
	if len(list.Values)%2 != 0 {
		return &Dictionary{}, []byte{}, errors.New("Malformed dictionary")
	}

	for i, j := 0, 1; j < len(list.Values); i, j = i+2, j+2 {
		key, value := list.Values[i], list.Values[j]

		// dictionary keys are required to be strings
		switch obj := key.(type) {
		case *String:
			dict.Values[*obj] = value
		default:
			return &Dictionary{}, []byte{}, errors.New("Invalid dictionary")
		}
	}

	return &dict, rest, nil
}

func ParseList(source []byte) (res *List, rest []byte, err error) {
	list := List{make([]Bencoder, 0)}
	if len(source) == 0 || source[0] != 'l' {
		return &List{}, []byte{}, errors.New("Invalid list")
	}

	source = source[1:]
	for len(source) > 0 && source[0] != 'e' {
		var val Bencoder
		if val, rest, err = Parse(source); err != nil {
			return &List{}, []byte{}, err
		}
		source = rest
		list.Values = append(list.Values, val)
	}

	if len(source) == 0 || source[0] != 'e' {
		return &List{}, []byte{}, errors.New("Malformed string given")
	}

	// remove the final 'e'
	return &list, source[1:], nil
}

func ParseString(source []byte) (res *String, rest []byte, err error) {
	var i int
	for idx, c := range source {
		if c == ':' {
			i = idx
			break
		}
	}

	var num int
	fmt.Sscanf(string(source[:i]), "%d", &num)
	if len(source) < i+num+1 {
		return &String{}, []byte{}, errors.New("String too short")
	}

	return &String{Value: string(source[i+1 : i+num+1])}, source[i+num+1:], nil
}

func ParseNumber(source []byte) (res *Number, rest []byte, err error) {
	length := len(source)
	if length < 2 || source[0] != 'i' {
		return &Number{}, []byte{}, errors.New("Invalid source given")
	}

	var idx int
	for i, c := range source {
		if c == 'e' {
			idx = i
			break
		}
	}

	if idx >= length {
		return &Number{}, []byte{}, errors.New("Malformed string given")
	}

	var num int64
	fmt.Sscanf(string(source[1:idx]), "%d", &num)
	return &Number{Value: num}, source[idx+1:], nil
}

// parse a generic bencoded string
func Parse(source []byte) (res Bencoder, rest []byte, err error) {
	if len(source) == 0 {
		return &Dictionary{}, []byte{}, errors.New("Empty string given")
	}

	switch source[0] {
	case 'd':
		return ParseDictionary(source)
	case 'l':
		return ParseList(source)
	case 'i':
		return ParseNumber(source)
	default:
		return ParseString(source)
	}
	return
}
