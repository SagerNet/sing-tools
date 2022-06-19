package main

import (
	"os"
	"strconv"

	"github.com/klauspost/cpuid/v2"
)

func main() {
	os.Stdout.WriteString("v")
	os.Stdout.WriteString(strconv.Itoa(cpuid.CPU.X64Level()))
}
