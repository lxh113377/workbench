package main

import "time"

// sleepMs 是测试用的毫秒级等待（避免每个测试文件重复 import time）。
func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
