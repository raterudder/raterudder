package main

import (
    "context"
    "fmt"
    "time"

    "github.com/raterudder/raterudder/pkg/utility"
)

func main() {
    u := utility.NewMap(nil)
    sys := u.GetProvider("comed_besh") // default or base
    fmt.Printf("%+v\n", sys)
}
