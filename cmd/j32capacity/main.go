// Command j32capacity measures the un-paced receive capacity of the J3.2
// Air Track message-type extension (TODO C14).
package main

import (
	"flag"
	"fmt"

	"datalink-workflow/internal/exp"
)

func main() {
	n := flag.Int("n", 20000, "number of messages")
	workers := flag.Int("workers", 20, "worker count")
	flag.Parse()

	res := exp.RunJ32Capacity(*n, *workers)
	fmt.Printf("J3.2 air-track capacity (%d msgs, %d workers): %.0f msg/s (errors=%d)\n",
		*n, *workers, res.Achieved, res.Errors)
}
