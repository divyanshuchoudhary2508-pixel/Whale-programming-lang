package main

import (
	"fmt"
)

func test_bitwise(a int64, b int64) int64 {
	var or_res = a | b
	var and_res = a & b
	var xor_res = a ^ b
	var not_res = ~a
	var shl_res = a << int64(2)
	var shr_res = b >> int64(1)
	return or_res + and_res + xor_res + not_res + shl_res + shr_res
}

func test_cast(x int64) int64 {
	var small = /* unsupported expr */
	return /* unsupported expr */
}

func test_asm() int64 {
	/* unsupported expr */
	return int64(0)
}

func main() int64 {
	test_bitwise(int64(5), int64(3))
	test_cast(int64(257))
	test_asm()
	return int64(0)
}

