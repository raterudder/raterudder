package main

import (
	"fmt"
	"io/ioutil"
	"strings"
)

func main() {
	content, err := ioutil.ReadFile("pkg/server/history_test.go")
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	str := string(content)

	// Fix indentation, shadowing, and error handling for targetParsed

	str = strings.Replace(str,
		`today := truncateDay(now)
			targetParsed, _ := time.Parse("2006-01-02", targetDate)`,
		`today := truncateDay(now)
		targetParsed, err := time.Parse("2006-01-02", targetDate)
		require.NoError(t, err)`,
		1)

	str = strings.ReplaceAll(str,
		`mock.MatchedBy(func(t time.Time) bool { return t.UTC().Equal`,
		`mock.MatchedBy(func(tm time.Time) bool { return tm.UTC().Equal`)

	err = ioutil.WriteFile("pkg/server/history_test.go", []byte(str), 0644)
	if err != nil {
		fmt.Println("Error writing file:", err)
	} else {
		fmt.Println("Successfully patched history_test.go")
	}
}
