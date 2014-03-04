package yomato

import (
    "io/ioutil"
)

func GetFile(path string) string {
    data, err := ioutil.ReadFile(path)
    if err != nil {
        panic(err)
    }
    return string(data[:])
}
