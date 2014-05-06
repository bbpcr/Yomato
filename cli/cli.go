package cli

import (
	"flag"
	"fmt"
	"os"
)

type StringList []string

func (sl *StringList) String() string {
	return fmt.Sprintf("%q", (*[]string)(sl))
}
func (sl *StringList) Set(value string) error {
	*sl = append(*sl, value)
	return nil
}

func Parse() (string, []string) {
	var excludes StringList
	flag.Var(&excludes, "exclude", "exclude files from the download")
	flag.Parse()
	path := os.Args[len(os.Args)-1]
	return path, ([]string)(excludes)
}
