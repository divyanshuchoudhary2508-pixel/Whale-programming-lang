package main

import (
	"fmt"
)

func fib(n int64) int64 {
	if n <= int64(1) {
		return n
	}
	return fib(n - int64(1)) + fib(n - int64(2))
}

func main() {
	fmt.Println("Computing Fib(38)...")
	var result = fib(int64(38))
	fmt.Println(result)
}

